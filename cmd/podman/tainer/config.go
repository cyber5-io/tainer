package tainer

import (
	"fmt"
	"os"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Tainer project configuration",
}

var configBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup tainer.yaml and .env for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		if !manifest.Exists(cwd) {
			return fmt.Errorf("no tainer.yaml found in current directory")
		}

		m, err := manifest.LoadFromDir(cwd)
		if err != nil {
			return fmt.Errorf("reading tainer.yaml: %w", err)
		}

		if err := config.Backup(m.Project.Name, cwd); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}

		fmt.Printf("Backed up config for '%s'\n", m.Project.Name)
		return nil
	},
}

var configRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore tainer.yaml and .env from backup",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		// Try to find project name from the registry by path
		projectName, ok := projRegistry.FindByPath(cwd)
		if !ok {
			// Try to find a backup that matches this path
			projectName, ok = config.FindBackupForPath(cwd)
			if !ok {
				return fmt.Errorf("no backup found for current directory")
			}
		}

		if !config.BackupExists(projectName) {
			return fmt.Errorf("no backup found for project '%s'", projectName)
		}

		if err := config.Restore(projectName, cwd); err != nil {
			return fmt.Errorf("restore failed: %w", err)
		}

		fmt.Printf("Restored config for '%s'\n", projectName)
		return nil
	},
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: configCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: configBackupCmd,
		Parent:  configCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: configRestoreCmd,
		Parent:  configCmd,
	})
}
