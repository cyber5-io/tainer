package status

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// ProjectInfo holds the data to display.
type ProjectInfo struct {
	Name   string
	Type   string
	Domain string
	Path   string
	Status string // "Running" or "stopped"
}

// ContainerInfo holds per-container status.
type ContainerInfo struct {
	Name   string
	Status string
	Ports  string
}

type keyMap struct {
	Scroll key.Binding
	Quit   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Scroll: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "scroll"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc", "ctrl+c"),
			key.WithHelp("any other key", "exit"),
		),
	}
}

func (km keyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Scroll, km.Quit}
}

func (km keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{km.ShortHelp()}
}

type model struct {
	project    ProjectInfo
	containers []ContainerInfo
	keys       keyMap
	helpM      help.Model
	viewport   viewport.Model
	width      int
	height     int
	quitting   bool
}

// Run starts the status TUI.
func Run(project ProjectInfo, containers []ContainerInfo) error {
	m := initialModel(project, containers)
	p := tui.NewProgram(m, true) // full screen
	_, err := p.Run()
	return err
}

func initialModel(project ProjectInfo, containers []ContainerInfo) model {
	c := tui.Colors()

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}
	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(c.Muted)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(c.Border)

	vp := viewport.New(
		viewport.WithWidth(80),
		viewport.WithHeight(10),
	)
	vp.KeyMap = viewport.KeyMap{}
	vp.MouseWheelEnabled = false

	return model{
		project:    project,
		containers: containers,
		keys:       newKeyMap(),
		helpM:      h,
		viewport:   vp,
		width:      80,
		height:     24,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		tui.SetDarkMode(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcViewport()
		return m, nil

	case tea.KeyPressMsg:
		k := msg.String()
		switch k {
		case "up", "k":
			yOff := m.viewport.YOffset()
			if yOff > 0 {
				m.viewport.SetYOffset(yOff - 1)
			}
			return m, nil
		case "down", "j":
			yOff := m.viewport.YOffset()
			m.viewport.SetYOffset(yOff + 1)
			return m, nil
		default:
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *model) recalcViewport() {
	innerW := m.width - 12
	if innerW < 40 {
		innerW = 40
	}
	m.helpM.SetWidth(innerW)
	m.viewport.SetWidth(innerW)

	// header(7: 5 logo + blank + wordmark) + border(2) + separator(1) + help(1) = 11
	chrome := 11
	vpH := m.height - chrome
	if vpH < 4 {
		vpH = 4
	}
	m.viewport.SetHeight(vpH)
	m.refreshViewportContent()
}

func (m *model) refreshViewportContent() {
	content := m.renderContent()
	yOff := m.viewport.YOffset()
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(yOff)
}

func (m model) renderHeader(innerW int) string {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Blue)
	valueStyle := lipgloss.NewStyle().Foreground(c.Text)

	// Project info (left side) — 6 lines to match small logo height
	statusDot := mutedStyle.Render("○")
	statusStr := mutedStyle.Render("stopped")
	if m.project.Status == "Running" {
		statusDot = tealStyle.Render("●")
		statusStr = tealStyle.Render("running")
	}

	var infoLines []string
	infoLines = append(infoLines, fmt.Sprintf("%s  %s  %s", statusDot, tui.TitleStyle().Render(m.project.Name), statusStr))
	infoLines = append(infoLines, "")
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Type    "), valueStyle.Render(m.project.Type)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Domain  "), tui.RenderURL(m.project.Domain)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Path    "), mutedStyle.Render(m.project.Path)))
	infoLines = append(infoLines, "")
	infoBlock := strings.Join(infoLines, "\n")

	// Small logo + wordmark (right side)
	logo := tui.LogoSmallFull()
	logoW := lipgloss.Width(logo)

	// Leave a gap between info and logo
	gap := 4
	infoW := innerW - logoW - gap
	if infoW < 30 {
		infoW = 30
	}

	left := lipgloss.NewStyle().Width(infoW).Render(infoBlock)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func (m model) renderContent() string {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	valueStyle := lipgloss.NewStyle().Foreground(c.Text)

	var lines []string

	// Container table
	if m.project.Status == "stopped" {
		lines = append(lines, "  "+mutedStyle.Render("Project is not running. Use 'tainer start' to launch."))
	} else if len(m.containers) > 0 {
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Muted)

		nameW, statusW := 10, 8
		for _, ct := range m.containers {
			if len(ct.Name) > nameW {
				nameW = len(ct.Name)
			}
			if len(ct.Status) > statusW {
				statusW = len(ct.Status)
			}
		}

		lines = append(lines, fmt.Sprintf("  %s  %s  %s",
			headerStyle.Render(padRight("CONTAINER", nameW)),
			headerStyle.Render(padRight("STATUS", statusW)),
			headerStyle.Render("PORTS")))
		lines = append(lines, "  "+tui.Separator(nameW+statusW+20))

		for _, ct := range m.containers {
			ctStatus := mutedStyle.Render(padRight(ct.Status, statusW))
			if strings.HasPrefix(ct.Status, "Up") {
				ctStatus = tealStyle.Render(padRight(ct.Status, statusW))
			}
			ports := mutedStyle.Render("—")
			if ct.Ports != "" {
				ports = valueStyle.Render(ct.Ports)
			}

			lines = append(lines, fmt.Sprintf("  %s  %s  %s",
				valueStyle.Render(padRight(ct.Name, nameW)),
				ctStatus,
				ports))
		}
	}
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	c := tui.Colors()
	frameW := m.width - 4
	if frameW < 50 {
		frameW = 50
	}
	innerW := frameW - 6 // border(2) + padding(4)

	var sections []string

	// Header: project info (left) + small logo (right)
	sections = append(sections, m.renderHeader(innerW))

	// Scrollable viewport (container table)
	sections = append(sections, m.viewport.View())

	// Separator + help footer
	sep := tui.Separator(innerW)
	helpStr := m.helpM.View(m.keys)

	inner := strings.Join(sections, "\n") + "\n" + sep + "\n" + helpStr

	frame := lipgloss.NewStyle().
		Width(frameW).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(c.Blue).
		Padding(0, 2).
		Render(inner)

	rendered := tui.FullScreen(frame, m.width, m.height)

	v := tea.NewView(rendered)
	v.AltScreen = true
	return v
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
