package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

// provisionRuntimeDir must refresh the user's boot disk when the bundled
// disk changes (a pkg upgrade), leave dev symlinks alone, and provision
// from scratch when nothing is there. Regression: copy-if-missing left
// upgraded machines running the previous release's in-VM agent.
func TestProvisionRuntimeDirRefresh(t *testing.T) {
	prefix := t.TempDir()
	share := filepath.Join(prefix, "share", "cyberstack")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(prefix, "bin", "cyberstackd")
	src := filepath.Join(share, "cyberstack-boot-"+runtime.GOARCH+".img")
	if err := os.WriteFile(src, []byte("bundle-v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	socket := filepath.Join(home, ".cyberstack", "cyberstack.sock")
	boot := filepath.Join(home, ".cyberstack", "boot-"+runtime.GOARCH+".img")

	// 1. Fresh: provisions the disk + provenance marker.
	if err := provisionRuntimeDir(bin, socket); err != nil {
		t.Fatalf("fresh provision: %v", err)
	}
	if got, _ := os.ReadFile(boot); string(got) != "bundle-v1" {
		t.Fatalf("fresh provision content = %q, want bundle-v1", got)
	}

	// 2. Unchanged bundle: no rewrite (simulate user-side drift to detect a copy).
	if err := os.WriteFile(boot, []byte("user-copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := provisionRuntimeDir(bin, socket); err != nil {
		t.Fatalf("steady-state provision: %v", err)
	}
	if got, _ := os.ReadFile(boot); string(got) != "user-copy" {
		t.Errorf("unchanged bundle must not rewrite the disk (got %q)", got)
	}

	// 3. Upgraded bundle (new content, new mtime): must refresh.
	if err := os.WriteFile(src, []byte("bundle-v2!"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if err := provisionRuntimeDir(bin, socket); err != nil {
		t.Fatalf("upgrade provision: %v", err)
	}
	if got, _ := os.ReadFile(boot); string(got) != "bundle-v2!" {
		t.Errorf("upgraded bundle must refresh the disk, got %q", got)
	}

	// 4. Dev symlink: never touched even when the bundle changes again.
	if err := os.Remove(boot); err != nil {
		t.Fatal(err)
	}
	devDisk := filepath.Join(home, "dev-disk.img")
	if err := os.WriteFile(devDisk, []byte("dev"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(devDisk, boot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("bundle-v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := provisionRuntimeDir(bin, socket); err != nil {
		t.Fatalf("symlink provision: %v", err)
	}
	if got, _ := os.ReadFile(devDisk); string(got) != "dev" {
		t.Errorf("dev symlink target must be untouched, got %q", got)
	}
	if fi, _ := os.Lstat(boot); fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("dev symlink must remain a symlink")
	}
}
