package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

func TestDataAdd_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeNodeJS, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
	}
	manifestPath := filepath.Join(dir, "tainer.yaml")
	manifest.Save(m, manifestPath)
	os.MkdirAll(filepath.Join(dir, "data"), 0755)

	err := DataAdd(dir, "media/uploads")
	if err != nil {
		t.Fatalf("DataAdd: %v", err)
	}

	// Check directory was created
	if _, err := os.Stat(filepath.Join(dir, "data", "media", "uploads")); err != nil {
		t.Error("data/media/uploads not created")
	}

	// Check manifest was updated
	updated, _ := manifest.LoadFromDir(dir)
	found := false
	for _, m := range updated.Data.Mounts {
		if m == "media/uploads" {
			found = true
		}
	}
	if !found {
		t.Error("media/uploads not added to manifest")
	}
}

func TestDataAdd_RejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	err := DataAdd(dir, "/etc/passwd")
	if err == nil {
		t.Error("expected error for absolute path")
	}
}

func TestDataAdd_RejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	err := DataAdd(dir, "../escape")
	if err == nil {
		t.Error("expected error for .. path")
	}
}

func TestDataDel_RemovesFromManifest(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeNodeJS, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
		Data:    manifest.DataConfig{Mounts: []string{"media/uploads", "storage"}},
	}
	manifestPath := filepath.Join(dir, "tainer.yaml")
	manifest.Save(m, manifestPath)

	err := DataDel(dir, "media/uploads")
	if err != nil {
		t.Fatalf("DataDel: %v", err)
	}

	updated, _ := manifest.LoadFromDir(dir)
	for _, m := range updated.Data.Mounts {
		if m == "media/uploads" {
			t.Error("media/uploads still in manifest")
		}
	}
}

func TestDataDel_RejectsDefaultMount(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypeWordPress, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.4", Database: manifest.DatabaseMariaDB},
	}
	manifest.Save(m, filepath.Join(dir, "tainer.yaml"))

	err := DataDel(dir, "wp-content/uploads")
	if err == nil {
		t.Error("expected error for deleting default mount")
	}
}
