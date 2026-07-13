package router

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// WriteConfig regenerates the Caddyfile + sshpiper upstream files from pods
// and writes them to the host paths the router containers bind-mount. It does
// NOT reload caddy, so it can safely run BEFORE router.Ensure creates those
// containers — which is required on a fresh install, since a bind mount whose
// source file doesn't exist fails container start (crun can't stat it).
func WriteConfig(pods []PodEndpoint) error {
	if err := os.MkdirAll(config.RouterDir(), 0755); err != nil {
		return fmt.Errorf("router: mkdir %s: %w", config.RouterDir(), err)
	}
	caddyContent := GenerateCaddyfileV2(pods, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if err := os.WriteFile(config.CaddyfilePath(), []byte(caddyContent), 0644); err != nil {
		return fmt.Errorf("router: write Caddyfile: %w", err)
	}
	if err := writeSSHPiperUpstreams(pods); err != nil {
		return fmt.Errorf("router: sshpiper: %w", err)
	}
	return nil
}

// UpdateConfig writes the router config (see WriteConfig) then reloads caddy
// via container exec (`caddy reload`) to apply changes on a running router.
// sshpiper's workingdir plugin re-reads upstream files on every new SSH
// connection, so it needs no reload.
//
// We use `caddy reload` over exec rather than caddy's admin HTTP API because
// the admin port is host-unreachable in vmnet-helper mode: DNAT to a
// 127.0.0.1-bound listener inside the container rewrites the destination to
// the container's veth IP, which the listener rejects. Exec-based reload
// sidesteps that entirely.
func UpdateConfig(ctx context.Context, eng *engine.Client, pods []PodEndpoint) error {
	if err := WriteConfig(pods); err != nil {
		return err
	}
	if err := reloadCaddy(ctx, eng); err != nil {
		return fmt.Errorf("router: reload caddy: %w", err)
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
