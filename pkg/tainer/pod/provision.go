package pod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/docker/docker/api/types/container"
)

// controlOps is everything EnsureControlPod touches outside the control
// folder, so tests can run it against an in-memory runtime and registry.
type controlOps struct {
	inspect  func(ctx context.Context, name string) (container.InspectResponse, error)
	remove   func(ctx context.Context, name string) error
	pull     func(ctx context.Context, ref string) error
	hasImage func(ctx context.Context, ref string) bool
	start    func(ctx context.Context, opts StartOptions) error
	regGet   func(name string) (registry.Project, bool)
	regPut   func(name string, p registry.Project) error
}

// ErrControlDownload and ErrControlStart classify EnsureControlPod failures
// so callers can explain them in plain words without echoing lower layers.
var (
	ErrControlDownload = errors.New("control image download failed")
	ErrControlStart    = errors.New("control pod start failed")
)

// writeManifestFile is a seam for tests.
var writeManifestFile = os.WriteFile

func liveControlOps(eng *engine.Client) controlOps {
	return controlOps{
		inspect: eng.Inspect,
		remove:  func(ctx context.Context, name string) error { return eng.Remove(ctx, name, true) },
		pull:    eng.Pull,
		hasImage: func(ctx context.Context, ref string) bool {
			_, err := eng.ImageInspect(ctx, ref)
			return err == nil
		},
		start: func(ctx context.Context, opts StartOptions) error {
			_, err := Start(ctx, eng, opts)
			return err
		},
		regGet: registry.Get,
		regPut: registry.Put,
	}
}

// EnsureControlPod provisions the built-in control pod and makes sure it is
// running. It is idempotent and safe to call on every runtime bring-up:
//
//   - creates ControlDir() and its data/ (0755) and (re)writes tainer.yaml
//     when missing or different from the built-in definition; existing
//     data/ contents are never touched;
//   - migrates a hand-registered control project: its data/settings.json and
//     data/thumbnails/ are copied over (only when absent here), the old folder
//     is left untouched, and the registry entry is re-pointed to ControlDir()
//     keeping its recorded image version;
//   - replaces control containers created from another folder or with an
//     html/ bind mount (the pre-sealed-image shape), pulling the images
//     first so an offline run leaves the old dashboard up;
//   - starts the pod when any of its containers is missing or stopped.
func EnsureControlPod(ctx context.Context, eng *engine.Client) error {
	return ensureControlPod(ctx, liveControlOps(eng))
}

// ControlImagesPresent reports whether every control pod image is already
// stored locally, i.e. whether EnsureControlPod can run without a download.
func ControlImagesPresent(ctx context.Context, eng *engine.Client) bool {
	return controlImagesPresent(ctx, liveControlOps(eng))
}

func controlImagesPresent(ctx context.Context, ops controlOps) bool {
	m, err := manifest.ParseBytes(ControlManifestYAML())
	if err != nil {
		return false
	}
	for _, role := range RolesForPod(m) {
		if !ops.hasImage(ctx, ImageRef(m, role)) {
			return false
		}
	}
	return true
}

func ensureControlPod(ctx context.Context, ops controlOps) error {
	m, err := manifest.ParseBytes(ControlManifestYAML())
	if err != nil {
		return fmt.Errorf("control definition: %w", err)
	}
	dir := config.ControlDir()
	manifestPath := filepath.Join(dir, manifest.FileName)

	if err := writeControlFolder(dir, manifestPath); err != nil {
		return err
	}
	if err := pointRegistryAtControlDir(ops, m, dir); err != nil {
		return err
	}

	roles := RolesForPod(m)
	stale, needStart := false, false
	for _, role := range roles {
		insp, err := ops.inspect(ctx, ContainerName(m.Project.Name, role))
		if err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("check the control pod: %w", err)
			}
			needStart = true
			continue
		}
		if staleControlContainer(insp, manifestPath) {
			stale = true
		}
		if insp.State == nil || !insp.State.Running {
			needStart = true
		}
	}

	if !stale && !needStart {
		return nil
	}

	// Download missing images before tearing anything down: if they can't
	// be fetched (offline), an old dashboard keeps running until next time.
	for _, role := range roles {
		ref := ImageRef(m, role)
		if ops.hasImage(ctx, ref) {
			continue
		}
		if err := ops.pull(ctx, ref); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrControlDownload, role, err)
		}
	}

	if stale {
		// Followers share the leader's network, so replace the whole pod.
		for _, role := range roles {
			if err := ops.remove(ctx, ContainerName(m.Project.Name, role)); err != nil {
				return fmt.Errorf("replace the old control pod: %w", err)
			}
		}
	}

	if err := ops.start(ctx, StartOptions{ManifestPath: manifestPath, ProjectDir: dir}); err != nil {
		return fmt.Errorf("%w: %w", ErrControlStart, err)
	}
	return nil
}

// writeControlFolder creates dir and dir/data and writes the built-in
// tainer.yaml when it is missing or differs. data/ contents are never touched.
func writeControlFolder(dir, manifestPath string) error {
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return fmt.Errorf("prepare the control folder: %w", err)
	}
	want := ControlManifestYAML()
	if have, err := os.ReadFile(manifestPath); err == nil && bytes.Equal(have, want) {
		return nil
	}
	tmp := manifestPath + ".partial"
	if err := writeManifestFile(tmp, want, 0o644); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write the control definition: %w", err)
	}
	if err := os.Rename(tmp, manifestPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write the control definition: %w", err)
	}
	return nil
}

// pointRegistryAtControlDir registers the control project at dir, migrating
// settings and thumbnails from a previously registered folder first.
func pointRegistryAtControlDir(ops controlOps, m *manifest.Manifest, dir string) error {
	name := m.Project.Name
	p, ok := ops.regGet(name)
	if ok && p.Path == dir {
		return nil
	}
	if ok && p.Path != "" {
		if err := migrateControlData(filepath.Join(p.Path, "data"), filepath.Join(dir, "data")); err != nil {
			return err
		}
	}
	if !ok {
		p = registry.Project{Created: time.Now().UTC().Format(time.RFC3339)}
	}
	p.Path = dir
	p.Type = string(m.Project.Type)
	p.Domain = m.Project.Domain
	if err := ops.regPut(name, p); err != nil {
		return fmt.Errorf("register the control pod: %w", err)
	}
	return nil
}

// migrateControlData copies settings.json and thumbnails/ from oldData into
// newData, each only when absent in newData. oldData is only read.
func migrateControlData(oldData, newData string) error {
	src, dst := filepath.Join(oldData, "settings.json"), filepath.Join(newData, "settings.json")
	if isRegular(src) && !pathExists(dst) {
		if err := copyFileMode(src, dst, 0o644); err != nil {
			return fmt.Errorf("migrate dashboard settings: %w", err)
		}
	}
	src, dst = filepath.Join(oldData, "thumbnails"), filepath.Join(newData, "thumbnails")
	if st, err := os.Stat(src); err == nil && st.IsDir() && !pathExists(dst) {
		// Copy into a scratch folder and rename, so an interrupted copy never
		// leaves a partial thumbnails/ that would block the next attempt.
		tmp := dst + ".partial"
		os.RemoveAll(tmp)
		if err := copyTree(src, tmp); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("migrate pod previews: %w", err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("migrate pod previews: %w", err)
		}
	}
	return nil
}

// copyTree copies the regular files and directories under src to dst.
// Symlinks and other special files are skipped.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFileMode(path, target, 0o644)
		default:
			return nil
		}
	})
}

// staleControlContainer reports whether a control container predates the
// built-in pod: it was created from another folder's tainer.yaml, or it
// bind-mounts an html/ app tree.
func staleControlContainer(insp container.InspectResponse, manifestPath string) bool {
	if insp.Config != nil {
		if mp, ok := insp.Config.Labels[LabelManifestPath]; ok && mp != manifestPath {
			return true
		}
	}
	for _, mt := range insp.Mounts {
		if mt.Destination == "/var/www/html" || filepath.Base(mt.Source) == "html" {
			return true
		}
	}
	return false
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func isRegular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
