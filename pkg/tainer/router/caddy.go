package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

type CaddyProject struct {
	Domain string
	IP     string
	Port   string // internal port the project pod listens on (typically 443 or 8080)
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
		b.WriteString("\t\ttransport http {\n")
		b.WriteString("\t\t\ttls_insecure_skip_verify\n")
		b.WriteString("\t\t}\n")
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

// ReloadCaddy tells a running Caddy to reload its config via the admin API.
func ReloadCaddy(caddyfilePath string) error {
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return fmt.Errorf("reading Caddyfile: %w", err)
	}
	req, err := http.NewRequest("POST", "http://127.0.0.1:2019/load",
		strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	q := req.URL.Query()
	q.Set("adapter", "caddyfile")
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Caddy reload failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Caddy reload returned HTTP %d", resp.StatusCode)
	}
	return nil
}
