package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestBuildMountsAppMountsWholeProject(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
	}
	// The app container is the pod's dev shell (WordPress has an app
	// role), so it mounts the whole project at /var/www — giving an ssh
	// session the git repo, not just the docroot.
	mounts := buildMounts(m, "/p/demo", RoleApp)
	wantProject := false
	for _, mt := range mounts {
		if mt.Source == "/p/demo" && mt.Target == "/var/www" {
			wantProject = true
		}
		if mt.Source == "/p/demo/app" {
			t.Errorf("legacy /app source should not appear: %+v", mt)
		}
	}
	if !wantProject {
		t.Errorf("whole-project bind missing: %+v", mounts)
	}
}

func TestBuildMountsWebEdgeIsIsolated(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
	}
	// The web (caddy) container is the internet-facing edge — kept narrow
	// to the docroot + data, and must never see the repo.
	mounts := buildMounts(m, "/p/demo", RoleWeb)
	wantHTML, wantData, sawRepo := false, false, false
	for _, mt := range mounts {
		switch {
		case mt.Source == "/p/demo/html" && mt.Target == "/var/www/html":
			wantHTML = true
		case mt.Source == "/p/demo/data" && mt.Target == "/var/www/data":
			wantData = true
		case mt.Source == "/p/demo" && mt.Target == "/var/www":
			sawRepo = true
		}
	}
	if !wantHTML {
		t.Errorf("html bind missing: %+v", mounts)
	}
	if !wantData {
		t.Errorf("data bind missing: %+v", mounts)
	}
	if sawRepo {
		t.Errorf("web edge must not mount the whole project: %+v", mounts)
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
