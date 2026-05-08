package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestRunWordPress(t *testing.T) {
	dir := t.TempDir()
	err := Run(Options{
		Type: manifest.TypeWordPress,
		Name: "mywp",
		Dir:  dir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	pdir := filepath.Join(dir, "mywp")
	if _, err := os.Stat(filepath.Join(pdir, "tainer.yaml")); err != nil {
		t.Errorf("tainer.yaml not created: %v", err)
	}
	gi, _ := os.ReadFile(filepath.Join(pdir, ".gitignore"))
	if !strings.Contains(string(gi), ".tainer.local.yaml") {
		t.Errorf(".gitignore missing local overlay: %q", gi)
	}
	m, err := manifest.Load(filepath.Join(pdir, "tainer.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Pod.Size != manifest.PodSizeSmall {
		t.Errorf("default pod size: got %q, want small", m.Pod.Size)
	}
	if m.Runtime.PHP != "8.3" {
		t.Errorf("default PHP: got %q, want 8.3", m.Runtime.PHP)
	}
}

func TestRunKompoziDefaultsToPostgres(t *testing.T) {
	dir := t.TempDir()
	if err := Run(Options{Type: manifest.TypeKompozi, Name: "site", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "site", "tainer.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Runtime.Database != manifest.DatabasePostgres {
		t.Errorf("Kompozi db: got %q, want postgres", m.Runtime.Database)
	}
}

func TestRunCreatesHtmlDataDbDirs(t *testing.T) {
	dir := t.TempDir()
	if err := Run(Options{Type: manifest.TypeWordPress, Name: "site", Dir: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, sub := range []string{"html", "data", "db"} {
		fi, err := os.Stat(filepath.Join(dir, "site", sub))
		if err != nil {
			t.Errorf("expected %s/ to exist: %v", sub, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%s exists but isn't a directory", sub)
		}
	}
}
