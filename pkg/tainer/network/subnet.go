package network

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/containers/podman/v6/pkg/tainer/config"
)

const (
	baseOctet1  = 10
	baseOctet2  = 77
	routerOctet = 0
	firstOctet  = 1
	maxOctet    = 254
)

type networksData struct {
	Allocations map[string]string `json:"allocations"` // project name → subnet CIDR
}

var (
	networks *networksData
	netMu    sync.Mutex
)

func loadNetworks() *networksData {
	if networks != nil {
		return networks
	}
	networks = &networksData{Allocations: make(map[string]string)}
	data, err := os.ReadFile(config.NetworksFile())
	if err != nil {
		return networks
	}
	if err := json.Unmarshal(data, networks); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: corrupted networks.json, starting fresh: %v\n", err)
		networks.Allocations = make(map[string]string)
	}
	if networks.Allocations == nil {
		networks.Allocations = make(map[string]string)
	}
	return networks
}

func saveNetworks() error {
	data, err := json.MarshalIndent(networks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.NetworksFile(), data, 0644)
}

func NetworkName(project string) string {
	return fmt.Sprintf("tainer-net-%s", project)
}

func RouterNetworkName() string {
	return "tainer-net-router"
}

func RouterSubnet() string {
	return fmt.Sprintf("%d.%d.%d.0/24", baseOctet1, baseOctet2, routerOctet)
}

func AllocateSubnet(project string) (string, error) {
	netMu.Lock()
	defer netMu.Unlock()
	n := loadNetworks()

	// Return existing allocation
	if subnet, ok := n.Allocations[project]; ok {
		return subnet, nil
	}

	// Find next free octet
	used := make(map[int]bool)
	for _, subnet := range n.Allocations {
		var o1, o2, o3 int
		fmt.Sscanf(subnet, "%d.%d.%d.0/24", &o1, &o2, &o3)
		used[o3] = true
	}

	for octet := firstOctet; octet <= maxOctet; octet++ {
		if !used[octet] {
			subnet := fmt.Sprintf("%d.%d.%d.0/24", baseOctet1, baseOctet2, octet)
			n.Allocations[project] = subnet
			saveNetworks()
			return subnet, nil
		}
	}

	return "", fmt.Errorf("no free subnets available (max %d projects)", maxOctet)
}

func FreeSubnet(project string) {
	netMu.Lock()
	defer netMu.Unlock()
	n := loadNetworks()
	delete(n.Allocations, project)
	saveNetworks()
}

func GetSubnet(project string) (string, bool) {
	netMu.Lock()
	defer netMu.Unlock()
	n := loadNetworks()
	subnet, ok := n.Allocations[project]
	return subnet, ok
}
