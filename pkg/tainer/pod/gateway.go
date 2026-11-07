package pod

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/docker/docker/api/types/container"
)

// gatewayTarget is where the daemon's control/ subdirectory appears inside the
// control pod. Only that subdirectory is mounted, never the state directory
// root: the root also holds account.json, whose refresh token the control pod
// must never see. The directory (not the individual files) is mounted because
// the files exist only while the gateway runs and the runtime hard-fails on a
// bind mount whose source is missing.
const gatewayTarget = "/run/tainer/control"

// controlDevAccessEnv tells the built-in dashboard how many dashboard
// checkouts are approved on this machine, so it can show a notice while any is.
const controlDevAccessEnv = "TAINER_CONTROL_DEV_ACCESS"

// controlAccess is one access decision for a pod, made once per start so a
// container's image, mounts, env and stamp all agree.
type controlAccess struct {
	granted  bool
	pinned   bool // the built-in control pod
	devCount int  // approved checkouts, reported to the control pod
}

// resolveControlAccess decides whether a project's app container gets the
// control pod's access (gateway token, router address, control runtime): the
// built-in control pod, or a checkout carrying the control-dev marker whose
// folder the user approved on this machine. The marker alone grants nothing,
// and neither does an approval for another folder. Evaluated at every
// container create and start.
func resolveControlAccess(m *manifest.Manifest, projectDir string) controlAccess {
	if tainer.IsPinnedProject(m.Project.Name) {
		return controlAccess{granted: true, pinned: true, devCount: controldev.Count()}
	}
	return controlAccess{granted: m.Project.ControlDev && projectDir != "" && controldev.Approved(projectDir)}
}

func hasControlAccess(m *manifest.Manifest, projectDir string) bool {
	return resolveControlAccess(m, projectDir).granted
}

// gatewayInjection returns the mounts and env that hand a project with
// control access its gateway address and bearer token. Other projects, other
// roles, and an unknown state directory get nothing; TAINER_PROJECT_PATH is the
// project's own host directory, which the webapp hands to the CLI for
// db-import file paths; the webapp then renders
// its gateway-unreachable state.
func gatewayInjection(m *manifest.Manifest, role, stateDir, projectDir string) ([]engine.Mount, []string) {
	return resolveControlAccess(m, projectDir).gateway(role, stateDir, projectDir)
}

// gateway is gatewayInjection for this decision. The built-in control pod
// also gets the number of approved checkouts.
func (a controlAccess) gateway(role, stateDir, projectDir string) ([]engine.Mount, []string) {
	if !a.granted || role != RoleApp || stateDir == "" {
		return nil, nil
	}
	env := []string{
		"TAINER_GATEWAY_ADDR_FILE=" + gatewayTarget + "/control.addr",
		"TAINER_CONTROL_TOKEN_FILE=" + gatewayTarget + "/control.token",
		"TAINER_PROJECT_PATH=" + projectDir,
	}
	if a.pinned {
		env = append(env, controlDevAccessEnv+"="+strconv.Itoa(a.devCount))
	}
	return []engine.Mount{{Source: filepath.Join(stateDir, "control"), Target: gatewayTarget, ReadOnly: true}}, env
}

// controlAccessStamp is the LabelControlAccess value a role's container must
// carry: "" without access (and on the control pod while no checkout is
// approved, matching containers made before the dev track), "granted" for an
// approved checkout, "dev-access:<n>" on the control pod with n approvals.
// A container whose stamp differs is recreated at the next start.
func controlAccessStamp(m *manifest.Manifest, projectDir, role string) string {
	return resolveControlAccess(m, projectDir).stamp(role)
}

func (a controlAccess) stamp(role string) string {
	switch {
	case !a.granted || role != RoleApp:
		return ""
	case a.pinned && a.devCount > 0:
		return "dev-access:" + strconv.Itoa(a.devCount)
	case a.pinned:
		return ""
	default:
		return "granted"
	}
}

// accessChanged reports whether an existing container was made under another
// access decision than want, so it must be recreated rather than restarted:
// env and mounts are fixed when a container is created.
func accessChanged(insp container.InspectResponse, want string) bool {
	have := ""
	if insp.Config != nil {
		have = insp.Config.Labels[LabelControlAccess]
	}
	return have != want
}

// ensureGatewayDirs creates each mount source that is missing, 0700 like the
// daemon's own control/ directory, so an older daemon or a gateway that is not
// up yet leaves an empty directory instead of a failed bind mount.
func ensureGatewayDirs(mounts []engine.Mount) {
	for _, mt := range mounts {
		_ = os.MkdirAll(mt.Source, 0o700)
	}
}
