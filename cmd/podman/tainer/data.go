package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var dataCmd = &cobra.Command{
	Use:   "data <add|del> <path>",
	Short: "Manage persistent data mounts for a Tainer project",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := tainerCli.InterceptData(cmd, args)
		return err
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: dataCmd,
	})
}
