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
// If volumes is true, also removes db/ and data/ directories.
func Destroy(projectDir string, force bool, volumes ...bool) error {
	if err := machine.EnsureRunning(); err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	removeVolumes := len(volumes) > 0 && volumes[0]

	if !force {
		msg := fmt.Sprintf("Destroy project %s? This will stop all containers.", m.Project.Name)
		if removeVolumes {
			msg += " Data and database directories will be PERMANENTLY DELETED."
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

	// 9. Remove volumes if requested
	if removeVolumes {
		dbDir := filepath.Join(projectDir, "db")
		if err := os.RemoveAll(dbDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", dbDir, err)
		}
		dataDir := filepath.Join(projectDir, "data")
		if err := os.RemoveAll(dataDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", dataDir, err)
		}
		fmt.Println("Removed db/ and data/ directories.")
	}

	// Clean up local state files
	os.Remove(filepath.Join(projectDir, ".tainer.local.yaml"))
	os.Remove(filepath.Join(projectDir, ".tainer-authorized_keys"))

	fmt.Printf("%s destroyed. Project files preserved.\n", m.Project.Name)
	return nil
}
