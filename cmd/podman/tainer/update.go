package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/update"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [core | <project-name>]",
	Short: "Update project images or the Tainer binary",
	Long: `Update container images for a project, or self-update the tainer binary.

  tainer update            Pull latest images for the current project directory
  tainer update <name>     Pull latest images for a named project
  tainer update core       Self-update the tainer binary from GitHub Releases`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return update.RunImagesWithTUI("")
		}
		if args[0] == "core" {
			return update.RunCoreWithTUI()
		}
		return update.RunImagesWithTUI(args[0])
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: updateCmd,
	})
}
