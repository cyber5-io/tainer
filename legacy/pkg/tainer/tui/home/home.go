package home

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

// ProjectSummary holds aggregate stats about registered projects.
type ProjectSummary struct {
	Total   int
	Running int
	Types   map[string]int // type → count
}

// ProjectEntry is a project available for selection in the picker.
type ProjectEntry struct {
	Name      string
	Type      string
	Domain    string
	Status    string // "Running" or "stopped"
	Path      string
	IsCurrent bool // true if CWD matches this project
}

// Command represents a menu entry.
type Command struct {
	Key          string // shortcut letter
	Label        string
	Desc         string
	Cmd          string // action identifier returned in Result
	NeedsProject bool   // true if command applies to a specific project
}

// Result returned after the TUI exits.
type Result struct {
	Action     string // command Cmd value, or ""
	ProjectDir string // project path (for project-scoped commands)
}

// tickMsg drives the banner shimmer animation.
type tickMsg time.Time

type screenMode int

const (
	modeMenu        screenMode = iota // command menu
	modePickProject                   // project picker for a chosen command
)

var commands = []Command{
	{Key: "i", Label: "init", Desc: "Create a new project", Cmd: "init"},
	{Key: "l", Label: "list", Desc: "View all projects", Cmd: "list"},
	{Key: "s", Label: "start", Desc: "Start a project", Cmd: "start", NeedsProject: true},
	{Key: "t", Label: "stop", Desc: "Stop a running project", Cmd: "stop", NeedsProject: true},
	{Key: "u", Label: "update", Desc: "Update project images", Cmd: "update", NeedsProject: true},
	{Key: "c", Label: "update core", Desc: "Update the tainer binary", Cmd: "update core"},
	{Key: "d", Label: "destroy", Desc: "Remove a project", Cmd: "destroy", NeedsProject: true},
}

type menuKeyMap struct {
	Navigate key.Binding
	Select   key.Binding
	Quit     key.Binding
}

func newMenuKeyMap() menuKeyMap {
	return menuKeyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "navigate"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (km menuKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Navigate, km.Select, km.Quit}
}

func (km menuKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{km.ShortHelp()}
}

type pickerKeyMap struct {
	Navigate key.Binding
	Select   key.Binding
	Back     key.Binding
	Quit     key.Binding
}

func newPickerKeyMap() pickerKeyMap {
	return pickerKeyMap{
		Navigate: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("↑↓", "navigate"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

func (km pickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Navigate, km.Select, km.Back, km.Quit}
}

func (km pickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{km.ShortHelp()}
}

type model struct {
	mode     screenMode
	summary  ProjectSummary
	projects []ProjectEntry

	// Menu state
	menuCursor int
	menuKeys   menuKeyMap

	// Picker state
	pickerCursor int
	pickerCmd    Command // the command that triggered the picker
	pickerKeys   pickerKeyMap

	// Shared
	animTick int
	helpM    help.Model
	viewport viewport.Model
	width    int
	height   int
	quitting bool
	result   Result
}

// Run starts the interactive home screen TUI.
func Run(summary ProjectSummary, projects []ProjectEntry) (*Result, error) {
	m := initialModel(summary, projects)
	p := tui.NewProgram(m, true) // full screen
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running home TUI: %w", err)
	}
	fm := final.(model)
	return &fm.result, nil
}

func initialModel(summary ProjectSummary, projects []ProjectEntry) model {
	c := tui.Colors()
	menuKeys := newMenuKeyMap()
	pickerKeys := newPickerKeyMap()

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}
	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(c.Muted)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(c.Border)

	// Sort projects: current dir first, then running, then alphabetical
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].IsCurrent != projects[j].IsCurrent {
			return projects[i].IsCurrent
		}
		ri := projects[i].Status == "Running"
		rj := projects[j].Status == "Running"
		if ri != rj {
			return ri
		}
		return projects[i].Name < projects[j].Name
	})

	vp := viewport.New(
		viewport.WithWidth(80),
		viewport.WithHeight(10),
	)
	// Disable viewport's own key handling — we manage scroll manually
	vp.KeyMap = viewport.KeyMap{}
	vp.MouseWheelEnabled = false

	return model{
		mode:       modeMenu,
		summary:    summary,
		projects:   projects,
		menuKeys:   menuKeys,
		pickerKeys: pickerKeys,
		helpM:      h,
		viewport:   vp,
		width:      80,
		height:     24,
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tickCmd()
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

	case tickMsg:
		m.animTick++
		return m, tickCmd()

	case tea.KeyPressMsg:
		switch m.mode {
		case modeMenu:
			return m.handleMenuKey(msg)
		case modePickProject:
			return m.handlePickerKey(msg)
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

	// Viewport height = total frame minus banner, stats, separator, help
	// Banner is rendered outside viewport; frame border(2) + padding top/bottom are in the frame
	// Reserve: frame border(2) + frame padding(0,2 → 0 vertical) + sep(1) + help(1) + blank after banner(0)
	// The banner height varies but is roughly 14 lines (logo 10 + name + subtitle + blanks)
	// For small terminals we skip the banner, so viewport gets more space
	bannerH := m.bannerHeight()
	// frame border(2) + separator(1) + help(1)
	chrome := 4
	if m.mode == modeMenu {
		chrome += 2 // stats line(1) + blank line(1)
	}
	vpH := m.height - bannerH - chrome
	if vpH < 4 {
		vpH = 4
	}
	m.viewport.SetHeight(vpH)
	m.refreshViewportContent()
}

func (m model) bannerHeight() int {
	// Skip banner entirely if terminal height < 20
	if m.height < 20 {
		return 0
	}
	// 1 blank + 10 logo + 2 blanks + name + blank + subtitle + blank = 17
	return 17
}

func (m *model) refreshViewportContent() {
	content := m.renderScrollableContent()
	yOff := m.viewport.YOffset()
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(yOff)
}

func (m *model) ensureCursorVisible(cursor, itemCount int) {
	if itemCount == 0 {
		return
	}
	vpH := m.viewport.Height()
	// Each item is 1 line; there's a header of ~3 lines in picker mode
	headerLines := 0
	if m.mode == modePickProject {
		headerLines = 3
	}
	itemLine := headerLines + cursor
	yOff := m.viewport.YOffset()

	if itemLine < yOff {
		m.viewport.SetYOffset(itemLine)
	} else if itemLine >= yOff+vpH {
		m.viewport.SetYOffset(itemLine - vpH + 1)
	}
}

// ---------- Menu mode ----------

func (m model) handleMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if k == "q" || k == "esc" || k == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	// Letter shortcuts
	for _, cmd := range commands {
		if k == cmd.Key {
			return m.selectCommand(cmd)
		}
	}

	switch k {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
			m.refreshViewportContent()
			m.ensureCursorVisible(m.menuCursor, len(commands))
		}
	case "down", "j":
		if m.menuCursor < len(commands)-1 {
			m.menuCursor++
			m.refreshViewportContent()
			m.ensureCursorVisible(m.menuCursor, len(commands))
		}
	case "enter":
		return m.selectCommand(commands[m.menuCursor])
	}

	return m, nil
}

func (m model) selectCommand(cmd Command) (model, tea.Cmd) {
	if !cmd.NeedsProject || len(m.projects) == 0 {
		m.result.Action = cmd.Cmd
		m.quitting = true
		return m, tea.Quit
	}

	// If only one project, use it directly
	if len(m.projects) == 1 {
		m.result.Action = cmd.Cmd
		m.result.ProjectDir = m.projects[0].Path
		m.quitting = true
		return m, tea.Quit
	}

	m.mode = modePickProject
	m.pickerCmd = cmd
	m.pickerCursor = 0
	m.refreshViewportContent()
	m.viewport.GotoTop()
	return m, nil
}

// ---------- Picker mode ----------

func (m model) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if k == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	switch k {
	case "esc":
		m.mode = modeMenu
		m.refreshViewportContent()
		m.viewport.GotoTop()
		m.ensureCursorVisible(m.menuCursor, len(commands))
		return m, nil
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m.refreshViewportContent()
			m.ensureCursorVisible(m.pickerCursor, len(m.projects))
		}
	case "down", "j":
		if m.pickerCursor < len(m.projects)-1 {
			m.pickerCursor++
			m.refreshViewportContent()
			m.ensureCursorVisible(m.pickerCursor, len(m.projects))
		}
	case "enter":
		p := m.projects[m.pickerCursor]
		m.result.Action = m.pickerCmd.Cmd
		m.result.ProjectDir = p.Path
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// ---------- View ----------

func (m model) renderScrollableContent() string {
	switch m.mode {
	case modeMenu:
		return m.renderMenu()
	case modePickProject:
		return m.renderPickerContent()
	}
	return ""
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

	// Banner (fixed, outside viewport) — skip if terminal too short
	if m.height >= 20 {
		banner := tui.Banner("", "Local development environments", innerW, m.animTick)
		sections = append(sections, banner)
	}

	// Stats line (fixed)
	if m.mode == modeMenu {
		sections = append(sections, m.renderStats(innerW))
		sections = append(sections, "")
	}

	// Scrollable viewport
	sections = append(sections, m.viewport.View())

	// Separator + help footer
	sep := tui.Separator(innerW)
	var helpStr string
	switch m.mode {
	case modeMenu:
		helpStr = m.helpM.View(m.menuKeys)
	case modePickProject:
		helpStr = m.helpM.View(m.pickerKeys)
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

// ---------- Stats ----------

func (m model) renderStats(innerW int) string {
	c := tui.Colors()
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)

	if m.summary.Total == 0 {
		return tui.CenterText(mutedStyle.Render("No projects registered"), innerW)
	}

	parts := []string{
		textStyle.Render(fmt.Sprintf("%d", m.summary.Total)),
		mutedStyle.Render(" projects"),
	}
	if m.summary.Running > 0 {
		parts = append(parts,
			mutedStyle.Render(", "),
			tealStyle.Render(fmt.Sprintf("%d", m.summary.Running)),
			mutedStyle.Render(" running"),
		)
	}

	if len(m.summary.Types) > 1 {
		typeNames := make([]string, 0, len(m.summary.Types))
		for t := range m.summary.Types {
			typeNames = append(typeNames, t)
		}
		sort.Strings(typeNames)
		var typeParts []string
		for _, t := range typeNames {
			typeParts = append(typeParts, fmt.Sprintf("%s: %d", t, m.summary.Types[t]))
		}
		parts = append(parts,
			mutedStyle.Render("  ("+strings.Join(typeParts, ", ")+")"),
		)
	}

	line := strings.Join(parts, "")
	return tui.CenterText(line, innerW)
}

// ---------- Menu rendering ----------

// highlightKey renders a label with the shortcut letter colored and bold.
func highlightKey(label, shortcutKey string, keyStyle, baseStyle lipgloss.Style) string {
	idx := strings.Index(label, shortcutKey)
	if idx < 0 {
		return baseStyle.Render(label)
	}
	var b strings.Builder
	if idx > 0 {
		b.WriteString(baseStyle.Render(label[:idx]))
	}
	b.WriteString(keyStyle.Render(string(label[idx])))
	if idx+1 < len(label) {
		b.WriteString(baseStyle.Render(label[idx+1:]))
	}
	return b.String()
}

func (m model) renderMenu() string {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	boldTextStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)

	helpKeyColor := lipgloss.Color("#FFCC33")
	if !tui.IsDarkBackground() {
		helpKeyColor = lipgloss.Color("#2563EB")
	}
	keyStyle := lipgloss.NewStyle().Foreground(helpKeyColor).Bold(true)

	maxLabel := 0
	for _, cmd := range commands {
		if len(cmd.Label) > maxLabel {
			maxLabel = len(cmd.Label)
		}
	}

	var lines []string
	for i, cmd := range commands {
		selected := i == m.menuCursor

		pointer := "  "
		if selected {
			pointer = tealStyle.Render("▸ ")
		}

		var labelStr string
		if selected {
			labelStr = highlightKey(cmd.Label, cmd.Key, keyStyle, boldTextStyle)
		} else {
			labelStr = highlightKey(cmd.Label, cmd.Key, keyStyle, textStyle)
		}

		labelPad := maxLabel - len(cmd.Label)
		if labelPad < 0 {
			labelPad = 0
		}

		desc := mutedStyle.Render(cmd.Desc)
		row := fmt.Sprintf("%s %s%s  %s", pointer, labelStr, strings.Repeat(" ", labelPad), desc)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

// ---------- Picker rendering ----------

func (m model) renderPickerContent() string {
	c := tui.Colors()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)

	var sections []string
	sections = append(sections, "")
	sections = append(sections, "  "+titleStyle.Render(m.pickerCmd.Label)+" "+mutedStyle.Render("— select a project"))
	sections = append(sections, "")

	maxName := 0
	for _, p := range m.projects {
		n := len(p.Name)
		if p.IsCurrent {
			n += 10
		}
		if n > maxName {
			maxName = n
		}
	}

	for i, p := range m.projects {
		selected := i == m.pickerCursor

		pointer := "  "
		if selected {
			pointer = tealStyle.Render("▸ ")
		}

		dot := mutedStyle.Render("○")
		if p.Status == "Running" {
			dot = tealStyle.Render("●")
		}

		nameStr := textStyle.Render(p.Name)
		nameLen := len(p.Name)
		if selected {
			nameStr = lipgloss.NewStyle().Bold(true).Foreground(c.Text).Render(p.Name)
		}
		if p.IsCurrent {
			nameStr += tealStyle.Render(" (current)")
			nameLen += 10
		}

		namePad := maxName - nameLen + 2
		if namePad < 2 {
			namePad = 2
		}

		typeStr := mutedStyle.Render(p.Type)
		domainStr := tui.RenderURL(p.Domain)

		row := fmt.Sprintf("%s %s %s%s%s  %s", pointer, dot, nameStr, strings.Repeat(" ", namePad), typeStr, domainStr)
		sections = append(sections, row)
	}

	sections = append(sections, "")
	return strings.Join(sections, "\n")
}
