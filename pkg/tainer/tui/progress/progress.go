package progress

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// Step describes one unit of work to display.
type Step struct {
	Label string
	Run   func() error
}

// Result is returned after the progress TUI exits.
type Result struct {
	Err error // non-nil if a step failed
}

// stepDoneMsg is sent when a step finishes.
type stepDoneMsg struct {
	err error
}

type model struct {
	title    string
	steps    []Step
	current  int
	done     bool
	err      error
	spinner  spinner.Model
	width    int
	height   int
	// Optional completion message lines (shown after all steps succeed).
	footer []string
}

// Run starts the progress TUI, executing steps sequentially with spinners.
// footer lines are displayed after all steps complete successfully.
func Run(title string, steps []Step, footer []string) (*Result, error) {
	sp := spinner.New()
	sp.Spinner = spinner.Meter

	m := model{
		title:   title,
		steps:   steps,
		spinner: sp,
		footer:  footer,
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running progress TUI: %w", err)
	}
	fm := final.(model)
	return &Result{Err: fm.err}, nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runCurrentStep())
}

func (m model) runCurrentStep() tea.Cmd {
	if m.current >= len(m.steps) {
		return nil
	}
	step := m.steps[m.current]
	return func() tea.Msg {
		err := step.Run()
		return stepDoneMsg{err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case stepDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.done = true
			return m, tea.Quit
		}
		m.current++
		if m.current >= len(m.steps) {
			m.done = true
			return m, tea.Quit
		}
		return m, m.runCurrentStep()

	case tea.KeyPressMsg:
		return m, nil
	}

	return m, nil
}

func (m model) View() tea.View {
	c := tui.Colors()
	var b strings.Builder

	b.WriteString("\n")

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
	b.WriteString("  " + titleStyle.Render(m.title) + "\n\n")

	checkDone := lipgloss.NewStyle().Foreground(c.Teal).Render("✓")
	checkFail := lipgloss.NewStyle().Foreground(c.Orange).Render("✗")
	labelStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	for i, step := range m.steps {
		switch {
		case i < m.current:
			b.WriteString("  " + checkDone + " " + labelStyle.Render(step.Label) + "\n")
		case i == m.current && !m.done:
			b.WriteString("  " + m.spinner.View() + " " + labelStyle.Render(step.Label) + "\n")
		case i == m.current && m.done && m.err != nil:
			b.WriteString("  " + checkFail + " " + labelStyle.Render(step.Label) + "\n")
		case i == m.current && m.done:
			b.WriteString("  " + checkDone + " " + labelStyle.Render(step.Label) + "\n")
		default:
			b.WriteString("  " + mutedStyle.Render("○") + " " + mutedStyle.Render(step.Label) + "\n")
		}
	}

	b.WriteString("\n")

	if m.done && m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(c.Orange)
		b.WriteString("  " + errStyle.Render("Error: "+m.err.Error()) + "\n")
	}

	if m.done && m.err == nil && len(m.footer) > 0 {
		for _, line := range m.footer {
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n")

	return tea.NewView(b.String())
}
