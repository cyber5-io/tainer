package router

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// UpdateConfig regenerates the Caddyfile + sshpiper upstream files
// from pods, writes them to host paths the router containers mount,
// and reloads caddy via container exec (`caddy reload`). sshpiper's
// workingdir plugin re-reads upstream files on every new SSH
// connection — no reload needed.
//
// We use `caddy reload` over exec rather than caddy's admin HTTP API
// because the admin port is host-unreachable in vmnet-helper mode:
// DNAT to a 127.0.0.1-bound listener inside the container rewrites
// the destination to the container's veth IP, which the listener
// rejects. Exec-based reload sidesteps that entirely.
func UpdateConfig(ctx context.Context, eng *engine.Client, pods []PodEndpoint) error {
	caddyContent := GenerateCaddyfileV2(pods, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if err := os.WriteFile(config.CaddyfilePath(), []byte(caddyContent), 0644); err != nil {
		return fmt.Errorf("router: write Caddyfile: %w", err)
	}
	if err := reloadCaddy(ctx, eng); err != nil {
		return fmt.Errorf("router: reload caddy: %w", err)
	}
	if err := writeSSHPiperUpstreams(pods); err != nil {
		return fmt.Errorf("router: sshpiper: %w", err)
	}
	return nil
}

func reloadCaddy(ctx context.Context, eng *engine.Client) error {
	res, err := eng.Exec(ctx, WebContainerName,
		[]string{"caddy", "reload", "--config", "/etc/caddy/Caddyfile"})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func writeSSHPiperUpstreams(pods []PodEndpoint) error {
	dir := config.SSHPiperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, p := range pods {
		if p.WebIP == "" {
			continue
		}
		// AddSSHPiperEntry already exists in pkg/tainer/router/sshpiper.go.
		if err := AddSSHPiperEntry(dir, p.Pod, p.WebIP, config.PrivateKey()); err != nil {
			return err
		}
	}
	return nil
}
