// Package initcmd scaffolds a new tainer project: creates the
// project directory, writes tainer.yaml v2, adds .tainer.local.yaml
// to .gitignore, and prints a "next: tainer start" hint.
package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"gopkg.in/yaml.v3"
)

// Options for a scaffold run.
type Options struct {
	Type    manifest.ProjectType
	Name    string
	Dir     string // parent dir; project goes to <Dir>/<Name>
	PHP     string // optional, default per type
	Node    string // optional, default per type
	PodSize manifest.PodSize
}

// Run creates the project directory and writes tainer.yaml.
func Run(opts Options) error {
	if opts.Name == "" {
		return fmt.Errorf("init: project name required")
	}
	if opts.Type == "" {
		return fmt.Errorf("init: project type required")
	}
	if opts.PodSize == "" {
		opts.PodSize = manifest.PodSizeSmall
	}

	projectDir := filepath.Join(opts.Dir, opts.Name)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return err
	}
	for _, sub := range []string{"html", "data", "db"} {
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0755); err != nil {
			return err
		}
	}

	m := defaultManifest(opts)
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(projectDir, manifest.FileName), out, 0644); err != nil {
		return err
	}

	if err := ensureGitignore(projectDir); err != nil {
		return err
	}
	return nil
}

func defaultManifest(opts Options) *manifest.Manifest {
	domain := opts.Name + ".tainer.me"
	m := &manifest.Manifest{
		Version: 2,
		Project: manifest.ProjectConfig{Name: opts.Name, Type: opts.Type, Domain: domain},
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
