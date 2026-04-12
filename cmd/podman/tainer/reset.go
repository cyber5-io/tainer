package tainer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Emergency reset — kill the VM and restart fresh",
	Long: `Force-kills the tainer VM and restarts it. Use this when tainer
commands hang or the VM becomes unresponsive.

All running pods will be stopped. Pulled images and project data on
the host are preserved.`,
	RunE: runReset,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: resetCmd,
	})
}

func runReset(cmd *cobra.Command, args []string) error {
	if runtime.GOOS == "linux" {
		return tui.StyledError("tainer reset is only needed on macOS/Windows where a VM is used.\nOn Linux, tainer runs natively.")
	}

	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)

	// Warn user before proceeding
	fmt.Println()
	fmt.Printf("  %s %s\n\n", orangeStyle.Render("⚠"), textStyle.Render("This will force-restart the tainer stack."))
	fmt.Printf("  %s\n", mutedStyle.Render("All running pods will be stopped."))
	fmt.Printf("  %s\n\n", mutedStyle.Render("No data will be lost — your project files, databases, and configs are safe."))
	fmt.Printf("  %s ", textStyle.Render("Continue? [y/N]"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("  Cancelled.")
		return nil
	}
	fmt.Println()

	var lines []string
	lines = append(lines, orangeStyle.Render("⚠")+" "+textStyle.Render("Resetting tainer"))
	lines = append(lines, "")

	// Kill vfkit (current VM provider)
	killed := false
	if out, err := exec.Command("pkill", "-9", "-f", "vfkit").CombinedOutput(); err == nil {
		killed = true
		_ = out
	}

	// Kill krunkit (legacy, pre-v0.2.0)
	if out, err := exec.Command("pkill", "-9", "-f", "krunkit").CombinedOutput(); err == nil {
		killed = true
		_ = out
	}

	// Kill gvproxy (networking helper)
	exec.Command("pkill", "-9", "-f", "gvproxy").CombinedOutput() //nolint:errcheck

	if killed {
		lines = append(lines, tealStyle.Render("✓")+" "+textStyle.Render("Stopped running processes"))
	}

	if !killed {
		lines = append(lines, mutedStyle.Render("  No VM processes found"))
	}

	lines = append(lines, "")

	// Wait briefly for processes to die
	exec.Command("sleep", "2").Run()

	// Restart the machine
	lines = append(lines, textStyle.Render("Starting tainer..."))
	tui.PrintWithLogo(strings.Join(lines, "\n"))

	startCmd := exec.Command("tainer", "machine", "start")
	startCmd.Stdout = nil
	startCmd.Stderr = nil
	if err := startCmd.Run(); err != nil {
		fmt.Println()
		return fmt.Errorf("machine start failed: %w\nTry: tainer machine rm -f tainer-machine-default && tainer machine init", err)
	}

	fmt.Println()
	tui.PrintWithLogo(tealStyle.Render("✓") + " " + textStyle.Render("tainer reset complete") +
		"\n  " + mutedStyle.Render("All projects need to be restarted: tainer start"))

	return nil
}
