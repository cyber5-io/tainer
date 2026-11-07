//go:build !unix

package pod

import (
	"fmt"
	"io/fs"
	"os"
)

// openNoFollow opens a file that came with the project for reading,
// refusing a link or anything but a regular file. Without O_NOFOLLOW the
// file opened is checked to be the one inspected.
func openNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read %s: it is a link; tainer only reads a real file there", path)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to read %s: it is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, fmt.Errorf("refusing to read %s: it changed while being read", path)
	}
	return f, nil
}
