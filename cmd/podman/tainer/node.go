package tainer

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/spf13/cobra"
)

var nodeCmd = &cobra.Command{
	Use:   "node <dev|prod>",
	Short: "Switch Node.js between development and production mode",
	Long: `Switch the Node.js container between development mode (next dev / npm run dev)
and production mode (next start / npm run start).

After switching, the node container is automatically restarted.`,
	Args: cobra.ExactArgs(1),
	RunE: nodeRun,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: nodeCmd,
	})
}

func nodeRun(cmd *cobra.Command, args []string) error {
	mode := args[0]
	if mode != "dev" && mode != "prod" {
		return fmt.Errorf("invalid mode %q — use 'dev' or 'prod'", mode)
	}

	name, dir, err := resolveProject(nil)
	if err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(dir)
	if err != nil {
		return fmt.Errorf("reading tainer.yaml: %w", err)
	}

	if !m.IsNode() {
		return fmt.Errorf("project %q is type %q — node mode switch is only available for Node.js projects", name, m.Project.Type)
	}

	podStatus := getPodStatus(name)
	if podStatus != "Running" {
		return fmt.Errorf("project %q is not running (status: %s). Start it first with: tainer start", name, podStatus)
	}

	containerName := fmt.Sprintf("tainer-%s-node-ct", name)

	// Determine the command to run based on mode and project type
	var startCmd string
	switch mode {
	case "dev":
		startCmd = resolveDevCommand(m)
	case "prod":
		startCmd = resolveProdCommand(m)
	}

	// Kill the current Node.js process inside the container
	fmt.Printf("Switching %s to %s mode...\n", name, mode)

	killCmd := exec.Command("tainer", "exec", containerName, "sh", "-c", "pkill -f 'node|next' 2>/dev/null; sleep 1")
	killCmd.Stdout = os.Stdout
	killCmd.Stderr = os.Stderr
	_ = killCmd.Run()

	// Start the new process in the background
	execCmd := exec.Command("tainer", "exec", "-d", containerName,
		"sh", "-c", fmt.Sprintf("cd /var/www/html && %s", startCmd))
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("failed to start node in %s mode: %w", mode, err)
	}

	fmt.Printf("✓ %s is now running in %s mode\n", name, mode)
	if mode == "prod" {
		fmt.Println("  Tip: run 'tainer yarn build' first if you haven't already")
	}
	return nil
}

func resolveDevCommand(m *manifest.Manifest) string {
	switch m.Project.Type {
	case manifest.TypeNextJS, manifest.TypeKompozi:
		return "yarn dev"
	case manifest.TypeNuxtJS:
		return "yarn dev"
	default:
		return "yarn dev"
	}
}

func resolveProdCommand(m *manifest.Manifest) string {
	switch m.Project.Type {
	case manifest.TypeNextJS, manifest.TypeKompozi:
		return "yarn start"
	case manifest.TypeNuxtJS:
		return "yarn start"
	default:
		return "yarn start"
	}
}
