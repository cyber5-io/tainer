package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

// fakeControlUpdate is an in-memory runtime and registry for updateProject.
type fakeControlUpdate struct {
	state    string // what the stable channel check answers
	checkErr error
	running  string // version the check was asked about
	reg      map[string]registry.Project
	pulled   []string
	updates  []pod.StartOptions
	// versionAtUpdate is the registry's ImageVersion when the pod was
	// recreated, i.e. the version the new containers resolve to.
	versionAtUpdate string
	log             []string
	updateErr       error
}

func (f *fakeControlUpdate) ops() controlUpdateOps {
	return controlUpdateOps{
		check: func(_ context.Context, running string) (imagecheck.ControlCheck, error) {
			f.running = running
			f.log = append(f.log, "check")
			if f.checkErr != nil {
				return imagecheck.ControlCheck{}, f.checkErr
			}
			return imagecheck.ControlCheck{Running: running, Stable: "1.2.0", MinTainer: "1.1.0", State: f.state}, nil
		},
		pull: func(_ context.Context, ref string) error {
			f.log = append(f.log, "pull")
			f.pulled = append(f.pulled, ref)
			return nil
		},
		update: func(_ context.Context, opts pod.StartOptions) (pod.UpdateResult, error) {
			f.log = append(f.log, "update")
			f.updates = append(f.updates, opts)
			f.versionAtUpdate = f.reg["control"].ImageVersion
			if f.updateErr != nil {
				return pod.UpdateResult{}, f.updateErr
			}
			return pod.UpdateResult{Started: true}, nil
		},
		lock: func(context.Context) (func(), error) {
			f.log = append(f.log, "lock")
			return func() { f.log = append(f.log, "unlock") }, nil
		},
		regGet: func(name string) (registry.Project, bool) { p, ok := f.reg[name]; return p, ok },
		regPut: func(name string, p registry.Project) error { f.reg[name] = p; return nil },
	}
}

// controlFolder writes a control folder with dashboard data under a temp
// config dir and registers it.
func controlFolder(t *testing.T, f *fakeControlUpdate, recorded string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := config.ControlDir()
	if err := os.MkdirAll(filepath.Join(dir, "data", "thumbnails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), pod.ControlManifestYAML(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "settings.json"), []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f.reg = map[string]registry.Project{"control": {Path: dir, Type: "nodejs", Domain: "my.tainer.me", Created: "2026-01-01T00:00:00Z", ImageVersion: recorded}}
	return dir
}

func TestControlUpdateKeepsDataAndRecordsTheVersion(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlUpdate}
	dir := controlFolder(t, f, "1.1.0")

	res, note, err := updateProject(context.Background(), "control", "/elsewhere/tainer.yaml", "/elsewhere", f.ops())
	if err != nil || !res.Started || note != "" {
		t.Fatalf("updateProject = %+v, %q, %v", res, note, err)
	}
	if f.running != "1.1.0" {
		t.Errorf("checked running version %q, want the recorded 1.1.0", f.running)
	}
	if strings.Join(f.log, ",") != "lock,check,pull,update,unlock" {
		t.Fatalf("order = %v, want the new image pulled before the pod is recreated, all under the ensure lock", f.log)
	}
	if len(f.pulled) != 1 || f.pulled[0] != "ghcr.io/cyber5-io/tainer-control:1.2.0" {
		t.Fatalf("pulled %v, want the versioned stable image", f.pulled)
	}
	if f.versionAtUpdate != "1.2.0" || f.reg["control"].ImageVersion != "1.2.0" {
		t.Fatalf("recorded version at recreate %q, after %q; want 1.2.0", f.versionAtUpdate, f.reg["control"].ImageVersion)
	}
	if p := f.reg["control"]; p.Path != dir || p.Created != "2026-01-01T00:00:00Z" || p.Domain != "my.tainer.me" {
		t.Errorf("registry entry changed beyond the version: %+v", p)
	}
	want := pod.StartOptions{ManifestPath: filepath.Join(dir, manifest.FileName), ProjectDir: dir}
	if len(f.updates) != 1 || f.updates[0] != want {
		t.Fatalf("recreated with %+v, want the built-in folder %+v", f.updates, want)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "data", "settings.json")); err != nil || string(b) != `{"theme":"dark"}` {
		t.Fatalf("data/settings.json = %q, %v", b, err)
	}
	if st, err := os.Stat(filepath.Join(dir, "data", "thumbnails")); err != nil || !st.IsDir() {
		t.Fatalf("data/thumbnails lost: %v", err)
	}
}

func TestControlUpdateHeldBackNeedsNewerTainer(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlNeedsNewerTainer}
	controlFolder(t, f, "1.1.0")
	res, note, err := updateProject(context.Background(), "control", "", "", f.ops())
	if err != nil || !res.Started {
		t.Fatalf("updateProject = %+v, %v", res, err)
	}
	if len(f.pulled) != 0 || f.reg["control"].ImageVersion != "1.1.0" {
		t.Fatalf("held image was pulled or recorded: pulled %v, version %q", f.pulled, f.reg["control"].ImageVersion)
	}
	if !strings.Contains(note, "1.2.0") || !strings.Contains(note, "needs a newer tainer") {
		t.Errorf("note = %q", note)
	}
	for _, banned := range []string{"podman", "docker", "engine", "VM", "container"} {
		if strings.Contains(note, banned) {
			t.Errorf("note %q mentions %q", note, banned)
		}
	}
	if len(f.updates) != 1 {
		t.Errorf("the pod's other parts should still refresh; updates = %d", len(f.updates))
	}
}

func TestControlUpdateUpToDateKeepsVersion(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlUpToDate}
	controlFolder(t, f, "1.3.0")
	if _, _, err := updateProject(context.Background(), "control", "", "", f.ops()); err != nil {
		t.Fatal(err)
	}
	if len(f.pulled) != 0 || f.reg["control"].ImageVersion != "1.3.0" {
		t.Fatalf("up-to-date pod moved: pulled %v, version %q", f.pulled, f.reg["control"].ImageVersion)
	}
}

func TestControlUpdateCheckFailureStopsTheUpdate(t *testing.T) {
	f := &fakeControlUpdate{checkErr: errors.New("offline")}
	controlFolder(t, f, "")
	_, _, err := updateProject(context.Background(), "control", "", "", f.ops())
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("err = %v", err)
	}
	if len(f.updates) != 0 || len(f.pulled) != 0 {
		t.Fatalf("nothing should run after a failed check: %v", f.log)
	}
}

func TestOtherProjectsUpdateWithoutControlCheck(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlUpdate, reg: map[string]registry.Project{}}
	_, _, err := updateProject(context.Background(), "blog", "/p/blog/tainer.yaml", "/p/blog", f.ops())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.log, ",") != "update" || f.updates[0].ProjectDir != "/p/blog" {
		t.Fatalf("log = %v updates = %+v", f.log, f.updates)
	}
}

func TestControlUpdateFailureRestoresTheVersion(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlUpdate, updateErr: errors.New("overlay busy")}
	controlFolder(t, f, "1.1.0")
	_, _, err := updateProject(context.Background(), "control", "", "", f.ops())
	if err == nil || !strings.Contains(err.Error(), "overlay busy") {
		t.Fatalf("err = %v, want the update failure", err)
	}
	if f.versionAtUpdate != "1.2.0" {
		t.Errorf("recreate saw %q, want the new version", f.versionAtUpdate)
	}
	if got := f.reg["control"].ImageVersion; got != "1.1.0" {
		t.Fatalf("ImageVersion after a failed update = %q, want the previous 1.1.0", got)
	}
	if f.log[len(f.log)-1] != "unlock" {
		t.Errorf("lock not released: %v", f.log)
	}
}

func TestControlUpdateWaitsForTheEnsureLock(t *testing.T) {
	f := &fakeControlUpdate{state: imagecheck.ControlUpToDate}
	controlFolder(t, f, "1.1.0")
	release, ok, err := tryLock(controlLockPath())
	if err != nil || !ok {
		t.Fatalf("hold lock: ok=%v err=%v", ok, err)
	}
	ops := f.ops()
	ops.lock = waitControlLock
	done := make(chan error, 1)
	go func() {
		_, _, err := updateProject(context.Background(), "control", "", "", ops)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("update ran while the ensure lock was held")
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("update never took the released lock")
	}
	if len(f.updates) != 1 {
		t.Fatalf("updates = %d", len(f.updates))
	}
	// While an update holds it, an ensure child can't take it.
	rel, _ := waitControlLock(context.Background())
	defer rel()
	if _, ok, _ := tryLock(controlLockPath()); ok {
		t.Error("the ensure child took the lock during an update")
	}
}

func TestControlLockWaitHonoursContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	release, ok, err := tryLock(controlLockPath())
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := waitControlLock(ctx); err == nil {
		t.Fatal("waitControlLock returned without the lock")
	}
}
