package network

import (
	"fmt"
	"os/exec"
	"strings"
)

// CreateNetwork creates a Podman network with the given name and subnet.
func CreateNetwork(name, subnet string) error {
	cmd := exec.Command("podman", "network", "create", "--subnet", subnet, name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("creating network %s: %s", name, string(output))
	}
	return nil
}

// RemoveNetwork removes a Podman network.
func RemoveNetwork(name string) error {
	cmd := exec.Command("podman", "network", "rm", "-f", name)
	cmd.CombinedOutput()
	return nil
}

// ConnectContainer connects a container to a network.
func ConnectContainer(network, container string) error {
	cmd := exec.Command("podman", "network", "connect", network, container)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already connected") {
			return nil
		}
		return fmt.Errorf("connecting %s to %s: %s", container, network, string(output))
	}
	return nil
}

// DisconnectContainer disconnects a container from a network.
func DisconnectContainer(network, container string) error {
	cmd := exec.Command("podman", "network", "disconnect", network, container)
	cmd.CombinedOutput() // ignore errors (container may not be connected)
	return nil
}

// NetworkExists checks if a Podman network with the given name exists.
func NetworkExists(name string) bool {
	cmd := exec.Command("podman", "network", "exists", name)
	return cmd.Run() == nil
}

// SubnetInUse checks if a subnet is already used by any Podman network.
func SubnetInUse(subnet string) bool {
	cmd := exec.Command("podman", "network", "ls", "--format", "{{.Subnets}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), strings.TrimSuffix(subnet, "/24"))
}
