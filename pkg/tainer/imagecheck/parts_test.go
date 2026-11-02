package imagecheck

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestPartsLine(t *testing.T) {
	cases := []struct {
		name string
		typ  manifest.ProjectType
		php  string
		node string
		db   manifest.DatabaseType
		want string
	}{
		{"wordpress", manifest.TypeWordPress, "8.3", "", manifest.DatabaseMariaDB, "WordPress · PHP 8.3 · MariaDB 11 · Caddy"},
		{"php", manifest.TypePHP, "8.2", "", manifest.DatabaseNone, "PHP 8.2 · Caddy"},
		{"php postgres", manifest.TypePHP, "8.4", "", manifest.DatabasePostgres, "PHP 8.4 · PostgreSQL 16 · Caddy"},
		{"nodejs", manifest.TypeNodeJS, "", "22", manifest.DatabaseNone, "Node 22 · Caddy"},
		{"nextjs", manifest.TypeNextJS, "", "22", manifest.DatabasePostgres, "Next.js · Node 22 · PostgreSQL 16 · Caddy"},
		{"nuxtjs", manifest.TypeNuxtJS, "", "22", manifest.DatabaseNone, "Nuxt · Node 22 · Caddy"},
		{"nestjs", manifest.TypeNestJS, "", "22", manifest.DatabaseMariaDB, "NestJS · Node 22 · MariaDB 11 · Caddy"},
		{"react", manifest.TypeReact, "", "22", manifest.DatabaseNone, "React · Node 22 · Caddy"},
		{"svelte", manifest.TypeSvelte, "", "22", manifest.DatabaseNone, "Svelte · Node 22 · Caddy"},
		{"sveltekit", manifest.TypeSvelteKit, "", "22", manifest.DatabaseNone, "SvelteKit · Node 22 · Caddy"},
		{"kompozi", manifest.TypeKompozi, "", "22", manifest.DatabasePostgres, "Kompozi · Node 22 · PostgreSQL 16 · Caddy"},
		{"php no version", manifest.TypePHP, "", "", manifest.DatabaseNone, "PHP · Caddy"},
		{"no version", manifest.TypeReact, "", "", manifest.DatabaseNone, "React · Caddy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &manifest.Manifest{}
			m.Project.Type = c.typ
			m.Runtime.PHP = c.php
			m.Runtime.Node = c.node
			m.Runtime.Database = c.db
			if got := PartsLine(m); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
