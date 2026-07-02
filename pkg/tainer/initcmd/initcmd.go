// Package initcmd scaffolds a new tainer project into the current working
// directory: writes tainer.yaml v2, creates the standard directory tree,
// generates a .env, and adds .tainer.local.yaml to .gitignore.
//
// Legacy tainer 0.2.x semantics: init operates on cwd, never on a
// <cwd>/<name> subdirectory.
package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/env"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/validate"
	"gopkg.in/yaml.v3"
)

// Options for a scaffold run.
//
// Name is optional: if empty, RunIn uses filepath.Base(dir) as the project
// name (matching legacy 0.2.x "tainer init <type>" with cwd basename).
// The Dir field has been removed — use RunIn to supply the target directory,
// or Run which reads os.Getwd().
type Options struct {
	Type    manifest.ProjectType
	Name    string // optional; defaults to filepath.Base(cwd) when empty
	PHP     string // optional, default per type
	Node    string // optional, default per type
	PodSize manifest.PodSize
}

// RunResult captures what an init produced. Each Step describes one
// artefact that was touched (created or adopted) so the CLI layer can
// surface them through the brand vocabulary ([✓] vs [i] marks) without
// having to re-discover the work after the fact.
type RunResult struct {
	ProjectName  string
	ManifestPath string
	Steps        []Step
}

// Step is one observable unit of work init did. JSON tags are camelCase
// to match the rest of the tainer CLI's --json output contract.
type Step struct {
	Action StepAction `json:"action"`
	Kind   StepKind   `json:"kind"`
	Path   string     `json:"path"` // display-friendly path, relative to project dir when possible
}

// StepAction describes what happened to the artefact.
//
//   - StepCreated: the artefact didn't exist before, init made it.
//   - StepAdopted: the artefact already existed (almost always from a
//     previous tainer install or a prior --clean) and init left it as-is.
//   - StepSkipped: init decided this step wasn't needed (e.g. .env
//     already populated). Distinct from StepAdopted because the artefact
//     was actively considered, not just left alone.
type StepAction string

const (
	StepCreated StepAction = "created"
	StepAdopted StepAction = "adopted"
	StepSkipped StepAction = "skipped"
)

// StepKind buckets the kind of artefact so the CLI can render with the
// right vocabulary ("dir" → "html/", "registry" → "registered as wp3").
type StepKind string

const (
	KindFile      StepKind = "file"
	KindDir       StepKind = "dir"
	KindRegistry  StepKind = "registry"
	KindGitignore StepKind = "gitignore"
)

// Run scaffolds a project into the current working directory.
// It delegates to RunIn(opts, os.Getwd()).
func Run(opts Options) (*RunResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("init: could not determine working directory: %w", err)
	}
	return RunIn(opts, cwd)
}

// RunIn scaffolds a project into dir. This is the testable entry point.
//
// Semantics (mirrors legacy tainer 0.2.x):
//   - Does NOT create a project subdirectory inside dir; files land in dir.
//   - Errors if tainer.yaml already exists in dir.
//   - Validates the project name against validate.ProjectName.
//   - Creates html/, data/, and (if HasDatabase) db/ under dir.
//   - For WordPress: also creates data/wp-content/{uploads,plugins,themes}.
//   - Writes tainer.yaml into dir.
//   - Writes .env into dir (skipped if the file already exists).
//   - Registers the project in the tainer registry.
//   - Appends .tainer.local.yaml to .gitignore.
//
// Returns a RunResult listing every artefact it touched, marked as
// created (didn't exist before) or adopted (left alone because it was
// already there). The CLI uses this to render brand-styled lines per
// step without re-discovering the filesystem after the fact.
func RunIn(opts Options, dir string) (*RunResult, error) {
	if opts.Type == "" {
		return nil, fmt.Errorf("init: project type required")
	}
	if opts.PodSize == "" {
		opts.PodSize = manifest.PodSizeSmall
	}

	// Default name to cwd basename when not given.
	name := opts.Name
	if name == "" {
		name = filepath.Base(dir)
	}

	// Guard: don't overwrite an existing project.
	manifestPath := filepath.Join(dir, manifest.FileName)
	if _, err := os.Stat(manifestPath); err == nil {
		return nil, fmt.Errorf("tainer.yaml already exists in %s", dir)
	}

	// Validate project name.
	if err := validate.ProjectName(name); err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}

	res := &RunResult{ProjectName: name, ManifestPath: manifestPath}

	// Build and write the manifest. Since we guarded against existence
	// above, this is always "created".
	m := defaultManifest(opts, name)
	out, err := yaml.Marshal(m)
	if err != nil {
		return res, err
	}
	if err := os.WriteFile(manifestPath, out, 0644); err != nil {
		return res, err
	}
	res.Steps = append(res.Steps, Step{Action: StepCreated, Kind: KindFile, Path: manifest.FileName})

	// Create html/ (or whatever HostAppDir() returns).
	if err := ensureDir(dir, m.HostAppDir(), res); err != nil {
		return res, err
	}

	// Create data/.
	if err := ensureDir(dir, "data", res); err != nil {
		return res, err
	}

	// Create db/ only for projects that use a database.
	if m.HasDatabase() {
		if err := ensureDir(dir, "db", res); err != nil {
			return res, err
		}
	}

	// WordPress-specific: wp-content subdirs (per legacy 0.2.x lines 151-155).
	if m.Project.Type == manifest.TypeWordPress {
		for _, sub := range []string{"wp-content/uploads", "wp-content/plugins", "wp-content/themes"} {
			if err := ensureDir(dir, filepath.Join("data", sub), res); err != nil {
				return res, err
			}
		}
	}

	// Generate .env. env.Generate is idempotent (skips when file already
	// has tainer-managed keys), but we want to report adopted vs created
	// to the user, so stat first.
	envPath := filepath.Join(dir, ".env")
	envExisted := pathExists(envPath)
	if err := env.Generate(m, envPath); err != nil {
		return res, fmt.Errorf("init: generating .env: %w", err)
	}
	envAction := StepCreated
	if envExisted {
		envAction = StepAdopted
	}
	res.Steps = append(res.Steps, Step{Action: envAction, Kind: KindFile, Path: ".env"})

	// Register the project in the tainer registry.
	if err := registry.Add(name, dir, string(m.Project.Type), m.Project.Domain); err != nil {
		// Non-fatal: registry errors shouldn't block project creation.
		// TODO(0.9.x parity): surface as a warning, not a hard error, to
		// match legacy behaviour where registry.Add failure was logged.
		fmt.Fprintf(os.Stderr, "warning: could not register project: %v\n", err)
	} else {
		res.Steps = append(res.Steps, Step{Action: StepCreated, Kind: KindRegistry, Path: name})
	}

	// .gitignore: ensureGitignore was already idempotent; report based on
	// whether the marker was added on this run.
	added, err := ensureGitignoreReport(dir)
	if err != nil {
		return res, err
	}
	gitAction := StepAdopted
	if added {
		gitAction = StepCreated
	}
	res.Steps = append(res.Steps, Step{Action: gitAction, Kind: KindGitignore, Path: ".gitignore"})

	return res, nil
}

// ensureDir creates `dir/path` if missing and records the step on res.
// Stat first so we can distinguish a fresh mkdir from adopting an
// existing directory.
func ensureDir(base, path string, res *RunResult) error {
	full := filepath.Join(base, path)
	action := StepCreated
	if pathExists(full) {
		action = StepAdopted
	}
	if err := os.MkdirAll(full, 0755); err != nil {
		return err
	}
	// Display path uses forward slashes + trailing slash for clarity.
	res.Steps = append(res.Steps, Step{Action: action, Kind: KindDir, Path: filepath.ToSlash(path) + "/"})
	return nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func defaultManifest(opts Options, name string) *manifest.Manifest {
	domain := name + ".tainer.me"
	m := &manifest.Manifest{
		Version: 2,
		Project: manifest.ProjectConfig{Name: name, Type: opts.Type, Domain: domain},
		Pod:     &manifest.PodConfig{Size: opts.PodSize},
	}
	// Runtime block: the manifest.TypeSpec table supplies the per-type
	// defaults (PHP/Node version, database engine); explicit --php /
	// --node flags override the version.
	if spec, ok := manifest.SpecFor(opts.Type); ok {
		m.Runtime = spec.DefaultRuntime
	} else {
		m.Runtime = manifest.RuntimeConfig{Node: "20", Database: manifest.DatabaseMariaDB}
	}
	// Only the family's own version field is overridable — a stray
	// --node on a wordpress init shouldn't write a node version into
	// the manifest.
	if opts.PHP != "" && m.Runtime.PHP != "" {
		m.Runtime.PHP = opts.PHP
	}
	if opts.Node != "" && m.Runtime.Node != "" {
		m.Runtime.Node = opts.Node
	}
	return m
}

func defaultStr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// ensureGitignoreReport is the same as ensureGitignore but reports
// whether it appended anything. Returns (added=true) when the marker
// for manifest.LocalOverlayName was newly added; (added=false) when it
// was already present.
func ensureGitignoreReport(dir string) (added bool, err error) {
	path := filepath.Join(dir, ".gitignore")
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), manifest.LocalOverlayName) {
		return false, nil
	}
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	add := prefix + "# tainer per-machine overlay\n" + manifest.LocalOverlayName + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(add); err != nil {
		return false, err
	}
	return true, nil
}
