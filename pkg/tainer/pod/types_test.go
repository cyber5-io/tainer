package pod

import (
	"reflect"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestRolesForType(t *testing.T) {
	cases := []struct {
		typ  manifest.ProjectType
		want []string
	}{
		{manifest.TypeWordPress, []string{"web", "app", "db"}},
		{manifest.TypePHP, []string{"web", "app", "db"}},
		{manifest.TypeNodeJS, []string{"web", "app", "db"}},
		{manifest.TypeKompozi, []string{"web", "app", "db"}},
		{manifest.TypeReact, []string{"web", "db"}},
	}
	for _, c := range cases {
		got := RolesForType(c.typ)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("RolesForType(%s): got %v, want %v", c.typ, got, c.want)
		}
	}
}

func TestRolesForPod(t *testing.T) {
	cases := []struct {
		name string
		typ  manifest.ProjectType
		db   manifest.DatabaseType
		want []string
	}{
		{"wp+mariadb", manifest.TypeWordPress, manifest.DatabaseMariaDB, []string{"web", "app", "db"}},
		{"wp+none", manifest.TypeWordPress, manifest.DatabaseNone, []string{"web", "app"}},
		{"node+postgres", manifest.TypeNodeJS, manifest.DatabasePostgres, []string{"web", "app", "db"}},
		{"node+none", manifest.TypeNodeJS, manifest.DatabaseNone, []string{"web", "app"}},
		{"react+mariadb", manifest.TypeReact, manifest.DatabaseMariaDB, []string{"web", "db"}},
		{"react+none", manifest.TypeReact, manifest.DatabaseNone, []string{"web"}},
	}
	for _, c := range cases {
		m := &manifest.Manifest{
			Project: manifest.ProjectConfig{Type: c.typ},
			Runtime: manifest.RuntimeConfig{Database: c.db},
		}
		got := RolesForPod(m)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDefaultExecRole(t *testing.T) {
	if got := DefaultExecRole(manifest.TypeWordPress); got != "app" {
		t.Errorf("WordPress: got %q, want app", got)
	}
	if got := DefaultExecRole(manifest.TypeNodeJS); got != "app" {
		t.Errorf("NodeJS: got %q, want app", got)
	}
	if got := DefaultExecRole(manifest.TypeReact); got != "web" {
		t.Errorf("React: got %q, want web", got)
	}
}

func TestImageRef(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress},
		Runtime: manifest.RuntimeConfig{PHP: "8.3"},
	}
	if got := ImageRef(m, "app"); got != "ghcr.io/cyber5-io/tainer-wordpress:php-8.3" {
		t.Errorf("WordPress app image: got %q", got)
	}
	if got := ImageRef(m, "db"); got != "ghcr.io/cyber5-io/tainer-mariadb:11" {
		t.Errorf("WordPress db image: got %q", got)
	}

	mNode := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeNextJS},
		Runtime: manifest.RuntimeConfig{Node: "20"},
	}
	if got := ImageRef(mNode, "app"); got != "ghcr.io/cyber5-io/tainer-nextjs:node-20" {
		t.Errorf("NextJS app image: got %q", got)
	}

	mKompozi := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeKompozi},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabasePostgres, Node: "20"},
	}
	if got := ImageRef(mKompozi, "db"); got != "ghcr.io/cyber5-io/tainer-postgres:16" {
		t.Errorf("Kompozi db image: got %q", got)
	}

	if got := ImageRef(m, "web"); got != "ghcr.io/cyber5-io/tainer-wordpress-web:wordpress" {
		t.Errorf("WordPress web image: got %q", got)
	}
	if got := ImageRef(mNode, "web"); got != "ghcr.io/cyber5-io/tainer-nextjs-web:nextjs" {
		t.Errorf("NextJS web image: got %q", got)
	}
}

// TestImageRefEnvOverride verifies that TAINER_IMAGE_REPO replaces the
// default ghcr.io/cyber5-io prefix for all roles and project types.
func TestImageRefEnvOverride(t *testing.T) {
	t.Setenv("TAINER_IMAGE_REPO", "localhost:5000/myorg")

	mWP := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress},
		Runtime: manifest.RuntimeConfig{PHP: "8.3", Database: manifest.DatabaseMariaDB},
	}
	cases := []struct {
		role string
		want string
	}{
		{"web", "localhost:5000/myorg/tainer-wordpress-web:wordpress"},
		{"app", "localhost:5000/myorg/tainer-wordpress:php-8.3"},
		{"db", "localhost:5000/myorg/tainer-mariadb:11"},
	}
	for _, c := range cases {
		if got := ImageRef(mWP, c.role); got != c.want {
			t.Errorf("role %s with override: got %q, want %q", c.role, got, c.want)
		}
	}

	// Trailing slash in TAINER_IMAGE_REPO must be normalised.
	t.Setenv("TAINER_IMAGE_REPO", "localhost:5000/myorg/")
	if got := ImageRef(mWP, "web"); got != "localhost:5000/myorg/tainer-wordpress-web:wordpress" {
		t.Errorf("trailing-slash override: got %q", got)
	}

	mKompozi := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeKompozi},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabasePostgres, Node: "20"},
	}
	t.Setenv("TAINER_IMAGE_REPO", "registry.example.com/ci")
	if got := ImageRef(mKompozi, "db"); got != "registry.example.com/ci/tainer-postgres:16" {
		t.Errorf("Kompozi postgres override: got %q", got)
	}
}
