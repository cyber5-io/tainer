package pod

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

// DestroyMode selects the destroy variant.
type DestroyMode int

const (
	DestroyDefault DestroyMode = iota
	DestroyClean
	DestroyNuke
)

// Destroy removes the pod's containers. Behaviour by mode:
//
//   - DestroyDefault: containers + registry/pod-id slot only; the
//     project's files (tainer.yaml, html/, data/, db/, secrets) are
//     untouched. Fully reversible via `tainer start` from the project
//     dir — same containers come back with the same data.
//   - DestroyClean:   above + tainer-managed files in the project dir
//     (tainer.yaml itself, all .tainer.* dotfiles) AND the per-project
//     secrets file at ~/.cyberstack/tainer/secrets/<name>.env. User
//     data (html/, data/, db/, .git/, anything else they put in the
//     dir) is preserved so a fresh `tainer init` on the same path can
//     adopt the existing data. Recoverable from version control + a
//     re-init.
//   - DestroyNuke:    above + everything else in projectDir. The user
//     is left standing in an empty project dir. Irreversible — the
//     interactive confirmation (`prompt`) gates this. Note the
//     projectDir itself is preserved so the caller's cwd remains
//     valid; only its contents go.
func Destroy(ctx context.Context, eng *engine.Client, podName, projectDir string, mode DestroyMode, prompt func() string) error {
	if mode == DestroyNuke {
		if err := confirmNuke(podName, prompt); err != nil {
			return err
		}
	}

	p, err := Get(ctx, eng, podName)
	if err != nil {
		return err
	}
	if p != nil {
		for _, c := range p.Containers {
			if err := eng.Remove(ctx, c.Name, true); err != nil {
				return fmt.Errorf("remove %s: %w", c.Name, err)
			}
		}
	}
	// Release the registry slot so the pod ID is reusable.
	if err := FreePodID(podName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: free pod id %q: %v\n", podName, err)
	}
	// Drop the project name from the global registry so `tainer init` can
	// re-use it on the same path or a different one. Without this, destroy
	// leaves the name occupied until the user hand-edits projects.json.
	registry.Remove(podName)

	switch mode {
	case DestroyClean:
		if err := cleanProjectDir(projectDir); err != nil {
			return err
		}
		if err := removeSecretsFile(podName); err != nil {
			return err
		}
	case DestroyNuke:
		// Nuke removes everything *in* projectDir (not the dir itself
		// — the caller may be cwd'd into it). We list the dir and
		// RemoveAll each entry; that handles dotfiles, html/, data/,
		// db/, .git/, and anything else the user dropped in there.
		// The secrets file lives outside the project dir so we still
		// have to remove it explicitly.
		if err := wipeProjectDirContents(projectDir); err != nil {
			return err
		}
		if err := removeSecretsFile(podName); err != nil {
			return err
		}
	}
	return nil
}

// tainerManagedFiles is the explicit list of files tainer creates in a
// project dir during init + lifecycle. --clean removes these and only
// these; user content (html/, data/, db/, .git/, etc.) is preserved.
// When adding new tainer-created files anywhere in the codebase, append
// them here so --clean stays exhaustive.
var tainerManagedFiles = []string{
	"tainer.yaml",             // the manifest itself
	".tainer.local.yaml",      // user's local manifest overlay
	".tainer-authorized_keys", // SSH keys staged by pod.Start
	".tainer.local.staging",   // transient staging file
}

// cleanProjectDir removes tainer-managed files from the project dir
// while preserving user content. Used by --clean. Each removal is
// best-effort: missing files are not an error.
func cleanProjectDir(dir string) error {
	for _, name := range tainerManagedFiles {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rm %s: %w", name, err)
		}
	}
	return nil
}

// wipeProjectDirContents removes every entry in dir (files and
// directories) but leaves dir itself in place. Used by --nuke after
// the typed-name confirmation. The dir is preserved because the
// caller is likely cwd'd into it — pulling it out from under them
// would leave the shell in an unusable state.
func wipeProjectDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("rm %s: %w", path, err)
		}
	}
	return nil
}

// removeSecretsFile removes the per-project secrets env file at
// ~/.cyberstack/tainer/secrets/<podName>.env. Best-effort: missing
// file is not an error, matching the rest of the destroy path.
func removeSecretsFile(podName string) error {
	path := SecretsPath(podName)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("rm %s: %w", path, err)
	}
	return nil
}

// confirmNuke prompts the user to type the pod name verbatim.
func confirmNuke(podName string, prompt func() string) error {
	fmt.Fprintf(os.Stderr, "Type %q to confirm nuking ALL data: ", podName)
	got := strings.TrimSpace(prompt())
	if got != podName {
		return fmt.Errorf("confirmation %q did not match pod name %q", got, podName)
	}
	return nil
}
