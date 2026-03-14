package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureKeyPair(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "tainer_rsa")
	pubPath := filepath.Join(dir, "tainer_rsa.pub")

	err := EnsureKeyPair(privPath, pubPath)
	if err != nil {
		t.Fatalf("EnsureKeyPair() error: %v", err)
	}

	// Check files exist
	if _, err := os.Stat(privPath); err != nil {
		t.Error("private key not created")
	}
	if _, err := os.Stat(pubPath); err != nil {
		t.Error("public key not created")
	}

	// Check private key permissions
	info, _ := os.Stat(privPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("private key permissions = %o, want 0600", info.Mode().Perm())
	}

	// Check public key format
	pubData, _ := os.ReadFile(pubPath)
	if !strings.HasPrefix(string(pubData), "ssh-rsa ") && !strings.HasPrefix(string(pubData), "ssh-ed25519 ") {
		t.Error("public key should start with ssh-rsa or ssh-ed25519")
	}
}

func TestEnsureKeyPair_Idempotent(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "tainer_rsa")
	pubPath := filepath.Join(dir, "tainer_rsa.pub")

	EnsureKeyPair(privPath, pubPath)
	data1, _ := os.ReadFile(privPath)

	EnsureKeyPair(privPath, pubPath)
	data2, _ := os.ReadFile(privPath)

	if string(data1) != string(data2) {
		t.Error("EnsureKeyPair() should not regenerate existing keys")
	}
}
