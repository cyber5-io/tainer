package wizard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containers/podman/v6/pkg/tainer/env"
	"github.com/containers/podman/v6/pkg/tainer/gitsetup"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	tuiwizard "github.com/containers/podman/v6/pkg/tainer/tui/wizard"
)

func DefaultDatabase(pt manifest.ProjectType) manifest.DatabaseType {
	switch pt {
	case manifest.TypeWordPress, manifest.TypePHP:
		return manifest.DatabaseMariaDB
	default:
		return manifest.DatabasePostgres
	}
}

func DefaultPHPVersion() string  { return "8.4" }
func DefaultNodeVersion() string { return "22" }

func BuildManifest(name string, pt manifest.ProjectType, version string, db manifest.DatabaseType, subdomain string) *manifest.Manifest {
	autoOpen := false
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{
			Name:     name,
			Type:     pt,
			Domain:   subdomain + ".tainer.me",
			AutoOpen: &autoOpen,
		},
		Runtime: manifest.RuntimeConfig{
			Database: db,
		},
	}
	if pt == manifest.TypeWordPress || pt == manifest.TypePHP {
		m.Runtime.PHP = version
		m.Runtime.Limits = manifest.DefaultPHPLimits
	} else {
		m.Runtime.Node = version
	}
	m.Runtime.Shell = "zsh"
	return m
}

// Run executes the interactive wizard in the given directory.
func Run(cwd string) error {
	dirName := filepath.Base(cwd)

	// Launch TUI wizard
	result, err := tuiwizard.Run(cwd, dirName)
	if err != nil {
		return err
	}
	if result.Cancelled {
		return fmt.Errorf("wizard cancelled")
	}

	// Build and save manifest
	m := BuildManifest(result.Name, result.Type, result.Version, result.Database, result.Subdomain)
	manifestPath := filepath.Join(cwd, manifest.FileName)
	if err := manifest.Save(m, manifestPath); err != nil {
		return err
	}
	fmt.Printf("\nCreated %s\n", manifest.FileName)

	// Create project directories
	if err := createProjectDirs(cwd, m); err != nil {
		return err
	}
	fmt.Printf("Created %s/ and data/ directories\n", m.HostAppDir())

	// Generate .env
	envPath := filepath.Join(cwd, ".env")
	if err := env.Generate(m, envPath); err != nil {
		return err
	}
	fmt.Println("Created .env")

	// Register project
	if err := registry.Add(result.Name, cwd, string(result.Type), m.Project.Domain); err != nil {
		return err
	}
	fmt.Println("Project registered")

	// Git setup — honour the TUI wizard's decision
	if result.HasGitRepo {
		if err := gitsetup.EnsureRootIgnore(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
		} else {
			fmt.Println("Updated .gitignore")
		}
	} else if result.InitGit {
		if err := gitsetup.InitRepo(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git init failed: %v\n", err)
		} else {
			fmt.Println("Git repository initialised with .gitignore")
		}
	}

	if result.StartPod {
		fmt.Println("\nStarting pod...")
		return startPod()
	}

	fmt.Println("\nRun 'tainer start' to launch.")
	return nil
}

func startPod() error {
	tainerBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding tainer binary: %w", err)
	}
	cmd := exec.Command(tainerBin, "start")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createProjectDirs(cwd string, m *manifest.Manifest) error {
	// Create source directory (html/ for PHP, app/ for Node)
	appDir := filepath.Join(cwd, m.HostAppDir())
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating %s directory: %w", m.HostAppDir(), err)
	}

	// Create data/ (persistent work) with .gitignore
	dataDir := filepath.Join(cwd, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	if err := gitsetup.WriteDirIgnore(dataDir); err != nil {
		return fmt.Errorf("writing data/.gitignore: %w", err)
	}

	// Create db/ at project root if database selected, with .gitignore
	if m.HasDatabase() {
		dbDir := filepath.Join(cwd, "db")
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("creating db directory: %w", err)
		}
		if err := gitsetup.WriteDirIgnore(dbDir); err != nil {
			return fmt.Errorf("writing db/.gitignore: %w", err)
		}
	}

	// Create wp-content subdirs for WordPress (used by post-deploy symlinks)
	if m.Project.Type == manifest.TypeWordPress {
		for _, sub := range []string{"wp-content/uploads", "wp-content/plugins", "wp-content/themes"} {
			if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
				return fmt.Errorf("creating data/%s directory: %w", sub, err)
			}
		}
	}

	return nil
}

