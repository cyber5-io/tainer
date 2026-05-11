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

// Run scaffolds a project into the current working directory.
// It delegates to RunIn(opts, os.Getwd()).
func Run(opts Options) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("init: could not determine working directory: %w", err)
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
func RunIn(opts Options, dir string) error {
	if opts.Type == "" {
		return fmt.Errorf("init: project type required")
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
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return fmt.Errorf("tainer.yaml already exists in %s", dir)
	}

	// Validate project name.
	if err := validate.ProjectName(name); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	// Build and write the manifest.
	m := defaultManifest(opts, name)
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), out, 0644); err != nil {
		return err
	}

	// Create html/ (or whatever HostAppDir() returns).
	if err := os.MkdirAll(filepath.Join(dir, m.HostAppDir()), 0755); err != nil {
		return err
	}

	// Create data/.
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	// Create db/ only for projects that use a database.
	if m.HasDatabase() {
		if err := os.MkdirAll(filepath.Join(dir, "db"), 0755); err != nil {
			return err
		}
	}

	// WordPress-specific: wp-content subdirs (per legacy 0.2.x lines 151-155).
	if m.Project.Type == manifest.TypeWordPress {
		for _, sub := range []string{"wp-content/uploads", "wp-content/plugins", "wp-content/themes"} {
			if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
				return err
			}
		}
	}

	// Generate .env (skipped if already present — env.Generate guards this).
	envPath := filepath.Join(dir, ".env")
	if err := env.Generate(m, envPath); err != nil {
		return fmt.Errorf("init: generating .env: %w", err)
	}

	// Register the project in the tainer registry.
	if err := registry.Add(name, dir, string(m.Project.Type), m.Project.Domain); err != nil {
		// Non-fatal: registry errors shouldn't block project creation.
		// TODO(0.9.x parity): surface as a warning, not a hard error, to
		// match legacy behaviour where registry.Add failure was logged.
		fmt.Fprintf(os.Stderr, "warning: could not register project: %v\n", err)
	}

	if err := ensureGitignore(dir); err != nil {
		return err
	}
	return nil
}

func defaultManifest(opts Options, name string) *manifest.Manifest {
	domain := name + ".tainer.me"
	m := &manifest.Manifest{
		Version: 2,
		Project: manifest.ProjectConfig{Name: name, Type: opts.Type, Domain: domain},
		Pod:     &manifest.PodConfig{Size: opts.PodSize},
	}
	switch opts.Type {
	case manifest.TypeWordPress, manifest.TypePHP:
		m.Runtime = manifest.RuntimeConfig{
			PHP:      defaultStr(opts.PHP, "8.3"),
			Database: manifest.DatabaseMariaDB,
		}
	case manifest.TypeKompozi:
		m.Runtime = manifest.RuntimeConfig{
			Node:     defaultStr(opts.Node, "20"),
			Database: manifest.DatabasePostgres,
		}
	default: // node-flavoured
		m.Runtime = manifest.RuntimeConfig{
			Node:     defaultStr(opts.Node, "20"),
			Database: manifest.DatabaseMariaDB,
		}
	}
	return m
}

func defaultStr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func ensureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), manifest.LocalOverlayName) {
		return nil
	}
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	add := prefix + "# tainer per-machine overlay\n" + manifest.LocalOverlayName + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(add)
	return err
}
