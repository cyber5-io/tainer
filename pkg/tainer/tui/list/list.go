package list

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// appKeyMap defines the application-level keybindings shown in the help footer.
type appKeyMap struct {
	Navigate key.Binding
	Open     key.Binding
	Toggle   key.Binding
	Sort     key.Binding
	Quit     key.Binding
}

func newAppKeyMap() appKeyMap {
	return appKeyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "navigate"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "start"),
		),
		Sort: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "sort:name"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (km appKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Navigate, km.Open, km.Toggle, km.Sort, km.Quit}
}

func (km appKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.Navigate, km.Open, km.Toggle},
		{km.Sort, km.Quit},
	}
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

type model struct {
	projects   []Project
	sorted     []Project // sorted view of projects
	table      table.Model
	helpModel  help.Model
	keys       appKeyMap
	sort       sortMode
	router     routerInfo
	width      int
	height     int
	quitting   bool
	busyName   string // project currently starting/stopping
	busyAction string // "start" or "stop"
	spinner    spinner.Model
}

// Run starts the interactive list TUI.
func Run(projects []Project, routerRunning bool, routerCount int) (*Result, error) {
	m := initialModel(projects, routerRunning, routerCount)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running list TUI: %w", err)
	}
	return &Result{Cancelled: final.(model).quitting}, nil
}

func initialModel(projects []Project, routerRunning bool, routerCount int) model {
	c := tui.Colors()
	keys := newAppKeyMap()

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

	// Unstyled spinner — no ANSI color codes, so it won't break the
	// table's Selected background highlight across the row.
	sp := spinner.New()
	sp.Spinner = spinner.Meter

	m := model{
		projects: projects,
		keys:     keys,
		spinner:  sp,
		sort:     sortDefault,
		router: routerInfo{
			Running: routerRunning,
			Count:   routerCount,
		},
	}
	m.buildSorted()

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}

	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(c.Muted)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(c.Border)
	m.helpModel = h

	cols := computeColumns(80)
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(m.buildTableRows()),
		table.WithFocused(true),
		table.WithStyles(s),
		table.WithWidth(80),
		table.WithHeight(20),
	)
	m.table = t

	return m
}

func computeColumns(termWidth int) []table.Column {
	tableW := termWidth - 8
	if tableW < 60 {
		tableW = 60
	}
	// Each column gets 2 chars padding from Cell style
	usable := tableW - 10
	typeW := 10
	statusW := 12
	remaining := usable - typeW - statusW
	nameW := remaining * 20 / 100
	domainW := remaining * 30 / 100
	pathW := remaining - nameW - domainW
	if nameW < 12 {
		nameW = 12
	}
	if domainW < 16 {
		domainW = 16
	}
	if pathW < 10 {
		pathW = 10
	}
	return []table.Column{
		{Title: "NAME", Width: nameW},
		{Title: "TYPE", Width: typeW},
		{Title: "DOMAIN", Width: domainW},
		{Title: "STATUS", Width: statusW},
		{Title: "PATH", Width: pathW},
	}
}

func (m *model) buildSorted() {
	m.sorted = make([]Project, len(m.projects))
	copy(m.sorted, m.projects)

	switch m.sort {
	case sortActive:
		sort.Slice(m.sorted, func(i, j int) bool {
			ri := m.sorted[i].Status == "Running"
			rj := m.sorted[j].Status == "Running"
			if ri != rj {
				return ri
			}
			return m.sorted[i].Name < m.sorted[j].Name
		})
	case sortType:
		sort.Slice(m.sorted, func(i, j int) bool {
			if m.sorted[i].Type != m.sorted[j].Type {
				return m.sorted[i].Type < m.sorted[j].Type
			}
			return m.sorted[i].Name < m.sorted[j].Name
		})
	default:
		sort.Slice(m.sorted, func(i, j int) bool {
			return m.sorted[i].Name < m.sorted[j].Name
		})
	}
}

func (m model) buildTableRows() []table.Row {
	c := tui.Colors()
	cursor := m.table.Cursor()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	blueStyle := lipgloss.NewStyle().Foreground(c.Blue)

	rows := make([]table.Row, len(m.sorted))
	for i, p := range m.sorted {
		selected := i == cursor
		status := "○ stopped"
		domainLink := "\x1b]8;;https://" + p.Domain + "\x1b\\" + p.Domain + "\x1b]8;;\x1b\\"
		domain := domainLink
		if p.Status == "Running" {
			status = "● running"
			domain = domainLink + " ↗"
			if !selected {
				status = tealStyle.Render(status)
				domain = blueStyle.Render(domainLink) + " ↗"
			}
		}
		if m.isBusy() && p.Name == m.busyName {
			if m.busyAction == "start" {
				status = m.spinner.View() + " starting"
			} else {
				status = m.spinner.View() + " stopping"
			}
		}
		rows[i] = table.Row{p.Name, p.Type, domain, status, p.Path}
	}
	return rows
}

func (m model) isBusy() bool {
	return m.busyName != ""
}

func (m model) selectedProject() *Project {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.sorted) {
		return nil
	}
	return &m.sorted[idx]
}

func (m *model) updateHelpBindings() {
	p := m.selectedProject()
	if p != nil && p.Status == "Running" {
		m.keys.Toggle.SetHelp("s", "stop")
		m.keys.Open.SetEnabled(true)
	} else {
		m.keys.Toggle.SetHelp("s", "start")
		m.keys.Open.SetEnabled(false)
	}
	m.keys.Sort.SetHelp("m", "sort:"+m.sort.label())
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		innerW := m.width - 8 // frame border(2) + padding(4) + margin(2)
		if innerW < 60 {
			innerW = 60
		}
		m.table.SetColumns(computeColumns(m.width))
		m.table.SetWidth(innerW)
		// Height: frame border(2) + padding(2) + header(7 logo) + blank(1) +
		//   separator(1) + path(1) + blank(1) + help(1) = 16
		tableH := m.height - 16
		if tableH < 5 {
			tableH = 5
		}
		m.table.SetHeight(tableH)
		m.table.SetRows(m.buildTableRows())
		m.helpModel.SetWidth(innerW)
		return m, nil

	case spinner.TickMsg:
		if m.isBusy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.table.SetRows(m.buildTableRows())
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
			m.buildSorted()
			m.table.SetRows(m.buildTableRows())
		}
		m.updateHelpBindings()
		return m, nil

	case tea.KeyPressMsg:
		if m.isBusy() {
			if key.Matches(msg, m.keys.Quit) {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Open):
			return m, m.openSelectedDomain()
		case key.Matches(msg, m.keys.Toggle):
			return m, m.toggleSelectedPod()
		case key.Matches(msg, m.keys.Sort):
			m.sort = (m.sort + 1) % 3
			m.buildSorted()
			m.table.SetRows(m.buildTableRows())
			m.updateHelpBindings()
			return m, nil
		}

	case tea.MouseWheelMsg:
		if m.isBusy() {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.table.MoveUp(1)
		} else if msg.Button == tea.MouseWheelDown {
			m.table.MoveDown(1)
		}
		m.table.SetRows(m.buildTableRows())
		m.updateHelpBindings()
		return m, nil

	case tea.MouseClickMsg:
		if m.isBusy() {
			return m, nil
		}
	}

	// Let the table handle navigation keys (up/down/pgup/pgdown/g/G)
	prev := m.table.Cursor()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if m.table.Cursor() != prev {
		m.table.SetRows(m.buildTableRows())
	}
	m.updateHelpBindings()
	return m, cmd
}

func (m model) openSelectedDomain() tea.Cmd {
	p := m.selectedProject()
	if p == nil || p.Status != "Running" {
		return nil
	}
	url := "https://" + p.Domain
	return func() tea.Msg {
		openBrowser(url)
		return nil
	}
}

func (m *model) toggleSelectedPod() tea.Cmd {
	p := m.selectedProject()
	if p == nil {
		return nil
	}
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
		exec.Command("open", url).Start() //nolint:errcheck
	case "linux":
		exec.Command("xdg-open", url).Start() //nolint:errcheck
	}
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	c := tui.Colors()
	frameW := m.width - 4
	if frameW < 60 {
		frameW = 60
	}

	innerW := frameW - 6 // border(2) + padding(4)

	var sections []string

	// Header: router status (left) + small logo (right)
	routerDot := lipgloss.NewStyle().Foreground(c.Muted).Render("●")
	routerStatus := lipgloss.NewStyle().Foreground(c.Muted).Render("stopped")
	if m.router.Running {
		routerDot = lipgloss.NewStyle().Foreground(c.Teal).Render("●")
		routerStatus = lipgloss.NewStyle().Foreground(c.Teal).Render(
			fmt.Sprintf("running (%d projects)", m.router.Count))
	}
	routerLine := fmt.Sprintf("%s %s  %s",
		routerDot,
		lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render("tainer-router"),
		routerStatus,
	)

	logo := tui.LogoSmallFull()
	logoW := lipgloss.Width(logo)
	gap := 4
	leftW := innerW - logoW - gap
	if leftW < 30 {
		leftW = 30
	}
	left := lipgloss.NewStyle().Width(leftW).Render(routerLine)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right))
	sections = append(sections, "")

	// Table (with built-in viewport scrolling)
	sections = append(sections, m.table.View())

	// Separator
	sections = append(sections,
		lipgloss.NewStyle().Foreground(c.Border).Render(strings.Repeat("─", innerW)))

	// Full path of selected project
	pathLine := ""
	if p := m.selectedProject(); p != nil {
		pathLine = lipgloss.NewStyle().Foreground(c.Muted).Render("  ") +
			lipgloss.NewStyle().Foreground(c.Text).Render(p.Path)
	}
	sections = append(sections, pathLine)
	sections = append(sections, "")

	// Help footer
	if m.isBusy() {
		busyLabel := "Starting"
		if m.busyAction == "stop" {
			busyLabel = "Stopping"
		}
		sections = append(sections,
			lipgloss.NewStyle().Foreground(c.Muted).Render(
				fmt.Sprintf("  %s %s…", busyLabel, m.busyName)))
	} else {
		sections = append(sections, m.helpModel.View(m.keys))
	}

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
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
