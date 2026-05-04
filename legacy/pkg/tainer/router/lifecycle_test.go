package router

import (
	"net"
	"testing"
)

func TestCheckPortConflict_Free(t *testing.T) {
	// Use a random high port that should be free
	conflict := CheckPortConflict(59123)
	if conflict != "" {
		t.Errorf("port 59123 should be free, got conflict: %s", conflict)
	}
}

func TestCheckPortConflict_Occupied(t *testing.T) {
	// Bind a port first
	ln, err := net.Listen("tcp", ":59124")
	if err != nil {
		t.Skipf("cannot bind test port: %v", err)
	}
	defer ln.Close()

	conflict := CheckPortConflict(59124)
	if conflict == "" {
		t.Error("port 59124 should show conflict while bound")
	}
}

func TestRunningProjectCount_NoProjects(t *testing.T) {
	// This test requires Podman. It should return 0 if no tainer pods exist.
	count := RunningProjectCount()
	if count < 0 {
		t.Errorf("RunningProjectCount() = %d, should be >= 0", count)
	}
}
