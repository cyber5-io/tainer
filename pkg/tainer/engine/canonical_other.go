//go:build !darwin

package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalDir returns the real path of the folder dir, with links
// resolved. Elsewhere names are spelled one way only, so resolving links
// is all there is to it.
func CanonicalDir(dir string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a folder", dir)
	}
	return real, nil
}
