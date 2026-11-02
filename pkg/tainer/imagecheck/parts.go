package imagecheck

import (
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

var typeLabels = map[manifest.ProjectType]string{
	manifest.TypeWordPress: "WordPress",
	manifest.TypePHP:       "PHP",
	manifest.TypeNodeJS:    "Node",
	manifest.TypeNextJS:    "Next.js",
	manifest.TypeNuxtJS:    "Nuxt",
	manifest.TypeNestJS:    "NestJS",
	manifest.TypeReact:     "React",
	manifest.TypeSvelte:    "Svelte",
	manifest.TypeSvelteKit: "SvelteKit",
	manifest.TypeKompozi:   "Kompozi",
}

// PartsLine describes what a pod is made of in user-facing words, e.g.
// "WordPress · PHP 8.3 · MariaDB 11 · Caddy".
func PartsLine(m *manifest.Manifest) string {
	label, ok := typeLabels[m.Project.Type]
	if !ok {
		label = string(m.Project.Type)
	}
	parts := []string{label}
	switch {
	case m.IsPHP() && m.Runtime.PHP != "":
		parts = append(parts, "PHP "+m.Runtime.PHP)
	case m.IsNode() && m.Runtime.Node != "":
		parts = append(parts, "Node "+m.Runtime.Node)
	}
	switch m.Runtime.Database {
	case manifest.DatabaseMariaDB:
		parts = append(parts, "MariaDB 11")
	case manifest.DatabasePostgres:
		parts = append(parts, "PostgreSQL 16")
	}
	parts = append(parts, "Caddy")
	return strings.Join(parts, " · ")
}
