package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSSHPiperEntry(t *testing.T) {
	dir := t.TempDir()

	// Create a fake private key file
	keyPath := filepath.Join(dir, "test_key")
	os.WriteFile(keyPath, []byte("fake-private-key"), 0600)

	err := AddSSHPiperEntry(dir, "my-client", "10.77.1.2", keyPath)
	if err != nil {
		t.Fatalf("AddSSHPiperEntry() error: %v", err)
	}

	// Check sshpiper_upstream file
	upstreamPath := filepath.Join(dir, "my-client", "sshpiper_upstream")
	if _, err := os.Stat(upstreamPath); err != nil {
		t.Fatal("sshpiper_upstream not created")
	}
	data, _ := os.ReadFile(upstreamPath)
	content := string(data)
	if !strings.Contains(content, "tainer@10.77.1.2:22") {
		t.Errorf("sshpiper_upstream should contain upstream address, got: %s", content)
	}

	// Check id_rsa was copied
	idRsaPath := filepath.Join(dir, "my-client", "id_rsa")
	if _, err := os.Stat(idRsaPath); err != nil {
		t.Fatal("id_rsa not created")
	}
}

func TestRemoveSSHPiperEntry(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test_key")
	os.WriteFile(keyPath, []byte("fake-private-key"), 0600)

	AddSSHPiperEntry(dir, "my-client", "10.77.1.2", keyPath)
	RemoveSSHPiperEntry(dir, "my-client")
	projectDir := filepath.Join(dir, "my-client")
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Error("project directory should be removed")
	}
}
