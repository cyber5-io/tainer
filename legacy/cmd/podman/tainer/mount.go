package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var mountCmd = &cobra.Command{
	Use:   "mount <add|del> <name>",
	Short: "Manage custom mounts for a Tainer project",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := tainerCli.InterceptMount(cmd, args)
		return err
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: mountCmd,
	})
}
