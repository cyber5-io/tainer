// Package controldev records which checkouts of the control dashboard this
// machine's user has approved to run with the control pod's access.
//
// A project opts in with `project.control-dev: true` in its tainer.yaml, but
// the marker alone grants nothing: the folder must also be approved here, by
// the user, from their own terminal (`tainer control dev allow`). Approvals
// live only in the user's local tainer config, never in a project, and are
// tied to the folder's absolute path: a moved checkout needs a new approval.
package controldev

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

const fileVersion = 1

type approvals struct {
	V        int      `json:"v"`
	Approved []string `json:"approved"`
}

// ErrNotApproved is returned by Revoke for a folder that isn't approved.
var ErrNotApproved = errors.New("this folder isn't approved")

// Path is the approvals file.
func Path() string { return filepath.Join(config.BaseDir(), "control-dev.json") }

// Canonical returns the absolute, cleaned form of path with symlinks
// resolved, so the same folder always compares equal however it is spelled.
// For a path that no longer exists, the deepest existing parent is resolved
// and the rest appended, so a deleted checkout can still be revoked.
func Canonical(path string) (string, error) {
	if path == "" {
		return "", errors.New("no folder given")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	rest := ""
	for p := abs; ; p = filepath.Dir(p) {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// load reads the approvals file. A missing file is an empty list; an
// unreadable or unknown-version one is an error (callers deny on error).
func load() (approvals, error) {
	a := approvals{V: fileVersion}
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return a, fmt.Errorf("read %s: %w", Path(), err)
	}
	if a.V != fileVersion {
		return a, fmt.Errorf("read %s: unknown version %d", Path(), a.V)
	}
	return a, nil
}

// save writes the approvals file atomically (temp file + rename), 0600.
func save(a approvals) error {
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	a.V = fileVersion
	if a.Approved == nil {
		a.Approved = []string{}
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".partial"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, Path()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Approved reports whether path is an approved folder. Any error (no path,
// unreadable approvals) means not approved.
func Approved(path string) bool {
	p, err := Canonical(path)
	if err != nil {
		return false
	}
	a, err := load()
	if err != nil {
		return false
	}
	return slices.Contains(a.Approved, p)
}

// Allow records path as approved. Approving an approved folder is a no-op.
func Allow(path string) error {
	p, err := Canonical(path)
	if err != nil {
		return err
	}
	a, err := load()
	if err != nil {
		return err
	}
	if slices.Contains(a.Approved, p) {
		return nil
	}
	a.Approved = append(a.Approved, p)
	return save(a)
}

// Revoke removes path's approval; ErrNotApproved when it had none.
func Revoke(path string) error {
	p, err := Canonical(path)
	if err != nil {
		return err
	}
	a, err := load()
	if err != nil {
		return err
	}
	i := slices.Index(a.Approved, p)
	if i < 0 {
		return ErrNotApproved
	}
	a.Approved = slices.Delete(a.Approved, i, i+1)
	return save(a)
}

// List returns the approved folders in the order they were approved.
func List() ([]string, error) {
	a, err := load()
	if err != nil {
		return nil, err
	}
	return a.Approved, nil
}

// Count is the number of approved folders; 0 when the file is unreadable.
func Count() int {
	a, err := load()
	if err != nil {
		return 0
	}
	return len(a.Approved)
}
