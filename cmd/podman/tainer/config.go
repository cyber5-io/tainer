package tainer

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Tainer project configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		if !manifest.Exists(cwd) {
			return tui.StyledError("No tainer.yaml found in current directory.\nRun from a project directory.")
		}

		m, err := manifest.LoadFromDir(cwd)
		if err != nil {
			return fmt.Errorf("reading tainer.yaml: %w", err)
		}

		c := tui.Colors()
		labelStyle := lipgloss.NewStyle().Foreground(c.Muted)
		valueStyle := lipgloss.NewStyle().Foreground(c.Text)
		boldStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
		cmdStyle := lipgloss.NewStyle().Foreground(c.Teal)

		var lines []string
		lines = append(lines, boldStyle.Render(m.Project.Name))
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("Type     ")+valueStyle.Render(string(m.Project.Type)))
		lines = append(lines, labelStyle.Render("Domain   ")+valueStyle.Render(m.Project.Domain))
		lines = append(lines, labelStyle.Render("Path     ")+valueStyle.Render(cwd))

		// Backup status
		if config.BackupExists(m.Project.Name) {
			lines = append(lines, labelStyle.Render("Backup   ")+lipgloss.NewStyle().Foreground(c.Teal).Render("✓ available"))
		} else {
			lines = append(lines, labelStyle.Render("Backup   ")+labelStyle.Render("none"))
		}

		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("Commands"))
		lines = append(lines, "  "+cmdStyle.Render("tainer config backup")+"   "+labelStyle.Render("Backup tainer.yaml and .env"))
		lines = append(lines, "  "+cmdStyle.Render("tainer config restore")+"  "+labelStyle.Render("Restore from backup"))

		tui.PrintWithLogo(strings.Join(lines, "\n"))
		return nil
	},
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
			return tui.StyledError("No tainer.yaml found in current directory.")
		}

		m, err := manifest.LoadFromDir(cwd)
		if err != nil {
			return fmt.Errorf("reading tainer.yaml: %w", err)
		}

		if err := config.Backup(m.Project.Name, cwd); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}

		c := tui.Colors()
		check := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
		content := check + " " + lipgloss.NewStyle().Foreground(c.Text).Render("Backed up config for ") +
			lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(m.Project.Name)
		tui.PrintWithLogo(content)
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
				return tui.StyledError("No backup found for current directory.")
			}
		}

		if !config.BackupExists(projectName) {
			return tui.StyledError("No backup found for project '" + projectName + "'.")
		}

		restored, err := config.Restore(projectName, cwd)
		if err != nil {
			return fmt.Errorf("restore failed: %w", err)
		}

		c := tui.Colors()
		check := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
		content := check + " " + lipgloss.NewStyle().Foreground(c.Text).Render("Restored config for ") +
			lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(projectName) +
			lipgloss.NewStyle().Foreground(c.Muted).Render(": "+strings.Join(restored, ", "))
		tui.PrintWithLogo(content)
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
