package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// controlBackgroundNote is what `tainer update` says for the control pod: the
// dashboard may be the one asking, and recreating the pod ends its request.
const controlBackgroundNote = "Updating tainer control in the background; my.tainer.me will reload"

// controlUpdateBudget bounds the detached control update worker.
const controlUpdateBudget = 10 * time.Minute

// controlUpdateOps is everything updateProject and the control update worker
// touch, so tests can run them against an in-memory runtime and registry.
type controlUpdateOps struct {
	check  func(ctx context.Context, running string) (imagecheck.ControlCheck, error)
	pull   func(ctx context.Context, ref string) error
	update func(ctx context.Context, opts pod.StartOptions) (pod.UpdateResult, error)
	regGet func(name string) (registry.Project, bool)
	regPut func(name string, p registry.Project) error
	// lock waits for the control lock the ensure also takes.
	lock controlLocker
	// launch starts the detached control update worker.
	launch func() error
}

func liveControlUpdateOps(eng *engine.Client) controlUpdateOps {
	return controlUpdateOps{
		check: func(ctx context.Context, running string) (imagecheck.ControlCheck, error) {
			return imagecheck.CheckControl(ctx, controlCheckDeps(), running)
		},
		pull: eng.Pull,
		update: func(ctx context.Context, opts pod.StartOptions) (pod.UpdateResult, error) {
			return pod.Update(ctx, eng, opts, pod.UpdateOptions{})
		},
		regGet: registry.Get,
		regPut: registry.Put,
		lock:   waitForControlLock,
		launch: func() error { return launchControlUpdate() },
	}
}

// Seams for tests.
var (
	launchControlUpdate = func() error { return launchDetached([]string{"control", "update"}, "control-update.log", nil) }
	controlUpdateOpsFor = liveControlUpdateOps
)

// controlCheckDeps are the registry lookups the control pod's channel check
// needs, with this build's version.
func controlCheckDeps() imagecheck.Deps {
	return imagecheck.Deps{
		RemoteLabels: func(ctx context.Context, ref imagecheck.Ref, arch string) (map[string]string, error) {
			return imagecheck.RemoteConfigLabels(ctx, nil, ref, arch)
		},
		TainerVersion: version,
	}
}

// updateOutcome is what updating one pod did.
type updateOutcome struct {
	Started bool
	// Note is a line for the user (a held-back image, a background update).
	Note string
	// Background: the update goes on in a detached worker (the control pod).
	Background bool
}

// updateProject refreshes one pod's images and recreates it. The control pod
// is handed to a detached worker and this returns at once: the dashboard may
// be the caller, and recreating the pod ends the dashboard's own request.
func updateProject(ctx context.Context, name, manifestPath, projectDir string, ops controlUpdateOps) (updateOutcome, error) {
	if !tainer.IsPinnedProject(name) {
		res, err := ops.update(ctx, pod.StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir})
		return updateOutcome{Started: res.Started}, err
	}
	p, ok := ops.regGet(name)
	if !ok {
		return updateOutcome{}, withCode("project-not-found", fmt.Errorf("the control dashboard isn't set up yet"))
	}
	if p.Path != config.ControlDir() {
		if err := pod.ControlNameConflict(p); err != nil {
			return updateOutcome{}, withCode("project-name-taken", errors.New(controlRenameMessage(controlConflict(err))))
		}
		return updateOutcome{}, fmt.Errorf("the control dashboard isn't set up yet — try again in a moment")
	}
	if err := ops.launch(); err != nil {
		return updateOutcome{}, fmt.Errorf("couldn't start the control dashboard update: %w", err)
	}
	return updateOutcome{Started: true, Note: controlBackgroundNote, Background: true}, nil
}

// updateControl is the control update itself, run by the detached worker
// under the control lock. It first checks the stable channel: a newer image
// this tainer may run is pulled by its version and recorded in the registry,
// so the recreated pod runs it; an older or equal one changes nothing (never a
// downgrade). The pod always updates from its built-in folder, keeping data/.
// note is set when a newer control image needs a newer tainer.
func updateControl(ctx context.Context, ops controlUpdateOps) (res pod.UpdateResult, note string, err error) {
	const name = "control"
	dir := config.ControlDir()
	p, ok := ops.regGet(name)
	if !ok || p.Path != dir {
		return res, "", fmt.Errorf("the control dashboard isn't set up yet")
	}
	c, err := ops.check(ctx, pod.ControlVersion(p.ImageVersion))
	if err != nil {
		return res, "", fmt.Errorf("couldn't check for a newer control dashboard: %w", err)
	}
	prev := p
	switch c.State {
	case imagecheck.ControlUpdate:
		if err := ops.pull(ctx, pod.ControlImageRef(c.Stable)); err != nil {
			return res, "", fmt.Errorf("download control dashboard %s: %w", c.Stable, err)
		}
		p.ImageVersion = c.Stable
		if err := ops.regPut(name, p); err != nil {
			return res, "", fmt.Errorf("record control dashboard %s: %w", c.Stable, err)
		}
	case imagecheck.ControlNeedsNewerTainer:
		note = controlHeldNote(c)
	}
	res, err = ops.update(ctx, pod.StartOptions{ManifestPath: filepath.Join(dir, manifest.FileName), ProjectDir: dir})
	if err != nil && p.ImageVersion != prev.ImageVersion {
		// The old dashboard may still be running: record what runs, so the
		// next check offers the update again.
		if rerr := ops.regPut(name, prev); rerr != nil {
			err = fmt.Errorf("%w (and restoring the recorded version failed: %v)", err, rerr)
		}
	}
	return res, note, err
}

// runControlUpdate is the detached `tainer control update` worker: it waits
// for the control lock, updates the control pod through recreateControl and
// logs the outcome to <BaseDir>/control-update.log.
func runControlUpdate() int {
	runtime.OnReady = nil // the worker recreates the pod itself
	ctx, cancel := context.WithTimeout(context.Background(), controlUpdateBudget)
	defer cancel()
	cli, err := controlEngine(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cli.Close()
	if err := controlUpdateWorker(ctx, controlBootID(cli), controlUpdateOpsFor(cli)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "tainer control updated")
	return 0
}

// controlUpdateWorker runs updateControl as a control recreate for boot.
func controlUpdateWorker(ctx context.Context, boot string, ops controlUpdateOps) error {
	_, err := recreateControl(ctx, boot, ops.lock, true, func(ctx context.Context) error {
		_, note, err := updateControl(ctx, ops)
		if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		return err
	})
	return err
}

// controlHeldNote is the user-facing line for a control image held back.
func controlHeldNote(c imagecheck.ControlCheck) string {
	return fmt.Sprintf("control dashboard %s needs a newer tainer (%s or later) — update tainer first", c.Stable, c.MinTainer)
}
