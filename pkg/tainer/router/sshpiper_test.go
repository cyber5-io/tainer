package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSSHPiperEntry(t *testing.T) {
	dir := t.TempDir()
	err := AddSSHPiperEntry(dir, "my-client", "10.77.1.2", "/keys/tainer_rsa")
	if err != nil {
		t.Fatalf("AddSSHPiperEntry() error: %v", err)
	}
	configPath := filepath.Join(dir, "my-client", "sshpiper.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal("sshpiper.yaml not created")
	}
	data, _ := os.ReadFile(configPath)
	content := string(data)
	if !strings.Contains(content, "10.77.1.2") {
		t.Error("config should contain project IP")
	}
	if !strings.Contains(content, "my-client") {
		t.Error("config should contain project name as username")
	}
}

func TestRemoveSSHPiperEntry(t *testing.T) {
	dir := t.TempDir()
	AddSSHPiperEntry(dir, "my-client", "10.77.1.2", "/keys/tainer_rsa")
	RemoveSSHPiperEntry(dir, "my-client")
	projectDir := filepath.Join(dir, "my-client")
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Error("project directory should be removed")
	}
}
