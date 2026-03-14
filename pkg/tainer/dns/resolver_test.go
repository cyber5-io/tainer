package dns

import (
	"runtime"
	"testing"
)

func TestResolverConfig_macOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}
	path, content := ResolverConfig()
	if path != "/etc/resolver/tainer.me" {
		t.Errorf("path = %q, want /etc/resolver/tainer.me", path)
	}
	if content != "nameserver 127.0.0.1\n" {
		t.Errorf("content = %q, want nameserver line", content)
	}
}

func TestResolverConfig_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}
	path, content := ResolverConfig()
	if path != "/etc/systemd/resolved.conf.d/tainer.conf" {
		t.Errorf("path = %q, want systemd-resolved config", path)
	}
	expected := "[Resolve]\nDNS=127.0.0.1\nDomains=~tainer.me\n"
	if content != expected {
		t.Errorf("content = %q, want %q", content, expected)
	}
}

func TestIsResolverInstalled_NotInstalled(t *testing.T) {
	// Test with a non-existent path
	if isInstalled("/nonexistent/path/tainer.conf") {
		t.Error("should be false for nonexistent path")
	}
}
