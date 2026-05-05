package pod

import (
	"context"
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// UpdateOptions configures an update run.
type UpdateOptions struct {
	BaseOnly bool                 // --base <type>: refresh cache, don't touch pods
	BaseType manifest.ProjectType // populated when BaseOnly=true
}

// Update refreshes a single pod's image and recreates its containers.
// On `tainer update --base <type>` (BaseOnly=true), only the cache
// gets refreshed — running pods are untouched.
func Update(ctx context.Context, eng *engine.Client, opts StartOptions, uopt UpdateOptions) error {
	if uopt.BaseOnly {
		// Pull each role's image for the base type. Use a synthesised
		// manifest so ImageRef can resolve tags.
		m := &manifest.Manifest{Project: manifest.ProjectConfig{Type: uopt.BaseType}}
		for _, role := range RolesForType(uopt.BaseType) {
			if err := eng.Pull(ctx, ImageRef(m, role)); err != nil {
				return fmt.Errorf("update --base: pull %s: %w", role, err)
			}
		}
		return nil
	}

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return err
	}
	for _, role := range RolesForPod(m) {
		if err := eng.Pull(ctx, ImageRef(m, role)); err != nil {
			return fmt.Errorf("update: pull %s: %w", role, err)
		}
	}
	// Recreate containers by stopping + removing + re-running. Volumes
	// preserved (DB data on the named volume; mount binds untouched).
	if err := Stop(ctx, eng, m.Project.Name); err != nil {
		return err
	}
	p, err := Get(ctx, eng, m.Project.Name)
	if err != nil {
		return err
	}
	if p != nil {
		for _, c := range p.Containers {
			if err := eng.Remove(ctx, c.Name, true); err != nil {
				return err
			}
		}
	}
	_, err = Start(ctx, eng, opts)
	return err
}
