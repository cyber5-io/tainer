package tainer

import (
	"fmt"
	"os"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open [project-name]",
	Short: "Open project URL in the browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			if p, ok := projRegistry.Get(args[0]); ok {
				return project.OpenBrowser(p.Path)
			}
			return tui.StyledError(fmt.Sprintf("Project %q not found.", args[0]))
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if !manifest.Exists(cwd) {
			return tui.StyledError("No tainer.yaml found.\nRun from a project directory or provide a project name.")
		}
		return project.OpenBrowser(cwd)
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: openCmd,
	})
}
