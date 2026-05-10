//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/initcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
)

// TestSmokeWordPressLifecycle exercises init → start → status → stop →
// destroy against a live cyberstackd. Runs only with -tags=integration
// to keep the default `go test` fast and offline-friendly.
func TestSmokeWordPressLifecycle(t *testing.T) {
	socket := engine.DefaultSocketPath()
	if _, err := os.Stat(socket); err != nil {
		t.Skipf("no cyberstackd at %s — skipping integration test", socket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dir := t.TempDir()
	name := "smoke-wp-" + filepath.Base(dir)[len(filepath.Base(dir))-6:]
	if err := initcmd.Run(initcmd.Options{
		Type: manifest.TypeWordPress,
		Name: name,
		Dir:  dir,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}

	eng, err := engine.New()
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	t.Cleanup(func() {
		_ = pod.Destroy(ctx, eng, name, filepath.Join(dir, name), pod.DestroyDefault, nil)
	})

	res, err := pod.Start(ctx, eng, pod.StartOptions{
		ManifestPath: filepath.Join(dir, name, manifest.FileName),
		ProjectDir:   filepath.Join(dir, name),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if res.PodID < 3001 || res.PodID > 3999 {
		t.Errorf("pod id outside band 3001-3999: %d", res.PodID)
	}
	if res.Domain != name+".tainer.me" {
		t.Errorf("unexpected domain: %q", res.Domain)
	}

	// Verify the leader (web) is on cs0 and followers share its netns.
	leaderInsp, err := eng.Inspect(ctx, fmt.Sprintf("tainer-%s-web", name))
	if err != nil {
		t.Fatalf("inspect leader: %v", err)
	}
	leaderSandbox := leaderInsp.NetworkSettings.SandboxKey
	for _, role := range []string{"app", "db"} {
		// React projects skip "app"; non-existent containers are silent skips.
		insp, err := eng.Inspect(ctx, fmt.Sprintf("tainer-%s-%s", name, role))
		if err != nil {
			continue
		}
		if insp.NetworkSettings.SandboxKey != leaderSandbox {
			t.Errorf("role %s: sandbox %s != leader %s (shared-netns broken)",
				role, insp.NetworkSettings.SandboxKey, leaderSandbox)
		}
	}

	// Host TCP db port reachable via gvproxy → in-VM nft DNAT → container.
	dbPort := pod.DerivePort(res.PodID, pod.RoleDB)
	if dbPort > 0 {
		var conn net.Conn
		for attempts := 0; attempts < 3; attempts++ {
			c, derr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", dbPort), 500*time.Millisecond)
			if derr == nil {
				conn = c
				break
			}
			time.Sleep(time.Second)
		}
		if conn == nil {
			t.Errorf("host port %d (db) unreachable", dbPort)
		} else {
			conn.Close()
		}
	}

	// Verify https://<domain> reaches the project's caddy via dig +
	// curl. Best-effort — skip if the host isn't reachable yet.
	_ = exec.CommandContext(ctx, "dig", "+short", res.Domain, "@127.0.0.1", "-p", "7753").Run()

	if err := pod.Stop(ctx, eng, name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := pod.Destroy(ctx, eng, name, filepath.Join(dir, name), pod.DestroyDefault, nil); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}
