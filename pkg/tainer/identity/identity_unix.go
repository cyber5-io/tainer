//go:build !windows

package identity

import (
	"fmt"
	"os"
	"syscall"
)

// DetectFromDir returns the uid/gid of the directory owner.
func DetectFromDir(dir string) (uint32, uint32, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return 0, 0, fmt.Errorf("stat %s: %w", dir, err)
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("unsupported platform: cannot read uid/gid")
	}
	return stat.Uid, stat.Gid, nil
}
