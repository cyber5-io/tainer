package mount

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/project"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// Result returned after the TUI exits.
type Result struct {
	Restart bool // user chose to restart after changes
}

type mode int

const (
	modeList mode = iota
	modeAdd
)

type keyMap struct {
	Navigate key.Binding
	Add      key.Binding
	Delete   key.Binding
	Restart  key.Binding
	Confirm  key.Binding
	Cancel   key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "navigate"),
		),
		Add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add mount"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restart"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

type listKeyMap struct{ km keyMap }

func (l listKeyMap) ShortHelp() []key.Binding {
	bindings := []key.Binding{l.km.Navigate, l.km.Add}
	if l.km.Delete.Enabled() {
		bindings = append(bindings, l.km.Delete)
	}
	if l.km.Restart.Enabled() {
		bindings = append(bindings, l.km.Restart)
	}
	bindings = append(bindings, l.km.Quit)
	return bindings
}
func (l listKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{l.ShortHelp()} }

type addKeyMap struct{ km keyMap }

func (a addKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{a.km.Confirm, a.km.Cancel}
}
func (a addKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{a.ShortHelp()} }

type model struct {
	projectName string
	projectDir  string
	mounts      []string // custom mounts (editable)
	internal    []string // internal mounts (read-only)
	cursor      int
	mode        mode
	inputBuf    string
	keys        keyMap
	helpM       help.Model
	changed     bool // any add/del performed
	restart     bool // user chose restart
	errMsg      string
	width       int
	height      int
	quitting    bool
}

// Run starts the interactive mount management TUI.
func Run(projectName, projectDir string, mounts, internal []string) (*Result, error) {
	m := initialModel(projectName, projectDir, mounts, internal)
	p := tui.NewProgram(m, true) // full screen
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running mount TUI: %w", err)
	}
	fm := final.(model)
	return &Result{Restart: fm.restart}, nil
}

func initialModel(projectName, projectDir string, mounts, internal []string) model {
	c := tui.Colors()
	keys := newKeyMap()
	keys.Restart.SetEnabled(false)
	if len(mounts) == 0 {
		keys.Delete.SetEnabled(false)
	}

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}
	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(c.Muted)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(c.Border)

	return model{
		projectName: projectName,
		projectDir:  projectDir,
		mounts:      mounts,
		internal:    internal,
		keys:        keys,
		helpM:       h,
		width:       80,
		height:      24,
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
		return m, nil

	case tea.KeyPressMsg:
		m.errMsg = "" // clear error on any key
		if m.mode == modeAdd {
			return m.updateAddMode(msg)
		}
		return m.updateListMode(msg)
	}

	return m, nil
}

func (m model) updateListMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Restart):
		if m.changed {
			m.restart = true
			return m, tea.Quit
		}
		return m, nil
	case key.Matches(msg, m.keys.Navigate):
		k := msg.String()
		if (k == "up" || k == "k") && m.cursor > 0 {
			m.cursor--
		}
		if (k == "down" || k == "j") && m.cursor < len(m.mounts)-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Add):
		m.mode = modeAdd
		m.inputBuf = ""
		return m, nil
	case key.Matches(msg, m.keys.Delete):
		if len(m.mounts) > 0 && m.cursor < len(m.mounts) {
			name := m.mounts[m.cursor]
			if err := project.MountDel(m.projectDir, name); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.mounts = append(m.mounts[:m.cursor], m.mounts[m.cursor+1:]...)
			if m.cursor >= len(m.mounts) && m.cursor > 0 {
				m.cursor--
			}
			m.changed = true
			m.keys.Restart.SetEnabled(true)
			m.keys.Delete.SetEnabled(len(m.mounts) > 0)
		}
		return m, nil
	}
	return m, nil
}

func (m model) updateAddMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeList
		m.inputBuf = ""
		return m, nil
	case key.Matches(msg, m.keys.Confirm):
		name := strings.TrimSpace(m.inputBuf)
		if name == "" {
			return m, nil
		}
		if err := project.MountAdd(m.projectDir, name); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.mounts = append(m.mounts, name)
		m.cursor = len(m.mounts) - 1
		m.changed = true
		m.keys.Restart.SetEnabled(true)
		m.keys.Delete.SetEnabled(true)
		m.mode = modeList
		m.inputBuf = ""
		return m, nil
	case k == "backspace":
		if len(m.inputBuf) > 0 {
			m.inputBuf = m.inputBuf[:len(m.inputBuf)-1]
		}
		return m, nil
	default:
		if len(k) == 1 && k[0] >= 32 && k[0] < 127 {
			m.inputBuf += k
		}
		return m, nil
	}
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

	// Header: mount list (left) + small logo (right)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)

	var infoLines []string
	infoLines = append(infoLines, fmt.Sprintf("%s  %s",
		tui.TitleStyle().Render("Mounts"),
		mutedStyle.Render(m.projectName)))
	infoLines = append(infoLines, "")

	// Custom mounts
	if len(m.mounts) == 0 {
		infoLines = append(infoLines, "  "+mutedStyle.Render("No custom mounts."))
	} else {
		for i, mount := range m.mounts {
			prefix := "  "
			style := textStyle
			if i == m.cursor && m.mode == modeList {
				prefix = tealStyle.Render("▸ ")
				style = lipgloss.NewStyle().Bold(true).Foreground(c.Teal)
			}
			infoLines = append(infoLines, prefix+style.Render(mount))
		}
	}

	// Separator between custom and internal
	infoLines = append(infoLines, "")
	infoLines = append(infoLines, "  "+mutedStyle.Render("Internal:"))
	for _, mount := range m.internal {
		infoLines = append(infoLines, "  "+mutedStyle.Render("  "+mount))
	}

	// Pad to at least 7 lines for logo alignment
	for len(infoLines) < 7 {
		infoLines = append(infoLines, "")
	}
	infoBlock := strings.Join(infoLines, "\n")

	logo := tui.LogoSmallFull()
	logoW := lipgloss.Width(logo)
	gap := 4
	leftW := innerW - logoW - gap
	if leftW < 30 {
		leftW = 30
	}
	left := lipgloss.NewStyle().Width(leftW).Render(infoBlock)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right))

	// Add mode: inline text input
	if m.mode == modeAdd {
		sections = append(sections, "")
		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Blue)
		cursor := tealStyle.Render("█")
		sections = append(sections, "  "+labelStyle.Render("New mount name: ")+
			textStyle.Render(m.inputBuf)+cursor)
	}

	// Error message
	if m.errMsg != "" {
		sections = append(sections, "")
		sections = append(sections, "  "+lipgloss.NewStyle().Foreground(c.Orange).Render("Error: "+m.errMsg))
	}

	// Changed indicator
	if m.changed {
		sections = append(sections, "")
		sections = append(sections, "  "+tealStyle.Render("●")+" "+
			mutedStyle.Render("Changes detected. Press")+
			" "+lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render("r")+
			" "+mutedStyle.Render("to restart project."))
	}

	// Separator + help
	sections = append(sections, "")
	sep := tui.Separator(innerW)
	var helpStr string
	if m.mode == modeAdd {
		helpStr = m.helpM.View(addKeyMap{m.keys})
	} else {
		helpStr = m.helpM.View(listKeyMap{m.keys})
	}

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
