package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Bookends frame styled-text output. Every multi-line command block in
// tainer's non-TUI surface opens with one and (if the command does work
// worth summarising) closes with another. The mark in each bookend is
// the brand `[=]` triad — see marks.go.
//
// Shape:
//
//	[=] tainer 0.9.0 · opp
//	  [✓] cyberstackd alive
//	  [✓] vfkit booted in 1.2s
//	  [i] reconciling 5 containers
//	  ...
//	[=] Ready · https://opp.tainer.me · 4.6s
//
// The opening bookend ALWAYS prefixes "tainer <version>" so users know
// what they're looking at without a logo splash on every command.
// Subsequent fields are caller-supplied and separated by " · " — usually
// (project, action) or just (action) for project-less commands.

// versionString is set by callers (typically cmd/tainer/main.go) to the
// build version. Defaults to "dev" if never set, so bookends still work
// in tests and ad-hoc invocations.
var versionString = "dev"

// SetVersion configures the version string used in opening bookends.
// Call once at process startup.
func SetVersion(v string) { versionString = v }

// sepStyle returns the muted dot-separator style. Used between bookend
// parts so the bullet is clearly subordinate to the content.
func sepStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Muted)
}

func textStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Text)
}

// joinParts renders parts as `a · b · c` with the brand mark prefixed.
// Empty parts are skipped. The separator is mutedso content reads first.
func joinParts(parts ...string) string {
	rendered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		rendered = append(rendered, textStyle().Render(p))
	}
	return MarkBrand() + strings.Join(rendered, sepStyle().Render(" · "))
}

// Bookend prints the opening bookend with parts. The first part is
// implicitly "tainer <version>" — callers supply only the
// per-invocation context (typically the project name or the action).
//
//	tui.Bookend("opp")                 -> [=] tainer 0.9.0 · opp
//	tui.Bookend("pod", "start")        -> [=] tainer 0.9.0 · pod · start
//	tui.Bookend()                      -> [=] tainer 0.9.0
func Bookend(parts ...string) {
	all := append([]string{fmt.Sprintf("tainer %s", versionString)}, parts...)
	fmt.Println(joinParts(all...))
	fmt.Println()
}

// BookendClose prints the closing bookend with parts plus the elapsed
// duration if non-zero. Pass zero duration to suppress the timing.
//
//	tui.BookendClose(time.Since(start), "Ready", "https://opp.tainer.me")
//	-> [=] Ready · https://opp.tainer.me · 4.6s
//
// Use BookendCloseError instead when the operation failed — the visual
// is the same but red coloring on the closing word communicates the
// outcome at a glance.
func BookendClose(elapsed time.Duration, parts ...string) {
	fmt.Println()
	fmt.Println(joinParts(append(parts, fmtElapsed(elapsed))...))
}

// BookendCloseError is the failure variant of BookendClose. The first
// part is rendered in red (typically a single word like "Aborted" or
// "Failed"); subsequent parts and the elapsed duration follow the
// normal styling.
func BookendCloseError(elapsed time.Duration, headline string, parts ...string) {
	fmt.Println()
	red := lipgloss.NewStyle().Foreground(Colors().Red).Bold(true).Render(headline)
	tail := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		if p != "" {
			tail = append(tail, textStyle().Render(p))
		}
	}
	if e := fmtElapsed(elapsed); e != "" {
		tail = append(tail, textStyle().Render(e))
	}
	out := MarkBrand() + red
	if len(tail) > 0 {
		out += sepStyle().Render(" · ") + strings.Join(tail, sepStyle().Render(" · "))
	}
	fmt.Println(out)
}

// fmtElapsed formats a duration for bookend display. Returns "" for
// durations under 100ms (too fast to be interesting) or zero.
func fmtElapsed(d time.Duration) string {
	if d == 0 || d < 100*time.Millisecond {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
