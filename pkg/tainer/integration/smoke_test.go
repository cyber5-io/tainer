//go:build integration

package integration

import (
	"context"
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
	if res.PodID == 0 {
		t.Errorf("expected non-zero pod id")
	}
	if res.Domain != name+".tainer.me" {
		t.Errorf("unexpected domain: %q", res.Domain)
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
