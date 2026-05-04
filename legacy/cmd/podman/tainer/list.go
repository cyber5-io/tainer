package tainer

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/cmd/podman/registry"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	tuiList "github.com/containers/podman/v6/pkg/tainer/tui/list"
	"github.com/spf13/cobra"
)

var (
	listRaw bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Tainer projects and their status",
	RunE:  listRun,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: listCmd,
	})
	listCmd.Flags().BoolVar(&listRaw, "raw", false, "Plain text output (no TUI)")
}

func listRun(cmd *cobra.Command, args []string) error {

	// Self-heal: prune stale entries
	pruned := projRegistry.SelfHeal()
	if len(pruned) > 0 {
		c := tui.Colors()
		warnStyle := lipgloss.NewStyle().Foreground(c.Orange)
		mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
		for _, name := range pruned {
			fmt.Fprintf(os.Stderr, "  %s %s\n", warnStyle.Render("!"), mutedStyle.Render("Pruned stale project "+name))
		}
	}

	projects := projRegistry.All()

	if len(projects) == 0 {
		content := tui.SubtitleStyle().Render("No projects registered. Run 'tainer init' to get started.")
		tui.PrintWithLogo(content)
		return nil
	}

	// Sort by name
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build project list (status loaded async in TUI)
	tuiProjects := make([]tuiList.Project, len(names))
	for i, name := range names {
		p := projects[name]
		tuiProjects[i] = tuiList.Project{
			Name:   name,
			Type:   p.Type,
			Domain: p.Domain,
			Path:   p.Path,
		}
	}

	if listRaw {
		// Plain text — fetch statuses synchronously
		fmt.Printf("%-20s %-16s %-30s %s\n", "NAME", "TYPE", "DOMAIN", "STATUS")
		fmt.Printf("%-20s %-16s %-30s %s\n", "----", "----", "------", "------")
		for _, p := range tuiProjects {
			status := getPodStatus(p.Name)
			if status == "" {
				status = "stopped"
			}
			fmt.Printf("%-20s %-16s %-30s %s\n", p.Name, p.Type, p.Domain, status)
		}
		return nil
	}

	// Fetch function — called async inside the TUI
	fetchFunc := func(projectNames []string) (map[string]string, bool, int) {
		statuses := make(map[string]string, len(projectNames))
		for _, name := range projectNames {
			statuses[name] = getPodStatus(name)
		}
		routerRunning := router.IsRouterRunning()
		routerCount := 0
		if routerRunning {
			routerCount = router.RunningProjectCount()
		}
		return statuses, routerRunning, routerCount
	}

	_, err := tuiList.Run(tuiProjects, fetchFunc)
	return err
}

func getPodStatus(projectName string) string {
	cmd := exec.Command("tainer", "pod", "inspect",
		"--format", "{{.State}}", fmt.Sprintf("tainer-%s", projectName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped"
	}
	return strings.TrimSpace(string(output))
}
