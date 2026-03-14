package tainer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/containers/podman/v6/cmd/podman/registry"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
	"github.com/spf13/cobra"
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
}

func listRun(cmd *cobra.Command, args []string) error {
	// Self-heal: prune stale entries
	pruned := projRegistry.SelfHeal()
	for _, name := range pruned {
		fmt.Fprintf(os.Stderr, "Warning: pruned stale project %q (path no longer exists)\n", name)
	}

	projects := projRegistry.All()
	if len(projects) == 0 {
		fmt.Println("\nNo projects registered. Run 'tainer init' in a project directory to get started.")
		return nil
	}

	// Router status
	if router.IsRouterRunning() {
		count := router.RunningProjectCount()
		fmt.Printf("\nROUTER          STATUS\n")
		fmt.Printf("tainer-router   running (%d projects)\n\n", count)
	} else {
		fmt.Printf("\nROUTER          STATUS\n")
		fmt.Printf("tainer-router   stopped\n\n")
	}

	// Project table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tDOMAIN\tSTATUS\tPATH")
	for name, p := range projects {
		status := getPodStatus(name)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, p.Type, p.Domain, status, p.Path)
	}
	w.Flush()

	return nil
}

func getPodStatus(projectName string) string {
	cmd := exec.Command("podman", "pod", "inspect",
		"--format", "{{.State}}", fmt.Sprintf("tainer-%s", projectName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped"
	}
	return strings.TrimSpace(string(output))
}
