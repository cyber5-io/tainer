// Package tainer holds cross-cutting definitions shared by the CLI and its
// subpackages.
package tainer

// pinnedProject is the reserved project that hosts the control dashboard.
const pinnedProject = "control"

// IsPinnedProject reports whether name is a reserved pinned project: one that
// runs whenever tainer runs and cannot be stopped, destroyed, or created by a
// user.
func IsPinnedProject(name string) bool {
	return name == pinnedProject
}
