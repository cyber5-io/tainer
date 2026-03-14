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

// InterceptInit checks if `tainer init` (bare) should run the project wizard.
// Returns true if it handled the command (wizard ran or error shown).
func InterceptInit(cmd *cobra.Command, args []string) bool {
	// Any args or flags → pass through to Podman
	if len(args) > 0 || cmd.Flags().NFlag() > 0 {
		return false
	}

	cwd, _ := os.Getwd()
	if manifest.Exists(cwd) {
		fmt.Fprintf(os.Stderr, "Error: tainer.yaml already exists in %s\n", cwd)
		os.Exit(1)
	}

	if RunWizard == nil {
		fmt.Fprintf(os.Stderr, "Error: wizard not available\n")
		os.Exit(1)
	}

	if err := RunWizard(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return true
}

// InterceptStart checks if `tainer start` should start a Tainer project.
func InterceptStart(cmd *cobra.Command, args []string) bool {
	return interceptProjectCommand(cmd, args, "start")
}

// InterceptStop checks if `tainer stop` should stop a Tainer project.
func InterceptStop(cmd *cobra.Command, args []string) bool {
	return interceptProjectCommand(cmd, args, "stop")
}

// InterceptRestart checks if `tainer restart` should restart a Tainer project.
func InterceptRestart(cmd *cobra.Command, args []string) bool {
	return interceptProjectCommand(cmd, args, "restart")
}

func interceptProjectCommand(cmd *cobra.Command, args []string, action string) bool {
	cwd, _ := os.Getwd()

	// No args: check for tainer.yaml in cwd
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		if !manifest.Exists(cwd) {
			fmt.Fprintf(os.Stderr, "Error: no tainer.yaml found in current directory.\n")
			fmt.Fprintf(os.Stderr, "  Run 'tainer init' to create a project, or provide a project/container name.\n")
			fmt.Fprintf(os.Stderr, "  Usage: tainer %s [project-name|container-name]\n", action)
			os.Exit(1)
		}
		m, err := manifest.LoadFromDir(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading tainer.yaml: %v\n", err)
			os.Exit(1)
		}
		executeProjectAction(m.Project.Name, cwd, action)
		return true
	}

	// With a single name arg: smart name resolution
	if len(args) == 1 && cmd.Flags().NFlag() == 0 {
		name := args[0]
		if p, ok := registry.Get(name); ok {
			executeProjectAction(name, p.Path, action)
			return true
		}
		// Not a registered project → fall through to Podman (container name)
		return false
	}

	// Multiple args or flags → Podman behavior
	return false
}

func executeProjectAction(name, path, action string) {
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
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
