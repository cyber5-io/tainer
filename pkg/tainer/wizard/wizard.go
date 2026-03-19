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
	defaultPHPVersions  = []string{"7.4", "8.1", "8.2", "8.3", "8.4", "8.5"}
	defaultNodeVersions = []string{"20", "22", "24"}
	projectTypes        = []struct {
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

func phpVersions() []string {
	tags, err := registry.FetchTags("phpfpm")
	if err != nil || len(tags) == 0 {
		if local := registry.LocalTags("phpfpm"); len(local) > 0 {
			fmt.Println("  (offline — showing locally cached versions)")
			return local
		}
		return defaultPHPVersions
	}
	return tags
}

func nodeVersions() []string {
	tags, err := registry.FetchTags("node")
	if err != nil || len(tags) == 0 {
		if local := registry.LocalTags("node"); len(local) > 0 {
			fmt.Println("  (offline — showing locally cached versions)")
			return local
		}
		return defaultNodeVersions
	}
	return tags
}

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
		fmt.Printf("  %d) %s\n", i+1, pt.Label)
	}
	fmt.Printf("Choose [1-%d]: ", len(projectTypes))
	typeIdx, err := promptInt(reader, "", 1, len(projectTypes))
	if err != nil {
		return err
	}
	selectedType := projectTypes[typeIdx-1].Type

	// Runtime version
	var version string
	if selectedType == manifest.TypeWordPress || selectedType == manifest.TypePHP {
		version, err = promptChoice(reader, "PHP version", phpVersions(), DefaultPHPVersion())
	} else {
		version, err = promptChoice(reader, "Node version", nodeVersions(), DefaultNodeVersion())
	}
	if err != nil {
		return err
	}

	// Database — choices depend on project type
	defaultDB := DefaultDatabase(selectedType)
	var dbChoices []string
	switch selectedType {
	case manifest.TypeWordPress:
		dbChoices = []string{"mariadb"}
	case manifest.TypePHP:
		dbChoices = []string{"mariadb", "postgres", "none"}
	case manifest.TypeNodeJS:
		dbChoices = []string{"postgres", "mariadb", "none"}
	default:
		// NextJS, NuxtJS, Kompozi — database is required
		dbChoices = []string{"postgres", "mariadb"}
	}
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
	fmt.Printf("Created %s/ and data/ directories\n", m.HostAppDir())

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
	// Create source directory (html/ for PHP, app/ for Node)
	appDir := filepath.Join(cwd, m.HostAppDir())
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating %s directory: %w", m.HostAppDir(), err)
	}

	// Create data/ (persistent work)
	dataDir := filepath.Join(cwd, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	// Create db/ at project root if database selected
	if m.HasDatabase() {
		if err := os.MkdirAll(filepath.Join(cwd, "db"), 0755); err != nil {
			return fmt.Errorf("creating db directory: %w", err)
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
	// Single option — auto-select, no question
	if len(choices) == 1 {
		fmt.Printf("\n%s: %s\n", label, choices[0])
		return choices[0], nil
	}

	// Find default index
	defaultIdx := 1
	for i, c := range choices {
		if c == defaultVal {
			defaultIdx = i + 1
			break
		}
	}

	fmt.Printf("\n%s:\n", label)
	for i, c := range choices {
		fmt.Printf("  %d) %s\n", i+1, c)
	}
	fmt.Printf("Choose [1-%d] (default: %d): ", len(choices), defaultIdx)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}
	var n int
	if _, err := fmt.Sscan(input, &n); err != nil || n < 1 || n > len(choices) {
		return "", fmt.Errorf("please enter a number between 1 and %d", len(choices))
	}
	return choices[n-1], nil
}
