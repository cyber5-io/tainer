package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// tain-131/132: doctor reports whether Tainer Menu opens at login.

func TestLoginItemResultForEachStatus(t *testing.T) {
	cases := []struct {
		status       string
		installed    bool
		err          error
		macOS        int
		want         Status
		detail, hint string
	}{
		{"enabled", true, nil, 15, StatusOK, "Tainer Menu opens at login", ""},
		{"requiresApproval", true, nil, 15, StatusWarn, "Tainer Menu is waiting for your approval to open at login",
			"turn it on in System Settings → General → Login Items & Extensions → Open at Login"},
		{"requiresApproval", true, nil, 14, StatusWarn, "Tainer Menu is waiting for your approval to open at login",
			"turn it on in System Settings → General → Login Items → Open at Login"},
		{"notRegistered", true, nil, 15, StatusWarn, "Tainer Menu doesn't open at login",
			"add it back in System Settings → General → Login Items & Extensions → Open at Login"},
		{"notFound", true, nil, 14, StatusWarn, "macOS can't find Tainer Menu's login item",
			"open Tainer Menu from /Applications once, or add it in System Settings → General → Login Items → Open at Login"},
		{"", false, nil, 15, StatusOK, "Tainer Menu isn't installed", ""},
		{"", true, errors.New("timed out"), 15, StatusWarn, "couldn't read Tainer Menu's login item: timed out", ""},
		{"somethingNew", true, nil, 15, StatusWarn, `Tainer Menu's login item is "somethingNew"`, ""},
		{"outdated 1.5.6", true, nil, 15, StatusWarn, "Tainer Menu 1.5.6 can't open at login on its own yet", "update it with: tainer update menu"},
	}
	for _, c := range cases {
		r := loginItemResult(c.status, c.installed, c.err, c.macOS)
		if r.Name != "login item" || r.Status != c.want || r.Detail != c.detail || r.Hint != c.hint {
			t.Errorf("%s (installed=%v, macOS %d):\n got %+v\nwant %s %q %q", c.status, c.installed, c.macOS, r, c.want, c.detail, c.hint)
		}
	}
}

func TestLoginItemCopyNeverNamesInternals(t *testing.T) {
	for _, s := range []string{"enabled", "requiresApproval", "notRegistered", "notFound"} {
		r := loginItemResult(s, true, nil, 15)
		text := strings.ToLower(r.Detail + " " + r.Hint)
		for _, word := range []string{"podman", "docker", "engine", " vm", "container", "smappservice"} {
			if strings.Contains(text, word) {
				t.Errorf("%s mentions %q: %q", s, word, text)
			}
		}
	}
}

func TestLoginItemCheckUsesTheProbe(t *testing.T) {
	orig := loginItemProbe
	t.Cleanup(func() { loginItemProbe = orig })
	loginItemProbe = func(context.Context) (string, bool, error) { return "requiresApproval", true, nil }
	d := &run{}
	d.checkLoginItem(context.Background())
	if len(d.report.Results) != 1 || d.report.Results[0].Status != StatusWarn || !strings.Contains(d.report.Results[0].Hint, "System Settings → General → Login Items") {
		t.Errorf("%+v", d.report.Results)
	}
}

// Only a menu that knows --login-item-status is ever run: an older one would
// ignore the flag and start a second menu.
func TestOnlyAMenuThatKnowsTheFlagIsAsked(t *testing.T) {
	for v, want := range map[string]bool{"1.5.6": false, "1.5.10": false, "1.6.0": true, "1.6.1": true, "2.0.0": true, "dev": true} {
		if got := menuReportsLoginItem(v); got != want {
			t.Errorf("%s: %v, want %v", v, got, want)
		}
	}
}
