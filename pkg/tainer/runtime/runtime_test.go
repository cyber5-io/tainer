package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentStatusMissingSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "absent.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	st := CurrentStatus(ctx, Options{SocketPath: socket})
	if st.Reachable {
		t.Fatalf("expected unreachable for missing socket, got reachable=true")
	}
	if st.Running {
		t.Fatalf("expected not-running for missing socket, got running=true")
	}
	if st.Socket != socket {
		t.Fatalf("socket path: want %s, got %s", socket, st.Socket)
	}
}

func TestEngineMissingSocketNoAutoStart(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "absent.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cli, err := Engine(ctx, Options{SocketPath: socket, AutoStart: false})
	if err == nil {
		_ = cli.Close()
		t.Fatalf("expected error for missing socket with autostart=false, got nil")
	}
}

func TestResolveSocketPrefersExplicitOpt(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/from-env.sock")
	got := resolveSocket(Options{SocketPath: "/tmp/explicit.sock"})
	if got != "/tmp/explicit.sock" {
		t.Fatalf("want explicit override, got %s", got)
	}
}

func TestResolveSocketFromDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/from-env.sock")
	got := resolveSocket(Options{})
	if got != "/tmp/from-env.sock" {
		t.Fatalf("want DOCKER_HOST resolution, got %s", got)
	}
}
