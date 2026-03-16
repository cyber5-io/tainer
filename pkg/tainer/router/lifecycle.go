package router

import (
	"fmt"
	"net"
	"os/exec"
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

// IsRouterRunning checks if the router pod exists and is running.
func IsRouterRunning() bool {
	cmd := exec.Command("tainer", "pod", "exists", RouterPodName)
	return cmd.Run() == nil
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

	// Check port conflicts
	for _, port := range []int{80, 443, 2222} {
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
		"-p", "2222:2222",
		"-p", "127.0.0.1:7753:53/udp",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating router pod: %s", string(output))
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

	// Start sshpiper container
	sshpiperCmd := exec.Command("tainer", "run", "-d",
		"--pod", RouterPodName,
		"--name", SSHPiperContainer,
		"-v", config.SSHPiperDir()+":/var/sshpiper:rw",
		"farmer1992/sshpiperd:latest",
		"/sshpiperd", "daemon", "--workingdir", "/var/sshpiper",
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

// SSHPort returns the SSH port the router is listening on.
// Prefers port 22 if available, falls back to 2222.
func SSHPort() int {
	if CheckPortConflict(22) == "" {
		return 22
	}
	return 2222
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
