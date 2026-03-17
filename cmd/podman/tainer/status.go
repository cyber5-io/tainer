package tainer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [project-name]",
	Short: "Show status of a Tainer project",
	RunE:  statusRun,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: statusCmd,
	})
}

func statusRun(cmd *cobra.Command, args []string) error {
	name, dir, err := resolveProject(args)
	if err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(dir)
	if err != nil {
		return fmt.Errorf("reading tainer.yaml: %w", err)
	}

	podName := fmt.Sprintf("tainer-%s", name)
	podStatus := getPodStatus(name)

	fmt.Printf("\nProject:  %s\n", name)
	fmt.Printf("Type:     %s\n", m.Project.Type)
	fmt.Printf("Domain:   %s\n", m.Project.Domain)
	fmt.Printf("Path:     %s\n", dir)
	fmt.Printf("Pod:      %s (%s)\n\n", podName, podStatus)

	if podStatus == "stopped" {
		fmt.Println("Project is not running. Use 'tainer start' to launch.")
		return nil
	}

	// Show container details
	containers, err := getContainerInfo(podName)
	if err != nil {
		return err
	}

	if len(containers) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "CONTAINER\tSTATUS\tPORTS")
		for _, c := range containers {
			fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, c.status, c.ports)
		}
		w.Flush()
	}

	return nil
}

func resolveProject(args []string) (name, dir string, err error) {
	if len(args) > 0 {
		name = args[0]
		p, ok := projRegistry.Get(name)
		if !ok {
			return "", "", fmt.Errorf("project %q not found in registry", name)
		}
		return name, p.Path, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("getting working directory: %w", err)
	}

	if !manifest.Exists(cwd) {
		return "", "", fmt.Errorf("no tainer.yaml found in current directory.\n  Usage: tainer status [project-name]")
	}

	m, err := manifest.LoadFromDir(cwd)
	if err != nil {
		return "", "", fmt.Errorf("reading tainer.yaml: %w", err)
	}

	return m.Project.Name, cwd, nil
}

type containerInfo struct {
	name   string
	status string
	ports  string
}

func getContainerInfo(podName string) ([]containerInfo, error) {
	cmd := exec.Command("tainer", "ps", "-a",
		"--filter", fmt.Sprintf("pod=%s", podName),
		"--format", "{{.Names}}\t{{.Status}}\t{{.Ports}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listing containers: %s", strings.TrimSpace(string(output)))
	}

	var containers []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		ci := containerInfo{name: parts[0]}
		if len(parts) > 1 {
			ci.status = parts[1]
		}
		if len(parts) > 2 {
			ci.ports = parts[2]
		}
		// Strip pod infra container
		if strings.HasSuffix(ci.name, "-infra") {
			continue
		}
		containers = append(containers, ci)
	}
	return containers, nil
}
