package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/release"
)

const loginItemCheck = "login item"

// minMenuForLoginItem is the first menu app that registers its own login item
// and answers --login-item-status. outdatedPrefix marks an older one in the
// probe's status ("outdated 1.5.6").
const (
	minMenuForLoginItem = "1.6.0"
	outdatedPrefix      = "outdated "
)

// menuReportsLoginItem reports whether a menu app of this version can be
// asked for its login item. Dev builds can.
func menuReportsLoginItem(version string) bool {
	return !release.Newer(minMenuForLoginItem, version)
}

// loginItemProbe reports the menu app's login item status ("enabled",
// "requiresApproval", "notRegistered", "notFound"), whether the app is
// installed at all, and any error reading it. Set per platform; a seam
// for tests.
var loginItemProbe = func(context.Context) (status string, installed bool, err error) {
	return "", false, nil
}

// checkLoginItem reports whether Tainer Menu opens at login.
func (d *run) checkLoginItem(ctx context.Context) {
	if !loginItemApplies {
		return
	}
	d.start(loginItemCheck)
	status, installed, err := loginItemProbe(ctx)
	d.add(loginItemResult(status, installed, err, macOSMajor()))
}

// loginItemsSettings is the System Settings path to the Open at Login list,
// which macOS 15 renamed.
func loginItemsSettings(macOS int) string {
	if macOS >= 15 {
		return "System Settings → General → Login Items & Extensions → Open at Login"
	}
	return "System Settings → General → Login Items → Open at Login"
}

func loginItemResult(status string, installed bool, err error, macOS int) Result {
	r := Result{Name: loginItemCheck}
	switch {
	case !installed:
		r.Status, r.Detail = StatusOK, "Tainer Menu isn't installed"
	case err != nil:
		r.Status, r.Detail = StatusWarn, "couldn't read Tainer Menu's login item: "+err.Error()
	case status == "enabled":
		r.Status, r.Detail = StatusOK, "Tainer Menu opens at login"
	case status == "requiresApproval":
		r.Status, r.Detail = StatusWarn, "Tainer Menu is waiting for your approval to open at login"
		r.Hint = "turn it on in " + loginItemsSettings(macOS)
	case status == "notRegistered":
		r.Status, r.Detail = StatusWarn, "Tainer Menu doesn't open at login"
		// Removed after it was added: opening the app respects that, so only
		// System Settings brings it back.
		r.Hint = "add it back in " + loginItemsSettings(macOS)
	case strings.HasPrefix(status, outdatedPrefix):
		r.Status = StatusWarn
		r.Detail = "Tainer Menu " + strings.TrimPrefix(status, outdatedPrefix) + " can't open at login on its own yet"
		r.Hint = "update it with: tainer update menu"
	case status == "notFound":
		r.Status, r.Detail = StatusWarn, "macOS can't find Tainer Menu's login item"
		// Never registered, or removed: opening the app registers only the
		// former, so the hint covers both.
		r.Hint = "open Tainer Menu from /Applications once, or add it in " + loginItemsSettings(macOS)
	default:
		r.Status, r.Detail = StatusWarn, fmt.Sprintf("Tainer Menu's login item is %q", status)
	}
	return r
}
