package tui

import (
	"fmt"
	"image/color"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// BrandSpinner is the tainer brand mark in motion: the equals motif
// builds and decays inside fixed blue/orange brackets, with the moving
// glyph in green (the "alive, doing work" color). On completion the
// caller stops the spinner and replaces the slot with MarkSuccess() or
// MarkError() — that snap-to-final is half of why the spinner reads as
// purposeful work rather than ambient busywork.
//
// Frames carry their colors baked in via raw ANSI escape sequences,
// so the returned spinner.Model has an empty Style (avoids
// double-coloring).
//
//	tui.BrandSpinner()  →  [-]  [=]  [≡]  [=]  [-]   (≈8 FPS)
//
// 8 FPS is deliberately slower than the bubbles defaults (10–15 FPS) —
// the brand mark is visually heavier than a single rune so it benefits
// from the slower cadence; it also reads as "deliberate" rather than
// "jittery".
//
// Why hand-rolled ANSI instead of lipgloss.NewStyle().Render():
// lipgloss emits a full SGR reset (`ESC[m`) after each styled segment,
// which also resets the *background* color. When the spinner ends up
// inside a row that has a Selected highlight (the list TUI's table
// uses one), the lipgloss resets blow away the row's background
// midway through the frame, producing a visible highlight cut. SGR 39
// resets the foreground only, preserving any background applied by the
// containing widget.

// rgb extracts the RGB components from a lipgloss/color value.
// Both light and dark palettes use lipgloss.Color (hex strings) so
// this conversion is always available.
func rgb(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// fgSegment wraps s in a truecolor foreground SGR + foreground-only
// reset (SGR 39). Use for any styled text that may live inside a cell
// whose background is set by a parent widget.
func fgSegment(c color.Color, s string) string {
	r, g, b := rgb(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", r, g, b, s)
}

// brandFrame renders one spinner frame: the triad-coloured brackets
// around a green center character. Uses fgSegment so the frame is safe
// inside a Selected-highlighted table row.
func brandFrame(center string) string {
	c := Colors()
	return fgSegment(c.Blue, "[") + fgSegment(c.Teal, center) + fgSegment(c.Orange, "]")
}

// BrandSpinner returns a bubbles spinner model preloaded with the
// tainer brand frames. Use in place of NewSpinner() for any new code
// that wants the on-brand look.
//
// Existing TUI screens still use NewSpinner() (Bubbles' built-in Meter
// spinner) — they'll migrate over time. Both live side by side until
// the migration is complete.
func BrandSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: brandFrameSequence(),
		FPS:    time.Second / 8,
	}
	// Style intentionally left empty: each frame is already colored.
	s.Style = lipgloss.NewStyle()
	return s
}

// brandFrameSequence is the canonical frame loop. Center character
// progresses `-` → `=` → `≡` → `=` → `-` so the equals motif fills
// then drains; the cycle reads as a heartbeat rather than as
// directional travel. No blank-center frame: the spinner sits in a
// single-character slot, and an empty middle frame reads as a glitch
// rather than as motion.
func brandFrameSequence() []string {
	return []string{
		brandFrame("-"),
		brandFrame("="),
		brandFrame("≡"),
		brandFrame("="),
		brandFrame("-"),
	}
}

// BrandFrames exposes the same frame strings BrandSpinner uses, for
// places (mostly tests and ad-hoc demos) that want to step through them
// manually rather than via the bubbletea event loop.
func BrandFrames() []string {
	return brandFrameSequence()
}
