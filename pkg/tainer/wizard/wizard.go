package wizard

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/env"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/validate"
)

var (
	phpVersions  = []string{"8.1", "8.2", "8.3", "8.4", "8.5"}
	nodeVersions = []string{"20", "22", "24"}
	projectTypes = []struct {
		Type  manifest.ProjectType
		Label string
	}{
		{manifest.TypeWordPress, "WordPress"},
		{manifest.TypePHP, "PHP"},
		{manifest.TypeNodeJS, "Node.js"},
		{manifest.TypeNextJS, "Next.js"},
		{manifest.TypeNuxtJS, "Nuxt.js"},
		{manifest.TypeKompozi, "Kompozi"},
	}
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
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{
			Name:   name,
			Type:   pt,
			Domain: subdomain + ".tainer.me",
		},
		Runtime: manifest.RuntimeConfig{
			Database: db,
		},
	}
	if pt == manifest.TypeWordPress || pt == manifest.TypePHP {
		m.Runtime.PHP = version
	} else {
		m.Runtime.Node = version
	}
	return m
}

// Run executes the interactive wizard in the given directory.
func Run(cwd string) error {
	reader := bufio.NewReader(os.Stdin)
	dirName := filepath.Base(cwd)

	// Project name (re-prompt on invalid input)
	var name string
	var err error
	for {
		name, err = promptString(reader, "Project name", dirName)
		if err != nil {
			return err
		}
		if err := validate.ProjectName(name); err != nil {
			fmt.Fprintf(os.Stderr, "  Invalid: %v\n", err)
			continue
		}
		if existing, ok := registry.Get(name); ok && existing.Path != cwd {
			fmt.Fprintf(os.Stderr, "  project name %q is already registered at %s\n", name, existing.Path)
			continue
		}
		break
	}

	// Project type
	fmt.Println("\nProject type:")
	for i, pt := range projectTypes {
		fmt.Printf("  %d. %s\n", i+1, pt.Label)
	}
	typeIdx, err := promptInt(reader, "> ", 1, len(projectTypes))
	if err != nil {
		return err
	}
	selectedType := projectTypes[typeIdx-1].Type

	// Runtime version
	var version string
	if selectedType == manifest.TypeWordPress || selectedType == manifest.TypePHP {
		version, err = promptChoice(reader, "PHP version", phpVersions, DefaultPHPVersion())
	} else {
		version, err = promptChoice(reader, "Node version", nodeVersions, DefaultNodeVersion())
	}
	if err != nil {
		return err
	}

	// Database
	defaultDB := DefaultDatabase(selectedType)
	dbChoices := []string{"mariadb", "postgres", "none"}
	dbStr, err := promptChoice(reader, "Database", dbChoices, string(defaultDB))
	if err != nil {
		return err
	}

	// Subdomain
	subdomain, err := promptString(reader, fmt.Sprintf("Subdomain: [___].tainer.me"), name)
	if err != nil {
		return err
	}

	// Build and save manifest
	m := BuildManifest(name, selectedType, version, manifest.DatabaseType(dbStr), subdomain)
	manifestPath := filepath.Join(cwd, manifest.FileName)
	if err := manifest.Save(m, manifestPath); err != nil {
		return err
	}
	fmt.Printf("\nCreated %s\n", manifest.FileName)

	// Create project directories
	if err := createProjectDirs(cwd, m); err != nil {
		return err
	}
	fmt.Println("Created app/ and data/ directories")

	// Generate .env
	envPath := filepath.Join(cwd, ".env")
	if err := env.Generate(m, envPath); err != nil {
		return err
	}
	fmt.Println("Created .env")

	// Register project
	if err := registry.Add(name, cwd, string(selectedType), m.Project.Domain); err != nil {
		return err
	}
	fmt.Println("Project registered")
	fmt.Println("\nRun 'tainer start' to launch.")

	return nil
}

func createProjectDirs(cwd string, m *manifest.Manifest) error {
	// Create app/ (disposable runtime)
	appDir := filepath.Join(cwd, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating app directory: %w", err)
	}

	// Create data/ (persistent work)
	dataDir := filepath.Join(cwd, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	// Create data/db/ if database selected
	if m.HasDatabase() {
		if err := os.MkdirAll(filepath.Join(dataDir, "db"), 0755); err != nil {
			return fmt.Errorf("creating data/db directory: %w", err)
		}
	}

	// Create directories for default data mounts
	for _, mount := range m.DefaultDataMounts() {
		if err := os.MkdirAll(filepath.Join(dataDir, mount), 0755); err != nil {
			return fmt.Errorf("creating data/%s directory: %w", mount, err)
		}
	}

	// Create wp-config.php placeholder for WordPress (needed for single-file bind mount)
	if m.Project.Type == manifest.TypeWordPress {
		wpConfigPath := filepath.Join(dataDir, "wp-config.php")
		if _, err := os.Stat(wpConfigPath); os.IsNotExist(err) {
			if err := os.WriteFile(wpConfigPath, []byte(""), 0644); err != nil {
				return fmt.Errorf("creating wp-config.php placeholder: %w", err)
			}
		}
	}

	return nil
}

func promptString(reader *bufio.Reader, label, defaultVal string) (string, error) {
	fmt.Printf("\n%s: (default: %s)\n> ", label, defaultVal)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}
	return input, nil
}

func promptInt(reader *bufio.Reader, prompt string, min, max int) (int, error) {
	fmt.Print(prompt)
	input, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscan(strings.TrimSpace(input), &n); err != nil {
		return 0, fmt.Errorf("please enter a number between %d and %d", min, max)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("choice must be between %d and %d", min, max)
	}
	return n, nil
}

func promptChoice(reader *bufio.Reader, label string, choices []string, defaultVal string) (string, error) {
	var parts []string
	for _, c := range choices {
		if c == defaultVal {
			parts = append(parts, fmt.Sprintf("[%s]", c))
		} else {
			parts = append(parts, c)
		}
	}
	fmt.Printf("\n%s: (default: %s)\n  %s\n> ", label, defaultVal, strings.Join(parts, " / "))
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}
	for _, c := range choices {
		if input == c {
			return input, nil
		}
	}
	return "", fmt.Errorf("invalid choice: %q", input)
}
