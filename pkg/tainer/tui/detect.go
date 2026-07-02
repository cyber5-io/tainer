package tui

import (
	"os"

	"golang.org/x/term"
)

// Detection helpers for output-mode auto-selection.
//
// The rule across the tainer surface: commands behave intelligently by
// default and let flags override per-invocation. There is no global
// "mode" setting — instead each command asks these helpers what shape
// of output to emit.
//
//   IsInteractive(): both stdin and stdout are TTYs (user can see and
//                    type — TUI fair game)
//   IsPiped():       stdout is not a TTY (piped, redirected, in a script
//                    — strip colors and box-drawing, emit plain text)
//   WantsColor():    color is appropriate (TTY + NO_COLOR not set)
//   WantsTUI():      interactive AND user didn't pass --plain/--json
//
// Each helper consults environment variables that are part of the
// terminal contract (`NO_COLOR`, `CI`) so tainer respects what the
// surrounding environment is already telling other CLIs.

// IsTTY reports whether the given file descriptor is a terminal.
// Used internally; prefer the more specific helpers below at call sites.
func IsTTY(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// IsInteractive reports whether both stdin and stdout are TTYs.
// A TUI shouldn't open unless this is true — otherwise the user has no
// way to type into it.
func IsInteractive() bool {
	return IsTTY(os.Stdin.Fd()) && IsTTY(os.Stdout.Fd())
}

// IsPiped reports whether stdout is being redirected (pipe, file, or
// other non-TTY destination). When true, output should be plain text
// with no color codes and no box-drawing characters — the recipient is
// likely a script or another command in a pipeline.
func IsPiped() bool {
	return !IsTTY(os.Stdout.Fd())
}

// WantsColor reports whether color output is appropriate. False when
// stdout is piped, when NO_COLOR is set (de-facto cross-tool standard),
// or when TERM=dumb (legacy convention for color-incapable terminals).
func WantsColor() bool {
	if IsPiped() {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

// WantsTUI reports whether an interactive TUI surface is appropriate
// for output. Returns true only when the terminal can actually host one
// (both stdin and stdout are TTYs) and color is on. Per-command
// `--plain` / `--no-ui` flags should bypass this and force the plain
// path regardless.
func WantsTUI() bool {
	return IsInteractive() && WantsColor()
}

// IsCI reports whether we're running in a CI environment. Most CI
// runners set CI=true; some also set specific vars (GITHUB_ACTIONS,
// CIRCLECI, etc.) but CI alone is enough for our gating purposes. CI
// runs typically pipe stdout to logs, but checking IsCI explicitly
// lets commands print extra diagnostic context when they detect they
// won't be read by a human in the terminal.
func IsCI() bool {
	return os.Getenv("CI") != ""
}
