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
	os.WriteFile(keyPath+".pub", []byte("fake-public-key"), 0644)

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

func TestAddSSHPiperEntryAuthorizedKeysFromTainerPub(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "tainer_rsa")
	if err := os.WriteFile(keyPath, []byte("fake-private"), 0600); err != nil {
		t.Fatal(err)
	}
	pub := "ssh-ed25519 AAAA_tainer_pub tainer@host\n"
	if err := os.WriteFile(keyPath+".pub", []byte(pub), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AddSSHPiperEntry(dir, "my-client", "10.77.1.2", keyPath); err != nil {
		t.Fatalf("AddSSHPiperEntry: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "my-client", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if string(got) != pub {
		t.Errorf("authorized_keys = %q, want %q", got, pub)
	}
}

func TestRemoveSSHPiperEntry(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test_key")
	os.WriteFile(keyPath, []byte("fake-private-key"), 0600)
	os.WriteFile(keyPath+".pub", []byte("fake-public-key"), 0644)

	AddSSHPiperEntry(dir, "my-client", "10.77.1.2", keyPath)
	RemoveSSHPiperEntry(dir, "my-client")
	projectDir := filepath.Join(dir, "my-client")
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Error("project directory should be removed")
	}
}
