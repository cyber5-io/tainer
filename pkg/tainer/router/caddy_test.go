package router

import (
	"strings"
	"testing"
)

func TestGenerateCaddyfile_Empty(t *testing.T) {
	config := GenerateCaddyfile(nil, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if !strings.Contains(config, ":80") {
		t.Error("Caddyfile should contain :80 redirect block")
	}
}

func TestGenerateCaddyfile_OneProject(t *testing.T) {
	projects := []CaddyProject{
		{Domain: "my-client.tainer.me", IP: "10.77.1.2", Port: "443"},
	}
	config := GenerateCaddyfile(projects, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if !strings.Contains(config, "my-client.tainer.me") {
		t.Error("Caddyfile should contain project domain")
	}
	if !strings.Contains(config, "reverse_proxy") {
		t.Error("Caddyfile should contain reverse_proxy directive")
	}
	if !strings.Contains(config, "tls /certs/tainer.me.crt /certs/tainer.me.key") {
		t.Error("Caddyfile should reference TLS cert paths")
	}
}

func TestGenerateCaddyfile_MultipleProjects(t *testing.T) {
	projects := []CaddyProject{
		{Domain: "a.tainer.me", IP: "10.77.1.2", Port: "443"},
		{Domain: "b.tainer.me", IP: "10.77.2.2", Port: "443"},
	}
	config := GenerateCaddyfile(projects, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if !strings.Contains(config, "a.tainer.me") || !strings.Contains(config, "b.tainer.me") {
		t.Error("Caddyfile should contain both project domains")
	}
}

func TestGenerateCaddyfileWithExtraHTTPPorts(t *testing.T) {
	pods := []PodEndpoint{
		{
			Pod:    "mywp",
			Domain: "mywp.tainer.me",
			WebIP:  "10.42.7.4",
			HTTPServices: []HTTPService{
				{Role: "mail", Port: 8025, IP: "10.42.7.5"},
			},
		},
	}
	out := GenerateCaddyfileV2(pods, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	if !strings.Contains(out, "mywp.tainer.me {") {
		t.Errorf("missing main site block:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy 10.42.7.4:80") {
		t.Errorf("missing main reverse_proxy:\n%s", out)
	}
	if !strings.Contains(out, "mywp.tainer.me:8025 {") {
		t.Errorf("missing extra-port site block:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy 10.42.7.5:8025") {
		t.Errorf("missing extra-port reverse_proxy:\n%s", out)
	}
	if !strings.Contains(out, "admin 127.0.0.1:2019") {
		t.Errorf("missing admin endpoint:\n%s", out)
	}
}

func siteBlock(t *testing.T, cfg, domain string) string {
	t.Helper()
	start := strings.Index(cfg, "\n"+domain+" {\n")
	if start < 0 {
		t.Fatalf("no site block for %s:\n%s", domain, cfg)
	}
	rest := cfg[start+1:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("unterminated site block for %s", domain)
	}
	return rest[:end]
}

func TestGenerateCaddyfileV2_HoldingPageWithoutControlPod(t *testing.T) {
	pods := []PodEndpoint{{Pod: "blog", Domain: "blog.tainer.me", WebIP: "10.42.1.4"}}
	cfg := GenerateCaddyfileV2(pods, "/certs/tainer.me.crt", "/certs/tainer.me.key")
	block := siteBlock(t, cfg, ControlDomain)
	for _, want := range []string{
		"tls /certs/tainer.me.crt /certs/tainer.me.key",
		"respond `" + HoldingPage + "` 503",
		`header Content-Type "text/html; charset=utf-8"`,
		`header Cache-Control "no-store"`,
		`header Retry-After "5"`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("holding block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "reverse_proxy") {
		t.Errorf("holding block must not proxy:\n%s", block)
	}
	if strings.Contains(siteBlock(t, cfg, "blog.tainer.me"), "handle_errors") {
		t.Error("other pods keep caddy's own error handling")
	}
}

func TestGenerateCaddyfileV2_HoldingPageWithNoPods(t *testing.T) {
	cfg := GenerateCaddyfileV2(nil, "/c.crt", "/c.key")
	if !strings.Contains(siteBlock(t, cfg, ControlDomain), "respond `"+HoldingPage+"` 503") {
		t.Errorf("an empty router still serves the holding page:\n%s", cfg)
	}
}

func TestGenerateCaddyfileV2_ControlPodServesTheDashboard(t *testing.T) {
	pods := []PodEndpoint{{Pod: "control", Domain: ControlDomain, WebIP: "10.42.9.4"}}
	cfg := GenerateCaddyfileV2(pods, "/c.crt", "/c.key")
	if n := strings.Count(cfg, "\n"+ControlDomain+" {\n"); n != 1 {
		t.Fatalf("want one %s block, got %d:\n%s", ControlDomain, n, cfg)
	}
	block := siteBlock(t, cfg, ControlDomain)
	proxy := strings.Index(block, "reverse_proxy 10.42.9.4:80 {")
	errs := strings.Index(block, "handle_errors {")
	if proxy < 0 || errs < proxy {
		t.Fatalf("want the dashboard proxied with the holding page only for caddy's own errors:\n%s", block)
	}
	if strings.Index(block, "respond `") < errs {
		t.Errorf("the holding page must only be served from handle_errors:\n%s", block)
	}
}

func TestHoldingPage(t *testing.T) {
	lower := strings.ToLower(HoldingPage)
	for _, want := range []string{
		`<meta http-equiv="refresh" content="5">`,
		"tainer is getting your dashboard ready &mdash; this page will reload by itself.",
	} {
		if !strings.Contains(HoldingPage, want) {
			t.Errorf("holding page missing %q", want)
		}
	}
	for _, banned := range []string{"{", "}", "`", "<script", "src=", "href=", "@import", "url("} {
		if strings.Contains(lower, banned) {
			t.Errorf("holding page must not contain %q", banned)
		}
	}
	for _, word := range []string{"podman", "docker", "engine", " vm", "container"} {
		if strings.Contains(lower, word) {
			t.Errorf("holding page copy must not say %q", word)
		}
	}
}
