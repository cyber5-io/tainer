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
	// Smoke-images mode (nginx + mariadb stock images) and legacy-images
	// mode (0.2.x tainer-* monolithic images) both run a single web
	// leader + optional db follower — no separate app role.
	if smokeImages() || legacyImages() {
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

// DefaultExecUser returns the crun --user value tainer exec should
// pass by default so commands like `tainer exec -- wp option get
// siteurl` don't run as root. UID:GID form because crun silently
// ignores the flag when only UID is given (the agent side also
// duplicates the UID as a safety net).
//
// PHP images (WordPress and plain PHP) ship with www-data at 82.
// Node images (Next/Nuxt/Nest/plain node/kompozi) use "node" at 1000.
// Only used for the app role — other roles (web, db) keep their
// image-baked defaults because that's what runs the actual daemon
// process.
func DefaultExecUser(t manifest.ProjectType, role string) string {
	if role != RoleApp {
		return "" // fall through to image default (usually root)
	}
	switch t {
	case manifest.TypeWordPress, manifest.TypePHP:
		return "82:82" // www-data
	case manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS,
		manifest.TypeNestJS, manifest.TypeKompozi:
		return "1000:1000" // node
	}
	return ""
}

// DefaultExecWorkdir returns the working directory tainer exec should
// cd into by default so `wp`, `artisan`, `npm`, etc. see the project
// files they expect without the user needing to pass --workdir every
// time.
//
// All app roles land in /var/www/html for PHP and /app for Node —
// matching what the container images set as their WORKDIR. The web
// and db roles have no useful "project" directory to cd into, so we
// return empty and let crun use the image default.
func DefaultExecWorkdir(t manifest.ProjectType, role string) string {
	if role != RoleApp {
		return ""
	}
	switch t {
	case manifest.TypeWordPress, manifest.TypePHP:
		return "/var/www/html"
	case manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS,
		manifest.TypeNestJS, manifest.TypeKompozi:
		return "/app"
	}
	return ""
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

// smokeImages reports whether TAINER_SMOKE_IMAGES is set, in which
// case ImageRef returns stock public images (nginx:alpine for web,
// mariadb:11 for db) that run with minimal env wiring. Used ONLY
// to validate orchestration end-to-end (init/start/stop, shared
// netns, port forwarding) without dragging in image-content quirks
// like the legacy tainer-entrypoint.sh's MYSQL_HOST requirements.
// Not a production mode — the real tainer 0.9 images will use caddy
// and our own build pipeline.
func smokeImages() bool {
	v := os.Getenv("TAINER_SMOKE_IMAGES")
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
	if smokeImages() {
		// Smoke goal is to validate orchestration plumbing
		// (init/start/stop, leader+follower shared netns, port
		// bindings on the leader). Real DB engines try to chown
		// /var/lib/mysql or /var/lib/postgresql on virtio-fs binds,
		// which fails ("Operation not permitted") on our minimal
		// mount setup. Use busybox + sleep infinity for both roles
		// so the smoke can focus on orchestration, not image
		// entrypoint quirks.
		return "docker.io/library/busybox:latest"
	}
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
	// 0.9 image family — generic engine images tagged tainer-<engine>-<role>.
	// One caddy image serves all project types (per-type Caddyfiles baked
	// in, picked at runtime via TAINER_PROJECT_TYPE env). App + db images
	// stay per-engine. See tainer-images/docs/0.9-images.md.
	switch role {
	case RoleWeb:
		return fmt.Sprintf("%s/tainer-caddy-web:2-alpine", repo)
	case RoleApp:
		switch m.Project.Type {
		case manifest.TypeWordPress, manifest.TypePHP:
			return fmt.Sprintf("%s/tainer-%s-app:php-%s", repo, m.Project.Type, m.Runtime.PHP)
		default: // node-flavoured
			return fmt.Sprintf("%s/tainer-%s-app:node-%s", repo, m.Project.Type, m.Runtime.Node)
		}
	case RoleDB:
		if m.Runtime.Database == manifest.DatabasePostgres {
			return fmt.Sprintf("%s/tainer-postgres-db:16", repo)
		}
		return fmt.Sprintf("%s/tainer-mariadb-db:11", repo)
	}
	return ""
}
