package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestBuildMountsUsesHTMLNotApp(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
	}
	mounts := buildMounts(m, "/p/demo", RoleApp)
	wantHTML := false
	wantData := false
	for _, mt := range mounts {
		if mt.Source == "/p/demo/html" && mt.Target == "/var/www/html" {
			wantHTML = true
		}
		if mt.Source == "/p/demo/data" && mt.Target == "/var/www/data" {
			wantData = true
		}
		if mt.Source == "/p/demo/app" {
			t.Errorf("legacy /app source should not appear: %+v", mt)
		}
	}
	if !wantHTML {
		t.Errorf("html bind missing: %+v", mounts)
	}
	if !wantData {
		t.Errorf("data bind missing: %+v", mounts)
	}
}

func TestBuildMountsDBIsBindNotVolume(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabaseMariaDB},
	}
	mounts := buildMounts(m, "/p/demo", RoleDB)
	if len(mounts) != 1 {
		t.Fatalf("db mounts: got %d, want 1", len(mounts))
	}
	mt := mounts[0]
	if mt.Source != "/p/demo/db" || mt.Target != "/var/lib/mysql" {
		t.Errorf("db mount: got %+v, want bind /p/demo/db -> /var/lib/mysql", mt)
	}
	if mt.Volume {
		t.Errorf("db mount must be a bind, not a named volume: %+v", mt)
	}
}

func TestBuildMountsDBPostgres(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeKompozi, Name: "blog"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabasePostgres},
	}
	mounts := buildMounts(m, "/p/blog", RoleDB)
	if len(mounts) != 1 || mounts[0].Target != "/var/lib/postgresql/data" {
		t.Errorf("postgres db mount: got %+v, want target /var/lib/postgresql/data", mounts)
	}
}
