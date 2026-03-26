package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Logo renders the [=] bracket icon programmatically using half-block characters.
// Proportions match the SVG brand mark: two brackets with colored fills, gap between.
// Adapts to dark/light terminal background automatically.
func Logo() string {
	c := Colors()

	// Fill colors — composited from SVG rgba(78,158,244,0.14) and rgba(255,107,53,0.14)
	// against typical dark (#0B0F16) and light (#EEF1FD) terminal backgrounds.
	var leftFill, rightFill color.Color
	if IsDarkBackground() {
		leftFill = lipgloss.Color("#142335")
		rightFill = lipgloss.Color("#2D1C1A")
	} else {
		leftFill = lipgloss.Color("#D8E5FC")
		rightFill = lipgloss.Color("#F0DEE1")
	}

	const (
		B = 1 // blue (left bracket)
		T = 2 // teal (equals bars)
		O = 3 // orange (right bracket)
		L = 4 // left fill (dim blue)
		R = 5 // right fill (dim orange)
	)

	// 20 pixel rows x 29 columns — proportions from SVG brand mark
	// Left bracket [: vert cols 0-1 (2 wide), arm tip to col 6
	// Right bracket ]: arm tip from col 22, vert cols 27-28 (2 wide)
	// Left fill: cols 2-9 (3 cols extend past arm into gap)
	// Right fill: cols 19-26 (3 cols extend past arm into gap)
	// Gap between fills: cols 10-18 (9 cols transparent)
	// Teal bars: cols 9-19 (11 wide, bridges gap)
	grid := [20][29]byte{
		{B, B, B, B, B, B, B, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, O, O, O, O, O, O, O},
		{B, B, B, B, B, B, B, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, O, O, O, O, O, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, T, T, T, T, T, T, T, T, T, T, T, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, T, T, T, T, T, T, T, T, T, T, T, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, T, T, T, T, T, T, T, T, T, T, T, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, T, T, T, T, T, T, T, T, T, T, T, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, L, L, L, L, L, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, R, R, R, R, R, O, O},
		{B, B, B, B, B, B, B, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, O, O, O, O, O, O, O},
		{B, B, B, B, B, B, B, L, L, L, 0, 0, 0, 0, 0, 0, 0, 0, 0, R, R, R, O, O, O, O, O, O, O},
	}

	clrs := [6]color.Color{nil, c.Blue, c.Teal, c.Orange, leftFill, rightFill}

	var lines []string
	for y := 0; y < 20; y += 2 {
		var buf strings.Builder
		for x := 0; x < 29; x++ {
			top, bot := grid[y][x], grid[y+1][x]
			switch {
			case top == 0 && bot == 0:
				buf.WriteRune(' ')
			case top == bot:
				buf.WriteString(lipgloss.NewStyle().Foreground(clrs[top]).Render("█"))
			case top == 0:
				buf.WriteString(lipgloss.NewStyle().Foreground(clrs[bot]).Render("▄"))
			case bot == 0:
				buf.WriteString(lipgloss.NewStyle().Foreground(clrs[top]).Render("▀"))
			default:
				buf.WriteString(lipgloss.NewStyle().
					Foreground(clrs[top]).
					Background(clrs[bot]).
					Render("▀"))
			}
		}
		lines = append(lines, buf.String())
	}

	return strings.Join(lines, "\n")
}

// LogoFull returns the bracket icon with "tainer.dev/" centered below.
func LogoFull() string {
	icon := Logo()
	c := Colors()
	textStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)

	name := textStyle.Render("tainer") + tealStyle.Render(".dev/")
	return icon + "\n\n" + name
}
