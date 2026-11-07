package pod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// A project folder can come from anywhere (a git clone, a zip), so the
// folders tainer shares with a pod are checked on disk before anything is
// mounted: each must be a real folder inside the project. A link — even one
// pointing back inside — is refused, because what a link points at is
// decided by whoever wrote the project, not by tainer.

// projectRoot resolves the project folder itself. Links above the project
// (~/Sites -> ~/dev/sites) are the user's own and are allowed; everything
// is then checked against, and mounted from, the real path — spelled as it
// is on disk, so `cd /users/leni/dev/proj` on a case-insensitive disk still
// gives mounts under the home folder's exact spelling.
func projectRoot(projectDir string) (string, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("project folder %s: %w", projectDir, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project folder %s: %w", projectDir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("project folder %s is not a folder", projectDir)
	}
	root, err := engine.CanonicalDir(abs)
	if err != nil {
		return "", fmt.Errorf("project folder %s: %w", projectDir, err)
	}
	return root, nil
}

// projectFolder returns root/rel after checking that every step from root
// down to it is a real folder (never a link), creating the missing ones
// (git doesn't carry empty folders, so a fresh clone often lacks data/, db/
// or even html/). label names the folder in errors. rel "" is the project
// itself.
func projectFolder(root, rel, label string) (string, error) {
	if rel == "" {
		return root, nil
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(rel) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to mount %s: it is outside the project", label)
	}
	cur := root
	for _, el := range strings.Split(clean, string(filepath.Separator)) {
		cur = filepath.Join(cur, el)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			// Mkdir (not MkdirAll) creates exactly this step; it fails
			// rather than follow anything that appeared in the meantime.
			if err := os.Mkdir(cur, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return "", fmt.Errorf("create %s: %w", label, err)
			}
			fi, err = os.Lstat(cur)
		}
		if err != nil {
			return "", fmt.Errorf("check %s: %w", label, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", linkError(root, cur, label)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("refusing to mount %s: it is not a folder", label)
		}
	}
	// Belt and braces: the real path must be the path we checked.
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", fmt.Errorf("check %s: %w", label, err)
	}
	if real != cur || !insideRoot(root, real) {
		return "", fmt.Errorf("refusing to mount %s: it is outside the project", label)
	}
	return cur, nil
}

func linkError(root, link, label string) error {
	if target, err := filepath.EvalSymlinks(link); err != nil || !insideRoot(root, target) {
		return fmt.Errorf("refusing to mount %s: it is a link pointing outside the project", label)
	}
	return fmt.Errorf("refusing to mount %s: it is a link; tainer only shares real folders inside the project", label)
}

func insideRoot(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
