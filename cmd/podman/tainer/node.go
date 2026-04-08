package tainer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/tui/progress"
	"github.com/spf13/cobra"
)

var nodeCmd = &cobra.Command{
	Use:   "node <dev|prod>",
	Short: "Switch Node.js between development and production mode",
	Long: `Switch the Node.js container between development mode (next dev / npm run dev)
and production mode (next start / npm run start).

This updates the "start" script in package.json and restarts the Node container.`,
	Args: cobra.ExactArgs(1),
	RunE: nodeRun,
}

// reactCmd is an alias of nodeCmd for React projects — same behaviour but
// more discoverable when the project is a SPA.
var reactCmd = &cobra.Command{
	Use:   "react <dev|prod>",
	Short: "Switch React between development and production mode",
	Long: `Switch the React container between development mode (Vite dev server)
and production mode (Caddy serving the static build).`,
	Args: cobra.ExactArgs(1),
	RunE: nodeRun,
}

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: nodeCmd,
	})
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: reactCmd,
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
		return fmt.Errorf("project %q is type %q — mode switch is only available for Node-based projects", name, m.Project.Type)
	}

	podStatus := getPodStatus(name)
	if podStatus != "Running" {
		return fmt.Errorf("project %q is not running (status: %s). Start it first with: tainer start", name, podStatus)
	}

	// React uses a different mode-switch path: no package.json changes,
	// just update the TAINER_MODE env var and recreate the container
	// (Caddy handles dev/prod switching internally).
	if m.IsReact() {
		return reactModeSwitch(name, dir, mode)
	}

	// Determine the start command based on mode and project type
	startCmd := resolveStartCommand(m, mode)

	// Read package.json
	pkgPath := filepath.Join(dir, "html", "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("reading package.json: %w", err)
	}

	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("parsing package.json: %w", err)
	}

	// Update the "start" script
	scripts, ok := pkg["scripts"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("package.json has no scripts section")
	}

	currentStart, _ := scripts["start"].(string)
	if currentStart == startCmd {
		fmt.Printf("✓ %s is already in %s mode\n", name, mode)
		return nil
	}

	scripts["start"] = startCmd

	// Write updated package.json
	updated, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling package.json: %w", err)
	}
	updated = append(updated, '\n')

	if err := os.WriteFile(pkgPath, updated, 0644); err != nil {
		return fmt.Errorf("writing package.json: %w", err)
	}

	// Container name depends on project type (React uses -web-ct, others use -node-ct)
	containerName := fmt.Sprintf("tainer-%s-node-ct", name)

	// Framework-specific build directory to clean
	buildDir := resolveBuildDir(m)

	// Build the steps
	var steps []progress.Step

	if mode == "prod" {
		steps = append(steps, progress.Step{
			Label: "Cleaning build cache",
			Run: func() error {
				cleanCmd := exec.Command("tainer", "exec", "--user", "tainer", containerName, "sh", "-c", "rm -rf /var/www/html/"+buildDir)
				cleanCmd.CombinedOutput() //nolint:errcheck
				return nil
			},
		})
		steps = append(steps, progress.Step{
			Label: "Building for production",
			Run: func() error {
				buildCmd := exec.Command("tainer", "exec", "--user", "tainer", containerName, "sh", "-c", "cd /var/www/html && yarn build")
				buildOutput, err := buildCmd.CombinedOutput()
				if err != nil {
					// Revert package.json
					scripts["start"] = currentStart
					reverted, _ := json.MarshalIndent(pkg, "", "  ")
					reverted = append(reverted, '\n')
					os.WriteFile(pkgPath, reverted, 0644) //nolint:errcheck
					return fmt.Errorf("build failed — reverted to previous mode\n%s", string(buildOutput))
				}
				return nil
			},
		})
	}

	if mode == "dev" {
		steps = append(steps, progress.Step{
			Label: "Cleaning build cache",
			Run: func() error {
				cleanCmd := exec.Command("tainer", "exec", containerName, "sh", "-c", "rm -rf /var/www/html/"+buildDir)
				cleanCmd.CombinedOutput() //nolint:errcheck
				return nil
			},
		})
	}

	steps = append(steps, progress.Step{
		Label: "Restarting container",
		Run: func() error {
			restartCmd := exec.Command("tainer", "restart", containerName)
			if output, err := restartCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("restart failed: %s", string(output))
			}
			return nil
		},
	})

	title := fmt.Sprintf("Switching %s to %s mode", name, mode)
	footer := []string{
		"",
		fmt.Sprintf("✓ %s is now in %s mode", name, mode),
	}

	result, err := progress.Run(title, steps, footer)
	if err != nil {
		return err
	}
	if result.Err != nil {
		return result.Err
	}

	return nil
}

// resolveBuildDir returns the framework-specific build output directory name.
func resolveBuildDir(m *manifest.Manifest) string {
	switch m.Project.Type {
	case manifest.TypeNextJS, manifest.TypeKompozi:
		return ".next"
	case manifest.TypeNuxtJS:
		return ".output"
	case manifest.TypeNestJS:
		return "dist"
	default:
		return ".next"
	}
}

// reactModeSwitch handles dev/prod switching for React projects.
// Unlike Next/Nuxt/Nest, React uses Caddy in all modes — in prod, Caddy
// serves static files directly; in dev, it proxies to Vite. The mode is
// stored in a file inside the container's entrypoint script and Caddy
// reloads its config accordingly. We don't touch package.json.
func reactModeSwitch(name, dir, mode string) error {
	containerName := fmt.Sprintf("tainer-%s-web-ct", name)

	var steps []progress.Step

	if mode == "prod" {
		steps = append(steps, progress.Step{
			Label: "Building for production",
			Run: func() error {
				buildCmd := exec.Command("tainer", "exec", "--user", "tainer", containerName, "sh", "-c", "cd /var/www/html && yarn build")
				output, err := buildCmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("build failed\n%s", string(output))
				}
				return nil
			},
		})
	}

	steps = append(steps, progress.Step{
		Label: "Switching mode",
		Run: func() error {
			// Write mode to a file that the container's entrypoint reads on start
			modeFile := filepath.Join(dir, "html", ".tainer-mode")
			if err := os.WriteFile(modeFile, []byte(mode+"\n"), 0644); err != nil {
				return fmt.Errorf("writing mode file: %w", err)
			}
			return nil
		},
	})

	steps = append(steps, progress.Step{
		Label: "Restarting container",
		Run: func() error {
			restartCmd := exec.Command("tainer", "restart", containerName)
			if output, err := restartCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("restart failed: %s", string(output))
			}
			return nil
		},
	})

	title := fmt.Sprintf("Switching %s to %s mode", name, mode)
	footer := []string{
		"",
		fmt.Sprintf("✓ %s is now in %s mode", name, mode),
	}

	result, err := progress.Run(title, steps, footer)
	if err != nil {
		return err
	}
	return result.Err
}

func resolveStartCommand(m *manifest.Manifest, mode string) string {
	switch mode {
	case "dev":
		switch m.Project.Type {
		case manifest.TypeNextJS, manifest.TypeKompozi:
			return "next dev"
		case manifest.TypeNuxtJS:
			return "nuxt dev"
		case manifest.TypeNestJS:
			return "nest start --watch"
		default:
			return "node index.js"
		}
	case "prod":
		switch m.Project.Type {
		case manifest.TypeNextJS, manifest.TypeKompozi:
			return "next start"
		case manifest.TypeNuxtJS:
			return "nuxt start"
		case manifest.TypeNestJS:
			return "node dist/main"
		default:
			return "node index.js"
		}
	}
	return "node index.js"
}
