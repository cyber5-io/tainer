package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tainer brand palette.
var (
	ColorTeal      = lipgloss.Color("#00D4AA")
	ColorTealDim   = lipgloss.Color("#009E80")
	ColorOrange    = lipgloss.Color("#FF6B35")
	ColorOrangeDim = lipgloss.Color("#CC4F1F")
	ColorBlue      = lipgloss.Color("#4E9EF4")
	ColorSurface   = lipgloss.Color("#111622")
	ColorBorder    = lipgloss.Color("#1E2C42")
	ColorText      = lipgloss.Color("#DCE6F8")
	ColorMuted     = lipgloss.Color("#5A6A8E")
)

// Shared Lip Gloss styles.
var (
	TitleStyle      = lipgloss.NewStyle().Bold(true).Foreground(ColorTeal)
	SubtitleStyle   = lipgloss.NewStyle().Foreground(ColorMuted)
	SuccessStyle    = lipgloss.NewStyle().Foreground(ColorTeal)
	WarningStyle    = lipgloss.NewStyle().Foreground(ColorOrange)
	ErrorStyle      = lipgloss.NewStyle().Bold(true).Foreground(ColorOrange)
	SelectedStyle   = lipgloss.NewStyle().Foreground(ColorTeal)
	UnselectedStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	PanelStyle      = lipgloss.NewStyle().
			Background(ColorSurface).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)
	BannerStyle = lipgloss.NewStyle().
			Background(ColorSurface).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorTeal).
			Padding(1, 3)
	URLStyle = lipgloss.NewStyle().Foreground(ColorTeal).Underline(true)
)

// Logo returns the tainer logo mark as a styled string.
// Left brackets in blue, equals in teal, right brackets in orange.
func Logo() string {
	blue := lipgloss.NewStyle().Foreground(ColorBlue)
	teal := lipgloss.NewStyle().Foreground(ColorTeal)
	orange := lipgloss.NewStyle().Foreground(ColorOrange)

	line := blue.Render(" \u2503") + "  " + teal.Render("\u2501\u2501") + "  " + orange.Render("\u2503")
	return line + "\n" + line
}

// Banner renders a styled panel with the logo, title, and subtitle.
func Banner(title, subtitle string) string {
	logo := Logo()
	t := TitleStyle.Render(title)
	s := SubtitleStyle.Render(subtitle)

	var parts []string
	parts = append(parts, logo)
	parts = append(parts, "")
	parts = append(parts, t)
	if subtitle != "" {
		parts = append(parts, s)
	}

	content := strings.Join(parts, "\n")
	return BannerStyle.Render(content)
}
