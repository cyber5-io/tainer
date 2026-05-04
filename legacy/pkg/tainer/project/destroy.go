package project

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/machine"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/network"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
)

// Destroy removes all Tainer resources for a project without deleting project files.
// If nuke is true, also removes the entire project directory contents.
func Destroy(projectDir string, force, nuke bool) error {
	if err := machine.EnsureRunning(); err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	if !force {
		msg := fmt.Sprintf("Destroy project %s? This will stop all containers.", m.Project.Name)
		if nuke {
			msg = fmt.Sprintf("NUKE project %s? This will stop all containers and DELETE ALL PROJECT FILES.", m.Project.Name)
		}
		fmt.Printf("%s [y/N] ", msg)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	podName := fmt.Sprintf("tainer-%s", m.Project.Name)
	netName := network.NetworkName(m.Project.Name)

	// 1-2. Stop and remove pod (then prune anonymous volumes)
	exec.Command("tainer", "pod", "stop", podName).CombinedOutput()
	exec.Command("tainer", "pod", "rm", "-f", podName).CombinedOutput()
	exec.Command("tainer", "volume", "prune", "-f").CombinedOutput()

	// 3. Disconnect router (warn on error)
	if router.IsRouterRunning() {
		router.DisconnectFromProjectNetwork(netName)
	}

	// 4. Remove project network
	if err := network.RemoveNetwork(netName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not remove network %s: %v\n", netName, err)
	}

	// 5. Remove sshpiper entry
	router.RemoveSSHPiperEntry(config.SSHPiperDir(), m.Project.Name)

	// 6. Remove from registry
	projRegistry.Remove(m.Project.Name)

	// 7. Release subnet
	network.FreeSubnet(m.Project.Name)

	// 8. Update Caddy config and reload, or stop router if no projects remain
	if router.IsRouterRunning() {
		if router.RunningProjectCount() == 0 {
			if err := router.StopRouter(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not stop router: %v\n", err)
			}
		} else {
			if err := updateRouterConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update router config: %v\n", err)
			}
		}
	}

	if nuke {
		// Remove everything in the project directory
		entries, err := os.ReadDir(projectDir)
		if err == nil {
			for _, entry := range entries {
				os.RemoveAll(filepath.Join(projectDir, entry.Name()))
			}
		}
		fmt.Printf("%s nuked.\n", m.Project.Name)
	} else {
		// Clean up local state files only
		os.Remove(filepath.Join(projectDir, ".tainer.local.yaml"))
		os.Remove(filepath.Join(projectDir, ".tainer-authorized_keys"))
		fmt.Printf("%s destroyed. Project files preserved.\n", m.Project.Name)
	}

	return nil
}
