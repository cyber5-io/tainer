package pod

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
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
	images     map[string]bool // refs already present locally
}

func newFakeControl() *fakeControl {
	return &fakeControl{containers: map[string]container.InspectResponse{}, reg: map[string]registry.Project{}, images: map[string]bool{}}
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
		hasImage: func(_ context.Context, ref string) bool { return f.images[ref] },
		start: func(_ context.Context, opts StartOptions) error {
			f.starts = append(f.starts, opts)
			return f.startErr
		},
		listGranted: func(_ context.Context) ([]container.Summary, error) {
			var out []container.Summary
			for name, c := range f.containers {
				if c.Config != nil && c.Config.Labels[LabelControlAccess] == "granted" {
					out = append(out, container.Summary{Names: []string{"/" + name}, Labels: c.Config.Labels})
				}
			}
			return out, nil
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
	// Created by this build from the built-in definition: its image and
	// definition hash are current.
	m, _ := manifest.ParseBytes(ControlManifestYAML())
	f.containers[ContainerName("control", role)] = container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
		Mounts:            mps,
		Config: &container.Config{
			Image:  ImageRefIn(m, role, config.ControlDir()),
			Labels: map[string]string{LabelManifestPath: manifestPath, LabelManifestHash: ManifestHash(m)},
		},
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
	want := StartOptions{ManifestPath: filepath.Join(dir, "tainer.yaml"), ProjectDir: dir, PullMissingOnly: true}
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
	f.reg["control"] = registry.Project{Path: old, Type: "nodejs", Domain: "my.tainer.me"}
	f.running(RoleWeb, filepath.Join(old, "tainer.yaml"), [2]string{filepath.Join(old, "html"), "/var/www/html"})
	f.running(RoleApp, filepath.Join(old, "tainer.yaml"), [2]string{filepath.Join(old, "html"), "/var/www/html"})
	f.pullErr = errors.New("offline")

	err := ensureControlPod(context.Background(), f.ops())
	if !errors.Is(err, ErrControlDownload) {
		t.Fatalf("want ErrControlDownload, got %v", err)
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
	if err := ensureControlPod(context.Background(), f.ops()); !errors.Is(err, ErrControlStart) {
		t.Fatalf("want ErrControlStart, got %v", err)
	}
	if len(f.starts) != 1 || len(f.removed) != 0 {
		t.Errorf("stopped pod: starts=%d removed=%v, want 1 start, no removal", len(f.starts), f.removed)
	}
}

func TestHtmlMountIsStaleEvenWithMatchingLabel(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf, [2]string{"/somewhere/html", "/var/www/html"})
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.removed) != 2 || len(f.starts) != 1 {
		t.Errorf("html mount: removed=%v starts=%d, want both removed and one start", f.removed, len(f.starts))
	}
}

func TestPartialPodIsStarted(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	f.running(RoleWeb, filepath.Join(dir, "tainer.yaml"))
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.removed) != 0 || len(f.starts) != 1 {
		t.Errorf("partial pod: removed=%v starts=%d, want no removal and one start", f.removed, len(f.starts))
	}
}

func TestLocalImagesAreNotPulled(t *testing.T) {
	controlTestHome(t)
	f := newFakeControl()
	m := controlManifest(t)
	for _, role := range RolesForPod(m) {
		f.images[ImageRef(m, role)] = true
	}
	if !controlImagesPresent(context.Background(), f.ops()) {
		t.Fatal("controlImagesPresent = false with every image local")
	}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.pulled) != 0 {
		t.Errorf("local images were pulled again: %v", f.pulled)
	}
}

func TestMissingImagesArePulledBeforeStart(t *testing.T) {
	controlTestHome(t)
	f := newFakeControl()
	if controlImagesPresent(context.Background(), f.ops()) {
		t.Fatal("controlImagesPresent = true with no images")
	}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.pulled) != len(RolesForPod(controlManifest(t))) || len(f.starts) != 1 {
		t.Errorf("pulled=%v starts=%d", f.pulled, len(f.starts))
	}
}

func TestPartialManifestIsRemovedOnWriteError(t *testing.T) {
	dir := controlTestHome(t)
	old := writeManifestFile
	t.Cleanup(func() { writeManifestFile = old })
	writeManifestFile = func(name string, data []byte, perm os.FileMode) error {
		os.WriteFile(name, data[:3], perm) // a short write, then the failure
		return errors.New("disk full")
	}
	if err := ensureControlPod(context.Background(), newFakeControl().ops()); err == nil {
		t.Fatal("want the write error back")
	}
	if _, err := os.Stat(filepath.Join(dir, "tainer.yaml.partial")); !os.IsNotExist(err) {
		t.Errorf("tainer.yaml.partial left behind (err=%v)", err)
	}
}

func TestOfflineWithLocalImagesStillStarts(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	m := controlManifest(t)
	for _, role := range RolesForPod(m) {
		f.images[ImageRef(m, role)] = true
	}
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf)
	f.containers[ContainerName("control", RoleApp)].State.Running = false
	f.pullErr = errors.New("offline")
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure offline with local images: %v", err)
	}
	if len(f.pulled) != 0 {
		t.Errorf("pulled %v while every image is local", f.pulled)
	}
	if len(f.starts) != 1 || !f.starts[0].PullMissingOnly {
		t.Errorf("starts = %+v, want one start with PullMissingOnly", f.starts)
	}
}

func TestPullImagesSkipsLocalRefsOnlyWhenAsked(t *testing.T) {
	refs := []string{"a:1", "b:1"}
	local := map[string]bool{"a:1": true}
	has := func(_ context.Context, ref string) bool { return local[ref] }
	for _, tc := range []struct {
		missingOnly bool
		want        []string
	}{{false, []string{"a:1", "b:1"}}, {true, []string{"b:1"}}} {
		var pulled []string
		pull := func(_ context.Context, ref string) error { pulled = append(pulled, ref); return nil }
		if err := pullImages(context.Background(), refs, tc.missingOnly, has, pull); err != nil {
			t.Fatal(err)
		}
		if strings.Join(pulled, ",") != strings.Join(tc.want, ",") {
			t.Errorf("missingOnly=%v: pulled %v, want %v", tc.missingOnly, pulled, tc.want)
		}
	}
	offline := func(context.Context, string) error { return errors.New("offline") }
	all := func(context.Context, string) bool { return true }
	if err := pullImages(context.Background(), refs, true, all, offline); err != nil {
		t.Errorf("offline with every image local must not fail: %v", err)
	}
}

// A registry that answers 401/403/404 for the control image means it isn't
// published (or reachable to us) yet — not that the user is offline.
func TestUnavailableControlImageIsItsOwnClass(t *testing.T) {
	for _, msg := range []string{
		"engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Error response from daemon: initializing source docker://ghcr.io/cyber5-io/tainer-control:1.0.0: reading manifest 1.0.0 in ghcr.io/cyber5-io/tainer-control: manifest unknown",
		"engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Error response from daemon: initializing source docker://ghcr.io/cyber5-io/tainer-control:1.0.0: reading manifest 1.0.0 in ghcr.io/cyber5-io/tainer-control: unauthorized: authentication required",
		"engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Error response from daemon: requested access to the resource is denied",
		"engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Error response from daemon: unexpected status code 403 (Forbidden)",
		"engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Error response from daemon: unexpected http status 404 Not Found",
	} {
		controlTestHome(t)
		f := newFakeControl()
		f.pullErr = errors.New(msg)
		err := ensureControlPod(context.Background(), f.ops())
		if !errors.Is(err, ErrControlUnavailable) || errors.Is(err, ErrControlDownload) {
			t.Errorf("%q: want ErrControlUnavailable only, got %v", msg, err)
		}
	}
	controlTestHome(t)
	f := newFakeControl()
	f.pullErr = errors.New(`engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Get "https://ghcr.io/v2/": dial tcp: lookup ghcr.io: no such host`)
	if err := ensureControlPod(context.Background(), f.ops()); !errors.Is(err, ErrControlDownload) || errors.Is(err, ErrControlUnavailable) {
		t.Errorf("offline: want ErrControlDownload only, got %v", err)
	}
}

// A tainer upgrade that raises the pinned control version reaches a pod that
// is already running: the app runs an older image than the pin, so the pod is
// replaced — the new image pulled first.
func TestOlderControlImageIsReplacedByThePin(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	f.running(RoleApp, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	f.containers[ContainerName("control", RoleApp)].Config.Image = ControlImageRef("0.9.0")
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	want := ImageRefIn(controlManifest(t), RoleApp, dir)
	if want == ControlImageRef("0.9.0") {
		t.Fatal("test needs a pin above 0.9.0")
	}
	if !contains(f.pulled, want) {
		t.Errorf("pulled %v, want %s before the old pod is removed", f.pulled, want)
	}
	if len(f.removed) != 2 || len(f.starts) != 1 {
		t.Errorf("older image: removed=%v starts=%d, want both removed and one start", f.removed, len(f.starts))
	}
}

func TestChangedDefinitionHashReplacesThePod(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf)
	f.containers[ContainerName("control", RoleWeb)].Config.Labels[LabelManifestHash] = "0123456789abcdef"
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.removed) != 2 || len(f.starts) != 1 {
		t.Errorf("changed definition: removed=%v starts=%d, want both removed and one start", f.removed, len(f.starts))
	}
}

func TestOlderControlImageStaysWhenOffline(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf)
	f.containers[ContainerName("control", RoleApp)].Config.Image = ControlImageRef("0.9.0")
	f.pullErr = errors.New("offline")
	if err := ensureControlPod(context.Background(), f.ops()); !errors.Is(err, ErrControlDownload) {
		t.Fatalf("want ErrControlDownload, got %v", err)
	}
	if len(f.removed) != 0 || len(f.starts) != 0 {
		t.Errorf("offline: removed=%v starts=%d, want the old dashboard left running", f.removed, len(f.starts))
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// The runtime reports a container's image ref at the top level and leaves
// Config.Image empty: a current pod is left alone, an older one replaced.
func TestImageIsReadFromTheRuntimesShape(t *testing.T) {
	for _, tc := range []struct {
		image   string
		replace bool
	}{{"", false}, {ControlImageRef("0.9.0"), true}} {
		dir := controlTestHome(t)
		f := newFakeControl()
		mf := filepath.Join(dir, "tainer.yaml")
		f.running(RoleWeb, mf)
		f.running(RoleApp, mf)
		app := f.containers[ContainerName("control", RoleApp)]
		img := tc.image
		if img == "" {
			img = app.Config.Image
		}
		app.Config.Image = ""
		app.ContainerJSONBase.Image = img
		f.containers[ContainerName("control", RoleApp)] = app
		if err := ensureControlPod(context.Background(), f.ops()); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		if got := len(f.removed) == 2 && len(f.starts) == 1; got != tc.replace {
			t.Errorf("image %q: removed=%v starts=%d, want replaced=%v", img, f.removed, len(f.starts), tc.replace)
		}
	}
}

// userControl is a 1.3.0 user's own project that happens to be named control:
// a site with a database, registered and running from its own folder.
func userControl(t *testing.T, f *fakeControl, body string) (string, map[string]string) {
	t.Helper()
	old := t.TempDir()
	files := map[string]string{
		"tainer.yaml":        body,
		"html/index.php":     "<?php echo 'mine';",
		"db/ibdata1":         "my database",
		"data/settings.json": `{"mine":true}`,
		"data/thumbnails/a":  "mine",
	}
	for rel, b := range files {
		writeFile(t, filepath.Join(old, rel), b)
	}
	f.reg["control"] = registry.Project{Path: old, Type: "php", Domain: "control.test", Created: "2026-01-01T00:00:00Z"}
	mf := filepath.Join(old, "tainer.yaml")
	for _, role := range []string{RoleWeb, RoleApp, RoleDB} {
		f.running(role, mf)
	}
	return old, files
}

func assertUserControlUntouched(t *testing.T, f *fakeControl, old string, files map[string]string, err error) {
	t.Helper()
	var taken *ControlNameTakenError
	if !errors.Is(err, ErrControlNameTaken) || !errors.As(err, &taken) || taken.Path != old {
		t.Fatalf("want ErrControlNameTaken for %s, got %v", old, err)
	}
	if p := f.reg["control"]; p.Path != old || p.Type == "nodejs" && p.Domain == "my.tainer.me" {
		t.Errorf("the user's registry entry was re-pointed: %+v", p)
	}
	if len(f.removed) != 0 || len(f.starts) != 0 || len(f.pulled) != 0 {
		t.Errorf("the user's pod was touched: removed=%v starts=%d pulled=%v", f.removed, len(f.starts), f.pulled)
	}
	for rel, body := range files {
		if got := readFile(t, filepath.Join(old, rel)); got != body {
			t.Errorf("user's %s changed: %q", rel, got)
		}
	}
	for _, rel := range []string{"settings.json", "thumbnails"} {
		if _, err := os.Stat(filepath.Join(config.ControlDir(), "data", rel)); !os.IsNotExist(err) {
			t.Errorf("the user's data/%s was copied into the dashboard (err=%v)", rel, err)
		}
	}
}

func TestAUsersOwnControlProjectIsNeverTakenOver(t *testing.T) {
	controlTestHome(t)
	f := newFakeControl()
	old, files := userControl(t, f, "version: 2\nproject:\n  name: control\n  type: php\n  domain: control.test\nruntime:\n  php: \"8.3\"\n  database: mariadb\n")
	err := ensureControlPod(context.Background(), f.ops())
	assertUserControlUntouched(t, f, old, files, err)
}

// A nodejs site of the user's on another domain is not the dashboard either,
// and neither is one whose tainer.yaml no longer parses.
func TestAUsersNodeControlOrBrokenOneIsNeverTakenOver(t *testing.T) {
	for _, body := range []string{
		"version: 2\nproject:\n  name: control\n  type: nodejs\n  domain: control.test\nruntime:\n  node: \"22\"\n  database: mariadb\n",
		"version: 2\nproject: [not yaml",
	} {
		controlTestHome(t)
		f := newFakeControl()
		old, files := userControl(t, f, body)
		err := ensureControlPod(context.Background(), f.ops())
		assertUserControlUntouched(t, f, old, files, err)
	}
}

// A checkout marked control-dev: true is the dashboard, whatever its domain.
func TestAMarkedDashboardCheckoutIsMigrated(t *testing.T) {
	dir := controlTestHome(t)
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "tainer.yaml"), "version: 2\nproject:\n  name: control\n  type: nodejs\n  domain: devmy.tainer.me\n  control-dev: true\nruntime:\n  node: \"22\"\n  database: none\n")
	writeFile(t, filepath.Join(old, "data", "settings.json"), `{"theme":"dark"}`)
	f := newFakeControl()
	f.reg["control"] = registry.Project{Path: old, Type: "nodejs", Domain: "devmy.tainer.me"}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if f.reg["control"].Path != dir || readFile(t, filepath.Join(dir, "data", "settings.json")) != `{"theme":"dark"}` {
		t.Errorf("marked checkout not migrated: %+v", f.reg["control"])
	}
}

// An entry whose folder has no tainer.yaml any more is stale: the name is
// taken over, but nothing of a non-dashboard folder is copied.
func TestAStaleControlEntryIsTakenOverWithoutCopying(t *testing.T) {
	dir := controlTestHome(t)
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "data", "settings.json"), `{"mine":true}`)
	f := newFakeControl()
	f.reg["control"] = registry.Project{Path: old, Type: "php", Domain: "control.test"}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if f.reg["control"].Path != dir {
		t.Errorf("stale entry not re-pointed: %+v", f.reg["control"])
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("a non-dashboard folder's data was copied (err=%v)", err)
	}
}
