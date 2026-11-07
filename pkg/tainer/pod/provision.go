package pod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	// listGranted lists containers stamped with dev control access.
	listGranted func(ctx context.Context) ([]container.Summary, error)
	regGet      func(name string) (registry.Project, bool)
	regPut      func(name string, p registry.Project) error
}

// ErrControlDownload and ErrControlStart classify EnsureControlPod failures
// so callers can explain them in plain words without echoing lower layers.
var (
	ErrControlDownload = errors.New("control image download failed")
	ErrControlStart    = errors.New("control pod start failed")
	// ErrControlUnavailable: the registry refused or doesn't have the image
	// (401/403/404) — it isn't published yet, rather than us being offline.
	ErrControlUnavailable = errors.New("control image not available")
)

// ErrControlNameTaken: the user has their own project named control (allowed
// before the name was reserved). tainer leaves it alone and doesn't set up
// the dashboard until the user renames it. Returned as *ControlNameTakenError.
var ErrControlNameTaken = errors.New("a project of yours is named control")

// ControlNameTakenError names the folder of the user's own control project.
type ControlNameTakenError struct{ Path string }

// Error tells the user how to free the name.
func (e *ControlNameTakenError) Error() string { return ControlRenameMessage(e.Path) }

// ControlRenameMessage tells the user whose project in path is named control
// how to free the name for the dashboard.
func ControlRenameMessage(path string) string {
	return fmt.Sprintf("your project \"control\" in %s has the name tainer needs for its dashboard — rename it (change project.name in its tainer.yaml) and tainer will set up my.tainer.me", path)
}

func (e *ControlNameTakenError) Is(target error) bool { return target == ErrControlNameTaken }

// unavailableImage matches a registry's 401/403/404 answer as the runtime
// reports it (its pull errors carry the registry's words, not a status).
var unavailableImage = regexp.MustCompile(`(?i)manifest unknown|name unknown|unauthorized|\bdenied\b|forbidden|not found|\b40[134]\b`)

// downloadErr classifies a failed control image pull.
func downloadErr(role string, err error) error {
	if unavailableImage.MatchString(err.Error()) {
		return fmt.Errorf("%w: %s: %w", ErrControlUnavailable, role, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrControlDownload, role, err)
}

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
		listGranted: func(ctx context.Context) ([]container.Summary, error) {
			// Filter on the pod label (as List does) and match the stamp
			// here, so a runtime without value filters can't widen this.
			all, err := eng.ContainerList(ctx, container.ListOptions{All: true, Filters: engine.LabelFilter(LabelPod, "")})
			if err != nil {
				return nil, err
			}
			var out []container.Summary
			for _, c := range all {
				if c.Labels[LabelControlAccess] == stampGranted {
					out = append(out, c)
				}
			}
			return out, nil
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
//   - migrates a hand-registered dashboard (a control project that is the
//     nodejs app on my.tainer.me, or marked control-dev): its
//     data/settings.json and data/thumbnails/ are copied over (only when
//     absent here), the old folder is left untouched, and the registry entry
//     is re-pointed to ControlDir() keeping its recorded image version;
//   - leaves a user's own project named control (folder, pod, data, entry)
//     untouched and returns ErrControlNameTaken until it is renamed;
//   - replaces control containers created from another folder, with an
//     html/ bind mount (the pre-sealed-image shape), from another definition
//     or (app) from another image than this build wants, pulling the images
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
		if !ops.hasImage(ctx, ImageRefIn(m, role, config.ControlDir())) {
			return false
		}
	}
	return true
}

func ensureControlPod(ctx context.Context, ops controlOps) error {
	// First, before anything that can fail offline: the runtime restores
	// containers on its own at boot, so drop any dev checkout's container
	// that holds control access its folder no longer has.
	sweepErr := sweepDevAccess(ctx, ops)
	if err := ensureBuiltInControlPod(ctx, ops); err != nil {
		return errors.Join(err, sweepErr)
	}
	return sweepErr
}

func ensureBuiltInControlPod(ctx context.Context, ops controlOps) error {
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
	access := resolveControlAccess(m, dir)
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
		if staleControlContainer(insp, manifestPath) || outdatedControlContainer(insp, m, role, dir) {
			stale = true
		}
		if insp.State == nil || !insp.State.Running {
			needStart = true
		}
		// A changed number of approved dashboard checkouts: Start recreates
		// the app container so the dashboard's notice is current.
		if accessChanged(insp, access.stamp(role)) {
			needStart = true
		}
	}

	if !stale && !needStart {
		return nil
	}

	// Download missing images before tearing anything down: if they can't
	// be fetched (offline), an old dashboard keeps running until next time.
	for _, role := range roles {
		ref := ImageRefIn(m, role, dir)
		if ops.hasImage(ctx, ref) {
			continue
		}
		if err := ops.pull(ctx, ref); err != nil {
			return downloadErr(role, err)
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

	if err := ops.start(ctx, StartOptions{ManifestPath: manifestPath, ProjectDir: dir, PullMissingOnly: true}); err != nil {
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

// pointRegistryAtControlDir registers the control project at dir. A folder
// registered as control before is migrated only when it is the dashboard
// (settings and thumbnails copied first). A user's own project named control
// is never touched: ErrControlNameTaken. One whose folder no longer has a
// tainer.yaml is stale; its name is taken over without copying anything.
func pointRegistryAtControlDir(ops controlOps, m *manifest.Manifest, dir string) error {
	name := m.Project.Name
	p, ok := ops.regGet(name)
	if ok && p.Path == dir {
		return nil
	}
	if ok && p.Path != "" {
		switch isDashboard, stale := dashboardEntry(p); {
		case isDashboard:
			if err := migrateControlData(filepath.Join(p.Path, "data"), filepath.Join(dir, "data")); err != nil {
				return err
			}
		case !stale:
			return &ControlNameTakenError{Path: p.Path}
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

// dashboardEntry reports whether a registered control project is the control
// dashboard — its tainer.yaml marks it control-dev, or it is the nodejs app on
// my.tainer.me — and whether it is stale (no tainer.yaml left). A tainer.yaml
// that can't be read is judged by the registry entry, and is never stale.
func dashboardEntry(p registry.Project) (isDashboard, stale bool) {
	mf := filepath.Join(p.Path, manifest.FileName)
	if !pathExists(mf) {
		return p.Type == string(manifest.TypeNodeJS) && p.Domain == controlDomain, true
	}
	if m, err := manifest.Load(mf); err == nil {
		return m.Project.ControlDev || (m.Project.Type == manifest.TypeNodeJS && m.Project.Domain == controlDomain), false
	}
	return p.Type == string(manifest.TypeNodeJS) && p.Domain == controlDomain, false
}

// ControlNameConflict returns a *ControlNameTakenError when the registered
// control project p is the user's own rather than the dashboard, else nil.
func ControlNameConflict(p registry.Project) error {
	if p.Path == "" || p.Path == config.ControlDir() {
		return nil
	}
	if isDashboard, stale := dashboardEntry(p); !isDashboard && !stale {
		return &ControlNameTakenError{Path: p.Path}
	}
	return nil
}

// controlDomain is the dashboard's domain, from the built-in definition.
const controlDomain = "my.tainer.me"

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

// outdatedControlContainer reports whether a control container was made by
// another tainer build: its definition hash differs from the built-in one, or
// the app runs another image than the one this build wants (a tainer upgrade
// that raised the pinned version, or an update that recorded a newer one).
func outdatedControlContainer(insp container.InspectResponse, m *manifest.Manifest, role, dir string) bool {
	if insp.Config == nil {
		return true
	}
	if insp.Config.Labels[LabelManifestHash] != ManifestHash(m) {
		return true
	}
	if role != RoleApp {
		return false
	}
	have := containerImage(insp)
	return have != "" && have != ImageRefIn(m, RoleApp, dir)
}

// containerImage is the image ref a container was created from. The runtime
// reports it at the top level and leaves Config.Image empty; a Docker-style
// API reports it in Config.Image (its top-level Image is the image ID).
func containerImage(insp container.InspectResponse) string {
	if insp.Config != nil && insp.Config.Image != "" {
		return insp.Config.Image
	}
	if insp.ContainerJSONBase != nil && !strings.HasPrefix(insp.Image, "sha256:") {
		return insp.Image
	}
	return ""
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func isRegular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// sweepDevAccess removes every container stamped with dev control access
// whose folder no longer grants it: approval revoked, marker or folder gone,
// project renamed. Removed containers come back only through a tainer start,
// which checks the approval again. Runs once per runtime boot.
func sweepDevAccess(ctx context.Context, ops controlOps) error {
	if ops.listGranted == nil {
		return nil
	}
	cs, err := ops.listGranted(ctx)
	if err != nil {
		return fmt.Errorf("check dashboard checkouts: %w", err)
	}
	var errs []error
	for _, c := range cs {
		project, role := c.Labels[LabelPod], c.Labels[LabelRole]
		if project == "" || role == "" {
			continue
		}
		if devAccessStillGranted(c.Labels[LabelManifestPath], project, role) {
			continue
		}
		if err := ops.remove(ctx, ContainerName(project, role)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// devAccessStillGranted reports whether the folder a granted container was
// created from still grants that container its access.
func devAccessStillGranted(manifestPath, project, role string) bool {
	if manifestPath == "" {
		return false
	}
	m, err := manifest.Load(manifestPath)
	if err != nil || m.Project.Name != project {
		return false
	}
	return controlAccessStamp(m, filepath.Dir(manifestPath), role) == stampGranted
}
