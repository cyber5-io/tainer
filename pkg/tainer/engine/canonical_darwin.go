package engine

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// CanonicalDir returns the real path of the folder dir, with links resolved
// and every element spelled as it is on disk. macOS disks are usually
// case-insensitive, so /users/leni/dev opens fine, but the runtime matches
// bind sources against the shared home folder by exact spelling. The open
// folder's path (F_GETPATH) carries the on-disk spelling.
func CanonicalDir(dir string) (string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a folder", dir)
	}
	buf := make([]byte, 1024) // MAXPATHLEN
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", fmt.Errorf("%s: %w", dir, errno)
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}
