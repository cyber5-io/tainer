package network

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestNetwork(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "tainer"), 0755)
	networks = nil
	return dir
}

func TestAllocateSubnet(t *testing.T) {
	setupTestNetwork(t)
	subnet, err := AllocateSubnet("my-client")
	if err != nil {
		t.Fatalf("AllocateSubnet() error: %v", err)
	}
	if subnet != "10.77.1.0/24" {
		t.Errorf("first subnet = %q, want %q", subnet, "10.77.1.0/24")
	}
}

func TestAllocateSubnet_Sequential(t *testing.T) {
	setupTestNetwork(t)
	AllocateSubnet("a")
	subnet, _ := AllocateSubnet("b")
	if subnet != "10.77.2.0/24" {
		t.Errorf("second subnet = %q, want %q", subnet, "10.77.2.0/24")
	}
}

func TestAllocateSubnet_Duplicate(t *testing.T) {
	setupTestNetwork(t)
	s1, _ := AllocateSubnet("my-client")
	s2, _ := AllocateSubnet("my-client")
	if s1 != s2 {
		t.Errorf("duplicate allocation should return same subnet: %q vs %q", s1, s2)
	}
}

func TestFreeSubnet(t *testing.T) {
	setupTestNetwork(t)
	AllocateSubnet("my-client")
	FreeSubnet("my-client")
	subnet, _ := AllocateSubnet("other")
	if subnet != "10.77.1.0/24" {
		t.Errorf("after free, next subnet = %q, want %q", subnet, "10.77.1.0/24")
	}
}

func TestGetSubnet(t *testing.T) {
	setupTestNetwork(t)
	AllocateSubnet("my-client")
	subnet, ok := GetSubnet("my-client")
	if !ok {
		t.Fatal("GetSubnet() should return true for allocated project")
	}
	if subnet != "10.77.1.0/24" {
		t.Errorf("GetSubnet() = %q, want %q", subnet, "10.77.1.0/24")
	}
}

func TestGetSubnet_NotFound(t *testing.T) {
	setupTestNetwork(t)
	_, ok := GetSubnet("nonexistent")
	if ok {
		t.Error("GetSubnet() should return false for unallocated project")
	}
}

func TestNetworkName(t *testing.T) {
	if got := NetworkName("my-client"); got != "tainer-net-my-client" {
		t.Errorf("NetworkName() = %q, want %q", got, "tainer-net-my-client")
	}
}
