package pod

import (
	"os"
	"path/filepath"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/config"
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

// controlDevEnv tells an approved dashboard checkout it is a development copy,
// so it can say so.
const controlDevEnv = "TAINER_CONTROL_DEV"

// stampGranted is the LabelControlAccess value of an approved checkout's app
// container.
const stampGranted = "granted"

// controlAccess is one access decision for a pod, made once per start so a
// container's image, mounts, env and stamp all agree.
type controlAccess struct {
	granted bool
	pinned  bool // the built-in control pod
}

// resolveControlAccess decides whether a project's app container gets the
// control pod's access (gateway token, router address, control runtime): the
// built-in control pod, or a checkout carrying the control-dev marker whose
// folder the user approved on this machine. The marker alone grants nothing,
// and neither does an approval for another folder. Evaluated at every
// container create and start.
//
// Only the built-in folder (config.ControlDir()) is the control pod: a project
// named control anywhere else gets nothing. A checkout must be a nodejs
// project, the dashboard's own type.
func resolveControlAccess(m *manifest.Manifest, projectDir string) controlAccess {
	if tainer.IsPinnedProject(m.Project.Name) {
		if isControlFolder(projectDir) {
			return controlAccess{granted: true, pinned: true}
		}
		return controlAccess{}
	}
	return controlAccess{granted: m.Project.ControlDev && m.Project.Type == manifest.TypeNodeJS &&
		projectDir != "" && controldev.Approved(projectDir)}
}

// isControlFolder reports whether dir is the built-in control pod's folder.
func isControlFolder(dir string) bool {
	if dir == "" {
		return false
	}
	a, err1 := controldev.Canonical(dir)
	b, err2 := controldev.Canonical(config.ControlDir())
	return err1 == nil && err2 == nil && a == b
}

// isControlPod reports whether m in projectDir is the built-in control pod.
func isControlPod(m *manifest.Manifest, projectDir string) bool {
	return tainer.IsPinnedProject(m.Project.Name) && isControlFolder(projectDir)
}

// checkControlName refuses a project that uses the built-in dashboard's
// reserved name from any other folder: *ControlNameTakenError, whose message
// is the one the runtime-ready warning shows.
func checkControlName(m *manifest.Manifest, projectDir string) error {
	if !tainer.IsPinnedProject(m.Project.Name) || isControlFolder(projectDir) {
		return nil
	}
	return &ControlNameTakenError{Path: projectDir}
}

// CheckControlName is checkControlName for the CLI's start/restart.
func CheckControlName(m *manifest.Manifest, projectDir string) error {
	return checkControlName(m, projectDir)
}

// restartPolicyFor is a container's restart policy. An approved checkout's
// app container never restarts by itself: the runtime would bring it back
// (after a crash or when tainer starts) without tainer re-checking the
// approval, so only a tainer start, which checks it, may run it again.
func restartPolicyFor(a controlAccess, role string) string {
	if a.granted && !a.pinned && role == RoleApp {
		return "no"
	}
	return "unless-stopped"
}

func hasControlAccess(m *manifest.Manifest, projectDir string) bool {
	return resolveControlAccess(m, projectDir).granted
}

// IsApprovedControlDev reports whether m in projectDir is a dashboard checkout
// whose access the user approved: the same decision the start path makes, minus
// the built-in control pod.
func IsApprovedControlDev(m *manifest.Manifest, projectDir string) bool {
	a := resolveControlAccess(m, projectDir)
	return a.granted && !a.pinned
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

// gateway is gatewayInjection for this decision. An approved checkout also
// learns it is a development copy.
func (a controlAccess) gateway(role, stateDir, projectDir string) ([]engine.Mount, []string) {
	if !a.granted || role != RoleApp || stateDir == "" {
		return nil, nil
	}
	env := []string{
		"TAINER_GATEWAY_ADDR_FILE=" + gatewayTarget + "/control.addr",
		"TAINER_CONTROL_TOKEN_FILE=" + gatewayTarget + "/control.token",
		"TAINER_PROJECT_PATH=" + projectDir,
	}
	if !a.pinned {
		env = append(env, controlDevEnv+"=1")
	}
	return []engine.Mount{{Source: filepath.Join(stateDir, "control"), Target: gatewayTarget, ReadOnly: true}}, env
}

// controlAccessStamp is the LabelControlAccess value a role's container must
// carry: "" without access and on the control pod, "granted" for an approved
// checkout. A container whose stamp differs is recreated at the next start.
func controlAccessStamp(m *manifest.Manifest, projectDir, role string) string {
	return resolveControlAccess(m, projectDir).stamp(role)
}

func (a controlAccess) stamp(role string) string {
	switch {
	case !a.granted || role != RoleApp:
		return ""
	case a.pinned:
		return ""
	default:
		return stampGranted
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
