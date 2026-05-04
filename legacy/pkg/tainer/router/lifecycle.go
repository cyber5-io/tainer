package router

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/network"
)

const (
	RouterPodName      = "tainer-router"
	CaddyContainerName = "tainer-router-caddy"
	SSHPiperContainer  = "tainer-router-sshpiper"
	DnsmasqContainer   = "tainer-router-dnsmasq"
)

// IsRouterRunning checks if the router pod is actually running (not just created/exited).
func IsRouterRunning() bool {
	cmd := exec.Command("tainer", "pod", "inspect", "--format", "{{.State}}", RouterPodName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "Running"
}

// cleanStaleRouter removes a router pod that exists but isn't running.
func cleanStaleRouter() {
	if IsRouterRunning() {
		return
	}
	// If pod exists but isn't running, remove it
	cmd := exec.Command("tainer", "pod", "exists", RouterPodName)
	if cmd.Run() == nil {
		exec.Command("tainer", "pod", "rm", "-f", RouterPodName).CombinedOutput()
	}
}

// RunningProjectCount returns how many Tainer project pods are currently running.
func RunningProjectCount() int {
	cmd := exec.Command("tainer", "pod", "ls",
		"--filter", "label=tainer.project",
		"--filter", "status=running",
		"--format", "{{.Name}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// CheckPortConflict checks if a port is already in use on the host.
// Returns the process name using the port, or empty string if free.
func CheckPortConflict(port int) string {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err.Error()
	}
	ln.Close()
	return ""
}

// StartRouter creates and starts the router pod with Caddy, sshpiper, and dnsmasq.
func StartRouter() error {
	if IsRouterRunning() {
		return nil
	}
	cleanStaleRouter()

	// Check port conflicts using the actual bind port
	sshBindPort := SSHBindPort()
	for _, port := range []int{80, 443, sshBindPort} {
		if conflict := CheckPortConflict(port); conflict != "" {
			return fmt.Errorf("port %d is already in use: %s", port, conflict)
		}
	}

	// Ensure router network exists
	subnet := network.RouterSubnet()
	netName := network.RouterNetworkName()
	if err := network.CreateNetwork(netName, subnet); err != nil {
		return fmt.Errorf("creating router network: %w", err)
	}

	// Ensure configs exist
	WriteDnsmasqConf(config.DnsmasqConf())

	// Create router pod
	cmd := exec.Command("tainer", "pod", "create",
		"--name", RouterPodName,
		"--network", netName,
		"-p", "80:80",
		"-p", "443:443",
		"-p", fmt.Sprintf("%d:2222", sshBindPort),
		"-p", "127.0.0.1:7753:53/udp",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating router pod: %s", string(output))
	}

	// Ensure the pod's infra container is running before adding containers
	startPod := exec.Command("tainer", "pod", "start", RouterPodName)
	if output, err := startPod.CombinedOutput(); err != nil {
		return fmt.Errorf("starting router pod: %s", string(output))
	}

	// Start Caddy container
	caddyCmd := exec.Command("tainer", "run", "-d",
		"--pod", RouterPodName,
		"--name", CaddyContainerName,
		"-v", config.CaddyfilePath()+":/etc/caddy/Caddyfile:ro",
		"-v", config.CertFile()+":/certs/tainer.me.crt:ro",
		"-v", config.KeyFile()+":/certs/tainer.me.key:ro",
		"caddy:2-alpine",
	)
	if output, err := caddyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting Caddy: %s", string(output))
	}

	// Start sshpiper container (workingdir plugin reads sshpiper_upstream files)
	sshpiperCmd := exec.Command("tainer", "run", "-d",
		"--pod", RouterPodName,
		"--name", SSHPiperContainer,
		"-v", config.SSHPiperDir()+":/var/sshpiper:rw",
		"-v", config.SSHPiperHostKey()+":/etc/ssh/ssh_host_ed25519_key:ro",
		"farmer1992/sshpiperd:latest",
		"/sshpiperd/plugins/workingdir", "--root", "/var/sshpiper", "--no-check-perm", "--allow-baduser-name",
	)
	if output, err := sshpiperCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting sshpiper: %s", string(output))
	}

	// Start dnsmasq container
	dnsmasqCmd := exec.Command("tainer", "run", "-d",
		"--pod", RouterPodName,
		"--name", DnsmasqContainer,
		"-v", config.DnsmasqConf()+":/etc/dnsmasq.conf:ro",
		"--cap-add=NET_ADMIN",
		"drpsychick/dnsmasq:latest",
	)
	if output, err := dnsmasqCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting dnsmasq: %s", string(output))
	}

	return nil
}

// StopRouter stops and removes the router pod.
func StopRouter() error {
	cmd := exec.Command("tainer", "pod", "rm", "-f", RouterPodName)
	cmd.CombinedOutput()
	return nil
}

// routerInfraContainer returns the infra container ID for the router pod.
// Pod containers share the infra container's network namespace, so network
// connections must target the infra container.
func routerInfraContainer() (string, error) {
	cmd := exec.Command("tainer", "pod", "inspect", RouterPodName, "--format", "{{.InfraContainerID}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("getting router infra container: %s", string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// SSHBindPort returns the port Podman actually binds on the host.
// On macOS, always 2222 (gvproxy can't bind privileged ports).
// On Linux, prefers 22 if available, falls back to 2222.
func SSHBindPort() int {
	if runtime.GOOS == "darwin" {
		return 2222
	}
	if CheckPortConflict(22) == "" {
		return 22
	}
	return 2222
}

// SSHPort returns the port users should connect to.
// On macOS with pf redirect (22→2222), returns 22.
// Otherwise returns the bind port.
func SSHPort() int {
	if runtime.GOOS == "darwin" && pfRedirectActive() {
		return 22
	}
	return SSHBindPort()
}

// pfRedirectActive checks if the pf anchor file for tainer exists.
func pfRedirectActive() bool {
	_, err := os.Stat("/etc/pf.anchors/tainer")
	return err == nil
}

// ConnectToProjectNetwork connects the router pod to a project's network
// so Caddy and sshpiper can reach the project pod.
func ConnectToProjectNetwork(projectNetworkName string) error {
	infraID, err := routerInfraContainer()
	if err != nil {
		return err
	}
	return network.ConnectContainer(projectNetworkName, infraID)
}

// DisconnectFromProjectNetwork disconnects the router pod from a project network.
func DisconnectFromProjectNetwork(projectNetworkName string) {
	infraID, err := routerInfraContainer()
	if err != nil {
		return
	}
	network.DisconnectContainer(projectNetworkName, infraID)
}
