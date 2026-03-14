package cli

import (
	"fmt"
	"os"

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

func interceptProjectCommand(cmd *cobra.Command, args []string, action string) (bool, error) {
	cwd, err := GetWorkingDir()
	if err != nil {
		return true, fmt.Errorf("getting working directory: %w", err)
	}

	// No args: check for tainer.yaml in cwd
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		if !manifest.Exists(cwd) {
			return true, fmt.Errorf("no tainer.yaml found in current directory.\n  Run 'tainer init' to create a project, or provide a project/container name.\n  Usage: tainer %s [project-name|container-name]", action)
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
