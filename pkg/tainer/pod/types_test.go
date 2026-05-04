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

func TestDefaultExecRole(t *testing.T) {
	if got := DefaultExecRole(manifest.TypeWordPress); got != "app" {
		t.Errorf("WordPress: got %q, want app", got)
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
}
