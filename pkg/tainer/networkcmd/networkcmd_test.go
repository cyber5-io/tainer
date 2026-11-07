package networkcmd

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/pod"
)

// Smoke test that the package compiles and the public API is
// callable. Current/Switch read & mutate user state respectively, so
// real coverage comes from the Task 25 integration smoke.
func TestPackageCompiles(t *testing.T) {
	_ = Current
	_ = Switch
}

func TestRestartSkipsThePinnedControlPod(t *testing.T) {
	got := podsToRestart([]string{"blog", "control", "shop"})
	if len(got) != 2 || got[0] != "blog" || got[1] != "shop" {
		t.Errorf("podsToRestart = %v, want [blog shop]", got)
	}
}

// A pod restarted by a network switch is started from its own folder, so an
// approved dashboard checkout keeps its bind mounts and access.
func TestRestartStartsFromThePodsFolder(t *testing.T) {
	got := restartOptions("/Users/me/sites/control-dev/tainer.yaml")
	want := pod.StartOptions{ManifestPath: "/Users/me/sites/control-dev/tainer.yaml", ProjectDir: "/Users/me/sites/control-dev"}
	if got != want {
		t.Errorf("restartOptions = %+v, want %+v", got, want)
	}
}
