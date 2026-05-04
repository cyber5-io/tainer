package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

func TestMountAdd_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeNodeJS, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
	}
	manifest.Save(m, filepath.Join(dir, "tainer.yaml"))

	err := MountAdd(dir, "media")
	if err != nil {
		t.Fatalf("MountAdd: %v", err)
	}

	// Check directory was created at project root
	if _, err := os.Stat(filepath.Join(dir, "media")); err != nil {
		t.Error("media/ not created")
	}

	// Check manifest was updated
	updated, _ := manifest.LoadFromDir(dir)
	found := false
	for _, name := range updated.Mounts {
		if name == "media" {
			found = true
		}
	}
	if !found {
		t.Error("media not added to manifest")
	}
}

func TestMountAdd_RejectsSlash(t *testing.T) {
	dir := t.TempDir()
	err := MountAdd(dir, "some/path")
	if err == nil {
		t.Error("expected error for path with slash")
	}
}

func TestMountAdd_RejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	err := MountAdd(dir, "..")
	if err == nil {
		t.Error("expected error for .. path")
	}
}

func TestMountAdd_RejectsReserved(t *testing.T) {
	for _, name := range []string{"html", "data", "db"} {
		dir := t.TempDir()
		err := MountAdd(dir, name)
		if err == nil {
			t.Errorf("expected error for reserved name %q", name)
		}
	}
}

func TestMountDel_RemovesFromManifest(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeNodeJS, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
		Mounts:  []string{"media", "storage"},
	}
	manifest.Save(m, filepath.Join(dir, "tainer.yaml"))

	err := MountDel(dir, "media")
	if err != nil {
		t.Fatalf("MountDel: %v", err)
	}

	updated, _ := manifest.LoadFromDir(dir)
	for _, name := range updated.Mounts {
		if name == "media" {
			t.Error("media still in manifest")
		}
	}
}

func TestMountDel_NotFound(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeWordPress, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.4", Database: manifest.DatabaseMariaDB},
	}
	manifest.Save(m, filepath.Join(dir, "tainer.yaml"))

	err := MountDel(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent mount")
	}
}
