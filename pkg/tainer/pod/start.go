package pod

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/network"
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

	subnet, err := network.AllocateSubnet(m.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("pod start: subnet: %w", err)
	}
	podID := podIDFromSubnet(subnet)

	netName := NetworkName(m.Project.Name)
	if err := eng.NetworkCreate(ctx, netName, subnet); err != nil {
		return nil, err
	}

	split, err := Split(m)
	if err != nil {
		return nil, err
	}

	mhash := ManifestHash(m)
	for _, role := range RolesForPod(m) {
		if err := startContainer(ctx, eng, m, opts, role, podID, mhash, split[role]); err != nil {
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
	if err := router.AttachToPod(ctx, eng, m.Project.Name); err != nil {
		return nil, err
	}
	// Re-build endpoints now that the router is attached so its IPs
	// resolve correctly when we POST the new Caddyfile.
	endpoints, err = buildEndpoints(ctx, eng, allPods)
	if err != nil {
		return nil, err
	}
	if err := router.UpdateConfig(ctx, eng, endpoints); err != nil {
		return nil, err
	}

	return makeStartResult(m, podID), nil
}

// startContainer creates+starts one role's container with the right
// labels, resources, mounts, and port bindings.
func startContainer(
	ctx context.Context, eng *engine.Client,
	m *manifest.Manifest, opts StartOptions,
	role string, podID int, mhash string, lim Limits,
) error {
	memBytes, _ := units.RAMInBytes(lim.Memory)
	envs := buildEnv(m, role)
	mounts := buildMounts(m, opts.ProjectDir, role)
	tcpBindings := buildTCPBindings(m, podID, role)

	labels := map[string]string{
		LabelPod:          m.Project.Name,
		LabelRole:         role,
		LabelPodID:        strconv.Itoa(podID),
		LabelManifestPath: opts.ManifestPath,
		LabelManifestHash: mhash,
	}
	for _, b := range tcpBindings {
		labels[PublishLabel(role)] = strconv.Itoa(b.Host)
	}

	spec := engine.RunSpec{
		Image:   ImageRef(m, role),
		Name:    ContainerName(m.Project.Name, role),
		Network: NetworkName(m.Project.Name),
		Env:     envs,
		Mounts:  mounts,
		Ports:   tcpBindings,
		Detach:  true,
		Restart: "unless-stopped",
		Resources: container.Resources{
			Memory:   memBytes,
			NanoCPUs: int64(lim.CPU * 1e9),
		},
		Labels: labels,
	}
	_, err := eng.Run(ctx, spec)
	return err
}

func podIDFromSubnet(subnet string) int {
	var o1, o2, o3 int
	fmt.Sscanf(subnet, "%d.%d.%d.0/24", &o1, &o2, &o3)
	return o3
}

// buildEnv, buildMounts, buildTCPBindings, buildEndpoints, makeStartResult:
// kept in adjacent files split.go / mounts.go for readability — the
// implementation stubs below should be expanded per project type during
// integration. For initial smoke (TestStartWordPress in Task 27) the
// minimal env/mount set below is enough.
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

func buildTCPBindings(m *manifest.Manifest, octet int, role string) []engine.PortMap {
	var out []engine.PortMap
	for _, p := range m.Ports {
		if p.Role != role || p.Protocol != manifest.PortTCP {
			continue
		}
		host := DerivePort(octet, role)
		out = append(out, engine.PortMap{Container: p.Container, Host: host, Proto: "tcp"})
	}
	return out
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
			netName := NetworkName(p.Name)
			netInfo, ok := insp.NetworkSettings.Networks[netName]
			if !ok {
				continue
			}
			ip := netInfo.IPAddress
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
