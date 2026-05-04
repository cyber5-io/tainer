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
