package manifest

// The pod settings tainer offers, by project type: the single source for
// `tainer config options`, `config set` validation, the init wizard and the
// pod budget.

// PHPVersions and NodeVersions are the runtime versions tainer's images are
// built for (the version tags of the app images in tainer-images'
// build-images workflow). tain-110's image labels are to verify this table
// against what is published.
var (
	PHPVersions  = []string{"7.4", "8.1", "8.2", "8.3", "8.4", "8.5"}
	NodeVersions = []string{"20", "22", "24"}
)

// DefaultPodSize is the size a new project gets.
const DefaultPodSize = PodSizeSmall

// PodBudget is one named size's pod total: memory as a size string ("2G")
// and CPU cores.
type PodBudget struct {
	Memory string
	CPU    float64
}

// podBudgets is the smart-pods table: a size names ONE budget shared by
// every container in the pod.
var podBudgets = map[PodSize]PodBudget{
	PodSizeNano:   {"512M", 1},
	PodSizeSmall:  {"1G", 1},
	PodSizeMedium: {"2G", 2},
	PodSizeLarge:  {"4G", 4},
	PodSizeXLarge: {"8G", 6},
	PodSizeXXL:    {"16G", 8},
}

// BudgetFor returns a named size's budget.
func BudgetFor(s PodSize) (PodBudget, bool) {
	b, ok := podBudgets[s]
	return b, ok
}

// DatabasesFor lists the databases a project type offers, its default first.
func DatabasesFor(t ProjectType) []DatabaseType {
	switch t {
	case TypeWordPress:
		return []DatabaseType{DatabaseMariaDB}
	case TypePHP:
		return []DatabaseType{DatabaseMariaDB, DatabasePostgres, DatabaseNone}
	case TypeKompozi:
		return []DatabaseType{DatabasePostgres, DatabaseMariaDB}
	default:
		return []DatabaseType{DatabaseNone, DatabasePostgres, DatabaseMariaDB}
	}
}

// RuntimeVersionsFor returns the settable runtime key ("runtime.php" or
// "runtime.node") and the versions offered for a project type.
func RuntimeVersionsFor(t ProjectType) (key string, versions []string) {
	spec, ok := SpecFor(t)
	if !ok {
		return "", nil
	}
	if spec.Family == FamilyPHP {
		return "runtime.php", PHPVersions
	}
	return "runtime.node", NodeVersions
}

// DefaultBuildDir is what caddy serves for SPA types when runtime.build-dir
// is unset.
const DefaultBuildDir = "dist"

// Options is what can be set for one project and what is set now.
type Options struct {
	Project  string          `json:"project"`
	Type     ProjectType     `json:"type"`
	Size     SizeOptions     `json:"size"`
	Database DatabaseOptions `json:"database"`
	Runtime  RuntimeOptions  `json:"runtime"`
	BuildDir BuildDirOptions `json:"buildDir"`
}

// SizeOptions lists the named sizes, smallest first.
type SizeOptions struct {
	Key     string       `json:"key"`
	Current PodSize      `json:"current"`
	Default PodSize      `json:"default"`
	Values  []SizeOption `json:"values"`
}

type SizeOption struct {
	Name   PodSize `json:"name"`
	Memory string  `json:"memory"`
	CPU    float64 `json:"cpu"`
}

type DatabaseOptions struct {
	Key     string         `json:"key"`
	Current DatabaseType   `json:"current"`
	Default DatabaseType   `json:"default"`
	Values  []DatabaseType `json:"values"`
}

// RuntimeOptions is the php or node version, by type.
type RuntimeOptions struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Current string   `json:"current"`
	Default string   `json:"default"`
	Values  []string `json:"values"`
}

// BuildDirOptions: only react and svelte serve a built bundle.
type BuildDirOptions struct {
	Key     string `json:"key"`
	Applies bool   `json:"applies"`
	Current string `json:"current"`
	Default string `json:"default"`
}

// OptionsFor describes what can be set for m and what is set now.
func OptionsFor(m *Manifest) Options {
	spec, _ := SpecFor(m.Project.Type)
	o := Options{Project: m.Project.Name, Type: m.Project.Type}

	o.Size = SizeOptions{Key: "pod.size", Default: DefaultPodSize, Values: []SizeOption{}}
	if m.Pod != nil {
		o.Size.Current = m.Pod.Size
	}
	for _, s := range NamedPodSizes() {
		b := podBudgets[s]
		o.Size.Values = append(o.Size.Values, SizeOption{Name: s, Memory: b.Memory, CPU: b.CPU})
	}

	o.Database = DatabaseOptions{Key: "database", Current: m.Runtime.Database,
		Default: spec.DefaultRuntime.Database, Values: DatabasesFor(m.Project.Type)}

	key, versions := RuntimeVersionsFor(m.Project.Type)
	o.Runtime = RuntimeOptions{Key: key, Values: versions}
	if key == "runtime.php" {
		o.Runtime.Name, o.Runtime.Current, o.Runtime.Default = "php", m.Runtime.PHP, spec.DefaultRuntime.PHP
	} else {
		o.Runtime.Name, o.Runtime.Current, o.Runtime.Default = "node", m.Runtime.Node, spec.DefaultRuntime.Node
	}

	o.BuildDir = BuildDirOptions{Key: "runtime.build-dir", Applies: m.IsSPA()}
	if o.BuildDir.Applies {
		o.BuildDir.Current, o.BuildDir.Default = m.BuildDirOrDefault(), DefaultBuildDir
	}
	return o
}
