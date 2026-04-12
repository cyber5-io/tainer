package picker

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
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

type model struct {
	title  string
	items  []string
	cursor int
	done   bool
	result Result
	width  int
	height int
}

// Run launches a vertical picker TUI and returns the selected item.
func Run(title string, items []string, defaultIndex int) (*Result, error) {
	if len(items) == 0 {
		return &Result{Cancelled: true}, nil
	}
	if defaultIndex < 0 || defaultIndex >= len(items) {
		defaultIndex = 0
	}
	ti := tui.GetTermInfo()
	m := model{
		title:  title,
		items:  items,
		cursor: defaultIndex,
		width:  ti.Width,
		height: ti.Height,
	}
	p := tui.NewProgram(m, false) // inline, no alt screen
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running picker: %w", err)
	}
	fm := final.(model)
	return &fm.result, nil
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
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			m.result = Result{
				Selected: m.items[m.cursor],
				Index:    m.cursor,
			}
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"))):
			m.result = Result{Cancelled: true}
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	c := tui.Colors()
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(m.title))
	b.WriteString("\n\n")

	for i, item := range m.items {
		if i == m.cursor {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(c.Teal).Bold(true).Render("▸ "+item))
		} else {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(c.Muted).Render("  "+item))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().Foreground(c.Muted).Render("↑↓ select   ↵ confirm   esc cancel"))
	b.WriteString("\n")

	return tea.NewView(b.String())
}
