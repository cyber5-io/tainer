package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette holds a resolved set of colors for dark or light terminals.
type Palette struct {
	Teal      color.Color
	TealDim   color.Color
	Orange    color.Color
	OrangeDim color.Color
	Blue      color.Color
	Text      color.Color
	Muted     color.Color
	Border    color.Color
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
	isDark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
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

// RenderURL renders a domain or URL as a styled clickable hyperlink (OSC 8).
func RenderURL(domain string) string {
	url := domain
	if !strings.HasPrefix(domain, "http") {
		url = "https://" + domain
	}
	return URLStyle().Hyperlink(url).Render(domain)
}

// Linkify wraps text in an OSC 8 hyperlink without changing its style.
// Use this when another component (e.g. table) controls the visual styling.
func Linkify(text, url string) string {
	return "\033]8;;" + url + "\033\\" + text + "\033]8;;\033\\"
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

// PrintWithLogo prints content on the left with the small logo on the right,
// vertically centered. The logo is pushed to the right edge (~80 cols).
func PrintWithLogo(content string) {
	logo := LogoSmallFull()
	logoW := lipgloss.Width(logo)

	totalW := 78 // usable width (80 - 2 indent)
	leftW := totalW - logoW - 4

	left := lipgloss.NewStyle().Width(leftW).Render(content)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)

	row := lipgloss.JoinHorizontal(lipgloss.Center, left, "    ", right)
	indented := lipgloss.NewStyle().PaddingLeft(2).Render(row)

	fmt.Println()
	fmt.Println(indented)
	fmt.Println()
}

// FullScreen centers the frame in the terminal.
func FullScreen(frame string, termW, termH int) string {
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, frame)
}

// shimmerText renders text with a bright highlight sweeping across it.
func shimmerText(runes []rune, pos int, baseStyles []lipgloss.Style, highlight color.Color) string {
	var buf strings.Builder
	for i, r := range runes {
		dist := pos - i
		if dist == 0 {
			buf.WriteString(lipgloss.NewStyle().Bold(true).Foreground(highlight).Render(string(r)))
		} else if dist == 1 || dist == -1 {
			buf.WriteString(lipgloss.NewStyle().Foreground(highlight).Render(string(r)))
		} else {
			buf.WriteString(baseStyles[i].Render(string(r)))
		}
	}
	return buf.String()
}

// Banner renders the full logo lockup centered, with optional subtitle below.
// animTick drives the shimmer animation (negative = fully revealed, no animation).
func Banner(title, subtitle string, width, animTick int) string {
	logo := Logo()
	c := Colors()

	var lines []string
	lines = append(lines, "")
	for _, l := range strings.Split(logo, "\n") {
		lines = append(lines, CenterText(l, width))
	}
	lines = append(lines, "")
	lines = append(lines, "")

	nameText := "tainer"
	nameSuffix := ".dev/"
	fullName := nameText + nameSuffix
	nameRunes := []rune(fullName)
	tainerLen := len([]rune(nameText))

	nameStyles := make([]lipgloss.Style, len(nameRunes))
	for i := range nameRunes {
		if i < tainerLen {
			nameStyles[i] = lipgloss.NewStyle().Bold(true).Foreground(c.Text)
		} else {
			nameStyles[i] = lipgloss.NewStyle().Foreground(c.Teal)
		}
	}

	const nameDelay = 8
	const shimmerGap = 6
	const repeatInterval = 900

	highlight := lipgloss.Color("#FFFFFF")

	renderNameStatic := func() string {
		return lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(nameText) +
			lipgloss.NewStyle().Foreground(c.Teal).Render(nameSuffix)
	}

	if animTick < 0 {
		lines = append(lines, CenterText(renderNameStatic(), width))
		if subtitle != "" {
			lines = append(lines, "")
			lines = append(lines, CenterText(SubtitleStyle().Render(subtitle), width))
		}
	} else {
		subtitleStart := nameDelay + len(nameRunes) + 2
		subtitleShimmerStart := subtitleStart + shimmerGap
		introEnd := subtitleShimmerStart + len([]rune(subtitle)) + 2

		var nameShimmerPos int
		if animTick < introEnd {
			nameShimmerPos = animTick - nameDelay
		} else {
			elapsed := animTick - introEnd
			cyclePos := elapsed % repeatInterval
			nameShimmerPos = cyclePos - (repeatInterval - len(nameRunes) - 4)
		}

		if nameShimmerPos >= -1 && nameShimmerPos <= len(nameRunes)+1 {
			lines = append(lines, CenterText(shimmerText(nameRunes, nameShimmerPos, nameStyles, highlight), width))
		} else {
			lines = append(lines, CenterText(renderNameStatic(), width))
		}

		if subtitle != "" {
			if animTick >= subtitleStart {
				lines = append(lines, "")
				subtitleShimmerPos := animTick - subtitleShimmerStart
				subtitleRunes := []rune(subtitle)
				if subtitleShimmerPos >= -1 && subtitleShimmerPos <= len(subtitleRunes)+1 {
					subtitleStyles := make([]lipgloss.Style, len(subtitleRunes))
					for i := range subtitleRunes {
						subtitleStyles[i] = SubtitleStyle()
					}
					lines = append(lines, CenterText(shimmerText(subtitleRunes, subtitleShimmerPos, subtitleStyles, highlight), width))
				} else {
					lines = append(lines, CenterText(SubtitleStyle().Render(subtitle), width))
				}
			} else {
				lines = append(lines, "")
				lines = append(lines, "")
			}
		}
	}

	lines = append(lines, "")

	return strings.Join(lines, "\n")
}
