package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

const (
	caddyImage    = "caddy:2-alpine"
	sshpiperImage = "farmer1992/sshpiperd:latest"
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

	exists, sameBindings, err := containerHasBindings(ctx, eng, WebContainerName, wantPorts)
	if err != nil {
		return err
	}
	if exists && sameBindings {
		return nil
	}
	if exists {
		// Bindings changed: recreate the container so Docker picks up new -p flags.
		if err := eng.Remove(ctx, WebContainerName, true); err != nil {
			return fmt.Errorf("router web: remove for recreate: %w", err)
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

func ensureSSH(ctx context.Context, eng *engine.Client) error {
	exists, _, err := containerHasBindings(ctx, eng, SSHContainerName, []engine.PortMap{
		{Container: 2222, Host: 2222, Proto: "tcp"},
	})
	if err != nil {
		return err
	}
	if exists {
		return nil
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
		Cmd: []string{
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
		if strings.Contains(err.Error(), "No such container") || strings.Contains(err.Error(), "not found") {
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
