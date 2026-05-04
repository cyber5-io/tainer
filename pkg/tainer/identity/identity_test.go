package identity

import (
	"os"
	"testing"
)

func TestDetectFromDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}
	dir := t.TempDir()
	uid, gid, err := DetectFromDir(dir)
	if err != nil {
		t.Fatalf("DetectFromDir: %v", err)
	}
	// Should return current user's uid/gid (owner of temp dir)
	if uid == 0 && gid == 0 {
		t.Error("expected non-zero uid/gid on macOS/Linux desktop")
	}
}

func TestDetectFromDir_NotExists(t *testing.T) {
	_, _, err := DetectFromDir("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestDetectReturnsLocalByDefault(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}
	dir := t.TempDir()
	uid, gid, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// Without /etc/blenzi.yaml, should fall back to local detection
	if uid == 0 && gid == 0 {
		t.Error("expected non-zero uid/gid")
	}
}

func TestEnvFlags(t *testing.T) {
	flags := EnvFlags(501, 20)
	expected := []string{"--env", "TAINER_UID=501", "--env", "TAINER_GID=20"}
	if len(flags) != len(expected) {
		t.Fatalf("expected %d flags, got %d", len(expected), len(flags))
	}
	for i, f := range flags {
		if f != expected[i] {
			t.Errorf("flag %d: expected %q, got %q", i, expected[i], f)
		}
	}
}
