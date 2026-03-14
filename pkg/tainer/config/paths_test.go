package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBaseDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := BaseDir()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "tainer")
	if dir != expected {
		t.Errorf("BaseDir() = %q, want %q", dir, expected)
	}
}

func TestBaseDirXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	dir := BaseDir()
	expected := filepath.Join("/tmp/xdgtest", "tainer")
	if dir != expected {
		t.Errorf("BaseDir() with XDG = %q, want %q", dir, expected)
	}
}

func TestSubPaths(t *testing.T) {
	base := BaseDir()
	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"ProjectsFile", ProjectsFile, filepath.Join(base, "projects.json")},
		{"NetworksFile", NetworksFile, filepath.Join(base, "networks.json")},
		{"KeysDir", KeysDir, filepath.Join(base, "keys")},
		{"CertsDir", CertsDir, filepath.Join(base, "certs")},
		{"TemplatesDir", TemplatesDir, filepath.Join(base, "templates")},
		{"RouterDir", RouterDir, filepath.Join(base, "router")},
		{"SSHPiperDir", SSHPiperDir, filepath.Join(base, "router", "sshpiper")},
		{"CaddyfilePath", CaddyfilePath, filepath.Join(base, "router", "Caddyfile")},
		{"DnsmasqConf", DnsmasqConf, filepath.Join(base, "router", "dnsmasq.conf")},
		{"CertFile", CertFile, filepath.Join(base, "certs", "tainer.me.crt")},
		{"KeyFile", KeyFile, filepath.Join(base, "certs", "tainer.me.key")},
		{"PrivateKey", PrivateKey, filepath.Join(base, "keys", "tainer_rsa")},
		{"PublicKey", PublicKey, filepath.Join(base, "keys", "tainer_rsa.pub")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Errorf("%s() = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestEnsureDirs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	err := EnsureDirs()
	if err != nil {
		t.Fatalf("EnsureDirs() error: %v", err)
	}
	for _, dir := range []string{"keys", "certs", "templates", "router", "router/sshpiper"} {
		path := filepath.Join(tmpDir, "tainer", dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("EnsureDirs() did not create %q", path)
		}
	}
}
