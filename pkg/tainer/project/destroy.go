package project

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/network"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
)

// Destroy removes all Tainer resources for a project without deleting project files.
func Destroy(projectDir string, force bool) error {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	if !force {
		fmt.Printf("Destroy project %s? This will stop all containers. [y/N] ", m.Project.Name)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	podName := fmt.Sprintf("tainer-%s", m.Project.Name)
	netName := network.NetworkName(m.Project.Name)

	// 1-2. Stop and remove pod
	exec.Command("podman", "pod", "stop", podName).CombinedOutput()
	exec.Command("podman", "pod", "rm", "-f", podName).CombinedOutput()

	// 3. Disconnect router (skip silently if not running)
	if router.IsRouterRunning() {
		router.DisconnectFromProjectNetwork(netName)
	}

	// 4. Remove project network
	network.RemoveNetwork(netName)

	// 5. Remove sshpiper entry
	router.RemoveSSHPiperEntry(config.SSHPiperDir(), m.Project.Name)

	// 6. Remove from registry
	projRegistry.Remove(m.Project.Name)

	// 7. Release subnet
	network.FreeSubnet(m.Project.Name)

	// 8. Update Caddy config and reload (skip if router not running)
	if router.IsRouterRunning() {
		updateRouterConfig()
	}

	// Clean up local state files
	os.Remove(filepath.Join(projectDir, ".tainer.local.yaml"))
	os.Remove(filepath.Join(projectDir, ".tainer-authorized_keys"))

	fmt.Printf("%s destroyed. Project files preserved.\n", m.Project.Name)
	return nil
}
