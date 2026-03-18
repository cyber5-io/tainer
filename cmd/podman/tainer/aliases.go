package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start a Tainer project (alias for start)",
	Long:  "Alias for 'tainer start'.",
	RunE: func(cmd *cobra.Command, args []string) error {
		handled, err := tainerCli.InterceptStart(cmd, args)
		if handled {
			return err
		}
		return nil
	},
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop a Tainer project (alias for stop)",
	Long:  "Alias for 'tainer stop'.",
	RunE: func(cmd *cobra.Command, args []string) error {
		handled, err := tainerCli.InterceptStop(cmd, args)
		if handled {
			return err
		}
		return nil
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: upCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: downCmd,
	})
}
