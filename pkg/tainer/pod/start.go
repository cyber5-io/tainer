package pod

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
	"github.com/docker/docker/api/types/container"
	units "github.com/docker/go-units"
)

// StartOptions configures a Start call.
type StartOptions struct {
	ManifestPath string
	ProjectDir   string // host directory containing tainer.yaml + app/ + data/
}

// StartResult is what the CLI prints after a successful start.
type StartResult struct {
	Pod          string
	PodID        int
	Domain       string
	HTTPServices []HTTPSvcResult
	TCPServices  []TCPSvcResult
}

type HTTPSvcResult struct {
	Role string
	URL  string // https://<domain>:<port>
}

type TCPSvcResult struct {
	Role string
	Host string // 127.0.0.1:<derived_port>
}

// Start brings up a pod from the resolved manifest at opts.ManifestPath.
// Idempotent: re-running on a partially-up pod recreates only what's
// missing.
func Start(ctx context.Context, eng *engine.Client, opts StartOptions) (*StartResult, error) {
	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return nil, err
	}

	podID, err := AllocatePodID(m.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("pod start: allocate pod id: %w", err)
	}

	// Persist any manifest-pinned ports up-front so future allocations
	// can avoid them across pods (manifest validation already restricts
	// pinned values to 40000-49000).
	for _, p := range m.Ports {
		if p.Protocol == manifest.PortTCP && p.HostPort != 0 {
			if err := PinTCPPort(m.Project.Name, p.Role, p.HostPort); err != nil {
				return nil, fmt.Errorf("pod start: pin %s: %w", p.Role, err)
			}
		}
	}

	split, err := Split(m)
	if err != nil {
		return nil, err
	}

	mhash := ManifestHash(m)
	leader := ContainerName(m.Project.Name, RoleWeb)

	// Build the full TCP port-binding set for the pod and apply it to
	// the leader. Followers don't get PortBindings — those would be
	// silently no-op'd by cyberstack since the netns is shared.
	leaderPortBindings := buildLeaderPortBindings(m, podID)

	roles := RolesForPod(m)
	for i, role := range roles {
		var bindings []engine.PortMap
		var netMode string
		if i == 0 { // leader
			bindings = leaderPortBindings
		} else {
			netMode = "container:" + leader
		}
		if err := startContainer(ctx, eng, m, opts, role, podID, mhash, split[role], bindings, netMode); err != nil {
			return nil, err
		}
	}

	allPods, err := List(ctx, eng)
	if err != nil {
		return nil, err
	}
	endpoints, err := buildEndpoints(ctx, eng, allPods)
	if err != nil {
		return nil, err
	}

	if err := router.Ensure(ctx, eng, router.ExtraHTTPPorts(endpoints)); err != nil {
		return nil, err
	}
	if err := router.UpdateConfig(ctx, eng, endpoints); err != nil {
		return nil, err
	}

	return makeStartResult(m, podID), nil
}

// buildLeaderPortBindings collects every TCP port binding for the pod
// — auto-derived from pod_id*10+offset for known roles, or the user's
// `host_port:` value where set.
func buildLeaderPortBindings(m *manifest.Manifest, podID int) []engine.PortMap {
	out := make([]engine.PortMap, 0, len(m.Ports))
	for _, p := range m.Ports {
		if p.Protocol != manifest.PortTCP {
			continue
		}
		host := p.HostPort
		if host == 0 {
			host = DerivePort(podID, p.Role)
		}
		if host == 0 {
			continue // unknown role with no pin — nothing to publish
		}
		out = append(out, engine.PortMap{
			Container: p.Container,
			Host:      host,
			Proto:     "tcp",
		})
	}
	return out
}

// startContainer creates+starts one role's container. The leader gets
// its own veth on cs0 (NetworkMode empty) and owns all PortBindings;
// followers attach via NetworkMode=container:<leader>.
func startContainer(
	ctx context.Context, eng *engine.Client,
	m *manifest.Manifest, opts StartOptions,
	role string, podID int, mhash string, lim Limits,
	bindings []engine.PortMap, netMode string,
) error {
	memBytes, _ := units.RAMInBytes(lim.Memory)
	envs := buildEnv(m, role)
	mounts := buildMounts(m, opts.ProjectDir, role)

	labels := map[string]string{
		LabelPod:          m.Project.Name,
		LabelRole:         role,
		LabelPodID:        strconv.Itoa(podID),
		LabelManifestPath: opts.ManifestPath,
		LabelManifestHash: mhash,
	}
	for _, b := range bindings {
		// One label per published role (we only emit one port per role).
		labels[PublishLabel(role)] = strconv.Itoa(b.Host)
	}

	spec := engine.RunSpec{
		Image:       ImageRef(m, role),
		Name:        ContainerName(m.Project.Name, role),
		NetworkMode: netMode,
		Env:         envs,
		Mounts:      mounts,
		Ports:       bindings,
		Detach:      true,
		Restart:     "unless-stopped",
		Resources: container.Resources{
			Memory:   memBytes,
			NanoCPUs: int64(lim.CPU * 1e9),
		},
		Labels: labels,
	}
	_, err := eng.Run(ctx, spec)
	return err
}

func buildEnv(m *manifest.Manifest, role string) []string {
	env := []string{
		"TAINER_PROJECT=" + m.Project.Name,
		"TAINER_DOMAIN=" + m.Project.Domain,
		"TAINER_ROLE=" + role,
	}
	if role == RoleApp && m.IsPHP() {
		env = append(env, m.Runtime.PHPLimits.EnvFlags()...)
	}
	return env
}

func buildMounts(m *manifest.Manifest, projectDir, role string) []engine.Mount {
	if role == RoleDB {
		return []engine.Mount{
			{Source: projectDir + "/db", Target: dbDataPath(m)},
		}
	}
	out := []engine.Mount{
		{Source: projectDir + "/" + m.HostAppDir(), Target: m.ContainerAppPath()},
		{Source: projectDir + "/data", Target: m.ContainerMountBase() + "/data"},
	}
	for _, name := range m.Mounts {
		out = append(out, engine.Mount{Source: projectDir + "/" + name, Target: m.ContainerMountBase() + "/" + name})
	}
	return out
}

func dbDataPath(m *manifest.Manifest) string {
	if m.Runtime.Database == manifest.DatabasePostgres {
		return "/var/lib/postgresql/data"
	}
	return "/var/lib/mysql"
}

func buildEndpoints(ctx context.Context, eng *engine.Client, pods []Pod) ([]router.PodEndpoint, error) {
	out := make([]router.PodEndpoint, 0, len(pods))
	for _, p := range pods {
		ep := router.PodEndpoint{Pod: p.Name}
		for _, c := range p.Containers {
			insp, err := eng.Inspect(ctx, c.Name)
			if err != nil {
				continue
			}
			ip := insp.NetworkSettings.IPAddress
			switch c.Role {
			case RoleWeb:
				ep.WebIP = ip
			case RoleApp:
				ep.SSHIP = ip
			}
			// HTTPServices are inferred by re-reading the manifest at the
			// labeled path. Out of scope for this stub — wired in Task 24.
		}
		out = append(out, ep)
	}
	return out, nil
}

func makeStartResult(m *manifest.Manifest, podID int) *StartResult {
	r := &StartResult{
		Pod:    m.Project.Name,
		PodID:  podID,
		Domain: m.Project.Domain,
	}
	for _, p := range m.Ports {
		switch p.Protocol {
		case manifest.PortHTTP:
			r.HTTPServices = append(r.HTTPServices, HTTPSvcResult{
				Role: p.Role,
				URL:  fmt.Sprintf("https://%s:%d", m.Project.Domain, p.Container),
			})
		case manifest.PortTCP:
			r.TCPServices = append(r.TCPServices, TCPSvcResult{
				Role: p.Role,
				Host: fmt.Sprintf("127.0.0.1:%d", DerivePort(podID, p.Role)),
			})
		}
	}
	return r
}
