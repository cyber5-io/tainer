package dns

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// PFAnchorPath is the on-disk file that pf.conf references via
// `load anchor`. Installer-managed; users shouldn't edit by hand.
const PFAnchorPath = "/etc/pf.anchors/tainer"

// PFConfPath is macOS's master pf config; we patch it idempotently.
const PFConfPath = "/etc/pf.conf"

// PFAnchorMarker uniquely identifies our two-line block in pf.conf so
// scans for "already installed" don't false-match.
const PFAnchorMarker = `anchor "tainer"`

// PFAnchorContent returns the static rule we drop at PFAnchorPath:
// redirect inbound TCP :22 → :2222 so users get `ssh @tainer.me`
// without specifying a port.
func PFAnchorContent() string {
	return "rdr pass on lo0 inet proto tcp from any to any port 22 -> 127.0.0.1 port 2222\n"
}

// PFConfPatched returns pf.conf with the tainer anchor + load lines
// added if they weren't already present. Idempotent.
func PFConfPatched(original string) string {
	if strings.Contains(original, PFAnchorMarker) {
		return original
	}
	if !strings.HasSuffix(original, "\n") {
		original += "\n"
	}
	return original + fmt.Sprintf(
		"%s\nload anchor \"tainer\" from \"%s\"\n",
		PFAnchorMarker, PFAnchorPath,
	)
}

// InstallPFAnchor writes PFAnchorPath, patches PFConfPath, and reloads
// pf via pfctl. Requires root — invoked from the .pkg postinstall
// script (or `sudo tainer post-install`).
func InstallPFAnchor() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if err := os.WriteFile(PFAnchorPath, []byte(PFAnchorContent()), 0644); err != nil {
		return fmt.Errorf("pf: write anchor %s: %w", PFAnchorPath, err)
	}
	conf, err := os.ReadFile(PFConfPath)
	if err != nil {
		return fmt.Errorf("pf: read %s: %w", PFConfPath, err)
	}
	patched := PFConfPatched(string(conf))
	if patched != string(conf) {
		if err := os.WriteFile(PFConfPath, []byte(patched), 0644); err != nil {
			return fmt.Errorf("pf: write %s: %w", PFConfPath, err)
		}
	}
	if out, err := exec.Command("pfctl", "-e", "-f", PFConfPath).CombinedOutput(); err != nil {
		// pfctl prints "pf already enabled" to stderr; ignore that case.
		if !strings.Contains(string(out), "already enabled") {
			return fmt.Errorf("pf: enable: %w (%s)", err, out)
		}
	}
	return nil
}

// RemovePFAnchor reverses InstallPFAnchor. Used by `tainer self-uninstall`.
func RemovePFAnchor() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	conf, err := os.ReadFile(PFConfPath)
	if err == nil {
		stripped := stripAnchor(string(conf))
		if stripped != string(conf) {
			_ = os.WriteFile(PFConfPath, []byte(stripped), 0644)
		}
	}
	_ = os.Remove(PFAnchorPath)
	_ = exec.Command("pfctl", "-f", PFConfPath).Run()
	return nil
}

func stripAnchor(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(l, PFAnchorMarker) || strings.Contains(l, `load anchor "tainer"`) {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
