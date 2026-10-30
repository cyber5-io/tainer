package router

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// ReloadCaddy must be a silent no-op when the router container is not
// running (or not reachable): the cert refresher calls it after
// installing files, and an install with no live router is a success.
func TestReloadCaddySkipsWhenRouterDown(t *testing.T) {
	prev, hadPrev := os.LookupEnv("DOCKER_HOST")
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("DOCKER_HOST", prev)
		} else {
			_ = os.Unsetenv("DOCKER_HOST")
		}
	})
	_ = os.Setenv("DOCKER_HOST", "unix:///tmp/cyberstack-test-does-not-exist.sock")

	cli, err := engine.New()
	if err != nil {
		t.Skipf("engine.New: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ReloadCaddy(ctx, cli); err != nil {
		t.Errorf("ReloadCaddy with unreachable engine = %v, want nil (skip)", err)
	}
}
