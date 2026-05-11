package pod

import (
	"fmt"
	"os"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// RolesForType returns the ordered set of roles that compose a pod of
// the given project type. Spec §"Role mapping per project type".
//
// This is the type-only view. For lifecycle decisions (start, update,
// resource split) prefer RolesForPod, which also drops the db role when
// the manifest declares no database.
func RolesForType(t manifest.ProjectType) []string {
	switch t {
	case manifest.TypeReact:
		return []string{RoleWeb, RoleDB}
	default:
		// All other types: 3-container web+app+db.
		return []string{RoleWeb, RoleApp, RoleDB}
	}
}

// RolesForPod is RolesForType filtered by the manifest's runtime
// config: pods declared with `database: none` skip the db role so we
// don't pull a database image we'll never use.
func RolesForPod(m *manifest.Manifest) []string {
	roles := RolesForType(m.Project.Type)
	if m.HasDatabase() {
		return roles
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if r == RoleDB {
			continue
		}
		out = append(out, r)
	}
	return out
}

// DefaultExecRole returns the role that `tainer exec <project>`
// targets when no role argument is given.
func DefaultExecRole(t manifest.ProjectType) string {
	if t == manifest.TypeReact {
		return RoleWeb
	}
	return RoleApp
}

// imageRepo returns the configured image registry prefix. Defaults to
// ghcr.io/cyber5-io but can be overridden via the TAINER_IMAGE_REPO env
// var for users who run their own registry or want to smoke against
// alternative builds. The trailing slash is normalised away.
func imageRepo() string {
	v := os.Getenv("TAINER_IMAGE_REPO")
	if v == "" {
		return "ghcr.io/cyber5-io"
	}
	return strings.TrimSuffix(v, "/")
}

// ImageRef returns the registry image reference for a (project, role).
// Tags are derived from the manifest's runtime block (PHP version,
// Node version) so a single tainer.yaml fully pins the image set.
//
// The registry prefix defaults to ghcr.io/cyber5-io and can be overridden
// by setting TAINER_IMAGE_REPO (e.g. localhost:5000/myorg).
func ImageRef(m *manifest.Manifest, role string) string {
	repo := imageRepo()
	switch role {
	case RoleWeb:
		// Project's edge caddy is bundled with the per-type web image.
		return fmt.Sprintf("%s/tainer-%s-web:%s", repo, m.Project.Type, m.Project.Type)
	case RoleApp:
		switch m.Project.Type {
		case manifest.TypeWordPress, manifest.TypePHP:
			return fmt.Sprintf("%s/tainer-%s:php-%s", repo, m.Project.Type, m.Runtime.PHP)
		default: // node-flavoured
			return fmt.Sprintf("%s/tainer-%s:node-%s", repo, m.Project.Type, m.Runtime.Node)
		}
	case RoleDB:
		if m.Runtime.Database == manifest.DatabasePostgres {
			return fmt.Sprintf("%s/tainer-postgres:16", repo)
		}
		return fmt.Sprintf("%s/tainer-mariadb:11", repo)
	}
	return ""
}
