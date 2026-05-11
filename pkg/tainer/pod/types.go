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
	// Legacy-image smoke mode: the 0.2.x tainer-* images are monolithic
	// (caddy + php-fpm or node baked into one), so there's no follower
	// app role to attach. Single leader + optional db.
	if legacyImages() {
		return []string{RoleWeb, RoleDB}
	}
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

// legacyImages reports whether TAINER_LEGACY_IMAGES is set, in which
// case ImageRef returns the 0.2.x single-image-per-type layout instead
// of the 0.9 role-split (-web/-app suffixes). Used for smoke testing
// against the already-published legacy images while the role-split
// images aren't built yet.
func legacyImages() bool {
	v := os.Getenv("TAINER_LEGACY_IMAGES")
	return v != "" && v != "0" && v != "false"
}

// legacyWebTag returns the tag for the legacy monolithic image of a
// given project type, derived from the manifest's runtime block.
func legacyWebTag(m *manifest.Manifest) string {
	switch m.Project.Type {
	case manifest.TypeWordPress, manifest.TypePHP:
		if m.Runtime.PHP != "" {
			return m.Runtime.PHP
		}
		return "latest"
	case manifest.TypeNodeJS:
		if m.Runtime.Node != "" {
			return m.Runtime.Node
		}
		return "latest"
	case manifest.TypeNextJS:
		return "15"
	case manifest.TypeNuxtJS:
		return "3"
	case manifest.TypeNestJS:
		return "11"
	case manifest.TypeReact:
		return "19"
	default: // kompozi and anything else
		return "latest"
	}
}

// ImageRef returns the registry image reference for a (project, role).
// Tags are derived from the manifest's runtime block (PHP version,
// Node version) so a single tainer.yaml fully pins the image set.
//
// The registry prefix defaults to ghcr.io/cyber5-io and can be overridden
// by setting TAINER_IMAGE_REPO (e.g. localhost:5000/myorg).
func ImageRef(m *manifest.Manifest, role string) string {
	repo := imageRepo()
	if legacyImages() {
		switch role {
		case RoleWeb:
			return fmt.Sprintf("%s/tainer-%s:%s", repo, m.Project.Type, legacyWebTag(m))
		case RoleDB:
			if m.Runtime.Database == manifest.DatabasePostgres {
				return fmt.Sprintf("%s/tainer-postgres:latest", repo)
			}
			return fmt.Sprintf("%s/tainer-mariadb:latest", repo)
		}
		return "" // RoleApp not used in legacy mode
	}
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
