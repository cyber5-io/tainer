package runtime

import (
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/network"
)

func TestDaemonArgsIncludesDNSPortAndMode(t *testing.T) {
	args := DaemonArgs("/tmp/cyberstack.sock", network.ModeCompatibility, 7753)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-socket /tmp/cyberstack.sock") {
		t.Errorf("missing socket flag: %s", joined)
	}
	if !strings.Contains(joined, "-dns-port 7753") {
		t.Errorf("missing dns-port flag: %s", joined)
	}
	if !strings.Contains(joined, "-network-mode external-gvproxy") {
		t.Errorf("missing network-mode flag: %s", joined)
	}
}

func TestDaemonArgsDefaultMode(t *testing.T) {
	args := DaemonArgs("/tmp/x.sock", network.ModePerformance, 7753)
	if !strings.Contains(strings.Join(args, " "), "vmnet-helper") {
		t.Errorf("performance mode should pass vmnet-helper: %v", args)
	}
}
