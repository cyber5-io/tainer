package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/env"
	"github.com/containers/podman/v6/pkg/tainer/gitsetup"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/pkg/tainer/validate"
	"github.com/containers/podman/v6/pkg/tainer/wizard"
	"github.com/spf13/cobra"
)

// InitOptions holds the flags for non-interactive tainer init.
type InitOptions struct {
	Name      string
	Type      string
	PHP       string
	Node      string
	DB        string
	Subdomain string
	Quiet     bool
	Start     bool
	GitInit   bool
}

// RunInit is the RunE handler for `tainer init`.
func RunInit(cmd *cobra.Command, _ []string) error {
	opts := initOptsFromFlags(cmd)

	cwd, err := GetWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	if manifest.Exists(cwd) {
		return tui.StyledError("tainer.yaml already exists in " + cwd)
	}

	// No flags at all → interactive wizard
	if !cmd.Flags().HasFlags() || cmd.Flags().NFlag() == 0 {
		if RunWizard == nil {
			return fmt.Errorf("wizard not available")
		}
		return RunWizard(cwd)
	}

	// Non-interactive: validate required flags
	if opts.Name == "" {
		return tui.StyledError("--name is required for non-interactive init")
	}
	if opts.Type == "" {
		return tui.StyledError("--type is required for non-interactive init")
	}

	return runNonInteractiveInit(opts, cwd)
}

func initOptsFromFlags(cmd *cobra.Command) InitOptions {
	var opts InitOptions
	if f := cmd.Flag("name"); f != nil {
		opts.Name = f.Value.String()
	}
	if f := cmd.Flag("type"); f != nil {
		opts.Type = f.Value.String()
	}
	if f := cmd.Flag("php"); f != nil {
		opts.PHP = f.Value.String()
	}
	if f := cmd.Flag("node"); f != nil {
		opts.Node = f.Value.String()
	}
	if f := cmd.Flag("db"); f != nil {
		opts.DB = f.Value.String()
	}
	if f := cmd.Flag("subdomain"); f != nil {
		opts.Subdomain = f.Value.String()
	}
	if f := cmd.Flag("quiet"); f != nil {
		opts.Quiet = f.Value.String() == "true"
	}
	if f := cmd.Flag("start"); f != nil {
		opts.Start = f.Value.String() == "true"
	}
	if f := cmd.Flag("git-init"); f != nil {
		opts.GitInit = f.Value.String() == "true"
	}
	return opts
}

func runNonInteractiveInit(opts InitOptions, cwd string) error {
	// Validate project name
	if err := validate.ProjectName(opts.Name); err != nil {
		return tui.StyledError(err.Error())
	}

	// Check if name is already registered elsewhere
	if existing, ok := registry.Get(opts.Name); ok && existing.Path != cwd {
		return tui.StyledError(fmt.Sprintf("project name %q is already registered at %s", opts.Name, existing.Path))
	}

	// Resolve project type
	pt, err := resolveProjectType(opts.Type)
	if err != nil {
		return tui.StyledError(err.Error())
	}

	// Resolve runtime version
	version, err := resolveVersion(pt, opts.PHP, opts.Node)
	if err != nil {
		return tui.StyledError(err.Error())
	}

	// Resolve database
	db := resolveDatabase(pt, opts.DB)

	// Resolve subdomain
	subdomain := opts.Subdomain
	if subdomain == "" {
		subdomain = opts.Name
	}

	// Build manifest
	m := wizard.BuildManifest(opts.Name, pt, version, db, subdomain)

	// Save manifest
	manifestPath := filepath.Join(cwd, manifest.FileName)
	if err := manifest.Save(m, manifestPath); err != nil {
		return err
	}
	log(opts.Quiet, "Created %s", manifest.FileName)

	// Create project directories
	appDir := filepath.Join(cwd, m.HostAppDir())
	os.MkdirAll(appDir, 0755)
	dataDir := filepath.Join(cwd, "data")
	os.MkdirAll(dataDir, 0755)
	gitsetup.WriteDirIgnore(dataDir)
	if m.HasDatabase() {
		// No .gitignore inside — Postgres requires an empty directory to initialise
		os.MkdirAll(filepath.Join(cwd, "db"), 0755)
	}
	if m.Project.Type == manifest.TypeWordPress {
		for _, sub := range []string{"wp-content/uploads", "wp-content/plugins", "wp-content/themes"} {
			os.MkdirAll(filepath.Join(dataDir, sub), 0755)
		}
	}
	log(opts.Quiet, "Created %s/ and data/ directories", m.HostAppDir())

	// Generate .env
	envPath := filepath.Join(cwd, ".env")
	if err := env.Generate(m, envPath); err != nil {
		return err
	}
	log(opts.Quiet, "Created .env")

	// Register project
	if err := registry.Add(opts.Name, cwd, string(pt), m.Project.Domain); err != nil {
		return err
	}
	log(opts.Quiet, "Project registered")

	// Git setup
	if opts.GitInit {
		if gitsetup.IsGitRepo(cwd) {
			gitsetup.EnsureRootIgnore(cwd)
			log(opts.Quiet, "Updated .gitignore")
		} else {
			if err := gitsetup.InitRepo(cwd); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: git init failed: %v\n", err)
			} else {
				log(opts.Quiet, "Git repository initialised with .gitignore")
			}
		}
	} else if gitsetup.IsGitRepo(cwd) {
		gitsetup.EnsureRootIgnore(cwd)
		log(opts.Quiet, "Updated .gitignore")
	}

	// Start pod
	if opts.Start {
		log(opts.Quiet, "\nStarting pod...")
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

	log(opts.Quiet, "\nRun 'tainer start' to launch.")
	return nil
}

func resolveProjectType(t string) (manifest.ProjectType, error) {
	switch strings.ToLower(t) {
	case "wordpress", "wp":
		return manifest.TypeWordPress, nil
	case "php":
		return manifest.TypePHP, nil
	case "nodejs", "node":
		return manifest.TypeNodeJS, nil
	case "nextjs", "next":
		return manifest.TypeNextJS, nil
	case "nuxtjs", "nuxt":
		return manifest.TypeNuxtJS, nil
	case "nestjs", "nest":
		return manifest.TypeNestJS, nil
	case "kompozi":
		return manifest.TypeKompozi, nil
	default:
		return "", fmt.Errorf("unknown project type %q\nValid types: wordpress, php, nodejs, nextjs, nuxtjs, nestjs, kompozi", t)
	}
}

var (
	knownPHPVersions  = []string{"7.4", "8.1", "8.2", "8.3", "8.4", "8.5"}
	knownNodeVersions = []string{"20", "22", "24"}
)

func resolveVersion(pt manifest.ProjectType, php, node string) (string, error) {
	if pt == manifest.TypeWordPress || pt == manifest.TypePHP {
		if php == "" {
			return wizard.DefaultPHPVersion(), nil
		}
		v, err := validateVersion(php, knownPHPVersions, true)
		if err != nil {
			return "", fmt.Errorf("invalid PHP version %q\nAvailable: %s", php, strings.Join(knownPHPVersions, ", "))
		}
		return v, nil
	}
	if node == "" {
		return wizard.DefaultNodeVersion(), nil
	}
	v, err := validateVersion(node, knownNodeVersions, false)
	if err != nil {
		return "", fmt.Errorf("invalid Node version %q\nAvailable: %s", node, strings.Join(knownNodeVersions, ", "))
	}
	return v, nil
}

// validateVersion checks if v is in the known list. For dotted versions (PHP),
// it tries auto-correcting a missing dot (e.g. "84" → "8.4").
func validateVersion(v string, known []string, dotted bool) (string, error) {
	for _, k := range known {
		if v == k {
			return v, nil
		}
	}
	// Auto-correct: 2-char string without dot → insert dot in middle
	if dotted && len(v) == 2 && !strings.Contains(v, ".") {
		corrected := string(v[0]) + "." + string(v[1])
		for _, k := range known {
			if corrected == k {
				return corrected, nil
			}
		}
	}
	return "", fmt.Errorf("unknown version")
}

func resolveDatabase(pt manifest.ProjectType, db string) manifest.DatabaseType {
	if db != "" {
		switch strings.ToLower(db) {
		case "mariadb", "mysql":
			return manifest.DatabaseMariaDB
		case "postgres", "postgresql":
			return manifest.DatabasePostgres
		case "none":
			return manifest.DatabaseNone
		}
	}
	return wizard.DefaultDatabase(pt)
}

func log(quiet bool, format string, args ...any) {
	if !quiet {
		fmt.Printf(format+"\n", args...)
	}
}

// InitHelp renders styled help for `tainer init`.
func InitHelp(cmd *cobra.Command, _ []string) {
	c := tui.Colors()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
	labelStyle := lipgloss.NewStyle().Foreground(c.Muted)
	flagStyle := lipgloss.NewStyle().Foreground(c.Teal)
	descStyle := lipgloss.NewStyle().Foreground(c.Text)
	exampleStyle := lipgloss.NewStyle().Foreground(c.Blue)

	var lines []string
	lines = append(lines, titleStyle.Render("Create a new Tainer project"))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Usage"))
	lines = append(lines, "  "+descStyle.Render("tainer init")+"                        "+labelStyle.Render("Interactive wizard"))
	lines = append(lines, "  "+descStyle.Render("tainer init --name=X --type=Y")+"   "+labelStyle.Render("Non-interactive"))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Required (non-interactive)"))
	lines = append(lines, "  "+flagStyle.Render("--name")+"        "+descStyle.Render("Project name"))
	lines = append(lines, "  "+flagStyle.Render("--type")+"        "+descStyle.Render("wordpress, php, nodejs, nextjs, nuxtjs, nestjs, kompozi"))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Optional"))
	lines = append(lines, "  "+flagStyle.Render("--php")+"         "+descStyle.Render("PHP version (default: 8.4)"))
	lines = append(lines, "  "+flagStyle.Render("--node")+"        "+descStyle.Render("Node.js version (default: 22)"))
	lines = append(lines, "  "+flagStyle.Render("--db")+"          "+descStyle.Render("mariadb, postgres, none (default depends on type)"))
	lines = append(lines, "  "+flagStyle.Render("--subdomain")+"   "+descStyle.Render("Subdomain for .tainer.me (default: project name)"))
	lines = append(lines, "  "+flagStyle.Render("--start")+"       "+descStyle.Render("Start the project after creation"))
	lines = append(lines, "  "+flagStyle.Render("--git-init")+"    "+descStyle.Render("Initialise a git repository"))
	lines = append(lines, "  "+flagStyle.Render("-q, --quiet")+"   "+descStyle.Render("Suppress output"))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Examples"))
	lines = append(lines, "  "+exampleStyle.Render("tainer init --name=blog --type=wordpress --start"))
	lines = append(lines, "  "+exampleStyle.Render("tainer init --name=api --type=nextjs --db=postgres"))
	lines = append(lines, "  "+exampleStyle.Render("tainer init --name=app --type=php --php=8.3 --db=none"))

	tui.PrintWithLogo(strings.Join(lines, "\n"))
}
