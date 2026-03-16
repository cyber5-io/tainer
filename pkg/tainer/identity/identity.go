package identity

import (
	"fmt"
	"os"
	"syscall"
)

// Detect returns the uid/gid to inject into containers.
// In local dev, reads from project directory owner.
func Detect(projectDir string) (uint32, uint32, error) {
	// TODO(prod): when /etc/blenzi.yaml exists, parse it for project-specific uid/gid
	return DetectFromDir(projectDir)
}

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

// EnvFlags returns the --env flags to pass to container run commands.
func EnvFlags(uid, gid uint32) []string {
	return []string{
		"--env", fmt.Sprintf("TAINER_UID=%d", uid),
		"--env", fmt.Sprintf("TAINER_GID=%d", gid),
	}
}
