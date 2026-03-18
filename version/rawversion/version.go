package rawversion

// RawVersion is the raw version string for the Podman engine compatibility layer.
//
// This indirection is needed to prevent semver packages from bloating
// Quadlet's binary size.
const RawVersion = "6.0.0-dev"

// TainerVersion is the Tainer product version, independent of the Podman engine version.
const TainerVersion = "0.1.19"
