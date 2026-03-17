package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy [project-name] [--nuke]",
	Short: "Destroy a Tainer project (stop, remove containers, clean up)",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := tainerCli.InterceptDestroy(cmd, args)
		return err
	},
}

func init() {
	destroyCmd.Flags().BoolP("force", "f", false, "Skip confirmation")
	destroyCmd.Flags().Bool("nuke", false, "Remove all project files (app/, data/, db/, tainer.yaml, etc.)")
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: destroyCmd,
	})
}
