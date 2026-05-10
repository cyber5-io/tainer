package router

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// UpdateConfig regenerates the Caddyfile + sshpiper upstream files
// from pods, writes them to host paths the router containers mount,
// and POSTs the new Caddyfile to caddy's admin API for hot reload.
// sshpiper's workingdir plugin re-reads upstream files on every new
// SSH connection — no reload needed.
func UpdateConfig(ctx context.Context, eng *engine.Client, pods []PodEndpoint) error {
	caddyContent := GenerateCaddyfileV2(pods, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if err := os.WriteFile(config.CaddyfilePath(), []byte(caddyContent), 0644); err != nil {
		return fmt.Errorf("router: write Caddyfile: %w", err)
	}
	if err := reloadCaddy([]byte(caddyContent)); err != nil {
		return fmt.Errorf("router: reload caddy: %w", err)
	}
	if err := writeSSHPiperUpstreams(pods); err != nil {
		return fmt.Errorf("router: sshpiper: %w", err)
	}
	return nil
}

func reloadCaddy(caddyfile []byte) error {
	req, err := http.NewRequest("POST", "http://"+caddyAdmin+"/load", bytes.NewReader(caddyfile))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("caddy admin returned %s", resp.Status)
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
