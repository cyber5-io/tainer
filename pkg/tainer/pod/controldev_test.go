package pod

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/docker/docker/api/types/container"
)

// devProject is a checkout of the dashboard: an ordinary nodejs project,
// optionally carrying the control-dev marker.
func devProject(marked bool) *manifest.Manifest {
	return &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "control-dev", Type: manifest.TypeNodeJS, Domain: "devmy.tainer.me", ControlDev: marked},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
	}
}

func devHome(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TAINER_SMOKE_IMAGES", "")
	t.Setenv("TAINER_LEGACY_IMAGES", "")
	t.Setenv("TAINER_IMAGE_REPO", "")
	return t.TempDir()
}

func envHas(env []string, prefix string) bool {
	return slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, prefix) })
}

func assertNoAccess(t *testing.T, m *manifest.Manifest, dir string) {
	t.Helper()
	if hasControlAccess(m, dir) {
		t.Error("hasControlAccess = true, want false")
	}
	if got, want := ImageRefIn(m, RoleApp, dir), "ghcr.io/cyber5-io/tainer-nodejs-app:node-22"; got != want {
		t.Errorf("app image = %q, want %q", got, want)
	}
	mounts, env := gatewayInjection(m, RoleApp, "/home/u/.cyberstack", dir)
	if len(mounts) != 0 || len(env) != 0 {
		t.Errorf("gateway injection = %+v %v, want none", mounts, env)
	}
	if s := controlAccessStamp(m, dir, RoleApp); s != "" {
		t.Errorf("stamp = %q, want empty", s)
	}
}

func TestMarkerWithoutApprovalGrantsNothing(t *testing.T) {
	dir := devHome(t)
	assertNoAccess(t, devProject(true), dir)
}

func TestApprovalWithoutMarkerGrantsNothing(t *testing.T) {
	dir := devHome(t)
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	assertNoAccess(t, devProject(false), dir)
}

func TestMarkerPlusApprovalGrantsControlAccess(t *testing.T) {
	dir := devHome(t)
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	m := devProject(true)
	if !hasControlAccess(m, dir) {
		t.Fatal("marked + approved must have control access")
	}
	if got, want := ImageRefIn(m, RoleApp, dir), "ghcr.io/cyber5-io/tainer-control-app:node-22"; got != want {
		t.Errorf("app image = %q, want %q (the runtime base, not the sealed image)", got, want)
	}
	if got := ImageRefIn(m, RoleWeb, dir); got != "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine" {
		t.Errorf("web image changed: %q", got)
	}
	mounts, env := gatewayInjection(m, RoleApp, "/home/u/.cyberstack", dir)
	if len(mounts) != 1 || mounts[0].Source != "/home/u/.cyberstack/control" || !mounts[0].ReadOnly {
		t.Errorf("gateway mounts = %+v", mounts)
	}
	for _, want := range []string{"TAINER_CONTROL_TOKEN_FILE=", "TAINER_GATEWAY_ADDR_FILE=", "TAINER_PROJECT_PATH=" + dir} {
		if !envHas(env, want) {
			t.Errorf("env missing %q: %v", want, env)
		}
	}
	if envHas(env, "TAINER_CONTROL_DEV_ACCESS=") {
		t.Errorf("the dev-access count is for the control pod only: %v", env)
	}
	if m, e := gatewayInjection(m, RoleWeb, "/home/u/.cyberstack", dir); len(m) != 0 || len(e) != 0 {
		t.Errorf("web role must get nothing: %+v %v", m, e)
	}
	// Still source-mounted like any nodejs project.
	found := false
	for _, mt := range buildMounts(m, dir, RoleApp) {
		if mt.Source == dir && mt.Target == "/var/www" {
			found = true
		}
	}
	if !found {
		t.Errorf("marked project must keep the whole-project mount: %+v", buildMounts(m, dir, RoleApp))
	}
	if s := controlAccessStamp(m, dir, RoleApp); s == "" {
		t.Error("approved app container must carry an access stamp")
	}
	if s := controlAccessStamp(m, dir, RoleWeb); s != "" {
		t.Errorf("web stamp = %q, want empty", s)
	}
}

func TestApprovalIsPathSpecific(t *testing.T) {
	dir := devHome(t)
	other := t.TempDir()
	if err := controldev.Allow(other); err != nil {
		t.Fatal(err)
	}
	assertNoAccess(t, devProject(true), dir)
}

func TestImageRefUsesTheRegisteredFolder(t *testing.T) {
	dir := devHome(t)
	m := devProject(true)
	if err := registry.Add(m.Project.Name, dir, string(m.Project.Type), m.Project.Domain); err != nil {
		t.Fatal(err)
	}
	if got := ImageRef(m, RoleApp); got != "ghcr.io/cyber5-io/tainer-nodejs-app:node-22" {
		t.Errorf("unapproved: %q", got)
	}
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	if got := ImageRef(m, RoleApp); got != "ghcr.io/cyber5-io/tainer-control-app:node-22" {
		t.Errorf("approved: %q", got)
	}
}

func TestControlPodGetsTheDevAccessCount(t *testing.T) {
	devHome(t)
	ctl := controlManifest(t)
	dir := filepath.Join(t.TempDir(), "control")
	_, env := gatewayInjection(ctl, RoleApp, "/home/u/.cyberstack", dir)
	if !slices.Contains(env, "TAINER_CONTROL_DEV_ACCESS=0") {
		t.Errorf("no approvals: env = %v", env)
	}
	if s := controlAccessStamp(ctl, dir, RoleApp); s != "" {
		t.Errorf("no approvals: stamp = %q, want empty (matches containers made before the dev track)", s)
	}
	for _, d := range []string{t.TempDir(), t.TempDir()} {
		if err := controldev.Allow(d); err != nil {
			t.Fatal(err)
		}
	}
	_, env = gatewayInjection(ctl, RoleApp, "/home/u/.cyberstack", dir)
	if !slices.Contains(env, "TAINER_CONTROL_DEV_ACCESS=2") {
		t.Errorf("two approvals: env = %v", env)
	}
	if got := ImageRefIn(ctl, RoleApp, dir); got != "ghcr.io/cyber5-io/tainer-control:1.0.0" {
		t.Errorf("pinned control pod keeps the sealed image, got %q", got)
	}
	if s := controlAccessStamp(ctl, dir, RoleApp); s != "dev-access:2" {
		t.Errorf("stamp = %q", s)
	}
}

func inspWithStamp(stamp string, running bool) container.InspectResponse {
	labels := map[string]string{}
	if stamp != "" {
		labels[LabelControlAccess] = stamp
	}
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: running}},
		Config:            &container.Config{Labels: labels},
	}
}

func TestAccessIsRecheckedOnEveryStart(t *testing.T) {
	cases := []struct {
		have, want string
		recreate   bool
	}{
		{"", "", false},
		{"granted", "granted", false},
		{"granted", "", true}, // revoked: drop the token
		{"", "granted", true}, // newly approved
		{"dev-access:1", "dev-access:2", true},
		{"dev-access:1", "", true},
	}
	for _, c := range cases {
		for _, running := range []bool{true, false} {
			if got := accessChanged(inspWithStamp(c.have, running), c.want); got != c.recreate {
				t.Errorf("have %q want %q running=%v: recreate=%v, want %v", c.have, c.want, running, got, c.recreate)
			}
		}
	}
}

func TestEnsureRestartsAControlAppWithAStaleCount(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	f.running(RoleApp, mf, [2]string{filepath.Join(dir, "data"), "/var/www/data"})
	if err := controldev.Allow(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	if len(f.starts) != 1 {
		t.Fatalf("stale dev-access count must restart the control pod, starts=%d", len(f.starts))
	}
	if len(f.removed) != 0 {
		t.Errorf("only the start path recreates the app container; removed=%v", f.removed)
	}
}

func devContainer(manifestPath, stamp string, running bool) container.InspectResponse {
	insp := inspWithStamp(stamp, running)
	insp.Config.Labels[LabelManifestPath] = manifestPath
	return insp
}

func writeDevManifest(t *testing.T, dir string) string {
	t.Helper()
	mf := filepath.Join(dir, "tainer.yaml")
	writeFile(t, mf, "version: 1\nproject:\n  name: control-dev\n  type: nodejs\n  domain: devmy.tainer.me\n  control-dev: true\nruntime:\n  node: \"22\"\n  database: none\n")
	return mf
}

func TestRevokeRecreatesARunningDevPodWithoutTheToken(t *testing.T) {
	dir := devHome(t)
	mf := writeDevManifest(t, dir)
	f := newFakeControl()
	f.containers[ContainerName("control-dev", RoleWeb)] = devContainer(mf, "", true)
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer(mf, "granted", true)
	// Not approved (revoked): the token-holding app container must go first.
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.removed, []string{ContainerName("control-dev", RoleApp)}) {
		t.Errorf("removed = %v, want the app container", f.removed)
	}
	if len(f.starts) != 1 || f.starts[0].ProjectDir != dir {
		t.Errorf("running pod must be started again: %+v", f.starts)
	}
}

func TestRevokeOfAStoppedDevPodRemovesTheTokenOnly(t *testing.T) {
	dir := devHome(t)
	mf := writeDevManifest(t, dir)
	f := newFakeControl()
	f.containers[ContainerName("control-dev", RoleWeb)] = devContainer(mf, "", false)
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer(mf, "granted", false)
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 1 || len(f.starts) != 0 {
		t.Errorf("stopped pod: removed=%v starts=%d, want app removed and no start", f.removed, len(f.starts))
	}
}

func TestRevokeFailsClosedWhenTheRestartFails(t *testing.T) {
	dir := devHome(t)
	mf := writeDevManifest(t, dir)
	f := newFakeControl()
	f.startErr = context.DeadlineExceeded
	f.containers[ContainerName("control-dev", RoleWeb)] = devContainer(mf, "", true)
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer(mf, "granted", true)
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err == nil {
		t.Fatal("a failed restart must be reported")
	}
	if _, ok := f.containers[ContainerName("control-dev", RoleApp)]; ok {
		t.Error("the token-holding container must be gone even when the restart fails")
	}
}

func TestReconcileLeavesMatchingAndForeignPodsAlone(t *testing.T) {
	dir := devHome(t)
	mf := writeDevManifest(t, dir)
	f := newFakeControl()
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer(mf, "", true)
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err != nil {
		t.Fatal(err)
	}
	// Same name, created from another folder: not this checkout's pod.
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer("/elsewhere/tainer.yaml", "granted", true)
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 0 || len(f.starts) != 0 {
		t.Errorf("removed=%v starts=%d, want nothing", f.removed, len(f.starts))
	}
}

func TestAllowRecreatesARunningDevPodWithAccess(t *testing.T) {
	dir := devHome(t)
	mf := writeDevManifest(t, dir)
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	f := newFakeControl()
	f.containers[ContainerName("control-dev", RoleWeb)] = devContainer(mf, "", true)
	f.containers[ContainerName("control-dev", RoleApp)] = devContainer(mf, "", true)
	if err := reconcileControlAccess(context.Background(), f.ops(), mf, dir); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 1 || len(f.starts) != 1 {
		t.Errorf("removed=%v starts=%d, want the app recreated", f.removed, len(f.starts))
	}
}
