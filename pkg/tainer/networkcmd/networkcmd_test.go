package networkcmd

import "testing"

// Smoke test that the package compiles and the public API is
// callable. Current/Switch read & mutate user state respectively, so
// real coverage comes from the Task 25 integration smoke.
func TestPackageCompiles(t *testing.T) {
	_ = Current
	_ = Switch
}
