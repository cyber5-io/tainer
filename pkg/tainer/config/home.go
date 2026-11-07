package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CheckInsideHome reports, with a plain message, when tainer's settings
// folder is outside the user's home folder. Pods can only be given files
// from the home folder, and the router and every pod mount files kept
// there (keys, certificates, router config).
func CheckInsideHome() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find your home folder: %w", err)
	}
	return checkInsideHome(home, BaseDir())
}

func checkInsideHome(home, dir string) error {
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return fmt.Errorf("home folder %s: %w", home, err)
	}
	real, err := resolveExisting(dir)
	if err != nil {
		return fmt.Errorf("tainer settings folder %s: %w", dir, err)
	}
	if real == realHome || strings.HasPrefix(real, realHome+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("tainer's settings folder %s is outside your home folder (%s), and tainer can only share files from your home folder with your pods; unset XDG_CONFIG_HOME, or point it (or ~/.config) at a folder inside your home folder", dir, home)
}

// resolveExisting resolves links in dir, which may not exist yet: its
// nearest existing ancestor is resolved and the rest is joined on.
func resolveExisting(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	rest := ""
	for p := abs; ; p = filepath.Dir(p) {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p {
			return "", err
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}
