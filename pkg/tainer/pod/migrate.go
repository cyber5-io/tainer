package pod

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// legacyAppDir is the source folder's name before the app/ rename. A
// project still carrying it is moved to app/ once, on its next start.
const legacyAppDir = "html"

// appDir is the source folder's name (manifest.HostAppDir). Kept local so
// the guard needs no manifest: it runs before anything is loaded or made.
const appDir = "app"

// MigrateAppDir moves a project's legacy html/ source folder to app/, in
// place, so nothing can make an empty app/ beside a full html/ — the user
// would think their code had vanished. Every path that might create app/
// (start's mounts, init, the generated app/.env, the scaffold hint) runs it
// first.
//
//   - app/ a real folder, no html/ beside it → nothing to do.
//   - app/ absent, html/ a real folder → rename, and say so on out.
//   - app/ an empty folder beside a real html/ (a half-migrated project) →
//     remove the empty app/, rename, same notice.
//   - both hold files → refuse; tainer will not pick one.
//   - html/ (or app/, when html/ is there) a link → refuse; only a real
//     folder is moved, the mount-safety rule.
//   - neither → nothing to do (a fresh project; scaffolding makes app/).
//
// Only os.Rename is used: the two names sit in the same folder, so it is
// one atomic step and nothing is ever copied.
func MigrateAppDir(projectDir string, out io.Writer) error {
	if projectDir == "" {
		return nil
	}
	legacy := filepath.Join(projectDir, legacyAppDir)
	hfi, err := os.Lstat(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check %s/: %w", legacyAppDir, err)
	}
	if hfi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a link; tainer only migrates a real folder", legacyAppDir)
	}
	if !hfi.IsDir() {
		// A file called html is not a source folder; leave it be.
		return nil
	}

	app := filepath.Join(projectDir, appDir)
	afi, err := os.Lstat(app)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The ordinary case: move it.
	case err != nil:
		return fmt.Errorf("check %s/: %w", appDir, err)
	case afi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a link; tainer only migrates a real folder", appDir)
	case !afi.IsDir():
		return bothExist()
	default:
		appEmpty, err := emptyDir(app)
		if err != nil {
			return fmt.Errorf("check %s/: %w", appDir, err)
		}
		if !appEmpty {
			legacyEmpty, err := emptyDir(legacy)
			if err != nil {
				return fmt.Errorf("check %s/: %w", legacyAppDir, err)
			}
			if legacyEmpty {
				// app/ holds the code; a leftover empty html/ is harmless.
				return nil
			}
			return bothExist()
		}
		// os.Remove only removes an empty folder, so anything written
		// into app/ since the check makes this fail rather than vanish.
		if err := os.Remove(app); err != nil {
			return fmt.Errorf("remove empty %s/: %w", appDir, err)
		}
	}

	if err := os.Rename(legacy, app); err != nil {
		return fmt.Errorf("rename %s/ to %s/: %w", legacyAppDir, appDir, err)
	}
	if out != nil {
		fmt.Fprintf(out, "renamed %s/ to %s/\n", legacyAppDir, appDir)
	}
	return nil
}

func bothExist() error {
	return fmt.Errorf("both %s/ and %s/ exist — move your code into %s/ and remove %s/", appDir, legacyAppDir, appDir, legacyAppDir)
}

// emptyDir reports whether dir has no entries at all; dotfiles count as
// content.
func emptyDir(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}
