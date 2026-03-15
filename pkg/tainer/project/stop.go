package project

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/network"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
)

// Stop executes the full tainer stop flow for a project by name.
func Stop(projectName string) error {
	// 1. Read domain from registry (not from tainer.yaml — handles domain changes)
	_, ok := projRegistry.Get(projectName)
	if !ok {
		return fmt.Errorf("project %q not found in registry", projectName)
	}

	podName := fmt.Sprintf("tainer-%s", projectName)
	netName := network.NetworkName(projectName)

	// 2. Stop project pod
	stopCmd := exec.Command("tainer", "pod", "stop", podName)
	stopCmd.CombinedOutput() // ignore error if already stopped

	// 3. Remove project pod (preserves volumes)
	rmCmd := exec.Command("tainer", "pod", "rm", "-f", podName)
	rmCmd.CombinedOutput()

	// 4. Update router config
	router.RemoveSSHPiperEntry(config.SSHPiperDir(), projectName)

	// 5. Disconnect router from project network
	router.DisconnectFromProjectNetwork(netName)

	// 6. Update Caddyfile and reload
	if err := updateRouterConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update router config: %v\n", err)
	}

	// 7. If no other projects running, stop router
	if router.RunningProjectCount() == 0 {
		router.StopRouter()
	}

	fmt.Printf("\n%s stopped\n", projectName)

	return nil
}
