package pod

import (
	"fmt"

	"golang.org/x/mod/semver"
)

// controlImageVersion is the sealed control image this tainer build pins as a
// floor. The control pod never runs older than this, and never rolls back
// below a version already recorded in the registry.
const controlImageVersion = "1.0.0"

// controlManifestYAML is the built-in definition of the control pod.
const controlManifestYAML = `version: 1
project:
  name: control
  type: nodejs
  domain: my.tainer.me
runtime:
  node: "22"
  database: none
pod:
  size: small
`

// ControlManifestYAML returns the built-in tainer.yaml for the control pod.
func ControlManifestYAML() []byte { return []byte(controlManifestYAML) }

// ControlImageRef returns the sealed control image reference for a version
// (or a channel tag such as "stable").
func ControlImageRef(version string) string {
	return fmt.Sprintf("%s/tainer-control:%s", imageRepo(), version)
}

// controlVersion returns the greater of the pinned floor and the recorded
// version. An empty or malformed recorded version yields the floor.
func controlVersion(recorded string) string {
	r := "v" + recorded
	if recorded == "" || !semver.IsValid(r) {
		return controlImageVersion
	}
	if semver.Compare(r, "v"+controlImageVersion) > 0 {
		return recorded
	}
	return controlImageVersion
}

// ControlVersion is the control image version the pod runs given the
// version recorded in the registry: never older than this build's pin.
func ControlVersion(recorded string) string { return controlVersion(recorded) }
