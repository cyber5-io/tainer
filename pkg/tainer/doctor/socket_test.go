package doctor

import (
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestClassifySocketErr(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantHint string
	}{
		{"missing", &os.PathError{Op: "stat", Err: fs.ErrNotExist}, "sudo apt install ./tainer.deb"},
		{"missing-unit", fs.ErrNotExist, "systemctl enable --now cyberstack.socket"},
		{"refused", syscall.ECONNREFUSED, "journalctl -u cyberstack"},
		{"denied", syscall.EACCES, "groups | grep cyberstack"},
		{"denied-fs", fs.ErrPermission, "groups | grep cyberstack"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			detail, hint := classifySocketErr("/run/cyberstack/cyberstack.sock", c.err)
			if detail == "" || !strings.Contains(hint, c.wantHint) {
				t.Errorf("detail=%q hint=%q, want hint containing %q", detail, hint, c.wantHint)
			}
		})
	}
}

func TestParseSSListener(t *testing.T) {
	out := `State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0      511          0.0.0.0:80        0.0.0.0:*     users:(("nginx",pid=812,fd=6),("nginx",pid=813,fd=6))
LISTEN 0      4096         0.0.0.0:443       0.0.0.0:*
LISTEN 0      128             [::]:8080         [::]:*     users:(("node",pid=9,fd=3))
`
	if l, name, pid := parseSSListener(out, "80"); !l || name != "nginx" || pid != 812 {
		t.Errorf("80: got %v %q %d", l, name, pid)
	}
	// Listening but unattributable (root-owned): must still be detected.
	if l, name, _ := parseSSListener(out, "443"); !l || name != "" {
		t.Errorf("443: listening=%v name=%q, want listening with empty owner", l, name)
	}
	if l, name, _ := parseSSListener(out, "8080"); !l || name != "node" {
		t.Errorf("8080: got %v %q", l, name)
	}
	if l, _, _ := parseSSListener(out, "8"); l {
		t.Error("port 8 must not match :80 or :8080")
	}
	if l, _, _ := parseSSListener(out, "9999"); l {
		t.Error("unlisted port must not be listening")
	}
}

func TestPortConflictUnknownOwnerIsFlagged(t *testing.T) {
	r := portConflictResult("443", "", 0, false)
	if r.Status != StatusFail || !strings.Contains(r.Detail, "owner not visible") || !strings.Contains(r.Hint, "no fallback") {
		t.Errorf("engine down + unknown owner must fail with guidance: %+v", r)
	}
	if r := portConflictResult("443", "", 0, true); r.Status != StatusWarn {
		t.Errorf("engine up + unknown owner should warn, got %+v", r)
	}
}

func TestPortConflictResult(t *testing.T) {
	r := portConflictResult("80", "nginx", 812, false)
	if r.Status != StatusFail || !strings.Contains(r.Detail, "nginx") || !strings.Contains(r.Detail, "812") || !strings.Contains(r.Hint, "80") {
		t.Errorf("unexpected result %+v", r)
	}
	if portConflictResult("443", "cyberstackd", 5, true).Status != StatusOK {
		t.Error("our own forwarder must not count as a conflict")
	}
}
