package tui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// NewSpinner returns a spinner configured with the tainer teal color and dot style.
func NewSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Meter
	s.Style = lipgloss.NewStyle().Foreground(Colors().Teal)
	return s
}
