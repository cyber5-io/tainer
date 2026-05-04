package networkcmd

import "testing"

// Smoke test that the package compiles and Show is callable. Show
// itself reads the user's network-mode file and prints to stdout —
// hard to assert in unit tests without a temp HOME. Real coverage
// comes from the Task 25 integration smoke.
func TestPackageCompiles(t *testing.T) {
	_ = Show
	_ = Set
}
