package pod

import (
	"context"
	"errors"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/docker/docker/api/types/container"
)

// inspWith is a running container made under the given mount version and
// manifest hash; an empty value leaves that label off.
func inspWith(mounts, hash string) container.InspectResponse {
	insp := inspWithMounts(mounts)
	if hash != "" {
		insp.Config.Labels[LabelManifestHash] = hash
	}
	return insp
}

// A container is made from the manifest as it was then: a changed size,
// port, runtime version or live domain only takes effect once it is
// recreated. A container without the label predates the hash and counts as
// changed.
func TestManifestDriftNeedsRecreating(t *testing.T) {
	cases := map[string]bool{"": true, "oldhash": true, "newhash": false}
	for have, want := range cases {
		if got := manifestDrifted(inspWith(MountsVersion, have), "newhash"); got != want {
			t.Errorf("label %q: drifted=%v, want %v", have, got, want)
		}
	}
	if !manifestDrifted(container.InspectResponse{}, "newhash") {
		t.Error("no config: want drifted")
	}
}

// A changed manifest replaces the whole pod, not one container: the budget
// is one cgroup for the whole pod, and a container left behind would put
// the old budget back when it starts. The pod is stopped gracefully first
// (app, db with time to shut down, web) and nothing is pulled — the images
// the new manifest needs are already present, so a size change works
// offline.
func TestChangedManifestReplacesWholePod(t *testing.T) {
	roles := []string{RoleWeb, RoleDB, RoleApp}
	refs := []string{"web:1", "db:1", "app:1"}
	n := func(role string) string { return ContainerName("site", role) }
	run := func(have map[string]container.InspectResponse) ([]string, error) {
		var calls []string
		ops := replaceOps{
			inspect: func(_ context.Context, name string) (container.InspectResponse, error) {
				c, ok := have[name]
				if !ok {
					return container.InspectResponse{}, errors.New("No such container: " + name)
				}
				return c, nil
			},
			pull: func(_ context.Context, ref string) error {
				calls = append(calls, "pull "+ref)
				return errors.New("dial tcp: no route")
			},
			stop: func(_ context.Context, name string, timeout *int) error {
				s := "stop " + name
				if timeout != nil && *timeout == dbStopTimeout {
					s += " graceful"
				}
				calls = append(calls, s)
				return nil
			},
			remove: func(_ context.Context, name string) error {
				calls = append(calls, "remove "+name)
				return nil
			},
		}
		return calls, refreshOutdatedPod(context.Background(), ops, "site", roles, refs, "newhash")
	}
	all := func(web, db, app container.InspectResponse) map[string]container.InspectResponse {
		return map[string]container.InspectResponse{n(RoleWeb): web, n(RoleDB): db, n(RoleApp): app}
	}
	whole := []string{
		"stop " + n(RoleApp), "stop " + n(RoleDB) + " graceful", "stop " + n(RoleWeb),
		"remove " + n(RoleWeb), "remove " + n(RoleDB), "remove " + n(RoleApp),
	}
	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	old := inspWith(MountsVersion, "oldhash")
	cur := inspWith(MountsVersion, "newhash")
	cases := []struct {
		name string
		have map[string]container.InspectResponse
		want []string
	}{
		{"size changed", all(old, old, old), whole},
		{"one container drifted", all(cur, cur, old), whole},
		{"one container without the label", all(cur, inspWith(MountsVersion, ""), cur), whole},
		{"unchanged", all(cur, cur, cur), nil},
		{"none yet", map[string]container.InspectResponse{}, nil},
	}
	for _, c := range cases {
		calls, err := run(c.have)
		if err != nil || !same(calls, c.want) {
			t.Errorf("%s: calls %v err %v, want %v", c.name, calls, err, c.want)
		}
	}
}

// A replaced pod is made again with the current hash, so the next start
// reuses it.
func TestRecreatedContainersCarryTheCurrentHash(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "site", Type: manifest.TypeWordPress, Domain: "site.tainer.me"},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
	}
	h := ManifestHash(m)
	labels := containerLabels(m, "/p/tainer.yaml", RoleWeb, 3, h, "", nil)
	if labels[LabelManifestHash] != h || manifestDrifted(inspWith(MountsVersion, labels[LabelManifestHash]), h) {
		t.Errorf("labels %v: want the current hash %q", labels, h)
	}
	m.Pod.Size = manifest.PodSizeLarge
	if ManifestHash(m) == h {
		t.Error("a size change must change the manifest hash")
	}
}

// A custom pod's budget lives in pod.memory/pod.cpu, so changing either
// changes the hash. Named sizes hash as before, so pods made by an earlier
// tainer aren't replaced for nothing.
func TestCustomBudgetIsInTheManifestHash(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "site", Type: manifest.TypeWordPress, Domain: "site.tainer.me"},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeCustom, Memory: "3G", CPU: 2},
	}
	h := ManifestHash(m)
	m.Pod.Memory = "4G"
	if ManifestHash(m) == h {
		t.Error("custom memory change: want a new hash")
	}
	h = ManifestHash(m)
	m.Pod.CPU = 3
	if ManifestHash(m) == h {
		t.Error("custom cpu change: want a new hash")
	}

	// Pinned: the hash a small wordpress pod has carried since TAINER_ENV.
	named := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "site", Type: manifest.TypeWordPress, Domain: "site.tainer.me"},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
	}
	if got := ManifestHash(named); got != "eb3f4aa26e89e6bd" {
		t.Errorf("named size hash = %q, want it unchanged", got)
	}
}
