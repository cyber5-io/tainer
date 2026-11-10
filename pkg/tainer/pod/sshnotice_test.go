package pod

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureClientSSHConfigAt_UnwritableConfigPrintsOneNotice(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	home := t.TempDir()
	for _, d := range []string{".ssh", ".cyberstack"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(home, ".ssh", "config")
	if err := os.WriteFile(cfg, []byte("Host x\n  User y\n"), 0400); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	ensureClientSSHConfigAt(home, &out)

	want := "couldn't update ~/.ssh/config (not writable) — tainer ssh still works; fix with: sudo chown $USER ~/.ssh/config\n"
	if out.String() != want {
		t.Fatalf("notice = %q, want %q", out.String(), want)
	}
	if _, err := os.Stat(filepath.Join(home, ".cyberstack", "0-tainer.conf")); err != nil {
		t.Errorf("drop-in should still be written: %v", err)
	}
}

func TestEnsureClientSSHConfigAt_WritableConfigIsSilent(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".ssh", ".cyberstack"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(home, ".ssh", "config")
	if err := os.WriteFile(cfg, []byte("Host x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ensureClientSSHConfigAt(home, &out)
	if out.Len() != 0 {
		t.Errorf("unexpected output: %q", out.String())
	}
	data, _ := os.ReadFile(cfg)
	if !strings.HasPrefix(string(data), "Include ") {
		t.Errorf("include not written: %q", data)
	}
}
