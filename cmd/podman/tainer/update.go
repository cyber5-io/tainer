package tainer

import (
	"fmt"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/update"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Tainer templates and TLS certificate",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := update.Run(); err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
		return nil
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: updateCmd,
	})
}
