package engine

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNewWithoutSocketReturnsError(t *testing.T) {
	// Force the SDK at a path that definitely doesn't exist so we exercise
	// the error path. We restore DOCKER_HOST so we don't poison sibling
	// tests in this package.
	prev, hadPrev := os.LookupEnv("DOCKER_HOST")
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("DOCKER_HOST", prev)
		} else {
			_ = os.Unsetenv("DOCKER_HOST")
		}
	})
	_ = os.Setenv("DOCKER_HOST", "unix:///tmp/cyberstack-test-does-not-exist.sock")

	c, err := New()
	if err != nil {
		// SDK construction is lazy — failure here would be unusual but ok.
		return
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); err == nil {
		t.Fatalf("expected ping to fail against missing socket, got nil")
	}
}

// TestPingLiveDaemon dials the default cyberstack socket if present.
// Skipped on machines that don't have cyberstackd running — keeps unit
// tests offline-friendly while giving developers a way to smoke-test
// the wiring.
func TestPingLiveDaemon(t *testing.T) {
	socket := DefaultSocketPath()
	if _, err := os.Stat(socket); err != nil {
		t.Skipf("no cyberstackd at %s — skipping live test", socket)
	}

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping live daemon at %s: %v", socket, err)
	}
}
