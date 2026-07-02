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
	// ModeHybrid is the 0.9 default: gvproxy for outbound (pod →
	// internet, VPN-safe) + direct gRPC-tunneled forwarder for
	// inbound (host → pod, low latency). Gives perf-mode TTFB
	// without perf mode's susceptibility to macOS VPN/network
	// extensions. See project_perf_vs_compat_benchmarks memory and
	// internal/vm/hybridforwarder for the design.
	ModeHybrid Mode = "hybrid"
)

// DefaultMode is what runtime.Engine uses when the persistence file
// is missing. Hybrid is the safe-and-fast choice for the 99% case;
// users who want absolute perf and have no VPN can flip to perf.
const DefaultMode = ModeHybrid

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
	case "hybrid", "h":
		return ModeHybrid, nil
	default:
		return "", fmt.Errorf("unknown network mode %q (want perf|compat|hybrid)", s)
	}
}

// String returns the persistence-format value (passed straight to
// cyberstackd's -network-mode flag).
func (m Mode) String() string { return string(m) }

// Display returns the label printed by `tainer network mode show` —
// the one surface where the implementation detail belongs, since
// that's where users pick a mode and may need to know what each one
// actually does. Everywhere else (doctor, status) use Name().
func (m Mode) Display() string {
	switch m {
	case ModePerformance:
		return "performance (vmnet-helper)"
	case ModeCompatibility:
		return "compatibility (external-gvproxy)"
	case ModeHybrid:
		return "hybrid (gvproxy out, direct gRPC in)"
	default:
		return string(m)
	}
}

// Name returns the plain user-facing mode word with no implementation
// detail: "performance", "compatibility", "hybrid".
func (m Mode) Name() string {
	switch m {
	case ModePerformance:
		return "performance"
	case ModeCompatibility:
		return "compatibility"
	case ModeHybrid:
		return "hybrid"
	default:
		return string(m)
	}
}
