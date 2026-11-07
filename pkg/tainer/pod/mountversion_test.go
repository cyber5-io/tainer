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
