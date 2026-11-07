//go:build unix

package pod

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens a file that came with the project for reading,
// refusing anything but a plain regular file. O_NOFOLLOW refuses a link at
// the last step, O_NONBLOCK keeps a FIFO from blocking the open, and the
// checks run on the opened file itself, so nothing can be swapped in
// between. A file with a second hard link is refused: it may be another
// project's file under this name.
func openNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("refusing to read %s: it is a link; tainer only reads a real file there", path)
		}
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("refusing to read %s: it is not a regular file", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		f.Close()
		return nil, fmt.Errorf("refusing to read %s: it is linked from somewhere else (a hard link); tainer only reads a file of the project's own there", path)
	}
	return f, nil
}
