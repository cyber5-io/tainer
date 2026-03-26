package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/machine"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
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

// InterceptInit checks if `tainer init` (bare) should run the project wizard.
// Returns (handled, error). If handled is true, the caller should not pass through to Podman.
func InterceptInit(cmd *cobra.Command, args []string) (bool, error) {
	// Any args or flags → pass through to Podman
	if len(args) > 0 || cmd.Flags().NFlag() > 0 {
		return false, nil
	}

	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	if manifest.Exists(cwd) {
		return true, fmt.Errorf("tainer.yaml already exists in %s", cwd)
	}

	// Ensure machine is running before starting the wizard
	if err := machine.EnsureRunning(); err != nil {
		return true, err
	}

	if RunWizard == nil {
		return true, fmt.Errorf("wizard not available")
	}

	if err := RunWizard(cwd); err != nil {
		return true, err
	}
	return true, nil
}

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
			return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project name.\n  Usage: tainer destroy [project-name] [--nuke]")
		}
		projectDir = cwd
	} else if len(args) == 1 {
		name := args[0]
		if p, ok := registry.Get(name); ok {
			projectDir = p.Path
		} else {
			return true, fmt.Errorf("project %q not found in registry", name)
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

// InterceptMount checks if `tainer mount add <name>` or `tainer mount del <name>` should be handled.
func InterceptMount(cmd *cobra.Command, args []string) (bool, error) {
	if len(args) == 0 {
		return true, fmt.Errorf("usage: tainer mount <add|del> <name>\n\nManage custom mounts for a Tainer project.\n\nSubcommands:\n  add <name>   Add a custom mount\n  del <name>   Remove a custom mount")
	}

	subCmd := args[0]
	if subCmd != "add" && subCmd != "del" {
		return true, fmt.Errorf("unknown subcommand %q\nUsage: tainer mount <add|del> <name>", subCmd)
	}

	if len(args) < 2 {
		return true, fmt.Errorf("missing name argument\nUsage: tainer mount %s <name>", subCmd)
	}

	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	if !manifest.Exists(cwd) {
		return true, fmt.Errorf("no tainer.yaml found in current directory")
	}

	name := args[1]

	switch subCmd {
	case "add":
		err = project.MountAdd(cwd, name)
	case "del":
		err = project.MountDel(cwd, name)
	}

	if err != nil {
		return true, err
	}
	return true, nil
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
					fmt.Printf("Missing config files: %s\n", strings.Join(missing, ", "))
					fmt.Print("Restore from backup? (y/n) ")
					reader := bufio.NewReader(os.Stdin)
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					if answer == "y" || answer == "yes" {
						restored, err := config.RestoreFiles(projectName, cwd, missing)
						if err != nil {
							return true, fmt.Errorf("restoring backup: %w", err)
						}
						for _, name := range restored {
							fmt.Printf("  Restored %s\n", name)
						}
					} else {
						return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project/container name.\n  Usage: tainer %s [project-name|container-name]", action)
					}
				}
			} else {
				return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project/container name.\n  Usage: tainer %s [project-name|container-name]", action)
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
			fmt.Printf("%s is already running\n", info.Name)
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
