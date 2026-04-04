package tainer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/spf13/cobra"
)

var yarnCmd = &cobra.Command{
	Use:                "yarn [args...]",
	Short:              "Run yarn in the Node container",
	Long:               "Execute yarn commands inside the project's Node.js container from the /var/www/html directory.",
	DisableFlagParsing: true,
	RunE:               runPackageManager("yarn"),
}

var npmCmd = &cobra.Command{
	Use:                "npm [args...]",
	Short:              "Run npm in the Node container",
	Long:               "Execute npm commands inside the project's Node.js container from the /var/www/html directory.",
	DisableFlagParsing: true,
	RunE:               runPackageManager("npm"),
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: yarnCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: npmCmd,
	})
}

func runPackageManager(pm string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		name, dir, err := resolveProject(nil)
		if err != nil {
			return err
		}

		m, err := manifest.LoadFromDir(dir)
		if err != nil {
			return fmt.Errorf("reading tainer.yaml: %w", err)
		}

		if !m.IsNode() {
			return fmt.Errorf("project %q is type %q — %s is only available for Node.js projects", name, m.Project.Type, pm)
		}

		podStatus := getPodStatus(name)
		if podStatus != "Running" {
			return fmt.Errorf("project %q is not running (status: %s). Start it first with: tainer start", name, podStatus)
		}

		// Intercept conflicting commands
		if err := interceptConflicting(pm, args); err != nil {
			return err
		}

		containerName := fmt.Sprintf("tainer-%s-node-ct", name)

		execArgs := []string{"exec", "-w", "/var/www/html", containerName, pm}
		execArgs = append(execArgs, args...)

		execCmd := exec.Command("tainer", execArgs...)
		execCmd.Stdin = os.Stdin
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		return execCmd.Run()
	}
}

// interceptConflicting checks for commands that would conflict with the running
// Node process and returns a helpful error instead of letting them fail.
func interceptConflicting(pm string, args []string) error {
	if len(args) == 0 {
		return nil
	}

	cmd := strings.Join(args, " ")

	// yarn start / npm start — conflicts with running entrypoint
	if args[0] == "start" {
		return tui.StyledError(fmt.Sprintf(
			"Cannot run '%s start' — the Node process is already running.\n"+
				"Use 'tainer node dev' or 'tainer node prod' to switch modes.", pm))
	}

	// yarn dev / npm run dev — should use tainer node dev
	if args[0] == "dev" || cmd == "run dev" {
		return tui.StyledError(fmt.Sprintf(
			"Cannot run '%s %s' — the Node process is already running.\n"+
				"Use 'tainer node dev' instead.", pm, cmd))
	}

	// yarn run start / npm run start
	if cmd == "run start" {
		return tui.StyledError(fmt.Sprintf(
			"Cannot run '%s run start' — the Node process is already running.\n"+
				"Use 'tainer node dev' or 'tainer node prod' to switch modes.", pm))
	}

	return nil
}
