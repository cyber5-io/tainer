package pod

import (
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// RolesForType returns the ordered set of roles that compose a pod of
// the given project type. Spec §"Role mapping per project type".
func RolesForType(t manifest.ProjectType) []string {
	switch t {
	case manifest.TypeReact:
		return []string{RoleWeb, RoleDB}
	default:
		// All other types: 3-container web+app+db.
		return []string{RoleWeb, RoleApp, RoleDB}
	}
}

// DefaultExecRole returns the role that `tainer exec <project>`
// targets when no role argument is given.
func DefaultExecRole(t manifest.ProjectType) string {
	if t == manifest.TypeReact {
		return RoleWeb
	}
	return RoleApp
}

// ImageRef returns the registry image reference for a (project, role).
// Tags are derived from the manifest's runtime block (PHP version,
// Node version) so a single tainer.yaml fully pins the image set.
func ImageRef(m *manifest.Manifest, role string) string {
	switch role {
	case RoleWeb:
		// Project's edge caddy is bundled with the per-type web image.
		return fmt.Sprintf("ghcr.io/cyber5-io/tainer-%s-web:%s", m.Project.Type, m.Project.Type)
	case RoleApp:
		switch m.Project.Type {
		case manifest.TypeWordPress, manifest.TypePHP:
			return fmt.Sprintf("ghcr.io/cyber5-io/tainer-%s:php-%s", m.Project.Type, m.Runtime.PHP)
		default: // node-flavoured
			return fmt.Sprintf("ghcr.io/cyber5-io/tainer-%s:node-%s", m.Project.Type, m.Runtime.Node)
		}
	case RoleDB:
		if m.Runtime.Database == manifest.DatabasePostgres {
			return "ghcr.io/cyber5-io/tainer-postgres:16"
		}
		return "ghcr.io/cyber5-io/tainer-mariadb:11"
	}
	return ""
}
