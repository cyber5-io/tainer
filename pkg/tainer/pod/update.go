package pod

import (
	"context"
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/progress"
)

// UpdateOptions configures an update run.
type UpdateOptions struct {
	BaseOnly bool                 // --base <type>: refresh cache, don't touch pods
	BaseType manifest.ProjectType // populated when BaseOnly=true
}

// UpdateResult reports what Update did beyond succeeding.
type UpdateResult struct {
	// Started is false when the pod was stopped before the update and
	// was deliberately left stopped. It is also false for BaseOnly runs.
	Started bool
}

// restartAfterUpdate decides whether an update brings the pod back up.
// prior is the pod as it was before the update stopped it. Update never
// boots anything: only a pod with running (or mixed) containers is
// restarted. A stopped pod, or one never started, stays down.
func restartAfterUpdate(prior *Pod) bool {
	return prior != nil && prior.State() != StateStopped
}

// Update refreshes a single pod's image and recreates its containers.
// On `tainer update --base <type>` (BaseOnly=true), only the cache
// gets refreshed — running pods are untouched. A pod that was stopped
// before the update is recreated but left stopped.
func Update(ctx context.Context, eng *engine.Client, opts StartOptions, uopt UpdateOptions) (UpdateResult, error) {
	if uopt.BaseOnly {
		// Pull each role's image for the base type. Use a synthesised
		// manifest so ImageRef can resolve tags. ImageRef interpolates
		// Runtime.PHP / Runtime.Node into the tag (e.g.
		// "tainer-wordpress-app:php-8.3"), so an empty Runtime would
		// give us "php-" → 404. Populate with the same defaults init
		// would write so `--base <type>` pre-caches the image a fresh
		// `tainer init <type>` would actually pull.
		//
		// TODO(0.9.x): consolidate the three places that hardcode
		// runtime defaults (initcmd.defaultManifest, wizard, here).
		// A single `manifest.DefaultRuntime(type)` would let `--base`
		// also grow `--php` / `--node` overrides later.
		m := &manifest.Manifest{
			Project: manifest.ProjectConfig{Type: uopt.BaseType},
			Runtime: defaultRuntimeFor(uopt.BaseType),
		}
		report := progress.Reporter(ctx, "update", string(uopt.BaseType))
		report("pull", "pulling base images", -1, false)
		for _, role := range RolesForType(uopt.BaseType) {
			if err := eng.Pull(ctx, ImageRef(m, role)); err != nil {
				return UpdateResult{}, fmt.Errorf("update --base: pull %s: %w", role, err)
			}
		}
		report("pull", "base images updated", 100, true)
		return UpdateResult{}, nil
	}

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return UpdateResult{}, err
	}
	report := progress.Reporter(ctx, "update", m.Project.Name)
	report("pull", "pulling images", -1, false)
	for _, role := range RolesForPod(m) {
		if err := eng.Pull(ctx, ImageRef(m, role)); err != nil {
			return UpdateResult{}, fmt.Errorf("update: pull %s: %w", role, err)
		}
	}
	report("pull", "images updated", -1, false)
	// Capture the prior state before Stop so a stopped pod stays stopped.
	prior, err := Get(ctx, eng, m.Project.Name)
	if err != nil {
		return UpdateResult{}, err
	}
	return recreate(prior, report, recreateOps{
		stop:   func() error { return Stop(ctx, eng, m.Project.Name) },
		remove: func(name string) error { return eng.Remove(ctx, name, true) },
		start: func() error {
			_, err := Start(ctx, eng, opts)
			return err
		},
	})
}

// recreateOps are the engine actions recreate drives; split out so the
// orchestration can be tested without an engine.
type recreateOps struct {
	stop   func() error
	remove func(name string) error
	start  func() error
}

// recreate stops and removes the prior pod's containers (volumes and
// bind mounts are untouched) and starts the pod again only when
// restartAfterUpdate says so. A pod that was never created is left alone.
func recreate(prior *Pod, report func(stage, msg string, pct int, done bool), ops recreateOps) (UpdateResult, error) {
	if prior == nil {
		report("restart", "images updated (pod not created)", 100, true)
		return UpdateResult{}, nil
	}
	if prior.State() != StateStopped {
		if err := ops.stop(); err != nil {
			return UpdateResult{}, err
		}
	}
	for _, c := range prior.Containers {
		if err := ops.remove(c.Name); err != nil {
			return UpdateResult{}, fmt.Errorf("update: remove %s: %w", c.Name, err)
		}
	}
	if !restartAfterUpdate(prior) {
		report("restart", "pod updated (still stopped)", 100, true)
		return UpdateResult{}, nil
	}
	if err := ops.start(); err != nil {
		return UpdateResult{}, err
	}
	report("restart", "pod updated", 100, true)
	return UpdateResult{Started: true}, nil
}

// defaultRuntimeFor returns the default RuntimeConfig for a project
// type, straight from the manifest.TypeSpec table — the same values
// initcmd writes into a fresh tainer.yaml.
func defaultRuntimeFor(t manifest.ProjectType) manifest.RuntimeConfig {
	if spec, ok := manifest.SpecFor(t); ok {
		return spec.DefaultRuntime
	}
	// Unknown type: node defaults keep Update usable on a manifest
	// the validator would reject anyway.
	return manifest.RuntimeConfig{Node: "20", Database: manifest.DatabaseMariaDB}
}
