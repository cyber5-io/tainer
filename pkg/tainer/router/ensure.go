package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

const (
	// Use fully-qualified docker.io refs so cyberstackd's image
	// lookup (which is case-sensitive on the ref string) matches the
	// pulled image's stored name. Short refs like "caddy:2-alpine"
	// get normalized at pull time but lookup is exact, causing
	// "image not known" on create.
	caddyImage    = "docker.io/library/caddy:2-alpine"
	sshpiperImage = "docker.io/farmer1992/sshpiperd:latest"
	caddyAdmin    = "127.0.0.1:2019"
)

func init() {
	ensureImpl = ensureRouter
}

func ensureRouter(ctx context.Context, eng *engine.Client, extraHTTPPorts []int) error {
	if err := ensureWeb(ctx, eng, extraHTTPPorts); err != nil {
		return err
	}
	if err := ensureSSH(ctx, eng); err != nil {
		return err
	}
	return nil
}

func ensureWeb(ctx context.Context, eng *engine.Client, extraHTTPPorts []int) error {
	wantPorts := append([]engine.PortMap{
		{Container: 80, Host: 80, Proto: "tcp"},
		{Container: 443, Host: 443, Proto: "tcp"},
		{Container: 2019, Host: 2019, Proto: "tcp"},
	}, extraPortMaps(extraHTTPPorts)...)

	insp, ierr := eng.Inspect(ctx, WebContainerName)
	exists := ierr == nil
	if !exists && !isNotFoundErr(ierr) {
		return ierr
	}
	if exists {
		// Check bindings match before deciding whether to reuse.
		// cyberstackd's inspect doesn't yet surface HostConfig so
		// nil-guard the map access — when HostConfig is unset, we
		// can't verify bindings and conservatively assume same
		// (so we don't endlessly recreate the router on every start).
		have := map[int]bool{}
		if insp.HostConfig != nil {
			for p := range insp.HostConfig.PortBindings {
				have[p.Int()] = true
			}
		}
		sameBindings := true
		if insp.HostConfig != nil {
			for _, w := range wantPorts {
				if !have[w.Container] {
					sameBindings = false
					break
				}
			}
		}
		if !sameBindings {
			// Bindings changed: recreate.
			if err := eng.Remove(ctx, WebContainerName, true); err != nil {
				return fmt.Errorf("router web: remove for recreate: %w", err)
			}
			exists = false
		} else {
			// Reuse the existing container. Start it if stopped;
			// nothing to do if already running.
			if insp.State == nil || !insp.State.Running {
				if err := eng.Start(ctx, WebContainerName); err != nil {
					return fmt.Errorf("router web: start existing: %w", err)
				}
			}
			return nil
		}
	}
	if err := eng.Pull(ctx, caddyImage); err != nil {
		return fmt.Errorf("router web: pull %s: %w", caddyImage, err)
	}
	spec := engine.RunSpec{
		Image:   caddyImage,
		Name:    WebContainerName,
		Mounts:  caddyMounts(),
		Ports:   wantPorts,
		Detach:  true,
		Restart: "unless-stopped",
		Cmd:     []string{"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
	}
	if _, err := eng.Run(ctx, spec); err != nil {
		return fmt.Errorf("router web: run: %w", err)
	}
	return nil
}

// isNotFoundErr recognises cyberstackd's + Docker's "container doesn't
// exist" error wording.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "No such container") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "state.json: no such file")
}

func ensureSSH(ctx context.Context, eng *engine.Client) error {
	insp, ierr := eng.Inspect(ctx, SSHContainerName)
	if ierr == nil {
		// Reuse existing container — start if stopped, no-op if running.
		if insp.State == nil || !insp.State.Running {
			if err := eng.Start(ctx, SSHContainerName); err != nil {
				return fmt.Errorf("router ssh: start existing: %w", err)
			}
		}
		return nil
	}
	if !isNotFoundErr(ierr) {
		return ierr
	}
	if err := eng.Pull(ctx, sshpiperImage); err != nil {
		return fmt.Errorf("router ssh: pull %s: %w", sshpiperImage, err)
	}
	spec := engine.RunSpec{
		Image: sshpiperImage,
		Name:  SSHContainerName,
		Mounts: []engine.Mount{
			{Source: config.SSHPiperDir(), Target: "/var/sshpiper"},
			{Source: config.SSHPiperHostKey(), Target: "/etc/ssh/ssh_host_ed25519_key", ReadOnly: true},
		},
		Ports:   []engine.PortMap{{Container: 2222, Host: 2222, Proto: "tcp"}},
		Detach:  true,
		Restart: "unless-stopped",
		// sshpiperd v1.5 CLI is `sshpiperd [opts] <plugin> [plugin opts]`
		// — the daemon serves SSH on :2222 and runs the plugin as its
		// child for routing. Launching the plugin alone (as this did)
		// means nothing listens on :2222 and every connection is closed
		// before key exchange. Port 2222 and the server key
		// (/etc/ssh/ssh_host_ed25519_key, mounted above) are sshpiperd's
		// defaults, so no extra flags are needed.
		Cmd: []string{
			"/sshpiperd/sshpiperd",
			"/sshpiperd/plugins/workingdir",
			"--root", "/var/sshpiper",
			"--no-check-perm",
			"--allow-baduser-name",
		},
	}
	if _, err := eng.Run(ctx, spec); err != nil {
		return fmt.Errorf("router ssh: run: %w", err)
	}
	return nil
}

func extraPortMaps(ports []int) []engine.PortMap {
	out := make([]engine.PortMap, 0, len(ports))
	for _, p := range ports {
		out = append(out, engine.PortMap{Container: p, Host: p, Proto: "tcp"})
	}
	return out
}

func caddyMounts() []engine.Mount {
	return []engine.Mount{
		{Source: config.CaddyfilePath(), Target: "/etc/caddy/Caddyfile"},
		{Source: config.CertFile(), Target: "/certs/tainer.me.crt", ReadOnly: true},
		{Source: config.KeyFile(), Target: "/certs/tainer.me.key", ReadOnly: true},
	}
}

// containerHasBindings reports (exists, sameBindings, err). Used to
// decide whether to recreate the container when extra HTTP ports
// change.
func containerHasBindings(ctx context.Context, eng *engine.Client, name string, want []engine.PortMap) (bool, bool, error) {
	insp, err := eng.Inspect(ctx, name)
	if err != nil {
		// Treat any "container doesn't exist" signal as not-present.
		// cyberstackd returns "open .../state.json: no such file or
		// directory" for missing containers; Docker proper returns
		// "No such container" or "not found".
		msg := err.Error()
		if strings.Contains(msg, "No such container") || strings.Contains(msg, "not found") ||
			strings.Contains(msg, "state.json: no such file") {
			return false, false, nil
		}
		return false, false, err
	}
	have := map[int]bool{}
	for p := range insp.HostConfig.PortBindings {
		have[p.Int()] = true
	}
	for _, w := range want {
		if !have[w.Container] {
			return true, false, nil
		}
	}
	return true, true, nil
}
