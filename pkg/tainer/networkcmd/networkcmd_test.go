package networkcmd

import "testing"

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
