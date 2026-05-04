package dns

import (
	"strings"
	"testing"
)

func TestPFAnchorContent(t *testing.T) {
	got := PFAnchorContent()
	if !strings.Contains(got, "rdr pass on lo0 inet proto tcp") {
		t.Errorf("missing rdr rule: %q", got)
	}
	if !strings.Contains(got, "port 22 -> 127.0.0.1 port 2222") {
		t.Errorf("missing redirect: %q", got)
	}
}

func TestPFConfPatch(t *testing.T) {
	original := "# pf.conf\nrdr-anchor \"*\"\n"
	patched := PFConfPatched(original)
	if !strings.Contains(patched, `anchor "tainer"`) {
		t.Errorf("missing anchor line: %q", patched)
	}
	if !strings.Contains(patched, `load anchor "tainer" from "/etc/pf.anchors/tainer"`) {
		t.Errorf("missing load line: %q", patched)
	}
	// Idempotent: applying again doesn't double-add.
	twice := PFConfPatched(patched)
	if twice != patched {
		t.Errorf("idempotency broken: second application differs from first")
	}
}
