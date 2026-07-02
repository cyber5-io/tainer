package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// The welcome surface: a one-screen orientation shown on the first
// bare `tainer` invocation and on demand via `tainer welcome`. It
// replaces the usage dump exactly once — a new user's first contact
// with the brand should be a hand extended, not a wall of flags.
//
// Deliberately NOT injected before other commands: someone whose
// first-ever invocation is `tainer init wp mysite` came knowing what
// they want, and scripts must never find a banner in their pipes.

// welcomeMarker is the file whose existence means "this user has seen
// the welcome". Lives in the config dir so it survives upgrades but
// resets with a clean install.
func welcomeMarker() string {
	return filepath.Join(config.BaseDir(), "welcomed")
}

// firstRun reports whether the welcome has never been shown. Errors
// read as "already welcomed" — a permissions hiccup must never trap a
// user in a welcome loop.
func firstRun() bool {
	_, err := os.Stat(welcomeMarker())
	return os.IsNotExist(err)
}

// markWelcomed records that the banner was shown. Best-effort.
func markWelcomed() {
	if err := os.MkdirAll(config.BaseDir(), 0755); err != nil {
		return
	}
	_ = os.WriteFile(welcomeMarker(), []byte("shown\n"), 0644)
}

// cmdWelcome renders the welcome screen and remembers it was seen.
func cmdWelcome() {
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)

	tui.Bookend("welcome to tainer")
	fmt.Println("  " + muted.Render("Instant local dev sites — real HTTPS domains, zero config."))
	fmt.Println()

	fmt.Println("  " + muted.Render("Quickstart"))
	fmt.Println("    " + tui.MarkInfo() + "tainer init wp mysite      " + muted.Render("scaffold a WordPress project"))
	fmt.Println("    " + tui.MarkInfo() + "tainer start               " + muted.Render("serve it at https://mysite.tainer.me"))
	fmt.Println("    " + tui.MarkInfo() + "tainer wp plugin list      " + muted.Render("run wp-cli inside the pod"))
	fmt.Println("    " + tui.MarkInfo() + "tainer list                " + muted.Render("see all your projects"))
	fmt.Println("    " + tui.MarkInfo() + "tainer doctor              " + muted.Render("check every layer when something's off"))
	fmt.Println()

	names := make([]string, 0, 8)
	for _, s := range manifest.AllSpecs() {
		names = append(names, string(s.Type))
	}
	fmt.Println("  " + muted.Render("Project types"))
	fmt.Println("    " + tui.MarkInfo() + strings.Join(names, " · "))
	fmt.Println()
	fmt.Println("  " + muted.Render("Every command: `tainer` · docs: https://tainer.dev"))

	tui.BookendClose(0, "Ready when you are")
	markWelcomed()
}
