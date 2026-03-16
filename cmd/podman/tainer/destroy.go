package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy [project-name] [--volumes]",
	Short: "Destroy a Tainer project (stop, remove containers, clean up)",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := tainerCli.InterceptDestroy(cmd, args)
		return err
	},
}

func init() {
	destroyCmd.Flags().BoolP("force", "f", false, "Skip confirmation")
	destroyCmd.Flags().Bool("volumes", false, "Also remove db/ and data/ directories")
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: destroyCmd,
	})
}
