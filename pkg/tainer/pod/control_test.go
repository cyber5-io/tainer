package pod

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func controlManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.ParseBytes(ControlManifestYAML())
	if err != nil {
		t.Fatalf("parse built-in manifest: %v", err)
	}
	return m
}

func TestControlManifestIsBuiltIn(t *testing.T) {
	m := controlManifest(t)
	if m.Project.Name != "control" || m.Project.Type != manifest.TypeNodeJS || m.Project.Domain != "my.tainer.me" {
		t.Errorf("project: %+v", m.Project)
	}
	if m.Runtime.Node != "22" || m.Runtime.Database != manifest.DatabaseNone {
		t.Errorf("runtime: %+v", m.Runtime)
	}
	if m.Pod == nil || m.Pod.Size != "small" {
		t.Errorf("pod: %+v", m.Pod)
	}
}

func TestControlVersionIsAFloor(t *testing.T) {
	cases := map[string]string{"": "1.0.0", "0.9.0": "1.0.0", "1.2.0": "1.2.0", "garbage": "1.0.0"}
	for in, want := range cases {
		if got := controlVersion(in); got != want {
			t.Errorf("controlVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPinnedControlImageRef(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := controlManifest(t)
	if got, want := ImageRef(m, RoleApp), "ghcr.io/cyber5-io/tainer-control:1.0.0"; got != want {
		t.Errorf("pinned app ref = %q, want %q", got, want)
	}
	if got, want := ControlImageRef("1.2.0"), "ghcr.io/cyber5-io/tainer-control:1.2.0"; got != want {
		t.Errorf("ControlImageRef = %q, want %q", got, want)
	}
	other := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "blog", Type: manifest.TypeNodeJS},
		Runtime: manifest.RuntimeConfig{Node: "22"},
	}
	if got, want := ImageRef(other, RoleApp), "ghcr.io/cyber5-io/tainer-nodejs-app:node-22"; got != want {
		t.Errorf("non-pinned app ref = %q, want %q", got, want)
	}
}

func TestPinnedControlMountsOnlyData(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := controlManifest(t)
	dir := config.ControlDir()
	wantSrc := filepath.Join(dir, "data")
	for _, role := range []string{RoleApp, RoleWeb} {
		n := 0
		for _, mt := range buildMounts(m, dir, role) {
			if strings.HasSuffix(mt.Source, "/html") || mt.Source == dir {
				t.Errorf("%s: forbidden mount %+v", role, mt)
			}
			if mt.Source == wantSrc && mt.Target == "/var/www/data" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: want exactly one data mount, got %d", role, n)
		}
	}
}

// The built-in definition is written in the current (v2) format, like new
// projects, so a start never rewrites it and the ensure never flips it back.
func TestControlManifestIsV2(t *testing.T) {
	raw := ControlManifestYAML()
	if !strings.HasPrefix(string(raw), "version: 2\n") {
		t.Fatalf("built-in definition must start with version: 2, got %q", raw)
	}
	path := filepath.Join(t.TempDir(), manifest.FileName)
	writeFile(t, path, string(raw))
	migrated, err := manifest.PersistV2Migration(path)
	if err != nil || migrated {
		t.Fatalf("PersistV2Migration = %v, %v; want a no-op on the built-in definition", migrated, err)
	}
	if got := readFile(t, path); got != string(raw) {
		t.Errorf("definition rewritten:\n%s", got)
	}
}
