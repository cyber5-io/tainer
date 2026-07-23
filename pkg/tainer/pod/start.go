package pod

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
	"github.com/cyber5-io/tainer/pkg/tainer/ssh"
	"github.com/docker/docker/api/types/container"
	units "github.com/docker/go-units"
)

// podCtx holds per-pod state that buildEnv / buildMounts / etc. need
// but that's stable across all containers in the pod. Built once in
// Start() and threaded through.
type podCtx struct {
	secrets *Secrets
	hostUID int
	hostGID int
}

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
	// AlreadyRunning is true when every container in the pod was already
	// up, so start created/started nothing. The CLI uses this to say
	// "already running" instead of pretending it did a fresh start.
	AlreadyRunning bool
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
// ensureHostSetup provisions the host-side files tainer needs before it can
// bring a pod up. On a fresh (or --purge'd) install the ~/.config/tainer tree
// and the SSH key material don't exist yet: the router containers bind-mount
// the sshpiper host key + pod key, and router.UpdateConfig hard-fails without
// tainer_rsa. These helpers all existed but were never wired into start — the
// dev machine just always had the files. All are idempotent (no-op if present).
func ensureHostSetup() (bool, error) {
	if err := config.EnsureDirs(); err != nil {
		return false, fmt.Errorf("pod start: create config dirs: %w", err)
	}
	keyGenerated, err := ssh.EnsureKeyPair(config.PrivateKey(), config.PublicKey())
	if err != nil {
		return false, fmt.Errorf("pod start: ssh key pair: %w", err)
	}
	if err := ssh.EnsureHostKey(config.SSHPiperHostKey()); err != nil {
		return false, fmt.Errorf("pod start: sshpiper host key: %w", err)
	}
	if err := ensureCerts(); err != nil {
		return false, fmt.Errorf("pod start: tls certs: %w", err)
	}
	ensureClientSSHConfig()
	return keyGenerated, nil
}

// ensureClientSSHConfig points the user's ssh at the tainer key for
// ssh.tainer.me. Best-effort and fail-soft: it must never abort a start.
// It manages the per-user side only: the drop-in at
// ~/.cyberstack/0-tainer.conf and the Include as line 1 of ~/.ssh/config
// (when that file exists). The system-wide drop-in at
// /etc/ssh/ssh_config.d/0-tainer.conf is installed by the package's
// postinstall (root) — tainer start runs unprivileged and must not touch it.
func ensureClientSSHConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("ssh client config: home dir: %v", err)
		return
	}
	userDrop := filepath.Join(home, ".cyberstack", ssh.DropInName)
	if err := ssh.WriteDropIn(userDrop, ssh.ClientIdentityTilde); err != nil {
		log.Printf("ssh client config: write %s: %v", userDrop, err)
	}
	sshCfg := filepath.Join(home, ".ssh", "config")
	if err := ssh.EnsureUserConfigInclude(sshCfg, ssh.UserConfigIncludeLine); err != nil {
		log.Printf("ssh client config: include in %s: %v", sshCfg, err)
	}
}

// ensureCerts provisions the *.tainer.me TLS cert+key into CertsDir on first
// run by copying them from the bundle shipped in the installer
// (<prefix>/share/tainer/certs, resolved relative to the tainer binary). Caddy
// bind-mounts these to serve browser-trusted HTTPS on the loopback-resolved
// project subdomains. No-op if the certs already exist, or if no bundle is
// present (a dev build outside the installer layout — the dev machine already
// has them under ~/.config/tainer/certs).
func ensureCerts() error {
	crt, key := config.CertFile(), config.KeyFile()
	if fileExists(crt) && fileExists(key) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved // /usr/local/bin/tainer -> /opt/tainer/bin/tainer
	}
	share := filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "tainer", "certs")
	srcCrt, srcKey := filepath.Join(share, "tainer.me.crt"), filepath.Join(share, "tainer.me.key")
	if !fileExists(srcCrt) || !fileExists(srcKey) {
		return nil // not bundled; leave it (doctor surfaces the missing cert)
	}
	if err := copyFileMode(srcCrt, crt, 0o644); err != nil {
		return err
	}
	return copyFileMode(srcKey, key, 0o600)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// copyFileMode copies src to dst with the given mode, atomically (temp +
// rename) so an interrupted copy can't leave a truncated cert/key behind.
func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func Start(ctx context.Context, eng *engine.Client, opts StartOptions) (*StartResult, error) {
	keyGenerated, err := ensureHostSetup()
	if err != nil {
		return nil, err
	}

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

	// Pull each role's image up-front. eng.Pull is idempotent — fast no-op
	// when the image is already local. Without this, Create fails with
	// "image not known" because the engine API doesn't auto-pull on create
	// (only the docker CLI does).
	for _, role := range RolesForPod(m) {
		if err := eng.Pull(ctx, ImageRef(m, role)); err != nil {
			return nil, fmt.Errorf("pod start: pull %s: %w", role, err)
		}
	}

	// Per-project DB credentials. Generated + persisted on first start;
	// loaded on subsequent starts. Skipped in smoke mode (busybox images
	// don't read these env vars and writing the file pollutes ~).
	var secrets *Secrets
	if !smokeImages() {
		secrets, err = LoadOrCreateSecrets(m.Project.Name, opts.ProjectDir)
		if err != nil {
			return nil, fmt.Errorf("pod start: load secrets: %w", err)
		}
	}
	pctx := &podCtx{
		secrets: secrets,
		hostUID: os.Getuid(),
		hostGID: os.Getgid(),
	}

	mhash := ManifestHash(m)
	leader := ContainerName(m.Project.Name, RoleWeb)

	// Build the full TCP port-binding set for the pod and apply it to
	// the leader. Followers don't get PortBindings — those would be
	// silently no-op'd by cyberstack since the netns is shared.
	leaderPortBindings := buildLeaderPortBindings(m, podID)

	roles := RolesForPod(m)
	if len(roles) == 0 || roles[0] != RoleWeb {
		return nil, fmt.Errorf("pod start: leader must be %q, got roles=%v", RoleWeb, roles)
	}

	// If the tainer key was just (re)generated, existing pod containers still
	// bind-mount the OLD pubkey at /etc/ssh/tainer_authorized_keys (mounts are
	// fixed at create time; startContainer reuses a stopped container in place,
	// so a stop/start would keep the stale mount). Remove them so the loop
	// below recreates them fresh with the new key. Leader is recreated first,
	// followers rejoin its netns — the existing create order handles that.
	if keyGenerated {
		for _, role := range roles {
			name := ContainerName(m.Project.Name, role)
			if err := eng.Remove(ctx, name, true); err != nil {
				log.Printf("key rotation: remove %s: %v", name, err)
			}
		}
	}

	anyWork := false
	for i, role := range roles {
		var bindings []engine.PortMap
		var netMode string
		if i == 0 { // leader
			bindings = leaderPortBindings
		} else {
			netMode = "container:" + leader
		}
		started, err := startContainer(ctx, eng, m, opts, pctx, role, podID, mhash, split[role], bindings, netMode)
		if err != nil {
			return nil, err
		}
		anyWork = anyWork || started
	}

	allPods, err := List(ctx, eng)
	if err != nil {
		return nil, err
	}
	endpoints, err := buildEndpoints(ctx, eng, allPods)
	if err != nil {
		return nil, err
	}

	// Seed the router config files (Caddyfile + sshpiper upstreams) BEFORE
	// creating the router containers, which bind-mount them — a bind mount
	// whose source doesn't exist fails container start on a fresh install.
	if err := router.WriteConfig(endpoints); err != nil {
		return nil, err
	}
	if err := router.Ensure(ctx, eng, router.ExtraHTTPPorts(endpoints)); err != nil {
		return nil, err
	}
	if err := router.UpdateConfig(ctx, eng, endpoints); err != nil {
		return nil, err
	}

	res := makeStartResult(m, podID)
	res.AlreadyRunning = !anyWork
	return res, nil
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
			Role:      p.Role,
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
	pctx *podCtx, role string, podID int, mhash string, lim Limits,
	bindings []engine.PortMap, netMode string,
) (started bool, err error) {
	memBytes, _ := units.RAMInBytes(lim.Memory)
	envs := buildEnv(m, role, pctx)
	mounts := buildMounts(m, opts.ProjectDir, role)

	labels := map[string]string{
		LabelPod:          m.Project.Name,
		LabelRole:         role,
		LabelPodID:        strconv.Itoa(podID),
		LabelDomain:       m.Project.Domain,
		LabelManifestPath: opts.ManifestPath,
		LabelManifestHash: mhash,
	}
	// Emit one published-port label per binding, keyed by the binding's
	// own role rather than this container's role. Under shared-netns the
	// leader carries every role's bindings, but List() / Inspect() expect
	// each role's port to live under its own PublishLabel(role) key.
	for _, b := range bindings {
		if b.Role == "" {
			continue
		}
		labels[PublishLabel(b.Role)] = strconv.Itoa(b.Host)
	}

	// Smoke mode: no restart policy. Real pods auto-restart on
	// crash (unless-stopped). Smoke containers shouldn't — a crash
	// in the smoke is a signal we want to investigate, not paper
	// over with a retry loop (which trips crun's container-id
	// reuse limitation).
	restartPolicy := "unless-stopped"
	if smokeImages() {
		restartPolicy = "no"
	}
	spec := engine.RunSpec{
		Image:       ImageRef(m, role),
		Name:        ContainerName(m.Project.Name, role),
		NetworkMode: netMode,
		Cmd:         smokeCmd(role),
		Env:         envs,
		Mounts:      mounts,
		Ports:       bindings,
		Detach:      true,
		Restart:     restartPolicy,
		Resources: container.Resources{
			Memory:   memBytes,
			NanoCPUs: int64(lim.CPU * 1e9),
		},
		Labels: labels,
	}

	// Idempotent start: if a container with this name already exists
	// from a previous `tainer start`/`stop` cycle, restart it in place
	// rather than recreating. Preserves any container-side state
	// (database files in the bind mount, etc) between cycles.
	//
	// This applies to shared-netns followers too: cyberstack 0.5.2
	// re-resolves the leader PID and rewrites the follower's config.json
	// at Start time, so a stale /proc/<pid>/ns/net no longer wedges the
	// restart and we no longer destroy + recreate followers each cycle.
	insp, ierr := eng.Inspect(ctx, spec.Name)
	if ierr == nil {
		if insp.State != nil && insp.State.Running {
			return false, nil // already up — nothing to do
		}
		// Container exists but is stopped — start it in place.
		return true, eng.Start(ctx, spec.Name)
	} else if !isNotFound(ierr) {
		return false, ierr
	}
	_, err = eng.Run(ctx, spec)
	return true, err
}

// isNotFound reports whether err is the engine's "container doesn't
// exist" error. cyberstackd returns "open .../state.json: no such file"
// and Docker proper returns "No such container" / "not found".
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "No such container") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "state.json: no such file")
}

// smokeCmd overrides the container's CMD in TAINER_SMOKE_IMAGES mode.
// Both roles run busybox + sleep infinity so the pod survives long
// enough to exercise orchestration (leader/follower netns sharing,
// port bindings, tainer stop). Real engine images are out of scope
// for smoke — they require uid-mapping chowns the virtio-fs binds
// don't permit.
func smokeCmd(role string) []string {
	if !smokeImages() {
		return nil
	}
	return []string{"sh", "-c", "echo tainer-smoke " + role + " up; exec sleep infinity"}
}

func buildEnv(m *manifest.Manifest, role string, pctx *podCtx) []string {
	env := []string{
		"TAINER_PROJECT=" + m.Project.Name,
		"TAINER_DOMAIN=" + m.Project.Domain,
		"TAINER_ROLE=" + role,
	}

	// Smoke-images mode: minimal env wiring. Stock mariadb/postgres need
	// a password env to boot; empty-password is fine for a throwaway smoke.
	if smokeImages() {
		if role == RoleDB {
			if m.Runtime.Database == manifest.DatabasePostgres {
				env = append(env, "POSTGRES_HOST_AUTH_METHOD=trust")
			} else {
				env = append(env, "MARIADB_ALLOW_EMPTY_ROOT_PASSWORD=yes")
			}
		}
		return env
	}

	// Real image env contract (see tainer-images/docs/0.9-images.md).
	env = append(env,
		"TAINER_UID="+strconv.Itoa(pctx.hostUID),
		"TAINER_GID="+strconv.Itoa(pctx.hostGID),
	)

	// DB connection details — everything sees the same loopback under
	// shared netns, so DB_HOST is always 127.0.0.1.
	if pctx.secrets != nil && m.HasDatabase() {
		env = append(env,
			"DB_HOST=127.0.0.1",
			"DB_PORT="+m.DBPort(),
			"DB_NAME="+pctx.secrets.DBName,
			"DB_USER="+pctx.secrets.DBUser,
			"DB_PASSWORD="+pctx.secrets.DBPassword,
		)
	}

	switch role {
	case RoleWeb:
		// caddy-web entrypoint picks /etc/caddy/sites/<type>.Caddyfile.
		env = append(env, "TAINER_PROJECT_TYPE="+string(m.Project.Type))

	case RoleApp:
		if m.IsPHP() {
			env = append(env, m.Runtime.PHPLimits.EnvFlags()...)
		}
		// WordPress wp-config needs the site URL.
		if m.Project.Type == manifest.TypeWordPress && m.Project.Domain != "" {
			env = append(env, "WP_HOME=https://"+m.Project.Domain)
		}

	case RoleDB:
		if pctx.secrets == nil {
			break
		}
		if m.Runtime.Database == manifest.DatabasePostgres {
			env = append(env,
				"POSTGRES_DB="+pctx.secrets.DBName,
				"POSTGRES_USER="+pctx.secrets.DBUser,
				"POSTGRES_PASSWORD="+pctx.secrets.DBPassword,
			)
		} else {
			env = append(env,
				"MARIADB_DATABASE="+pctx.secrets.DBName,
				"MARIADB_USER="+pctx.secrets.DBUser,
				"MARIADB_PASSWORD="+pctx.secrets.DBPassword,
				"MARIADB_ROOT_PASSWORD="+pctx.secrets.DBRootPassword,
			)
		}
	}

	// Mark the pod's single SSH container (see isSSHContainer). Its
	// start script runs sshd only when TAINER_SSHD=1, so app and web
	// never collide on :22 in the shared netns. TAINER_SHELL=zsh tells
	// tainer-entrypoint.sh to flip the tainer user's login shell to zsh
	// (oh-my-zsh + af-magic), so an interactive ssh lands in a proper
	// shell rather than busybox sh. Both only apply where an SSH session
	// terminates — the app container.
	if isSSHContainer(m, role) {
		env = append(env, "TAINER_SSHD=1", "TAINER_SHELL=zsh")
	}

	return env
}

// isSSHContainer reports whether this role owns the pod's single sshd
// (:22 in the shared netns): the app (php-fpm/node) when the pod has
// one, otherwise the web caddy (react/static).
func isSSHContainer(m *manifest.Manifest, role string) bool {
	hasApp := false
	for _, r := range RolesForPod(m) {
		if r == RoleApp {
			hasApp = true
			break
		}
	}
	if hasApp {
		return role == RoleApp
	}
	return role == RoleWeb
}

func buildMounts(m *manifest.Manifest, projectDir, role string) []engine.Mount {
	var out []engine.Mount
	switch {
	case role == RoleDB:
		out = []engine.Mount{
			{Source: projectDir + "/db", Target: dbDataPath(m)},
		}
	case isSSHContainer(m, role):
		// The pod's dev shell (the app container, or web on app-less
		// types). Mount the WHOLE project at /var/www so an interactive
		// ssh lands in the real git repo — .git, tainer.yaml and tooling
		// are all present and af-magic's git prompt works. html/ and
		// data/ come along as subdirs, so this replaces the narrow
		// mounts below. The docroot stays /var/www/html, so nothing above
		// it (.git, .env, tainer.yaml) is ever web-served. Safe for local
		// dev — the repo is already on the developer's host; production
		// deploys will need the isolated mount shape instead.
		out = []engine.Mount{
			{Source: projectDir, Target: m.ContainerMountBase()},
		}
	default:
		// Internet-facing edge (caddy web on app-having types) — kept
		// narrow: only the docroot + data, never the repo, secrets, or
		// db files.
		out = []engine.Mount{
			{Source: projectDir + "/" + m.HostAppDir(), Target: m.ContainerAppPath()},
			{Source: projectDir + "/data", Target: m.ContainerMountBase() + "/data"},
		}
		for _, name := range m.Mounts {
			out = append(out, engine.Mount{Source: projectDir + "/" + name, Target: m.ContainerMountBase() + "/" + name})
		}
	}
	// Clone-then-start UX: a project cloned from git carries
	// tainer.yaml but not the (gitignored, often empty) data/, db/ or
	// even html/ dirs — init created them for the original author but
	// git doesn't transport empty directories. crun hard-fails on a
	// bind mount whose source is missing, so ensure every mount
	// source exists before the container spec goes anywhere near it.
	// The whole-project mount only lists the parent, so also ensure the
	// html/ and data/ subdirs the runtime expects.
	sources := make([]string, 0, len(out)+2)
	for _, mt := range out {
		sources = append(sources, mt.Source)
	}
	if isSSHContainer(m, role) && role != RoleDB {
		sources = append(sources, projectDir+"/"+m.HostAppDir(), projectDir+"/data")
	}
	for _, s := range sources {
		if _, err := os.Stat(s); os.IsNotExist(err) {
			_ = os.MkdirAll(s, 0755)
		}
	}
	// SSH: stage the tainer public key into the pod's SSH container so
	// its sshd (AuthorizedKeysFile /etc/ssh/tainer_authorized_keys)
	// accepts sshpiperd's tainer-key auth. Added after the auto-create
	// pass — this source is a file, not a dir.
	if isSSHContainer(m, role) {
		if fi, err := os.Stat(config.PublicKey()); err == nil && !fi.IsDir() {
			out = append(out, engine.Mount{Source: config.PublicKey(), Target: "/etc/ssh/tainer_authorized_keys", ReadOnly: true})
		}
	}
	return out
}

func dbDataPath(m *manifest.Manifest) string {
	if m.Runtime.Database == manifest.DatabasePostgres {
		return "/var/lib/postgresql/data"
	}
	return "/var/lib/mysql"
}

// buildEndpoints derives a PodEndpoint per pod by inspecting the leader
// (web) container's IP. Under shared-netns / shared bridge the leader owns
// the whole pod's network identity; use the first non-empty IP from the
// Networks map (IPAddress is only set for the default bridge).
func buildEndpoints(ctx context.Context, eng *engine.Client, pods []Pod) ([]router.PodEndpoint, error) {
	out := make([]router.PodEndpoint, 0, len(pods))
	for _, p := range pods {
		ep := router.PodEndpoint{Pod: p.Name, Domain: p.Domain}
		var webName string
		for _, c := range p.Containers {
			if c.Role == RoleWeb {
				webName = c.Name
				break
			}
		}
		if webName == "" {
			continue
		}
		insp, err := eng.Inspect(ctx, webName)
		if err != nil {
			continue
		}
		for _, netInfo := range insp.NetworkSettings.Networks {
			if netInfo.IPAddress != "" {
				ep.WebIP = netInfo.IPAddress
				break
			}
		}
		// cyberstackd reports the leader's IP at the top-level
		// NetworkSettings.IPAddress (no per-network map). Fall back
		// to that when Networks is empty.
		if ep.WebIP == "" {
			ep.WebIP = insp.NetworkSettings.IPAddress
		}
		// Skip pods we can't route to (e.g. stopped — no IP). The
		// router doesn't need a site block for them and an empty
		// Domain/WebIP would produce a malformed Caddyfile.
		if ep.Domain == "" || ep.WebIP == "" {
			continue
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
			host := p.HostPort
			if host == 0 {
				host = DerivePort(podID, p.Role)
			}
			if host == 0 {
				continue
			}
			r.TCPServices = append(r.TCPServices, TCPSvcResult{
				Role: p.Role,
				Host: fmt.Sprintf("127.0.0.1:%d", host),
			})
		}
	}
	return r
}
