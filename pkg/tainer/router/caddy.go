package router

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// CaddyContainerName is the engine name of the long-running Caddy
// reverse proxy container. The router orchestration that creates the
// container (currently in legacy/, will be redesigned for Step 8) must
// agree on this name.
const CaddyContainerName = "tainer-router-caddy"

type CaddyProject struct {
	Domain string
	IP     string
	Port   string // internal port the project pod listens on
}

// GenerateCaddyfile creates a Caddyfile with reverse proxy entries for all running projects.
func GenerateCaddyfile(projects []CaddyProject, certPath, keyPath string) string {
	var b strings.Builder

	// Global options
	b.WriteString("{\n")
	b.WriteString("\tadmin 127.0.0.1:2019\n")
	b.WriteString("\tauto_https off\n")
	b.WriteString("}\n\n")

	// HTTP → HTTPS redirect
	b.WriteString(":80 {\n")
	b.WriteString("\tredir https://{host}{uri} permanent\n")
	b.WriteString("}\n\n")

	// Per-project HTTPS blocks
	for _, p := range projects {
		b.WriteString(fmt.Sprintf("%s {\n", p.Domain))
		b.WriteString(fmt.Sprintf("\ttls %s %s\n", certPath, keyPath))
		b.WriteString(fmt.Sprintf("\treverse_proxy %s:%s {\n", p.IP, p.Port))
		b.WriteString("\t\theader_up X-Forwarded-Proto https\n")
		// Retry for up to 30s if upstream isn't ready yet (first-hit race
		// when a project starts but the app hasn't bound the port yet).
		b.WriteString("\t\tlb_try_duration 30s\n")
		b.WriteString("\t\tlb_try_interval 500ms\n")
		b.WriteString("\t\tfail_duration 5s\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n\n")
	}

	return b.String()
}

// WriteCaddyfile writes the generated config to disk.
func WriteCaddyfile(path string, projects []CaddyProject, certPath, keyPath string) error {
	content := GenerateCaddyfile(projects, certPath, keyPath)
	return os.WriteFile(path, []byte(content), 0644)
}

// ReloadCaddy tells the running Caddy router to re-read its Caddyfile.
// The container itself is started by the router orchestration layer
// (legacy/router until Step 8 redesign); this helper is the engine-API
// equivalent of `caddy reload --config /etc/caddy/Caddyfile`.
func ReloadCaddy(eng *engine.Client) error {
	res, err := eng.Exec(context.Background(), CaddyContainerName,
		[]string{"caddy", "reload", "--config", "/etc/caddy/Caddyfile"})
	if err != nil {
		return fmt.Errorf("Caddy reload: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("Caddy reload exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}
