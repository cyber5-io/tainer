package list

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ltable "github.com/charmbracelet/lipgloss/table"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// Project holds the data needed to render a row.
type Project struct {
	Name   string
	Type   string
	Domain string
	Status string // "Running" or "stopped"
	Path   string
}

// Result returned after the TUI exits.
type Result struct {
	Cancelled bool
}

type sortMode int

const (
	sortDefault sortMode = iota // alphabetical
	sortActive                  // running first, then stopped
	sortType                    // grouped by type
)

func (s sortMode) label() string {
	switch s {
	case sortActive:
		return "active"
	case sortType:
		return "type"
	default:
		return "name"
	}
}

// displayRow is a project row or a separator.
type displayRow struct {
	project   *Project // nil for separator rows
	separator bool
}

type model struct {
	projects   []Project
	rows       []displayRow
	cursor     int
	sort       sortMode
	router     routerInfo
	width      int
	height     int
	quitting   bool
	busyName   string // project currently starting/stopping
	busyAction string // "start" or "stop"
	spinner    spinner.Model
}

type routerInfo struct {
	Running bool
	Count   int
}

// podActionMsg is sent after a start/stop completes.
type podActionMsg struct {
	name   string
	action string // "start" or "stop"
	err    error
}

func Run(projects []Project, routerRunning bool, routerCount int) (*Result, error) {
	m := initialModel(projects, routerRunning, routerCount)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running list TUI: %w", err)
	}
	return &Result{Cancelled: final.(model).quitting}, nil
}

func initialModel(projects []Project, routerRunning bool, routerCount int) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(tui.Colors().Teal)

	m := model{
		projects: projects,
		spinner:  s,
		router: routerInfo{
			Running: routerRunning,
			Count:   routerCount,
		},
	}
	m.buildRows()
	return m
}

func (m *model) buildRows() {
	m.rows = nil

	switch m.sort {
	case sortActive:
		var running, stopped []Project
		for _, p := range m.projects {
			if p.Status == "Running" {
				running = append(running, p)
			} else {
				stopped = append(stopped, p)
			}
		}
		sort.Slice(running, func(i, j int) bool { return running[i].Name < running[j].Name })
		sort.Slice(stopped, func(i, j int) bool { return stopped[i].Name < stopped[j].Name })
		for i := range running {
			m.rows = append(m.rows, displayRow{project: &running[i]})
		}
		if len(running) > 0 && len(stopped) > 0 {
			m.rows = append(m.rows, displayRow{separator: true})
		}
		for i := range stopped {
			m.rows = append(m.rows, displayRow{project: &stopped[i]})
		}

	case sortType:
		types := map[string][]Project{}
		var typeOrder []string
		sorted := make([]Project, len(m.projects))
		copy(sorted, m.projects)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		for _, p := range sorted {
			if _, ok := types[p.Type]; !ok {
				typeOrder = append(typeOrder, p.Type)
			}
			types[p.Type] = append(types[p.Type], p)
		}
		sort.Strings(typeOrder)
		for ti, t := range typeOrder {
			if ti > 0 {
				m.rows = append(m.rows, displayRow{separator: true})
			}
			for i := range types[t] {
				m.rows = append(m.rows, displayRow{project: &types[t][i]})
			}
		}

	default:
		sorted := make([]Project, len(m.projects))
		copy(sorted, m.projects)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		for i := range sorted {
			m.rows = append(m.rows, displayRow{project: &sorted[i]})
		}
	}

	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.skipToProject(0)
}

func (m *model) skipToProject(dir int) {
	if len(m.rows) == 0 {
		return
	}
	if m.rows[m.cursor].project != nil {
		return
	}
	if dir >= 0 {
		for m.cursor < len(m.rows)-1 && m.rows[m.cursor].project == nil {
			m.cursor++
		}
	} else {
		for m.cursor > 0 && m.rows[m.cursor].project == nil {
			m.cursor--
		}
	}
}

func (m model) isBusy() bool {
	return m.busyName != ""
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		if m.isBusy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case podActionMsg:
		m.busyName = ""
		m.busyAction = ""
		if msg.err == nil {
			for i := range m.projects {
				if m.projects[i].Name == msg.name {
					if msg.action == "start" {
						m.projects[i].Status = "Running"
					} else {
						m.projects[i].Status = "stopped"
					}
				}
			}
			m.buildRows()
		}
		return m, nil

	case tea.KeyMsg:
		if m.isBusy() {
			// Only allow quit while busy
			if msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.skipToProject(-1)
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.skipToProject(1)
			}
			return m, nil
		case "enter":
			return m, m.openSelectedDomain()
		case "s":
			return m, m.toggleSelectedPod()
		case "m":
			m.sort = (m.sort + 1) % 3
			m.buildRows()
			return m, nil
		}

	case tea.MouseMsg:
		if m.isBusy() {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonLeft:
			return m, m.handleClick(msg)
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
				m.skipToProject(-1)
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.skipToProject(1)
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *model) handleClick(msg tea.MouseMsg) tea.Cmd {
	frameH := m.totalFrameHeight()
	frameStartY := (m.height - frameH) / 2
	// border(1) + padding(1) + router(1) + blank(1) + header(1) + border_line(1) = 6
	tableDataStartY := frameStartY + 6

	clickedRow := msg.Y - tableDataStartY
	if clickedRow >= 0 && clickedRow < len(m.rows) && m.rows[clickedRow].project != nil {
		m.cursor = clickedRow
	}
	return nil
}

func (m model) totalFrameHeight() int {
	// border(2) + padding(2) + router(1) + blank(1) + header(1) + border_line(1)
	// + rows + separator(1) + path_line(1) + blank(1) + footer(1)
	return 2 + 2 + 1 + 1 + 1 + 1 + len(m.rows) + 1 + 1 + 1 + 1
}

func (m model) openSelectedDomain() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].project == nil {
		return nil
	}
	p := m.rows[m.cursor].project
	if p.Status != "Running" {
		return nil
	}
	url := "https://" + p.Domain
	return func() tea.Msg {
		openBrowser(url)
		return nil
	}
}

func (m *model) toggleSelectedPod() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].project == nil {
		return nil
	}
	p := m.rows[m.cursor].project
	name := p.Name
	path := p.Path
	action := "start"
	if p.Status == "Running" {
		action = "stop"
	}

	m.busyName = name
	m.busyAction = action

	execCmd := func() tea.Msg {
		cmd := exec.Command("tainer", action)
		cmd.Dir = path
		err := cmd.Run()
		return podActionMsg{name: name, action: action, err: err}
	}

	return tea.Batch(m.spinner.Tick, execCmd)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	}
}

// truncatePath shortens a path keeping the beginning and end visible.
func truncatePath(path string, maxW int) string {
	if len(path) <= maxW || maxW < 10 {
		return path
	}
	endLen := (maxW - 1) * 3 / 5
	startLen := maxW - 1 - endLen
	return path[:startLen] + "…" + path[len(path)-endLen:]
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	c := tui.Colors()
	// Frame dimensions
	frameW := m.width - 4
	if frameW < 60 {
		frameW = 60
	}
	tableW := frameW - 6 // border(2) + padding(4)

	var sections []string

	// Router status
	routerDot := lipgloss.NewStyle().Foreground(c.Muted).Render("●")
	routerStatus := lipgloss.NewStyle().Foreground(c.Muted).Render("stopped")
	if m.router.Running {
		routerDot = lipgloss.NewStyle().Foreground(c.Teal).Render("●")
		routerStatus = lipgloss.NewStyle().Foreground(c.Teal).Render(fmt.Sprintf("running (%d projects)", m.router.Count))
	}
	sections = append(sections, fmt.Sprintf("%s %s  %s",
		routerDot,
		lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render("tainer-router"),
		routerStatus,
	))
	sections = append(sections, "")

	// Build table data
	var tableRows [][]string
	rowToDisplayIdx := make(map[int]int)
	for i, row := range m.rows {
		if row.separator {
			tableRows = append(tableRows, []string{"", "", "", "", ""})
		} else {
			p := row.project
			status := "● stopped"
			domain := p.Domain
			if p.Status == "Running" {
				status = "● running"
				domain = p.Domain + " ↗"
			}
			// Show spinner for busy project
			if m.isBusy() && p.Name == m.busyName {
				if m.busyAction == "start" {
					status = m.spinner.View() + " starting"
				} else {
					status = m.spinner.View() + " stopping"
				}
			}
			pathW := tableW - 16 - 10 - 24 - 14 - 14
			if pathW < 10 {
				pathW = 10
			}
			tableRows = append(tableRows, []string{
				p.Name,
				p.Type,
				domain,
				status,
				truncatePath(p.Path, pathW),
			})
		}
		rowToDisplayIdx[len(tableRows)-1] = i
	}

	// Highlight colors
	highlightFg := c.Text
	highlightBg := lipgloss.Color("#162032")
	if !tui.IsDarkBackground() {
		highlightBg = lipgloss.Color("#CBD5E1")
	}

	cursor := m.cursor
	t := ltable.New().
		Headers("NAME", "TYPE", "DOMAIN", "STATUS", "PATH").
		Rows(tableRows...).
		Width(tableW).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderHeader(true).
		BorderStyle(lipgloss.NewStyle().Foreground(c.Border)).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(2)

			if row == ltable.HeaderRow {
				return s.Bold(true).Foreground(c.Muted)
			}

			displayIdx, ok := rowToDisplayIdx[row]
			if ok && m.rows[displayIdx].separator {
				return s.Foreground(c.Muted)
			}

			isSelected := ok && displayIdx == cursor

			if isSelected {
				base := s.Background(highlightBg).Foreground(highlightFg).Bold(true)
				switch col {
				case 2:
					if ok && m.rows[displayIdx].project != nil && m.rows[displayIdx].project.Status == "Running" {
						return base.Foreground(c.Blue)
					}
					return base
				case 3:
					if ok && m.rows[displayIdx].project != nil && m.rows[displayIdx].project.Status == "Running" {
						return base.Foreground(c.Teal)
					}
					return base
				default:
					return base
				}
			}

			switch col {
			case 0:
				return s.Bold(true).Foreground(c.Text)
			case 1:
				return s.Foreground(c.Muted)
			case 2:
				if ok && m.rows[displayIdx].project != nil && m.rows[displayIdx].project.Status == "Running" {
					return s.Foreground(c.Blue)
				}
				return s.Foreground(c.Muted)
			case 3:
				if ok && m.rows[displayIdx].project != nil && m.rows[displayIdx].project.Status == "Running" {
					return s.Foreground(c.Teal)
				}
				return s.Foreground(c.Muted)
			case 4:
				return s.Foreground(c.Muted)
			}
			return s
		})

	sections = append(sections, t.Render())

	// Separator before footer
	sections = append(sections, lipgloss.NewStyle().Foreground(c.Border).Render(strings.Repeat("─", tableW)))

	// Path info line (full path of selected project)
	pathLine := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].project != nil {
		p := m.rows[m.cursor].project
		pathLine = lipgloss.NewStyle().Foreground(c.Muted).Render("  ") +
			lipgloss.NewStyle().Foreground(c.Text).Render(p.Path)
	}
	sections = append(sections, pathLine)

	// Footer with actions
	sections = append(sections, "")

	if m.isBusy() {
		busyLabel := "Starting"
		if m.busyAction == "stop" {
			busyLabel = "Stopping"
		}
		sections = append(sections,
			lipgloss.NewStyle().Foreground(c.Muted).Render(fmt.Sprintf("  %s %s…", busyLabel, m.busyName)),
		)
	} else {
		keyStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
		descStyle := lipgloss.NewStyle().Foreground(c.Muted)
		sep := descStyle.Render("    ")

		var parts []string
		parts = append(parts, keyStyle.Render("↑↓")+descStyle.Render(" navigate"))

		if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].project != nil {
			p := m.rows[m.cursor].project
			if p.Status == "Running" {
				parts = append(parts,
					keyStyle.Render("enter")+descStyle.Render(" open"),
					keyStyle.Render("s")+lipgloss.NewStyle().Foreground(c.Orange).Render(" stop"),
				)
			} else {
				parts = append(parts,
					keyStyle.Render("s")+lipgloss.NewStyle().Foreground(c.Teal).Render(" start"),
				)
			}
		}

		sortLabel := lipgloss.NewStyle().Foreground(c.Teal).Render(m.sort.label())
		parts = append(parts,
			keyStyle.Render("m")+descStyle.Render(" sort:")+sortLabel,
			keyStyle.Render("q")+descStyle.Render(" quit"),
		)

		sections = append(sections, "  "+strings.Join(parts, sep))
	}

	content := strings.Join(sections, "\n")

	frame := lipgloss.NewStyle().
		Width(frameW).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(c.Border).
		Padding(1, 2).
		Render(content)

	return tui.FullScreen(frame, m.width, m.height)
}
