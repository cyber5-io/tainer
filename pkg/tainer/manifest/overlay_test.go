package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesLocalOverlay(t *testing.T) {
	dir := t.TempDir()
	base := []byte(`
version: 2
project: { name: mywp, type: wordpress, domain: mywp.tainer.me }
runtime: { php: "8.3", database: mariadb }
pod: { size: small }
`)
	overlay := []byte(`
pod: { size: large }
runtime: { php: "8.4" }
`)
	if err := os.WriteFile(filepath.Join(dir, FileName), base, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LocalOverlayName), overlay, 0644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Pod.Size != PodSizeLarge {
		t.Errorf("Pod.Size after overlay: got %q, want large", m.Pod.Size)
	}
	if m.Runtime.PHP != "8.4" {
		t.Errorf("Runtime.PHP after overlay: got %q, want 8.4", m.Runtime.PHP)
	}
	if m.Project.Name != "mywp" {
		t.Errorf("Project.Name preserved: got %q, want mywp", m.Project.Name)
	}
}

func TestLoadNoOverlay(t *testing.T) {
	dir := t.TempDir()
	base := []byte(`
version: 2
project: { name: mywp, type: wordpress, domain: mywp.tainer.me }
runtime: { php: "8.3", database: mariadb }
pod: { size: small }
`)
	if err := os.WriteFile(filepath.Join(dir, FileName), base, 0644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Pod.Size != PodSizeSmall {
		t.Errorf("Pod.Size without overlay: got %q, want small", m.Pod.Size)
	}
}
