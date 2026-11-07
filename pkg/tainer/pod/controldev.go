package pod

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// ReconcileControlAccess re-applies the control-access check to the pod of
// the project at projectDir right away, after its approval changed: an app
// container made under another decision is removed, and recreated if the pod
// was running. It fails closed: the old container is gone even when the
// restart fails. A pod created from another folder, or none, is left alone.
func ReconcileControlAccess(ctx context.Context, eng *engine.Client, manifestPath, projectDir string) error {
	return reconcileControlAccess(ctx, liveControlOps(eng), manifestPath, projectDir)
}

func reconcileControlAccess(ctx context.Context, ops controlOps, manifestPath, projectDir string) error {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}
	if isControlPod(m, projectDir) {
		return nil // the built-in pod is EnsureControlPod's
	}
	app := ContainerName(m.Project.Name, RoleApp)
	insp, err := ops.inspect(ctx, app)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if insp.Config == nil || !sameFolder(insp.Config.Labels[LabelManifestPath], manifestPath) {
		return nil
	}
	if !accessChanged(insp, controlAccessStamp(m, projectDir, RoleApp)) {
		return nil
	}
	running := insp.State != nil && insp.State.Running
	if !running {
		if web, err := ops.inspect(ctx, ContainerName(m.Project.Name, RoleWeb)); err == nil && web.State != nil && web.State.Running {
			running = true
		}
	}
	if err := ops.remove(ctx, app); err != nil {
		return err
	}
	if !running {
		return nil
	}
	if err := ops.start(ctx, StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir}); err != nil {
		return fmt.Errorf("restart %s: %w", m.Project.Name, err)
	}
	return nil
}

// sameFolder reports whether two tainer.yaml paths are in the same folder,
// however each is spelled.
func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, err1 := controldev.Canonical(filepath.Dir(a))
	cb, err2 := controldev.Canonical(filepath.Dir(b))
	return err1 == nil && err2 == nil && ca == cb
}
