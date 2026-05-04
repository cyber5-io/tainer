package network

import (
	"context"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// CreateNetwork creates a user-defined bridge with the given subnet.
// Idempotent.
func CreateNetwork(eng *engine.Client, name, subnet string) error {
	return eng.NetworkCreate(context.Background(), name, subnet)
}

// RemoveNetwork removes a user-defined bridge. Idempotent.
func RemoveNetwork(eng *engine.Client, name string) error {
	return eng.NetworkRemove(context.Background(), name)
}

// ConnectContainer attaches a container to a network. Idempotent.
func ConnectContainer(eng *engine.Client, networkName, containerName string) error {
	return eng.NetworkConnect(context.Background(), networkName, containerName)
}

// DisconnectContainer detaches a container from a network. Idempotent.
func DisconnectContainer(eng *engine.Client, networkName, containerName string) error {
	return eng.NetworkDisconnect(context.Background(), networkName, containerName)
}

// NetworkExists reports whether a user-defined bridge with the given
// name exists on the engine.
func NetworkExists(eng *engine.Client, name string) bool {
	exists, err := eng.NetworkExists(context.Background(), name)
	if err != nil {
		return false
	}
	return exists
}

// SubnetInUse reports whether subnet (e.g. "10.42.7.0/24") overlaps with
// any subnet currently configured on the engine. Matching is by CIDR
// stem (preserves the legacy /24-only behaviour).
func SubnetInUse(eng *engine.Client, subnet string) bool {
	in, err := eng.NetworkSubnets(context.Background())
	if err != nil {
		return false
	}
	stem := strings.TrimSuffix(subnet, "/24")
	for _, s := range in {
		if strings.Contains(s, stem) {
			return true
		}
	}
	return false
}
