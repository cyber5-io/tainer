package pod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/docker/docker/api/types/container"
)

// fakeControl is an in-memory engine + registry for EnsureControlPod.
type fakeControl struct {
	containers map[string]container.InspectResponse
	reg        map[string]registry.Project
	removed    []string
	pulled     []string
	starts     []StartOptions
	startErr   error
	pullErr    error
}

func newFakeControl() *fakeControl {
	return &fakeControl{containers: map[string]container.InspectResponse{}, reg: map[string]registry.Project{}}
}

func (f *fakeControl) ops() controlOps {
	return controlOps{
		inspect: func(_ context.Context, name string) (container.InspectResponse, error) {
			c, ok := f.containers[name]
			if !ok {
				return container.InspectResponse{}, errors.New("No such container: " + name)
			}
			return c, nil
		},
		remove: func(_ context.Context, name string) error {
			f.removed = append(f.removed, name)
			delete(f.containers, name)
			return nil
		},
		pull: func(_ context.Context, ref string) error {
			f.pulled = append(f.pulled, ref)
			return f.pullErr
		},
		start: func(_ context.Context, opts StartOptions) error {
			f.starts = append(f.starts, opts)
			return f.startErr
		},
		regGet: func(name string) (registry.Project, bool) { p, ok := f.reg[name]; return p, ok },
		regPut: func(name string, p registry.Project) error { f.reg[name] = p; return nil },
	}
}

// running adds a running control container for role, mounting the given
// host->container pairs.
func (f *fakeControl) running(role, manifestPath string, mounts ...[2]string) {
	var mps []container.MountPoint
	for _, m := range mounts {
		mps = append(mps, container.MountPoint{Source: m[0], Destination: m[1]})
	}
	f.containers[ContainerName("control", role)] = container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
		Mounts:            mps,
		Config:            &container.Config{Labels: map[string]string{LabelManifestPath: manifestPath}},
	}
}

func controlTestHome(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TAINER_SMOKE_IMAGES", "")
	return config.ControlDir()
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	sort.Strings(out)
	return out
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestFreshProvisionCreatesManifestAndDataOnly(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got := entries(t, dir); len(got) != 2 || got[0] != "data" || got[1] != "tainer.yaml" {
		t.Fatalf("control dir = %v, want [data tainer.yaml]", got)
	}
	if got := entries(t, filepath.Join(dir, "data")); len(got) != 0 {
		t.Errorf("data/ should start empty, got %v", got)
	}
	for _, p := range []string{dir, filepath.Join(dir, "data")} {
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, want 0755", p, st.Mode().Perm())
		}
	}
	if got := readFile(t, filepath.Join(dir, "tainer.yaml")); got != string(ControlManifestYAML()) {
		t.Errorf("tainer.yaml differs from the built-in definition:\n%s", got)
	}
	p, ok := f.reg["control"]
	if !ok || p.Path != dir || p.Type != "nodejs" || p.Domain != "my.tainer.me" {
		t.Errorf("registry entry = %+v (ok=%v)", p, ok)
	}
	want := StartOptions{ManifestPath: filepath.Join(dir, "tainer.yaml"), ProjectDir: dir}
	if len(f.starts) != 1 || f.starts[0] != want {
		t.Errorf("starts = %+v, want one %+v", f.starts, want)
	}
	if len(f.removed) != 0 {
		t.Errorf("nothing to remove on a fresh install, removed %v", f.removed)
	}
}

func TestExistingDataIsNeverOverwritten(t *testing.T) {
	dir := controlTestHome(t)
	writeFile(t, filepath.Join(dir, "data", "settings.json"), `{"mine":true}`)
	writeFile(t, filepath.Join(dir, "data", "thumbnails", "keep.png"), "new")
	writeFile(t, filepath.Join(dir, "tainer.yaml"), "version: 1\nproject:\n  name: edited\n")
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "tainer.yaml"), "x")
	writeFile(t, filepath.Join(old, "data", "settings.json"), `{"old":true}`)
	writeFile(t, filepath.Join(old, "data", "thumbnails", "a.png"), "old")

	f := newFakeControl()
	f.reg["control"] = registry.Project{Path: old, Type: "nodejs", Domain: "my.tainer.me"}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "data", "settings.json")); got != `{"mine":true}` {
		t.Errorf("settings.json overwritten: %s", got)
	}
	if got := entries(t, filepath.Join(dir, "data", "thumbnails")); len(got) != 1 || got[0] != "keep.png" {
		t.Errorf("thumbnails/ merged into an existing folder: %v", got)
	}
	if got := readFile(t, filepath.Join(dir, "tainer.yaml")); got != string(ControlManifestYAML()) {
		t.Errorf("an edited tainer.yaml must be restored to the built-in definition, got:\n%s", got)
	}
}

func TestMigratesAHandRegisteredControlProject(t *testing.T) {
	dir := controlTestHome(t)
	old := t.TempDir()
	oldFiles := map[string]string{
		"tainer.yaml":               "version: 1\nproject:\n  name: control\n",
		"html/package.json":         "{}",
		"data/settings.json":        `{"theme":"dark"}`,
		"data/thumbnails/a.png":     "png-bytes",
		"data/thumbnails/sub/b.png": "more",
		"data/other.txt":            "not migrated",
	}
	for rel, body := range oldFiles {
		writeFile(t, filepath.Join(old, rel), body)
	}

	f := newFakeControl()
	f.reg["control"] = registry.Project{Path: old, Type: "nodejs", Domain: "my.tainer.me", Created: "2026-01-01T00:00:00Z", ImageVersion: "1.2.0"}
	oldManifest := filepath.Join(old, "tainer.yaml")
	f.running(RoleWeb, oldManifest, [2]string{filepath.Join(old, "html"), "/var/www/html"}, [2]string{filepath.Join(old, "data"), "/var/www/data"})
	f.running(RoleApp, oldManifest, [2]string{filepath.Join(old, "html"), "/var/www/html"})

	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if got := readFile(t, filepath.Join(dir, "data", "settings.json")); got != `{"theme":"dark"}` {
		t.Errorf("settings.json = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "data", "thumbnails", "a.png")); got != "png-bytes" {
		t.Errorf("thumbnails/a.png = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "data", "thumbnails", "sub", "b.png")); got != "more" {
		t.Errorf("thumbnails/sub/b.png = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "other.txt")); !os.IsNotExist(err) {
		t.Errorf("only settings.json and thumbnails/ migrate; other.txt err=%v", err)
	}
	if got := entries(t, dir); len(got) != 2 {
		t.Errorf("control dir = %v, want only data + tainer.yaml", got)
	}

	p := f.reg["control"]
	if p.Path != dir || p.ImageVersion != "1.2.0" || p.Created != "2026-01-01T00:00:00Z" {
		t.Errorf("registry not re-pointed correctly: %+v", p)
	}

	for rel, body := range oldFiles {
		if got := readFile(t, filepath.Join(old, rel)); got != body {
			t.Errorf("old checkout changed: %s = %q", rel, got)
		}
	}

	sort.Strings(f.removed)
	if len(f.removed) != 2 || f.removed[0] != ContainerName("control", RoleApp) || f.removed[1] != ContainerName("control", RoleWeb) {
		t.Errorf("removed = %v, want both old control containers", f.removed)
	}
	if len(f.pulled) == 0 {
		t.Error("images must be pulled before the old pod is removed")
	}
	if len(f.starts) != 1 || f.starts[0].ProjectDir != dir {
		t.Errorf("starts = %+v, want one start from %s", f.starts, dir)
	}
}

func TestOfflineKeepsTheOldPodRunning(t *testing.T) {
	controlTestHome(t)
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "tainer.yaml"), "x")
	f := newFakeControl()
	f.reg["control"] = registry.Project{Path: old}
	f.running(RoleWeb, filepath.Join(old, "tainer.yaml"), [2]string{filepath.Join(old, "html"), "/var/www/html"})
	f.running(RoleApp, filepath.Join(old, "tainer.yaml"), [2]string{filepath.Join(old, "html"), "/var/www/html"})
	f.pullErr = errors.New("offline")

	if err := ensureControlPod(context.Background(), f.ops()); err == nil {
		t.Fatal("want the pull error back")
	}
	if len(f.removed) != 0 || len(f.starts) != 0 {
		t.Errorf("a failed pull must leave the old pod alone: removed=%v starts=%d", f.removed, len(f.starts))
	}
}

func TestRunningControlPodIsLeftAlone(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	f.running(RoleApp, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	for i := 0; i < 2; i++ {
		if err := ensureControlPod(context.Background(), f.ops()); err != nil {
			t.Fatalf("ensure #%d: %v", i, err)
		}
	}
	if len(f.starts) != 0 || len(f.removed) != 0 {
		t.Errorf("running pod: starts=%d removed=%v, want none", len(f.starts), f.removed)
	}
}

func TestStoppedControlPodIsStarted(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf)
	f.containers[ContainerName("control", RoleApp)].State.Running = false
	f.startErr = errors.New("boom")
	if err := ensureControlPod(context.Background(), f.ops()); err == nil {
		t.Fatal("want the start error back")
	}
	if len(f.starts) != 1 || len(f.removed) != 0 {
		t.Errorf("stopped pod: starts=%d removed=%v, want 1 start, no removal", len(f.starts), f.removed)
	}
}
