package tui

import "charm.land/lipgloss/v2"

// Status and brand marks. Five symbols carry tainer's visual identity at
// every operational line; together with the bookends in bookend.go they
// account for ~all of the per-line brand surface.
//
// Convention:
//
//   [=]  brand identity     — full triad coloring (logo in miniature)
//   [✓]  success            — single semantic color (green/teal)
//   [i]  info               — single semantic color (blue)
//   [!]  warning            — single semantic color (orange)
//   [✗]  failure            — single semantic color (red)
//
// The brand mark is the *only* one that gets the triad treatment: the
// `[` blue, the `=` green, the `]` orange. Status marks stay monochrome
// because they're signaling a single thing — readability beats decoration.
//
// All marks include the surrounding brackets and one trailing space, so
// the caller writes `tui.MarkSuccess() + "cyberstackd alive"`. Spacing is
// part of the contract — callers shouldn't have to think about it.

// MarkBrand returns the brand identity mark `[=]` with the triad
// coloring (blue / green / orange). Used by bookends, dividers,
// interactive prompts, and command bullets — anywhere the line is
// structurally tainer-the-tool rather than a status update.
func MarkBrand() string {
	c := Colors()
	return lipgloss.NewStyle().Foreground(c.Blue).Render("[") +
		lipgloss.NewStyle().Foreground(c.Teal).Render("=") +
		lipgloss.NewStyle().Foreground(c.Orange).Render("]") + " "
}

// MarkSuccess returns the success mark `[✓]` in tainer green.
func MarkSuccess() string {
	return lipgloss.NewStyle().Foreground(Colors().Teal).Render("[✓]") + " "
}

// MarkInfo returns the info mark `[i]` in tainer blue. Use for neutral
// status that isn't success or failure (e.g. "reconciling 5 containers").
func MarkInfo() string {
	return lipgloss.NewStyle().Foreground(Colors().Blue).Render("[i]") + " "
}

// MarkWarn returns the warning mark `[!]` in tainer orange. Use for
// attention-required states that aren't outright failures (e.g.
// "running in compat mode — slower per-request latency").
func MarkWarn() string {
	return lipgloss.NewStyle().Foreground(Colors().Orange).Render("[!]") + " "
}

// MarkError returns the failure mark `[✗]` in red. Red is intentionally
// outside the brand triad — failure shouldn't carry brand emotion.
func MarkError() string {
	return lipgloss.NewStyle().Foreground(Colors().Red).Render("[✗]") + " "
}

// PromptMark returns the brand mark sized for use as the interactive
// prompt character (replacing `>` or `?`). Currently identical to
// MarkBrand; exposed as a separate helper so the prompt visual can
// evolve independently if needed.
func PromptMark() string { return MarkBrand() }
