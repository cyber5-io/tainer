package tui

import (
	"fmt"
	"os"
	"strings"
)

// OutputFlags captures the per-command output overrides every tainer
// command supports. The defaults all auto-detect from the terminal; the
// flags exist to let users override on a per-invocation basis without
// touching global config.
//
//	--ui      force TUI even if auto-detection would skip it
//	--plain   force plain text (no color, no box drawing, no TUI)
//	--json    force JSON output (overrides everything)
//	--no-color drop colors but keep the structured text layout
//
// The four flags compose in priority order: --json wins absolutely
// (machines reading our output don't care about humans), then --plain,
// then --ui, then auto.
type OutputFlags struct {
	UI      bool // --ui
	Plain   bool // --plain
	JSON    bool // --json
	NoColor bool // --no-color
}

// Mode resolves the flag combination + terminal detection into a single
// concrete output mode. Call exactly once at the top of a command.
type OutputMode int

const (
	// ModeJSON emits machine-readable structured output. No marks, no
	// colors, no bookends — just the data.
	ModeJSON OutputMode = iota
	// ModePlain emits plain text suitable for piping. Marks degrade to
	// their ASCII form (no color) and box-drawing characters are stripped.
	ModePlain
	// ModeStyled emits styled text — colored marks, bookends, table
	// chrome. Stays on a single screen (no alt-screen, no key handling).
	ModeStyled
	// ModeTUI launches a full Bubble Tea program with arrow-key
	// navigation, alt-screen, etc. Requires an interactive terminal.
	ModeTUI
)

func (m OutputMode) String() string {
	switch m {
	case ModeJSON:
		return "json"
	case ModePlain:
		return "plain"
	case ModeStyled:
		return "styled"
	case ModeTUI:
		return "tui"
	}
	return "?"
}

// Resolve picks an OutputMode from the parsed flags + the current
// terminal context. `commandSupportsTUI` is set by the caller for
// commands that actually have a TUI implementation — commands without
// one (`tainer version`, `tainer stop`, etc.) pass false and ignore
// --ui.
//
// Default policy: bare invocations get the lightest readable surface
// (styled text in a TTY, plain text when piped). The heavy TUI is
// opt-in via --ui because it takes over the terminal — that's powerful
// for navigation but overkill when the user just wants to see what's
// there. This mirrors `git log` vs `git log -p`, `kubectl get` vs
// `k9s`, etc.: rich is always available, never imposed.
func (f OutputFlags) Resolve(commandSupportsTUI bool) OutputMode {
	switch {
	case f.JSON:
		return ModeJSON
	case f.Plain:
		return ModePlain
	case f.UI && commandSupportsTUI:
		// Explicit opt-in to the full TUI.
		return ModeTUI
	}
	// Auto-pick from the terminal — styled if we can color, plain if
	// piped/dumb/NO_COLOR set. Note: we deliberately do NOT auto-route
	// to ModeTUI even when commandSupportsTUI is true; users should
	// know when the screen is about to be taken over.
	if !WantsColor() {
		return ModePlain
	}
	return ModeStyled
}

// ParseOutputFlags extracts --ui/--plain/--json/--no-color from args.
// Returns the parsed flags and the remaining args (with the recognised
// flags stripped). Unknown flags pass through untouched — each command
// is responsible for its own command-specific flags.
//
// Bool-only flag parsing keeps things simple; no flag values to worry
// about. If we later need flags with values (--format=table, etc.) we
// can introduce a richer parser, but the four output flags don't need
// one.
func ParseOutputFlags(args []string) (OutputFlags, []string) {
	var f OutputFlags
	remaining := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--ui":
			f.UI = true
		case "--plain":
			f.Plain = true
		case "--json":
			f.JSON = true
		case "--no-color":
			f.NoColor = true
		default:
			remaining = append(remaining, a)
		}
	}
	// --no-color sets the environment so downstream lipgloss calls drop
	// color too. Setting it on the global env keeps the rest of the
	// process honest without having to thread the flag through every
	// rendering call.
	if f.NoColor {
		_ = os.Setenv("NO_COLOR", "1")
	}
	return f, remaining
}

// UsageOutputFlags returns a short one-line summary suitable for
// inclusion in per-command help text.
func UsageOutputFlags() string {
	return strings.Join([]string{
		"--ui", "--plain", "--json", "--no-color",
	}, " | ")
}

// errPrintf is a small helper for stderr writes from places that don't
// want to import fmt purely for the call. Exported for symmetry with
// the other helpers in this file.
func errPrintf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
