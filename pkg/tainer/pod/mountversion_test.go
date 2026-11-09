package pod

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/docker/docker/api/types/container"
)

func inspWithMounts(version string) container.InspectResponse {
	labels := map[string]string{}
	if version != "" {
		labels[LabelMounts] = version
	}
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
		Config:            &container.Config{Labels: labels},
	}
}

// Every container records the mount layout it was made with, so a later
// tainer can tell when its mounts are older than the current rules.
func TestContainersCarryTheMountVersion(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.ProjectConfig{Name: "site", Type: manifest.TypeWordPress, Domain: "site.tainer.me"}}
	for _, role := range []string{RoleWeb, RoleDB, RoleApp} {
		labels := containerLabels(m, "/p/tainer.yaml", role, 3, "hash", "", []engine.PortMap{{Role: RoleDB, Host: 30032}})
		if labels[LabelMounts] != MountsVersion {
			t.Errorf("%s: %s = %q, want %q", role, LabelMounts, labels[LabelMounts], MountsVersion)
		}
		if labels[LabelRole] != role || labels[LabelPod] != "site" || labels[PublishLabel(RoleDB)] != "30032" {
			t.Errorf("%s: labels %v", role, labels)
		}
	}
}

// Mounts are fixed when a container is made: one from before the current
// rules (no label, or an older one) still has the old mounts — a writable
// web docroot — until it is recreated.
func TestOlderMountsNeedRecreating(t *testing.T) {
	cases := map[string]bool{"": true, "1": true, MountsVersion: false}
	for have, want := range cases {
		if got := mountsOutdated(inspWithMounts(have)); got != want {
			t.Errorf("label %q: outdated=%v, want %v", have, got, want)
		}
	}
	if !mountsOutdated(container.InspectResponse{}) {
		t.Error("no config: want outdated")
	}
}

// One outdated container replaces the whole pod: the others share web's
// network, so web can't be recreated alone.
func TestPodWithOlderMountsIsReplacedWhole(t *testing.T) {
	roles := []string{RoleWeb, RoleDB, RoleApp}
	inspect := func(have map[string]container.InspectResponse, err error) func(context.Context, string) (container.InspectResponse, error) {
		return func(_ context.Context, name string) (container.InspectResponse, error) {
			if err != nil {
				return container.InspectResponse{}, err
			}
			c, ok := have[name]
			if !ok {
				return container.InspectResponse{}, errors.New("No such container: " + name)
			}
			return c, nil
		}
	}
	current := inspWithMounts(MountsVersion)
	cases := []struct {
		name string
		have map[string]container.InspectResponse
		want bool
	}{
		{"none yet", map[string]container.InspectResponse{}, false},
		{"all current", map[string]container.InspectResponse{
			ContainerName("site", RoleWeb): current, ContainerName("site", RoleDB): current, ContainerName("site", RoleApp): current}, false},
		{"app older", map[string]container.InspectResponse{
			ContainerName("site", RoleWeb): current, ContainerName("site", RoleDB): current, ContainerName("site", RoleApp): inspWithMounts("")}, true},
		{"made before this tainer", map[string]container.InspectResponse{
			ContainerName("site", RoleWeb): inspWithMounts(""), ContainerName("site", RoleDB): inspWithMounts("")}, true},
	}
	for _, c := range cases {
		got, err := podMountsOutdated(context.Background(), inspect(c.have, nil), "site", roles)
		if err != nil || got != c.want {
			t.Errorf("%s: outdated=%v err=%v, want %v", c.name, got, err, c.want)
		}
	}
	if _, err := podMountsOutdated(context.Background(), inspect(nil, errors.New("runtime unreachable")), "site", roles); err == nil {
		t.Error("inspect failure: want an error, not a silent reuse")
	}
}

// The built-in control pod is replaced too when its containers predate the
// current mount rules (its web edge now reads data/ read-only).
func TestControlPodWithOlderMountsIsReplaced(t *testing.T) {
	dir := controlTestHome(t)
	f := newFakeControl()
	mf := filepath.Join(dir, "tainer.yaml")
	f.running(RoleWeb, mf)
	f.running(RoleApp, mf)
	delete(f.containers[ContainerName("control", RoleWeb)].Config.Labels, LabelMounts)
	if err := ensureControlPod(context.Background(), f.ops()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.removed) != 2 || len(f.starts) != 1 {
		t.Errorf("older mounts: removed=%v starts=%d, want both removed and one start", f.removed, len(f.starts))
	}
}

// Replacing a running pod made by an older tainer stops it gracefully
// first, app before db before web, giving the database 30 seconds to shut
// down cleanly, then removes every container. A stop that fails doesn't
// keep the old container: it is still removed. A removal that fails stops
// the start.
func TestReplaceOutdatedPodStopsGracefullyFirst(t *testing.T) {
	type call struct {
		op, name string
		timeout  int // -1: the default
	}
	run := func(stopErr, removeErr error) ([]call, error) {
		var calls []call
		ops := replaceOps{
			stop: func(_ context.Context, name string, timeout *int) error {
				c := call{"stop", name, -1}
				if timeout != nil {
					c.timeout = *timeout
				}
				calls = append(calls, c)
				return stopErr
			},
			remove: func(_ context.Context, name string) error {
				calls = append(calls, call{"remove", name, 0})
				return removeErr
			},
		}
		err := replaceOutdatedPod(context.Background(), ops, "site", []string{RoleWeb, RoleDB, RoleApp})
		return calls, err
	}
	n := func(role string) string { return ContainerName("site", role) }
	want := []call{
		{"stop", n(RoleApp), -1}, {"stop", n(RoleDB), 30}, {"stop", n(RoleWeb), -1},
		{"remove", n(RoleWeb), 0}, {"remove", n(RoleDB), 0}, {"remove", n(RoleApp), 0},
	}
	calls, err := run(nil, nil)
	if err != nil || len(calls) != len(want) {
		t.Fatalf("calls %v, err %v; want %v", calls, err, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %v, want %v", i, calls[i], want[i])
		}
	}
	calls, err = run(errors.New("stop timed out"), nil)
	removed := 0
	for _, c := range calls {
		if c.op == "remove" {
			removed++
		}
	}
	if err != nil || removed != 3 {
		t.Errorf("failed stop: removed %d, err %v; want all 3 removed", removed, err)
	}
	if _, err := run(nil, errors.New("busy")); err == nil {
		t.Error("failed removal: want the start stopped")
	}
}

// A pod recreated for outdated mounts gets fresh images first: every role's
// ref is pulled (moving tags), so new mounts never run old images. A failed
// pull refuses the start before anything is removed. A pod whose mounts are
// current is left alone and nothing is pulled.
func TestOutdatedPodPullsEveryImageBeforeReplacing(t *testing.T) {
	roles := []string{RoleWeb, RoleDB, RoleApp}
	refs := []string{"web:1", "db:1", "app:1"}
	run := func(label string, pullErr error) ([]string, error) {
		var calls []string
		ops := replaceOps{
			inspect: func(_ context.Context, name string) (container.InspectResponse, error) {
				return inspWithMounts(label), nil
			},
			pull: func(_ context.Context, ref string) error {
				calls = append(calls, "pull "+ref)
				return pullErr
			},
			stop: func(_ context.Context, name string, _ *int) error {
				calls = append(calls, "stop "+name)
				return nil
			},
			remove: func(_ context.Context, name string) error {
				calls = append(calls, "remove "+name)
				return nil
			},
		}
		return calls, refreshOutdatedPod(context.Background(), ops, "site", roles, refs)
	}

	calls, err := run("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) < 3 || calls[0] != "pull web:1" || calls[1] != "pull db:1" || calls[2] != "pull app:1" {
		t.Fatalf("calls %v: want every image pulled before any stop/remove", calls)
	}
	removes := 0
	for _, c := range calls {
		if len(c) > 6 && c[:6] == "remove" {
			removes++
		}
	}
	if removes != 3 {
		t.Errorf("calls %v: want the whole pod replaced", calls)
	}

	calls, err = run("", errors.New("dial tcp: no route"))
	var de *engine.DownloadError
	if !errors.As(err, &de) || err.Error() != "pod start: couldn't download web:1 — are you offline?" {
		t.Fatalf("err = %v, want the offline refusal", err)
	}
	for _, c := range calls {
		if c[:4] != "pull" {
			t.Errorf("calls %v: a failed pull must not touch the pod", calls)
		}
	}

	calls, err = run(MountsVersion, nil)
	if err != nil || len(calls) != 0 {
		t.Errorf("current mounts: calls %v err %v, want nothing pulled or replaced", calls, err)
	}
}
