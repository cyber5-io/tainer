package main

import (
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

// pinnedOverrideFlag is deliberately absent from every help text: it exists for
// development and packaging scripts that must manage the pinned project.
const pinnedOverrideFlag = "--pinned-override"

// stripPinnedOverride removes the hidden override flag from args so the
// per-command flag parsing never sees it.
func stripPinnedOverride(args []string) (bool, []string) {
	override := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == pinnedOverrideFlag {
			override = true
			continue
		}
		rest = append(rest, a)
	}
	return override, rest
}

// guardPinned refuses to stop or destroy a pinned project.
func guardPinned(name string, override bool) error {
	if override || !tainer.IsPinnedProject(name) {
		return nil
	}
	return withCode("pod-pinned", fmt.Errorf("the control dashboard runs whenever tainer runs"))
}

// guardInitName refuses to scaffold a project under a reserved name.
func guardInitName(name string, override bool) error {
	if override || !tainer.IsPinnedProject(name) {
		return nil
	}
	return withCode("invalid-arguments", fmt.Errorf("%q is a reserved project name", name))
}

// visibleProjects drops pinned projects from a registry view meant for users.
func visibleProjects(all map[string]registry.Project) map[string]registry.Project {
	out := make(map[string]registry.Project, len(all))
	for name, p := range all {
		if !tainer.IsPinnedProject(name) {
			out[name] = p
		}
	}
	return out
}

// visiblePods drops pinned pods from a status listing meant for users.
func visiblePods(pods []pod.Pod) []pod.Pod {
	out := make([]pod.Pod, 0, len(pods))
	for _, p := range pods {
		if !tainer.IsPinnedProject(p.Name) {
			out = append(out, p)
		}
	}
	return out
}
