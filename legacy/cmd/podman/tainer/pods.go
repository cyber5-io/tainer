package tainer

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/spf13/cobra"
)

var podsCmd = &cobra.Command{
	Use:   "pods",
	Short: "List all Tainer projects and their status",
	Long:  "Alias for 'tainer list'.",
	RunE:  listRun,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: podsCmd,
	})
}
