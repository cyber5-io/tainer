package imagecheck

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cyber5-io/tainer/pkg/tainer/pod"
)

// Labels the sealed control image carries.
const (
	LabelVersion    = "org.opencontainers.image.version"
	LabelMinVersion = "io.tainer.min-version"
)

// ControlChannel is the tag the control image is released on.
const ControlChannel = "stable"

// Control pod check states.
const (
	ControlUpToDate         = "up-to-date"
	ControlUpdate           = "update"
	ControlNeedsNewerTainer = "needs-newer-tainer"
	// ControlUnknown: the channel couldn't be checked (see the pod's error).
	ControlUnknown = "unknown"
)

// ControlCheck is the control pod's version check against the stable channel.
type ControlCheck struct {
	Running   string `json:"running"`
	Stable    string `json:"stable,omitempty"`
	MinTainer string `json:"minTainer,omitempty"`
	State     string `json:"state"`
}

// CheckControl reads the stable channel's labels from the registry (nothing
// is pulled) and decides whether the control pod, running version running,
// should move to it.
func CheckControl(ctx context.Context, d Deps, running string) (ControlCheck, error) {
	ref, err := ParseRef(pod.ControlImageRef(ControlChannel))
	if err != nil {
		return ControlCheck{}, err
	}
	if d.RemoteLabels == nil {
		return ControlCheck{}, fmt.Errorf("no registry label lookup configured")
	}
	labels, err := d.RemoteLabels(ctx, ref, goruntime.GOARCH)
	if err != nil {
		return ControlCheck{}, fmt.Errorf("remote %s: %w", pod.ControlImageRef(ControlChannel), err)
	}
	c := ControlCheck{
		Running:   running,
		Stable:    trimV(labels[LabelVersion]),
		MinTainer: trimV(labels[LabelMinVersion]),
	}
	if c.State, err = ControlDecision(c.Running, c.Stable, c.MinTainer, d.TainerVersion); err != nil {
		return ControlCheck{}, err
	}
	return c, nil
}

// ControlDecision compares versions (with or without a leading "v"):
//
//   - stable ≤ running → up-to-date (an older channel never rolls back);
//   - stable > running and this tainer satisfies min-version → update;
//     a missing min-version or a dev build of tainer ("dev", empty or not a
//     release version) always satisfies it;
//   - otherwise → needs-newer-tainer.
func ControlDecision(running, stable, minVersion, tainerVersion string) (string, error) {
	sv := semverOf(stable)
	if sv == "" {
		if stable == "" {
			return "", fmt.Errorf("the stable control image has no %s label", LabelVersion)
		}
		return "", fmt.Errorf("the stable control image has a bad version %q", stable)
	}
	rv := semverOf(running)
	if rv != "" && semver.Compare(sv, rv) <= 0 {
		return ControlUpToDate, nil
	}
	if minVersion == "" {
		return ControlUpdate, nil
	}
	mv := semverOf(minVersion)
	if mv == "" {
		return "", fmt.Errorf("the stable control image has a bad %s %q", LabelMinVersion, minVersion)
	}
	tv := semverOf(tainerVersion)
	if tv == "" || semver.Compare(tv, mv) >= 0 {
		return ControlUpdate, nil
	}
	return ControlNeedsNewerTainer, nil
}

// semverOf returns v as a "vX.Y.Z" semver string, or "" when it is not one.
func semverOf(v string) string {
	v = trimV(v)
	if v == "" || !semver.IsValid("v"+v) {
		return ""
	}
	return "v" + v
}

func trimV(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
