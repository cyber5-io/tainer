package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// TermInfo holds pre-detected terminal dimensions.
type TermInfo struct {
	Width  int
	Height int
}

// GetTermInfo returns the current terminal dimensions, with sensible defaults.
func GetTermInfo() TermInfo {
	w, h := 80, 24
	if tw, th, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 && th > 0 {
		w, h = tw, th
	}
	return TermInfo{Width: w, Height: h}
}

// NewProgram creates a bubbletea program with tainer's standard options.
// In bubbletea v2, alt screen is controlled by the model's View() return
// (v.AltScreen = true), not by program options.
func NewProgram(model tea.Model, _ bool) *tea.Program {
	return tea.NewProgram(model)
}
