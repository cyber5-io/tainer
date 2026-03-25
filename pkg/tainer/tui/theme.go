package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette holds a resolved set of colors for dark or light terminals.
type Palette struct {
	Teal      lipgloss.Color
	TealDim   lipgloss.Color
	Orange    lipgloss.Color
	OrangeDim lipgloss.Color
	Blue      lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
}

// Colors match the SVG brand kit exactly.
var darkPalette = Palette{
	Teal:      lipgloss.Color("#00D4AA"),
	TealDim:   lipgloss.Color("#009E80"),
	Orange:    lipgloss.Color("#FF6B35"),
	OrangeDim: lipgloss.Color("#CC4F1F"),
	Blue:      lipgloss.Color("#4E9EF4"),
	Text:      lipgloss.Color("#DCE6F8"),
	Muted:     lipgloss.Color("#7A8AAA"),
	Border:    lipgloss.Color("#3A4A6E"),
}

var lightPalette = Palette{
	Teal:      lipgloss.Color("#009E80"),
	TealDim:   lipgloss.Color("#006B55"),
	Orange:    lipgloss.Color("#EA580C"),
	OrangeDim: lipgloss.Color("#A83D10"),
	Blue:      lipgloss.Color("#2563EB"),
	Text:      lipgloss.Color("#0C1018"),
	Muted:     lipgloss.Color("#5A6478"),
	Border:    lipgloss.Color("#A0AAB8"),
}

var (
	themeResolved bool
	isDark        bool
	colors        Palette
)

func resolveTheme() {
	if themeResolved {
		return
	}
	themeResolved = true
	isDark = lipgloss.HasDarkBackground()
	if isDark {
		colors = darkPalette
	} else {
		colors = lightPalette
	}
}

// Colors returns the active palette.
func Colors() Palette {
	resolveTheme()
	return colors
}

// IsDarkBackground returns whether the terminal has a dark background.
func IsDarkBackground() bool {
	resolveTheme()
	return isDark
}

// --- Style constructors (theme-aware) ---

func TitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(Colors().Teal)
}

func LabelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(Colors().Blue)
}

func SubtitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Muted)
}

func SuccessStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Teal)
}

func WarningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Orange)
}

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(Colors().Orange)
}

func SelectedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Orange)
}

func UnselectedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Muted)
}

func URLStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Teal).Underline(true)
}

func TextStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Colors().Text)
}

// Logo and LogoFull are defined in logo.go (embedded ANSI art from SVG).

// ProgressDots renders step progress as colored dots.
func ProgressDots(current, total int) string {
	c := Colors()
	done := lipgloss.NewStyle().Foreground(c.Teal)
	active := lipgloss.NewStyle().Foreground(c.Orange)
	upcoming := lipgloss.NewStyle().Foreground(c.Muted)

	var dots []string
	for i := 1; i <= total; i++ {
		if i < current {
			dots = append(dots, done.Render("●"))
		} else if i == current {
			dots = append(dots, active.Render("●"))
		} else {
			dots = append(dots, upcoming.Render("○"))
		}
	}
	return strings.Join(dots, " ")
}

// CenterText centers s horizontally within the given width.
func CenterText(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	pad := (width - w) / 2
	return strings.Repeat(" ", pad) + s
}

// Separator returns a horizontal rule in the border color.
func Separator(width int) string {
	return lipgloss.NewStyle().
		Foreground(Colors().Border).
		Render(strings.Repeat("─", width))
}

// FullScreen centers the frame in the terminal.
func FullScreen(frame string, termW, termH int) string {
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, frame)
}

// Banner renders the full logo lockup centered, with optional subtitle below.
func Banner(title, subtitle string, width int) string {
	logo := LogoFull()

	var lines []string
	lines = append(lines, "")
	for _, l := range strings.Split(logo, "\n") {
		lines = append(lines, CenterText(l, width))
	}
	lines = append(lines, "")
	lines = append(lines, "")
	if title != "" {
		lines = append(lines, CenterText(TitleStyle().Render(title), width))
	}
	if subtitle != "" {
		lines = append(lines, CenterText(SubtitleStyle().Render(subtitle), width))
	}
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}