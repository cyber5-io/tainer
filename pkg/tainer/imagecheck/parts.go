package imagecheck

import (
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
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
	runtime := ""
	switch {
	case m.IsPHP() && m.Runtime.PHP != "":
		runtime = "PHP " + m.Runtime.PHP
	case m.IsNode() && m.Runtime.Node != "":
		runtime = "Node " + m.Runtime.Node
	}
	if runtime != "" {
		if m.Project.Type == manifest.TypePHP || m.Project.Type == manifest.TypeNodeJS {
			parts[0] = runtime
		} else {
			parts = append(parts, runtime)
		}
	}
	var dbName string
	switch m.Runtime.Database {
	case manifest.DatabaseMariaDB:
		dbName = "MariaDB"
	case manifest.DatabasePostgres:
		dbName = "PostgreSQL"
	}
	if dbName != "" {
		if v := dbVersion(m); v != "" {
			dbName += " " + v
		}
		parts = append(parts, dbName)
	}
	parts = append(parts, "Caddy")
	return strings.Join(parts, " · ")
}

// dbVersion reads the database version from the db image tag so the
// line follows the tag the pod is actually built from.
func dbVersion(m *manifest.Manifest) string {
	ref := pod.ImageRef(m, pod.RoleDB)
	i := strings.LastIndex(ref, ":")
	if i < 0 || ref[i+1:] == "latest" {
		return ""
	}
	return ref[i+1:]
}
