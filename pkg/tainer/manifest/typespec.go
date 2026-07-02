package manifest

import "strings"

// This file is the single source of truth for everything that varies
// per project type. Adding a project type = adding one TypeSpec entry
// here (plus its images in tainer-images). Nothing else in the tree
// should switch on ProjectType for *defaults* — behavioural quirks
// (WordPress wp-content dirs, Kompozi payload secrets) stay at their
// call sites, but any "which user / which version / which tools /
// which roles" question is answered by this table.

// Family groups project types by runtime stack. It drives the app
// image flavour (php-fpm vs node), which runtime version field
// applies (Runtime.PHP vs Runtime.Node), and the exec identity.
type Family string

const (
	FamilyPHP  Family = "php"
	FamilyNode Family = "node"
)

// TypeSpec describes one supported project type.
type TypeSpec struct {
	Type   ProjectType
	Family Family

	// Aliases are the shorthand spellings `tainer init` accepts in
	// addition to the canonical name (e.g. "wp" → wordpress).
	Aliases []string

	// DefaultRuntime seeds a new manifest's runtime block and fills
	// blanks when updating base images. Versions here must exist as
	// tags in tainer-images.
	DefaultRuntime RuntimeConfig

	// HasAppRole is false for types served purely as static/SPA
	// bundles (react): those pods run web+db only, and exec targets
	// the web role.
	HasAppRole bool

	// ExecUser (UID:GID) and ExecWorkdir are the defaults `tainer
	// exec` applies for the app role so tools run as the site owner
	// inside the project directory. MUST match the app image's user
	// database and WORKDIR in tainer-images — change them together.
	ExecUser    string
	ExecWorkdir string

	// Tools are the CLI wrappers (`tainer wp`, `tainer npm`, ...)
	// available for this type — i.e. binaries the app image ships.
	Tools []string
}

// nodeTools is shared by every node-flavoured type.
var nodeTools = []string{"npm", "yarn", "pnpm", "node"}

// typeSpecs is ordered for display (usage text, wizard lists).
var typeSpecs = []TypeSpec{
	{
		Type:           TypeWordPress,
		Family:         FamilyPHP,
		Aliases:        []string{"wp"},
		DefaultRuntime: RuntimeConfig{PHP: "8.3", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "82:82", // www-data in the alpine php images
		ExecWorkdir:    "/var/www/html",
		Tools:          []string{"wp", "composer", "php"},
	},
	{
		Type:           TypePHP,
		Family:         FamilyPHP,
		DefaultRuntime: RuntimeConfig{PHP: "8.3", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "82:82",
		ExecWorkdir:    "/var/www/html",
		Tools:          []string{"artisan", "composer", "php"},
	},
	{
		Type:           TypeNodeJS,
		Family:         FamilyNode,
		Aliases:        []string{"node"},
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "1000:1000", // node in the official-node-derived images
		ExecWorkdir:    "/app",
		Tools:          nodeTools,
	},
	{
		Type:           TypeNextJS,
		Family:         FamilyNode,
		Aliases:        []string{"next"},
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "1000:1000",
		ExecWorkdir:    "/app",
		Tools:          nodeTools,
	},
	{
		Type:           TypeNuxtJS,
		Family:         FamilyNode,
		Aliases:        []string{"nuxt"},
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "1000:1000",
		ExecWorkdir:    "/app",
		Tools:          nodeTools,
	},
	{
		Type:           TypeNestJS,
		Family:         FamilyNode,
		Aliases:        []string{"nest"},
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
		HasAppRole:     true,
		ExecUser:       "1000:1000",
		ExecWorkdir:    "/app",
		Tools:          nodeTools,
	},
	{
		Type:           TypeReact,
		Family:         FamilyNode,
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
		HasAppRole:     false, // SPA: web (serves the bundle) + db only
		Tools:          nodeTools,
	},
	{
		Type:           TypeKompozi,
		Family:         FamilyNode,
		DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabasePostgres},
		HasAppRole:     true,
		ExecUser:       "1000:1000",
		ExecWorkdir:    "/app",
		Tools:          nodeTools,
	},
}

// SpecFor returns the TypeSpec for a canonical project type.
func SpecFor(t ProjectType) (TypeSpec, bool) {
	for _, s := range typeSpecs {
		if s.Type == t {
			return s, true
		}
	}
	return TypeSpec{}, false
}

// AllSpecs returns every supported type in display order. Callers
// must not mutate the returned slice.
func AllSpecs() []TypeSpec {
	return typeSpecs
}

// CanonicalType resolves a user-typed name — canonical or alias,
// case-insensitive — to its ProjectType.
func CanonicalType(s string) (ProjectType, bool) {
	s = strings.ToLower(s)
	for _, spec := range typeSpecs {
		if string(spec.Type) == s {
			return spec.Type, true
		}
		for _, a := range spec.Aliases {
			if a == s {
				return spec.Type, true
			}
		}
	}
	return "", false
}

// ToolTypes returns the project types whose app image ships the given
// CLI tool, or nil when no type does (i.e. the tool isn't a tainer
// wrapper). Drives both the `tainer <tool>` dispatch and its
// per-project type guard.
func ToolTypes(tool string) []ProjectType {
	var out []ProjectType
	for _, spec := range typeSpecs {
		for _, t := range spec.Tools {
			if t == tool {
				out = append(out, spec.Type)
				break
			}
		}
	}
	return out
}
