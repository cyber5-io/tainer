package picker

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// Result holds the user's selection.
type Result struct {
	Selected  string
	Index     int
	Cancelled bool
}

type pickerKeyMap struct {
	Navigate key.Binding
	Confirm  key.Binding
	Quit     key.Binding
}

func newPickerKeyMap() pickerKeyMap {
	return pickerKeyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "navigate"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc", "ctrl+c"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

func (km pickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Navigate, km.Confirm, km.Quit}
}

func (km pickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{km.Navigate, km.Confirm, km.Quit}}
}

type model struct {
	title     string
	items     []string
	table     table.Model
	helpModel help.Model
	keys      pickerKeyMap
	done      bool
	result    Result
	width     int
	height    int
}

// Run launches a picker TUI and returns the selected item.
func Run(title string, items []string, defaultIndex int) (*Result, error) {
	if len(items) == 0 {
		return &Result{Cancelled: true}, nil
	}
	if defaultIndex < 0 || defaultIndex >= len(items) {
		defaultIndex = 0
	}
	m := initialModel(title, items, defaultIndex)
	p := tui.NewProgram(m, true) // full screen
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running picker: %w", err)
	}
	fm := final.(model)
	return &fm.result, nil
}

func initialModel(title string, items []string, defaultIndex int) model {
	c := tui.Colors()
	keys := newPickerKeyMap()

	var selected lipgloss.Style
	if tui.IsDarkBackground() {
		selected = lipgloss.NewStyle().Bold(true).
			Background(lipgloss.Color("#FFCC33")).
			Foreground(lipgloss.Color("#000000"))
	} else {
		selected = lipgloss.NewStyle().Bold(true).
			Background(lipgloss.Color("#2563EB")).
			Foreground(lipgloss.Color("#FFFFFF"))
	}

	s := table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(c.Muted).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
		Selected: selected,
	}

	ti := tui.GetTermInfo()
	tableW := ti.Width - 8
	if tableW < 40 {
		tableW = 40
	}
	if tableW > 60 {
		tableW = 60
	}

	rows := make([]table.Row, len(items))
	for i, item := range items {
		rows[i] = table.Row{item}
	}

	cols := []table.Column{
		{Title: "FILE", Width: tableW - 4},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithStyles(s),
		table.WithWidth(tableW),
		table.WithHeight(len(items)+1),
	)
	t.SetCursor(defaultIndex)

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}

	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(c.Muted)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(c.Border)

	return model{
		title:     title,
		items:     items,
		table:     t,
		helpModel: h,
		keys:      keys,
		width:     ti.Width,
		height:    ti.Height,
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
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			idx := m.table.Cursor()
			if idx >= 0 && idx < len(m.items) {
				m.result = Result{
					Selected: m.items[idx],
					Index:    idx,
				}
			}
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"))):
			m.result = Result{Cancelled: true}
			m.done = true
			return m, tea.Quit
		}
	}

	// Let the table handle navigation (up/down/j/k)
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	c := tui.Colors()

	frameW := m.width - 4
	if frameW < 40 {
		frameW = 40
	}
	if frameW > 64 {
		frameW = 64
	}
	innerW := frameW - 6

	var sections []string

	// Title + logo
	logo := tui.LogoSmallFull()
	logoW := lipgloss.Width(logo)
	gap := 4
	leftW := innerW - logoW - gap
	if leftW < 20 {
		leftW = 20
	}
	titleLine := lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(m.title)
	left := lipgloss.NewStyle().Width(leftW).Render(titleLine)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right))
	sections = append(sections, "")

	// Table
	sections = append(sections, m.table.View())

	// Separator
	sections = append(sections,
		lipgloss.NewStyle().Foreground(c.Border).Render(strings.Repeat("─", innerW)))

	// Help footer
	sections = append(sections, m.helpModel.View(m.keys))

	content := strings.Join(sections, "\n")

	frame := lipgloss.NewStyle().
		Width(frameW).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(c.Border).
		Padding(1, 2).
		Render(content)

	rendered := tui.FullScreen(frame, m.width, m.height)

	v := tea.NewView(rendered)
	v.AltScreen = true
	return v
}
