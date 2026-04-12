package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/env"
	"github.com/containers/podman/v6/pkg/tainer/gitsetup"
	"github.com/containers/podman/v6/pkg/tainer/machine"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	mountTui "github.com/containers/podman/v6/pkg/tainer/tui/mount"
	"github.com/containers/podman/v6/pkg/tainer/tui/progress"
	"github.com/containers/podman/v6/pkg/tainer/wizard"
	"github.com/spf13/cobra"
)

// RunWizard delegates to the wizard package.
var RunWizard = func(cwd string) error {
	return wizard.Run(cwd)
}

// ProjectStart delegates to the project package.
var ProjectStart = project.Start

// ProjectStop delegates to the project package.
var ProjectStop = project.Stop

// ProjectDestroy delegates to the project package.
var ProjectDestroy = func(dir string, force, nuke bool) error {
	return project.Destroy(dir, force, nuke)
}

// GetWorkingDir returns the current working directory. Replaceable for testing.
var GetWorkingDir = os.Getwd

// InterceptStart checks if `tainer start` should start a Tainer project.
func InterceptStart(cmd *cobra.Command, args []string) (bool, error) {
	return interceptProjectCommand(cmd, args, "start")
}

// InterceptStop checks if `tainer stop` should stop a Tainer project.
func InterceptStop(cmd *cobra.Command, args []string) (bool, error) {
	return interceptProjectCommand(cmd, args, "stop")
}

// InterceptRestart checks if `tainer restart` should restart a Tainer project.
func InterceptRestart(cmd *cobra.Command, args []string) (bool, error) {
	return interceptProjectCommand(cmd, args, "restart")
}

// InterceptDestroy checks if `tainer destroy` should destroy a Tainer project.
func InterceptDestroy(cmd *cobra.Command, args []string) (bool, error) {
	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	force := false
	nuke := false
	for _, a := range os.Args {
		if a == "--force" || a == "-f" {
			force = true
		}
		if a == "--nuke" {
			nuke = true
		}
	}

	var projectDir string

	// No args: check for tainer.yaml in cwd
	if len(args) == 0 {
		if !manifest.Exists(cwd) {
			return true, tui.StyledError("No tainer.yaml found in current directory.\nRun 'tainer init' to create a project, or provide a project name.\nUsage: tainer destroy [project-name] [--nuke]")
		}
		projectDir = cwd
	} else if len(args) == 1 {
		name := args[0]
		if p, ok := registry.Get(name); ok {
			projectDir = p.Path
		} else {
			return true, tui.StyledError(fmt.Sprintf("Project %q not found in registry", name))
		}
	} else {
		return false, nil
	}

	return true, executeDestroy(projectDir, force, nuke)
}

func executeDestroy(projectDir string, force, nuke bool) error {
	if err := machine.EnsureRunning(); err != nil {
		return err
	}

	steps, name, err := project.DestroySteps(projectDir)
	if err != nil {
		return err
	}

	if nuke {
		steps = append(steps, project.DestroyNukeStep(projectDir))
	}

	// Confirmation prompt (unless --force)
	if !force {
		c := tui.Colors()
		orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)
		textStyle := lipgloss.NewStyle().Foreground(c.Text)

		msg := fmt.Sprintf("Destroy project %s?", name)
		if nuke {
			msg = fmt.Sprintf("NUKE project %s? All project files will be deleted.", name)
		}
		fmt.Printf("\n  %s %s ", orangeStyle.Render("✖"), textStyle.Render(msg))
		fmt.Print("[y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Cancelled.")
			return nil
		}
		fmt.Println()
	}

	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	suffix := " destroyed"
	if nuke {
		suffix = " nuked"
	}
	footer := []string{
		tealStyle.Render("✓") + " " + textStyle.Render(name) + mutedStyle.Render(suffix),
	}

	result, err := progress.Run("Destroying "+name, steps, footer)
	if err != nil {
		return err
	}
	return result.Err
}

// InterceptMount handles `tainer mount`, `tainer mount add <name>`, and `tainer mount del <name>`.
func InterceptMount(cmd *cobra.Command, args []string) (bool, error) {
	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	if !manifest.Exists(cwd) {
		return true, tui.StyledError("No tainer.yaml found in current directory.\nRun 'tainer init' to create a project first.")
	}

	m, err := manifest.LoadFromDir(cwd)
	if err != nil {
		return true, fmt.Errorf("reading tainer.yaml: %w", err)
	}

	// Build internal mounts list
	internal := []string{"html", "data"}
	if m.HasDatabase() {
		internal = append(internal, "db")
	}

	// No args: launch interactive mount TUI
	if len(args) == 0 {
		result, err := mountTui.Run(m.Project.Name, cwd, m.Mounts, internal)
		if err != nil {
			return true, err
		}
		if result.Restart {
			return true, executeProjectAction(m.Project.Name, cwd, "restart")
		}
		return true, nil
	}

	subCmd := args[0]
	if subCmd != "add" && subCmd != "del" {
		return true, tui.StyledError(fmt.Sprintf("Unknown subcommand %q\nUsage: tainer mount [add|del] <name>", subCmd))
	}

	if len(args) < 2 {
		c := tui.Colors()
		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Blue)
		mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
		content := labelStyle.Render("Usage: ") + mutedStyle.Render("tainer mount "+subCmd+" <name>") +
			"\n\n" + mutedStyle.Render("Or run 'tainer mount' for interactive mode.")
		tui.PrintWithLogo(content)
		return true, nil
	}

	return true, executeMountAction(cwd, subCmd, args[1])
}

func executeMountAction(cwd, action, name string) error {
	c := tui.Colors()

	var err error
	switch action {
	case "add":
		err = project.MountAdd(cwd, name)
	case "del":
		err = project.MountDel(cwd, name)
	}
	if err != nil {
		return err
	}

	check := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
	msg := "Added mount: " + name
	if action == "del" {
		msg = "Removed mount: " + name
	}
	content := check + " " + lipgloss.NewStyle().Foreground(c.Text).Render(msg) +
		"\n" + lipgloss.NewStyle().Foreground(c.Muted).Render("Restart project to apply changes.")
	tui.PrintWithLogo(content)
	return nil
}

func interceptProjectCommand(cmd *cobra.Command, args []string, action string) (bool, error) {
	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	// No args: check for tainer.yaml in cwd
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		if !manifest.Exists(cwd) {
			// Check if a backup exists for this directory and offer to restore
			if projectName, ok := config.FindBackupForPath(cwd); ok {
				missing := config.MissingWithBackup(projectName, cwd)
				if len(missing) > 0 {
					c := tui.Colors()
					orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)
					textStyle := lipgloss.NewStyle().Foreground(c.Text)
					mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
					fmt.Printf("\n  %s %s %s\n",
						orangeStyle.Render("!"),
						textStyle.Render("Missing config files:"),
						mutedStyle.Render(strings.Join(missing, ", ")))
					fmt.Printf("  %s ", textStyle.Render("Restore from backup? [y/N]"))
					reader := bufio.NewReader(os.Stdin)
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					if answer == "y" || answer == "yes" {
						restored, err := config.RestoreFiles(projectName, cwd, missing)
						if err != nil {
							return true, fmt.Errorf("restoring backup: %w", err)
						}
						tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
						for _, name := range restored {
							fmt.Printf("  %s %s\n", tealStyle.Render("✓"), textStyle.Render("Restored "+name))
						}
						fmt.Println()
					} else {
						return true, tui.StyledError(fmt.Sprintf("No tainer.yaml found in current directory.\nRun 'tainer init' to create a project, or provide a project/container name.\nUsage: tainer %s [project-name|container-name]", action))
					}
				}
			} else {
				return true, tui.StyledError(fmt.Sprintf("No tainer.yaml found in current directory.\nRun 'tainer init' to create a project, or provide a project/container name.\nUsage: tainer %s [project-name|container-name]", action))
			}
		}

		// Ensure machine is running before project actions
		if action != "stop" {
			if err := machine.EnsureRunning(); err != nil {
				return true, err
			}
		}

		m, err := manifest.LoadFromDir(cwd)
		if err != nil {
			return true, fmt.Errorf("reading tainer.yaml: %w", err)
		}

		// Auto-init: if the project is not registered, this is likely a freshly cloned project
		if action == "start" {
			if _, ok := registry.Get(m.Project.Name); !ok {
				if err := autoInitProject(m, cwd); err != nil {
					return true, err
				}
			}
		}

		return true, executeProjectAction(m.Project.Name, cwd, action)
	}

	// With a single name arg: smart name resolution
	if len(args) == 1 && cmd.Flags().NFlag() == 0 {
		name := args[0]
		if p, ok := registry.Get(name); ok {
			if action != "stop" {
				if err := machine.EnsureRunning(); err != nil {
					return true, err
				}
			}
			return true, executeProjectAction(name, p.Path, action)
		}
		// Not a registered project → fall through to Podman (container name)
		return false, nil
	}

	// Multiple args or flags → Podman behavior
	return false, nil
}

// autoInitProject handles first-time setup for a cloned project that has tainer.yaml
// but is not yet registered on this machine. It prompts the user, generates fresh
// credentials, creates required directories, and registers the project.
func autoInitProject(m *manifest.Manifest, projectDir string) error {
	c := tui.Colors()
	orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	boldStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)

	// Show confirmation prompt
	fmt.Println()
	fmt.Printf("  %s %s\n\n", orangeStyle.Render("⚠"), textStyle.Render("Project "+boldStyle.Render(m.Project.Name)+" is not registered on this machine."))
	fmt.Printf("  %s\n", mutedStyle.Render("This looks like a freshly cloned project."))
	fmt.Printf("  %s\n\n", mutedStyle.Render("tainer will set it up for local development:"))
	fmt.Printf("  %s %s\n", tealStyle.Render("•"), textStyle.Render("Generate .env with fresh credentials"))
	dirsLabel := "data/"
	if m.HasDatabase() {
		dirsLabel = "data/ and db/"
	}
	fmt.Printf("  %s %s\n", tealStyle.Render("•"), textStyle.Render("Create "+dirsLabel+" directories"))
	fmt.Printf("  %s %s\n\n", tealStyle.Render("•"), textStyle.Render("Register project in local registry"))
	fmt.Printf("  %s ", textStyle.Render("Continue? [y/N]"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("  Cancelled.")
		return fmt.Errorf("auto-init cancelled")
	}
	fmt.Println()

	// Remove machine-specific files (could be stale from another machine)
	for _, stale := range []string{".env", ".tainer-authorized_keys", ".tainer.local.yaml"} {
		os.Remove(filepath.Join(projectDir, stale))
	}

	// Generate fresh .env with new credentials
	envPath := filepath.Join(projectDir, ".env")
	if err := env.Generate(m, envPath); err != nil {
		return fmt.Errorf("generating .env: %w", err)
	}

	// Create required directories
	dataDir := filepath.Join(projectDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	if err := gitsetup.WriteDirIgnore(dataDir); err != nil {
		return fmt.Errorf("writing data/.gitignore: %w", err)
	}
	if m.HasDatabase() {
		dbDir := filepath.Join(projectDir, "db")
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("creating db directory: %w", err)
		}
		if err := gitsetup.WriteDirIgnore(dbDir); err != nil {
			return fmt.Errorf("writing db/.gitignore: %w", err)
		}
	}

	// Register project
	if err := registry.Add(m.Project.Name, projectDir, string(m.Project.Type), m.Project.Domain); err != nil {
		return fmt.Errorf("registering project: %w", err)
	}

	// Git setup
	if gitsetup.IsGitRepo(projectDir) {
		if err := gitsetup.EnsureRootIgnore(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not update .gitignore: %v\n", err)
		}
	} else {
		fmt.Printf("  %s %s\n", mutedStyle.Render("ℹ"), textStyle.Render("No git repository detected."))
		fmt.Printf("  %s ", textStyle.Render("Initialise git repo? [y/N]"))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			if err := gitsetup.InitRepo(projectDir); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: git init failed: %v\n", err)
			} else {
				fmt.Printf("  %s %s\n", tealStyle.Render("✓"), textStyle.Render("Git repository initialised"))
			}
		}
	}

	fmt.Printf("  %s %s\n\n", tealStyle.Render("✓"), textStyle.Render("Project "+boldStyle.Render(m.Project.Name)+" initialised"))
	return nil
}

func executeProjectAction(name, path, action string) error {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	switch action {
	case "start":
		steps, info, err := project.StartSteps(path)
		if err != nil {
			return err
		}

		// Check if already running before showing TUI
		podName := fmt.Sprintf("tainer-%s", info.Name)
		if project.IsPodRunning(podName) {
			tui.PrintWithLogo(textStyle.Render(info.Name) + mutedStyle.Render(" is already running"))
			return nil
		}

		footer := []string{
			"",
			tealStyle.Render("✓") + " " + textStyle.Render(info.Name) + mutedStyle.Render(" started"),
			"  " + tealStyle.Render("https://"+info.Domain),
			"  " + mutedStyle.Render(info.SSHCmd),
		}

		result, err := progress.Run("Starting "+info.Name, steps, footer)
		if err != nil {
			return err
		}
		return result.Err

	case "stop":
		steps, err := project.StopSteps(name)
		if err != nil {
			return err
		}
		footer := []string{
			tealStyle.Render("✓") + " " + textStyle.Render(name) + mutedStyle.Render(" stopped"),
		}
		result, err := progress.Run("Stopping "+name, steps, footer)
		if err != nil {
			return err
		}
		return result.Err

	case "restart":
		// Stop phase
		stopSteps, err := project.StopSteps(name)
		if err != nil {
			return err
		}
		stopResult, err := progress.Run("Stopping "+name, stopSteps, nil)
		if err != nil {
			return err
		}
		if stopResult.Err != nil {
			return stopResult.Err
		}

		// Start phase
		startSteps, info, err := project.StartSteps(path)
		if err != nil {
			return err
		}
		footer := []string{
			"",
			tealStyle.Render("✓") + " " + textStyle.Render(info.Name) + mutedStyle.Render(" restarted"),
			"  " + tealStyle.Render("https://"+info.Domain),
			"  " + mutedStyle.Render(info.SSHCmd),
		}
		result, err := progress.Run("Starting "+info.Name, startSteps, footer)
		if err != nil {
			return err
		}
		return result.Err
	}
	return nil
}
