package pod

import (
	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// gatewayStateTarget is where the daemon state directory appears inside the
// control pod. The directory is mounted rather than the individual token and
// address files: those only exist while the control gateway runs, and the
// runtime hard-fails on a bind mount whose source is missing (the auto-create
// pass in buildMounts would turn a missing file path into a directory).
const gatewayStateTarget = "/run/tainer/state"

// gatewayInjection returns the mounts and env that hand the control pod's app
// container the gateway address and bearer token. Ordinary projects, other
// roles, and an unknown state directory get nothing; the webapp then renders
// its gateway-unreachable state.
func gatewayInjection(project, role, stateDir string) ([]engine.Mount, []string) {
	if !tainer.IsPinnedProject(project) || role != RoleApp || stateDir == "" {
		return nil, nil
	}
	return []engine.Mount{{Source: stateDir, Target: gatewayStateTarget, ReadOnly: true}},
		[]string{
			"TAINER_GATEWAY_ADDR_FILE=" + gatewayStateTarget + "/control.addr",
			"TAINER_CONTROL_TOKEN_FILE=" + gatewayStateTarget + "/control.token",
		}
}
