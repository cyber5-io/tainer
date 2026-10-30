package engine

import (
	"os"
	"path/filepath"
	goruntime "runtime"
)

// DefaultSocketPath is where cyberstackd listens by default. On macOS it
// mirrors cmd/cyberstackd/main.go's defaultPath("cyberstack.sock"); on Linux
// the daemon is systemd socket-activated at a fixed system path.
func DefaultSocketPath() string {
	home, _ := os.UserHomeDir()
	return socketPathFor(goruntime.GOOS, home)
}

func socketPathFor(goos, home string) string {
	if goos == "linux" {
		return "/run/cyberstack/cyberstack.sock"
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cyberstack", "cyberstack.sock")
}
