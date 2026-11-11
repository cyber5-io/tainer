package router

import (
	"fmt"
	"os"
	"strings"
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

// ControlDomain is the built-in dashboard's domain (the control pod).
const ControlDomain = "my.tainer.me"

// HoldingPage is what my.tainer.me shows while the dashboard isn't serving:
// the control pod is down while its image downloads (after a tainer update,
// on first run) or while its setup keeps failing, and without a site block
// caddy would answer an empty 200. It is
// self-contained and reloads itself without JavaScript. It must not contain
// braces (caddy placeholders) or backticks (the Caddyfile quoting).
const HoldingPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="5">
<title>tainer</title>
</head>
<body style="margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#0f1115;color:#e6e8eb;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif">
<main style="text-align:center;padding:24px">
<div style="font-size:32px;font-weight:700;letter-spacing:-0.02em">tainer</div>
<p style="margin:16px 0 0;font-size:16px;color:#a8adb5">tainer is getting your dashboard ready &mdash; this page will reload by itself.</p>
</main>
</body>
</html>`

// holdingResponse serves HoldingPage as a 503 that is retried, never cached.
func holdingResponse(indent string) string {
	return indent + "header Content-Type \"text/html; charset=utf-8\"\n" +
		indent + "header Cache-Control \"no-store\"\n" +
		indent + "header Retry-After \"5\"\n" +
		indent + "respond `" + HoldingPage + "` 503\n"
}

// GenerateCaddyfileV2 renders site blocks from PodEndpoint records.
// Replaces the v1 (CaddyProject IP/Port) generator — kept side-by-side
// during the rebuild but lifecycle code calls only V2.
//
// my.tainer.me always has a site block: the control pod's when it is
// listed, with the holding page for errors caddy raises itself (the pod's
// web role unreachable), otherwise one that serves only the holding page.
// Responses the dashboard sends, including its own 5xx, pass through.
func GenerateCaddyfileV2(pods []PodEndpoint, certPath, keyPath string) string {
	var b strings.Builder
	b.WriteString("{\n\tadmin 127.0.0.1:2019\n\tauto_https off\n}\n\n")
	b.WriteString(":80 {\n\tredir https://{host}{uri} permanent\n}\n\n")

	control := false
	for _, p := range pods {
		b.WriteString(fmt.Sprintf("%s {\n", p.Domain))
		b.WriteString(fmt.Sprintf("\ttls %s %s\n", certPath, keyPath))
		b.WriteString(fmt.Sprintf("\treverse_proxy %s:80 {\n", p.WebIP))
		b.WriteString("\t\theader_up X-Forwarded-Proto https\n")
		b.WriteString("\t\tlb_try_duration 30s\n")
		b.WriteString("\t\tlb_try_interval 500ms\n")
		b.WriteString("\t}\n")
		if p.Domain == ControlDomain {
			control = true
			b.WriteString("\thandle_errors {\n")
			b.WriteString(holdingResponse("\t\t"))
			b.WriteString("\t}\n")
		}
		b.WriteString("}\n\n")
		for _, h := range p.HTTPServices {
			b.WriteString(fmt.Sprintf("%s:%d {\n", p.Domain, h.Port))
			b.WriteString(fmt.Sprintf("\ttls %s %s\n", certPath, keyPath))
			b.WriteString(fmt.Sprintf("\treverse_proxy %s:%d\n", h.IP, h.Port))
			b.WriteString("}\n\n")
		}
	}
	if !control {
		b.WriteString(fmt.Sprintf("%s {\n", ControlDomain))
		b.WriteString(fmt.Sprintf("\ttls %s %s\n", certPath, keyPath))
		b.WriteString(holdingResponse("\t"))
		b.WriteString("}\n\n")
	}
	return b.String()
}
