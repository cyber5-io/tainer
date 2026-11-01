package pod

import (
	"os"
	"path/filepath"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// gatewayTarget is where the daemon's control/ subdirectory appears inside the
// control pod. Only that subdirectory is mounted, never the state directory
// root: the root also holds account.json, whose refresh token the control pod
// must never see. The directory (not the individual files) is mounted because
// the files exist only while the gateway runs and the runtime hard-fails on a
// bind mount whose source is missing.
const gatewayTarget = "/run/tainer/control"

// gatewayInjection returns the mounts and env that hand the control pod's app
// container the gateway address and bearer token. Ordinary projects, other
// roles, and an unknown state directory get nothing; the webapp then renders
// its gateway-unreachable state.
func gatewayInjection(project, role, stateDir string) ([]engine.Mount, []string) {
	if !tainer.IsPinnedProject(project) || role != RoleApp || stateDir == "" {
		return nil, nil
	}
	return []engine.Mount{{Source: filepath.Join(stateDir, "control"), Target: gatewayTarget, ReadOnly: true}},
		[]string{
			"TAINER_GATEWAY_ADDR_FILE=" + gatewayTarget + "/control.addr",
			"TAINER_CONTROL_TOKEN_FILE=" + gatewayTarget + "/control.token",
		}
}

// ensureGatewayDirs creates each mount source that is missing, 0700 like the
// daemon's own control/ directory, so an older daemon or a gateway that is not
// up yet leaves an empty directory instead of a failed bind mount.
func ensureGatewayDirs(mounts []engine.Mount) {
	for _, mt := range mounts {
		_ = os.MkdirAll(mt.Source, 0o700)
	}
}
