package dns

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolverConfig returns the file path and content for the OS-specific DNS resolver.
func ResolverConfig() (path, content string) {
	switch runtime.GOOS {
	case "darwin":
		return "/etc/resolver/tainer.me", "nameserver 127.0.0.1\nport 7753\n"
	case "linux":
		return "/etc/systemd/resolved.conf.d/tainer.conf",
			"[Resolve]\nDNS=127.0.0.1\nDomains=~tainer.me\n"
	default:
		return "", ""
	}
}

func isInstalled(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsResolverInstalled checks if the offline DNS resolver is already set up.
func IsResolverInstalled() bool {
	path, _ := ResolverConfig()
	if path == "" {
		return true // unsupported OS, skip
	}
	return isInstalled(path)
}

// InstallResolver installs the offline DNS resolver config.
// Requires sudo — prompts the user.
func InstallResolver() error {
	path, content := ResolverConfig()
	if path == "" {
		return fmt.Errorf("offline DNS not supported on %s", runtime.GOOS)
	}

	dir := filepath.Dir(path)

	// Create parent directory if needed (e.g., /etc/resolver/ on macOS)
	mkdirCmd := exec.Command("sudo", "mkdir", "-p", dir)
	mkdirCmd.Stdout = os.Stdout
	mkdirCmd.Stderr = os.Stderr
	mkdirCmd.Stdin = os.Stdin
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	// Write resolver config via sudo tee
	teeCmd := exec.Command("sudo", "tee", path)
	teeCmd.Stdin = strings.NewReader(content)
	teeCmd.Stderr = os.Stderr
	if err := teeCmd.Run(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	// On Linux, restart systemd-resolved
	if runtime.GOOS == "linux" {
		restartCmd := exec.Command("sudo", "systemctl", "restart", "systemd-resolved")
		restartCmd.Stdout = os.Stdout
		restartCmd.Stderr = os.Stderr
		if err := restartCmd.Run(); err != nil {
			return fmt.Errorf("restarting systemd-resolved: %w", err)
		}
	}

	return nil
}
