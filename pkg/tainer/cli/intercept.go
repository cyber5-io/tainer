package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/machine"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	"github.com/containers/podman/v6/pkg/tainer/registry"
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

	// No args: check for tainer.yaml in cwd
	if len(args) == 0 {
		if !manifest.Exists(cwd) {
			return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project name.\n  Usage: tainer destroy [project-name] [--nuke]")
		}
		if ProjectDestroy != nil {
			if err := ProjectDestroy(cwd, force, nuke); err != nil {
				return true, err
			}
		}
		return true, nil
	}

	// With a single name arg: look up in registry
	if len(args) == 1 {
		name := args[0]
		if p, ok := registry.Get(name); ok {
			if ProjectDestroy != nil {
				if err := ProjectDestroy(p.Path, force, nuke); err != nil {
					return true, err
				}
			}
			return true, nil
		}
		return true, fmt.Errorf("project %q not found in registry", name)
	}

	// Multiple args → not a Tainer command
	return false, nil
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
				fmt.Printf("Found backup for project '%s'. Restore? (y/n) ", projectName)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer == "y" || answer == "yes" {
					if err := config.Restore(projectName, cwd); err != nil {
						return true, fmt.Errorf("restoring backup: %w", err)
					}
					fmt.Println("Config restored from backup.")
				} else {
					return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project/container name.\n  Usage: tainer %s [project-name|container-name]", action)
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
	var err error
	switch action {
	case "start":
		if ProjectStart != nil {
			err = ProjectStart(path)
		}
	case "stop":
		if ProjectStop != nil {
			err = ProjectStop(name)
		}
	case "restart":
		if ProjectStop != nil {
			err = ProjectStop(name)
		}
		if err == nil && ProjectStart != nil {
			err = ProjectStart(path)
		}
	}
	return err
}
