package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode is the cyberstackd network supervisor mode. Persisted to a
// single file at ~/.cyberstack/network-mode; tainer.runtime reads it
// when spawning cyberstackd and passes it as the -network-mode flag.
type Mode string

const (
	ModePerformance   Mode = "vmnet-helper"
	ModeCompatibility Mode = "external-gvproxy"
)

// DefaultMode is what runtime.Engine uses when the persistence file
// is missing — vmnet-helper for speed, mirrors cyberstackd's default.
const DefaultMode = ModePerformance

// DefaultModeFile returns ~/.cyberstack/network-mode.
func DefaultModeFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cyberstack", "network-mode")
}

// ReadMode reads the mode from path. Returns DefaultMode if the file
// doesn't exist; surfaces other errors.
func ReadMode(path string) (Mode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultMode, nil
		}
		return "", fmt.Errorf("network: read mode %s: %w", path, err)
	}
	return ParseMode(strings.TrimSpace(string(data)))
}

// WriteMode persists the mode to path, creating the parent directory
// if needed. Atomic via tmp-file + rename.
func WriteMode(path string, m Mode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("network: mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(string(m)+"\n"), 0644); err != nil {
		return fmt.Errorf("network: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("network: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// ParseMode accepts the persistence-format strings (vmnet-helper /
// external-gvproxy) plus the user-friendly short forms.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "perf", "performance", "vmnet-helper":
		return ModePerformance, nil
	case "compat", "compatibility", "external-gvproxy":
		return ModeCompatibility, nil
	default:
		return "", fmt.Errorf("unknown network mode %q (want perf|compat)", s)
	}
}

// String returns the persistence-format value (passed straight to
// cyberstackd's -network-mode flag).
func (m Mode) String() string { return string(m) }

// Display returns the user-friendly label printed by `tainer network mode show`.
func (m Mode) Display() string {
	switch m {
	case ModePerformance:
		return "performance (vmnet-helper)"
	case ModeCompatibility:
		return "compatibility (external-gvproxy)"
	default:
		return string(m)
	}
}
