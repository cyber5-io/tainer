package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

// controlUpdateOps is everything updateProject touches, so tests can run it
// against an in-memory runtime and registry.
type controlUpdateOps struct {
	check  func(ctx context.Context, running string) (imagecheck.ControlCheck, error)
	pull   func(ctx context.Context, ref string) error
	update func(ctx context.Context, opts pod.StartOptions) (pod.UpdateResult, error)
	regGet func(name string) (registry.Project, bool)
	regPut func(name string, p registry.Project) error
	// lock waits for the control lock the ensure also takes.
	lock func(ctx context.Context) (release func(), err error)
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
		lock:   waitControlLock,
	}
}

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

// updateProject refreshes one pod's images and recreates it. For the control
// pod it first checks the stable channel: a newer image this tainer may run
// is pulled by its version and recorded in the registry, so the recreated
// pod runs it; an older or equal one changes nothing (never a downgrade).
// The control pod always updates from its built-in folder, keeping data/.
// note is set when a newer control image needs a newer tainer.
func updateProject(ctx context.Context, name, manifestPath, projectDir string, ops controlUpdateOps) (res pod.UpdateResult, note string, err error) {
	if !tainer.IsPinnedProject(name) {
		res, err = ops.update(ctx, pod.StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir})
		return res, "", err
	}
	// Hold the ensure's lock throughout, so it never recreates the pod from
	// the old version between this update's remove and start.
	release, err := ops.lock(ctx)
	if err != nil {
		return res, "", err
	}
	defer release()

	dir := config.ControlDir()
	p, ok := ops.regGet(name)
	if !ok {
		return res, "", withCode("project-not-found", fmt.Errorf("the control dashboard isn't set up yet"))
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

// controlHeldNote is the user-facing line for a control image held back.
func controlHeldNote(c imagecheck.ControlCheck) string {
	return fmt.Sprintf("control dashboard %s needs a newer tainer (%s or later) — update tainer first", c.Stable, c.MinTainer)
}
