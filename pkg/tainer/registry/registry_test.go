package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestRegistry(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "tainer"), 0755)
	registry = nil
	return dir
}

func TestAddAndGet(t *testing.T) {
	setupTestRegistry(t)
	err := Add("my-client", "/tmp/projects/my-client", "wordpress", "my-client.tainer.me")
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	p, ok := Get("my-client")
	if !ok {
		t.Fatal("Get() returned false for registered project")
	}
	if p.Path != "/tmp/projects/my-client" {
		t.Errorf("Path = %q, want %q", p.Path, "/tmp/projects/my-client")
	}
	if p.Type != "wordpress" {
		t.Errorf("Type = %q, want %q", p.Type, "wordpress")
	}
	if p.Domain != "my-client.tainer.me" {
		t.Errorf("Domain = %q, want %q", p.Domain, "my-client.tainer.me")
	}
}

func TestGetNotFound(t *testing.T) {
	setupTestRegistry(t)
	_, ok := Get("nonexistent")
	if ok {
		t.Error("Get() should return false for unregistered project")
	}
}

func TestAddDuplicateName(t *testing.T) {
	setupTestRegistry(t)
	Add("my-client", "/tmp/a", "wordpress", "a.tainer.me")
	err := Add("my-client", "/tmp/b", "wordpress", "b.tainer.me")
	if err == nil {
		t.Fatal("Add() should error on duplicate name from different path")
	}
}

func TestAddSamePathUpdates(t *testing.T) {
	setupTestRegistry(t)
	Add("my-client", "/tmp/a", "wordpress", "old.tainer.me")
	err := Add("my-client", "/tmp/a", "wordpress", "new.tainer.me")
	if err != nil {
		t.Fatalf("Add() same path should update, got error: %v", err)
	}
	p, _ := Get("my-client")
	if p.Domain != "new.tainer.me" {
		t.Errorf("Domain = %q, want %q after update", p.Domain, "new.tainer.me")
	}
}

func TestRemove(t *testing.T) {
	setupTestRegistry(t)
	Add("my-client", "/tmp/a", "wordpress", "a.tainer.me")
	Remove("my-client")
	_, ok := Get("my-client")
	if ok {
		t.Error("Get() should return false after Remove()")
	}
}

func TestAll(t *testing.T) {
	setupTestRegistry(t)
	Add("a", "/tmp/a", "wordpress", "a.tainer.me")
	Add("b", "/tmp/b", "nodejs", "b.tainer.me")
	all := All()
	if len(all) != 2 {
		t.Errorf("All() returned %d projects, want 2", len(all))
	}
}

func TestSelfHeal(t *testing.T) {
	setupTestRegistry(t)
	Add("stale", "/tmp/nonexistent-path-xyz", "wordpress", "stale.tainer.me")
	pruned := SelfHeal()
	if len(pruned) != 1 {
		t.Errorf("SelfHeal() pruned %d, want 1", len(pruned))
	}
	_, ok := Get("stale")
	if ok {
		t.Error("stale project should be pruned after SelfHeal()")
	}
}

func TestPersistence(t *testing.T) {
	setupTestRegistry(t)
	Add("persistent", "/tmp/p", "nodejs", "p.tainer.me")

	// Force reload from disk
	registry = nil
	p, ok := Get("persistent")
	if !ok {
		t.Fatal("project should survive reload from disk")
	}
	if p.Path != "/tmp/p" {
		t.Errorf("Path = %q after reload, want %q", p.Path, "/tmp/p")
	}
}
