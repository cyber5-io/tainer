package tainer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/pkg/tainer/tui/picker"
	"github.com/spf13/cobra"
)

var dbRaw bool

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage project database",
	Long:  "Export and import database dumps for the current project.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dbExportCmd = &cobra.Command{
	Use:   "export [filename]",
	Short: "Export the database to a SQL file",
	Long:  "Exports the project database to a SQL file at the project root.\nDefaults to <project-name>-db.sql if no filename is given.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  dbExport,
}

var dbImportCmd = &cobra.Command{
	Use:   "import [filename]",
	Short: "Import a SQL file into the database",
	Long:  "Imports a SQL file into the project database.\nDefaults to <project-name>-db.sql if no filename is given.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  dbImport,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: dbCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: dbExportCmd,
		Parent:  dbCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: dbImportCmd,
		Parent:  dbCmd,
	})
	dbCmd.PersistentFlags().BoolVar(&dbRaw, "raw", false, "Plain text output (no TUI)")
}

func dbExport(cmd *cobra.Command, args []string) error {
	name, dir, err := resolveProject(nil)
	if err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(dir)
	if err != nil {
		return fmt.Errorf("reading tainer.yaml: %w", err)
	}

	if !m.HasDatabase() {
		return tui.StyledError("This project has no database configured")
	}

	podStatus := getPodStatus(name)
	if podStatus != "Running" {
		return fmt.Errorf("project %q is not running (status: %s). Start it first with: tainer start", name, podStatus)
	}

	// Determine output filename
	filename := fmt.Sprintf("%s-db.sql", name)
	if len(args) > 0 {
		filename = args[0]
	}
	outputPath := filepath.Join(dir, filename)

	// Build the dump command based on db type
	containerName := fmt.Sprintf("tainer-%s-db-ct", name)
	var dumpCmd *exec.Cmd

	switch m.Runtime.Database {
	case manifest.DatabasePostgres:
		dumpCmd = exec.Command("tainer", "exec", containerName,
			"pg_dump", "-U", "tainer", "-d", "tainer", "--no-owner", "--no-acl")
	case manifest.DatabaseMariaDB:
		dumpCmd = exec.Command("tainer", "exec", containerName,
			"mysqldump", "-u", "tainer", "--password=tainer", "tainer")
	default:
		return tui.StyledError(fmt.Sprintf("Unsupported database type: %s", m.Runtime.Database))
	}

	output, err := dumpCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("database export failed: %s", string(output))
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("writing dump file: %w", err)
	}

	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	tui.PrintWithLogo(
		tealStyle.Render("✓") + " " + textStyle.Render("Database exported") +
			"\n  " + mutedStyle.Render(filename))

	// Git awareness: offer to add/commit if in a git repo
	if isGitRepo(dir) {
		offerGitCommit(dir, filename, name)
	}

	return nil
}

func dbImport(cmd *cobra.Command, args []string) error {
	name, dir, err := resolveProject(nil)
	if err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(dir)
	if err != nil {
		return fmt.Errorf("reading tainer.yaml: %w", err)
	}

	if !m.HasDatabase() {
		return tui.StyledError("This project has no database configured")
	}

	podStatus := getPodStatus(name)
	if podStatus != "Running" {
		return fmt.Errorf("project %q is not running (status: %s). Start it first with: tainer start", name, podStatus)
	}

	// Determine input filename
	filename := ""
	if len(args) > 0 {
		filename = args[0]
		inputPath := filepath.Join(dir, filename)
		if _, err := os.Stat(inputPath); os.IsNotExist(err) {
			return tui.StyledError(fmt.Sprintf("File not found: %s", filename))
		}
	} else {
		// No filename specified — find SQL files and let user pick
		filename = selectSQLFile(dir, name, dbRaw)
		if filename == "" {
			return nil // user cancelled or no files found
		}
	}

	// Confirm before importing
	c := tui.Colors()
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)

	fmt.Printf("\n  %s %s\n", orangeStyle.Render("⚠"), textStyle.Render("This will import "+filename+" into the database."))
	fmt.Printf("  %s\n\n", lipgloss.NewStyle().Foreground(c.Muted).Render("Existing data may be overwritten."))
	fmt.Printf("  %s ", textStyle.Render("Continue? [y/N]"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("  Cancelled.")
		return nil
	}
	fmt.Println()

	inputPath := filepath.Join(dir, filename)

	// Read the SQL file
	sqlData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading dump file: %w", err)
	}

	// Build the import command based on db type
	containerName := fmt.Sprintf("tainer-%s-db-ct", name)
	var importCmd *exec.Cmd

	switch m.Runtime.Database {
	case manifest.DatabasePostgres:
		importCmd = exec.Command("tainer", "exec", "-i", containerName,
			"psql", "-U", "tainer", "-d", "tainer")
	case manifest.DatabaseMariaDB:
		importCmd = exec.Command("tainer", "exec", "-i", containerName,
			"mysql", "-u", "tainer", "--password=tainer", "tainer")
	default:
		return tui.StyledError(fmt.Sprintf("Unsupported database type: %s", m.Runtime.Database))
	}

	importCmd.Stdin = strings.NewReader(string(sqlData))
	output, err := importCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("database import failed: %s", string(output))
	}

	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	tui.PrintWithLogo(
		tealStyle.Render("✓") + " " + textStyle.Render("Database imported") +
			"\n  " + mutedStyle.Render(filename))

	return nil
}

// selectSQLFile finds SQL files at the project root and lets the user pick one.
// When raw is true, uses plain text numbered list instead of TUI picker.
// Returns empty string if no files found or user cancelled.
func selectSQLFile(dir, projectName string, raw bool) string {
	sqlFiles := FindSQLFiles(dir)
	if len(sqlFiles) == 0 {
		c := tui.Colors()
		fmt.Printf("\n  %s\n", lipgloss.NewStyle().Foreground(c.Muted).Render("No .sql files found at project root."))
		return ""
	}

	// Sort: default file first
	defaultFile := fmt.Sprintf("%s-db.sql", projectName)
	sorted := make([]string, 0, len(sqlFiles))
	defaultIdx := 0
	for _, f := range sqlFiles {
		if f == defaultFile {
			sorted = append([]string{f}, sorted...)
		} else {
			sorted = append(sorted, f)
		}
	}
	sqlFiles = sorted

	// Single file — no picker needed
	if len(sqlFiles) == 1 {
		return sqlFiles[0]
	}

	if raw {
		// Plain text selection
		fmt.Printf("\n  Select database dump:\n\n")
		for i, f := range sqlFiles {
			fmt.Printf("  %d) %s\n", i+1, f)
		}
		fmt.Printf("\n  Enter number [1-%d]: ", len(sqlFiles))

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "" || answer == "q" {
			return ""
		}
		var idx int
		if _, err := fmt.Sscanf(answer, "%d", &idx); err != nil || idx < 1 || idx > len(sqlFiles) {
			fmt.Println("  Invalid selection.")
			return ""
		}
		return sqlFiles[idx-1]
	}

	// Multiple files — launch TUI picker
	result, err := picker.Run("Select database dump", sqlFiles, defaultIdx)
	if err != nil || result.Cancelled {
		return ""
	}
	return result.Selected
}

// isGitRepo checks if the directory has a .git folder
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// offerGitCommit checks if the SQL file is tracked and offers to add/commit
func offerGitCommit(dir, filename, projectName string) {
	c := tui.Colors()
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	// Check if already tracked
	checkCmd := exec.Command("git", "-C", dir, "ls-files", filename)
	tracked, _ := checkCmd.CombinedOutput()
	isTracked := strings.TrimSpace(string(tracked)) != ""

	action := "Add"
	if isTracked {
		action = "Update"
	}

	fmt.Printf("\n  %s %s\n", mutedStyle.Render("ℹ"), textStyle.Render(fmt.Sprintf("%s %s in git?", action, filename)))
	fmt.Printf("  %s ", textStyle.Render("[y/N]"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		return
	}

	// Stage the file
	addCmd := exec.Command("git", "-C", dir, "add", filename)
	if out, err := addCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: git add failed: %s\n", string(out))
		return
	}

	// Commit
	msg := fmt.Sprintf("chore: %s database dump for %s", strings.ToLower(action), projectName)
	commitCmd := exec.Command("git", "-C", dir, "commit", "-m", msg)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: git commit failed: %s\n", string(out))
		return
	}

	fmt.Printf("  %s %s\n", tealStyle.Render("✓"), textStyle.Render("Committed to git"))
}

// FindSQLFiles returns all .sql files at the project root
func FindSQLFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	return files
}
