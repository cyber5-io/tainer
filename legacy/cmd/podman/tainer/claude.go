package tainer

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/spf13/cobra"
)

//go:embed all:claude-skill
var claudeSkillFS embed.FS

const (
	skillDirName = "tainer"
)

var claudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Manage Claude Code integration",
	Long:  "Install or uninstall the Tainer Claude Code skill, which teaches Claude how to work with Tainer projects.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var claudeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the Tainer Claude Code skill",
	Long:  "Copy the Tainer skill to ~/.claude/skills/tainer/ so Claude Code can recognise and work with Tainer projects.",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}

		skillsDir := filepath.Join(home, ".claude", "skills")
		targetDir := filepath.Join(skillsDir, skillDirName)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("creating skill directory: %w", err)
		}

		count, err := extractSkill(targetDir)
		if err != nil {
			return fmt.Errorf("extracting skill files: %w", err)
		}

		c := tui.Colors()
		check := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
		path := lipgloss.NewStyle().Foreground(c.Muted).Render(targetDir)
		content := check + " " + lipgloss.NewStyle().Foreground(c.Text).Render(
			fmt.Sprintf("Tainer skill installed (%d files)", count)) +
			"\n  " + path +
			"\n\n  " + lipgloss.NewStyle().Foreground(c.Muted).Render("Restart Claude Code to load the skill.")
		tui.PrintWithLogo(content)
		return nil
	},
}

var claudeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the Tainer Claude Code skill",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}

		targetDir := filepath.Join(home, ".claude", "skills", skillDirName)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			return tui.StyledError("Tainer skill is not installed")
		}

		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("removing skill directory: %w", err)
		}

		c := tui.Colors()
		check := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
		content := check + " " + lipgloss.NewStyle().Foreground(c.Text).Render("Tainer skill removed")
		tui.PrintWithLogo(content)
		return nil
	},
}

// extractSkill walks the embedded skill FS and writes each file to targetDir.
func extractSkill(targetDir string) (int, error) {
	count := 0
	err := fs.WalkDir(claudeSkillFS, "claude-skill", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Strip the embed prefix
		rel := path[len("claude-skill"):]
		if rel == "" {
			return nil
		}
		rel = rel[1:] // strip leading slash

		dest := filepath.Join(targetDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}

		data, err := claudeSkillFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: claudeCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: claudeInstallCmd,
		Parent:  claudeCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: claudeUninstallCmd,
		Parent:  claudeCmd,
	})
}
