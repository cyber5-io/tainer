package wizard

import (
	"testing"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

func TestDefaultDatabase(t *testing.T) {
	tests := []struct {
		projType manifest.ProjectType
		want     manifest.DatabaseType
	}{
		{manifest.TypeWordPress, manifest.DatabaseMariaDB},
		{manifest.TypePHP, manifest.DatabaseMariaDB},
		{manifest.TypeNodeJS, manifest.DatabasePostgres},
		{manifest.TypeKompozi, manifest.DatabasePostgres},
	}
	for _, tt := range tests {
		t.Run(string(tt.projType), func(t *testing.T) {
			got := DefaultDatabase(tt.projType)
			if got != tt.want {
				t.Errorf("DefaultDatabase(%s) = %s, want %s", tt.projType, got, tt.want)
			}
		})
	}
}

func TestDefaultRuntime(t *testing.T) {
	if got := DefaultPHPVersion(); got != "8.4" {
		t.Errorf("DefaultPHPVersion() = %q, want %q", got, "8.4")
	}
	if got := DefaultNodeVersion(); got != "22" {
		t.Errorf("DefaultNodeVersion() = %q, want %q", got, "22")
	}
}

func TestBuildManifest(t *testing.T) {
	m := BuildManifest("my-site", manifest.TypeWordPress, "8.4", manifest.DatabaseMariaDB, "my-site")
	if m.Version != 1 {
		t.Errorf("Version = %d, want 1", m.Version)
	}
	if m.Project.Domain != "my-site.tainer.me" {
		t.Errorf("Domain = %q, want %q", m.Project.Domain, "my-site.tainer.me")
	}
	if m.Runtime.PHP != "8.4" {
		t.Errorf("PHP = %q, want %q", m.Runtime.PHP, "8.4")
	}
}
