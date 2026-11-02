package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
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

// parseInitSize validates the --size value of init against the named pod
// sizes. Empty keeps the default (small).
func parseInitSize(v string) (manifest.PodSize, error) {
	if v == "" {
		return manifest.PodSizeSmall, nil
	}
	names := make([]string, 0, 6)
	for _, s := range manifest.NamedPodSizes() {
		if string(s) == v {
			return s, nil
		}
		names = append(names, string(s))
	}
	return "", withCode("invalid-arguments", fmt.Errorf("unknown pod size %q (one of: %s)", v, strings.Join(names, ", ")))
}

// withRegisteredProjects completes an engine-derived pod list with a stopped
// row for every registered project that has no pod, so all-projects status
// agrees with `list`. Pinned projects stay out; the added rows follow the
// engine rows in name order.
func withRegisteredProjects(pods []podStatus, reg map[string]registry.Project) []podStatus {
	seen := make(map[string]bool, len(pods))
	for _, p := range pods {
		seen[p.Name] = true
	}
	var missing []string
	for name := range visibleProjects(reg) {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		pods = append(pods, podStatus{Name: name, State: string(pod.StateStopped)})
	}
	return pods
}

// visibleProjectNames lists every user-visible registered project name.
func visibleProjectNames() []string {
	all := visibleProjects(registry.All())
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	return names
}
