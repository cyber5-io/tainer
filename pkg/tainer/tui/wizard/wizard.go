package wizard

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/gitsetup"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/pkg/tainer/validate"
	"golang.org/x/term"
)

// tickMsg drives the welcome screen text animation.
type tickMsg time.Time

// versionsFetchedMsg delivers async version fetch results.
type versionsFetchedMsg struct {
	php, node []string
	msg       string
}

// Result holds the user's choices from the wizard.
type Result struct {
	Name       string
	Type       manifest.ProjectType
	TypeLabel  string
	Version    string
	Database   manifest.DatabaseType
	Subdomain  string
	Cancelled  bool
	StartPod   bool
	InitGit    bool // user chose to initialise a git repo
	HasGitRepo bool // git repo already existed
}

type step int

const (
	stepWelcome step = iota
	stepName
	stepType
	stepVersion
	stepDatabase
	stepSubdomain
	stepGit
	stepConfirm
	userSteps = 6 // steps 1-6 shown in progress (git is bonus)
)

var (
	defaultPHPVersions  = []string{"7.4", "8.1", "8.2", "8.3", "8.4", "8.5"}
	defaultNodeVersions = []string{"20", "22", "24"}
	projectTypes        = []struct {
		Type  manifest.ProjectType
		Label string
	}{
		{manifest.TypeWordPress, "WordPress"},
		{manifest.TypePHP, "PHP"},
		{manifest.TypeNodeJS, "Node.js"},
		{manifest.TypeNextJS, "Next.js"},
		{manifest.TypeNuxtJS, "Nuxt.js"},
		{manifest.TypeNestJS, "Nest.js"},
		{manifest.TypeKompozi, "Kompozi"},
	}
)

type model struct {
	step     step
	cwd      string
	dirName  string
	result   Result
	quitting bool
	width    int
	height   int

	// text input
	textInput  string
	textCursor int
	inputError string

	// horizontal choice
	choices   []string
	choiceIdx int

	// version data
	phpVersions  []string
	nodeVersions []string
	versionMsg   string

	// confirm screen
	confirmIdx int // 0 = Create Project, 1 = Start Pod

	// git step
	hasGitRepo bool
	gitIdx     int // 0 = Skip, 1 = Init

	// welcome animation
	animTick int // current tick (drives character reveal)
}

// ---------------------------------------------------------------------------
// Version fetching
// ---------------------------------------------------------------------------

func fetchPHPVersions() ([]string, string) {
	tags, err := registry.FetchTags("phpfpm")
	if err != nil || len(tags) == 0 {
		if local := registry.LocalTags("phpfpm"); len(local) > 0 {
			return local, "(offline \u2014 cached versions)"
		}
		return defaultPHPVersions, ""
	}
	return tags, ""
}

func fetchNodeVersions() ([]string, string) {
	tags, err := registry.FetchTags("node")
	if err != nil || len(tags) == 0 {
		if local := registry.LocalTags("node"); len(local) > 0 {
			return local, "(offline \u2014 cached versions)"
		}
		return defaultNodeVersions, ""
	}
	return tags, ""
}

func defaultDatabase(pt manifest.ProjectType) manifest.DatabaseType {
	switch pt {
	case manifest.TypeWordPress, manifest.TypePHP:
		return manifest.DatabaseMariaDB
	default:
		return manifest.DatabasePostgres
	}
}

func dbChoices(pt manifest.ProjectType) []string {
	switch pt {
	case manifest.TypeWordPress:
		return []string{"mariadb"}
	case manifest.TypePHP:
		return []string{"mariadb", "postgres", "none"}
	case manifest.TypeNodeJS, manifest.TypeNestJS:
		return []string{"postgres", "mariadb", "none"}
	default:
		return []string{"postgres", "mariadb"}
	}
}

// ---------------------------------------------------------------------------
// BubbleTea lifecycle
// ---------------------------------------------------------------------------

func initialModel(cwd, dirName string) model {
	w, h := 80, 24
	if tw, th, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		w, h = tw, th
	}
	return model{
		step:         stepWelcome,
		cwd:          cwd,
		dirName:      dirName,
		phpVersions:  defaultPHPVersions,
		nodeVersions: defaultNodeVersions,
		hasGitRepo:   gitsetup.IsGitRepo(cwd),
		width:        w,
		height:       h,
	}
}

func fetchVersionsCmd() tea.Cmd {
	return func() tea.Msg {
		php, phpMsg := fetchPHPVersions()
		node, nodeMsg := fetchNodeVersions()
		msg := phpMsg
		if msg == "" {
			msg = nodeMsg
		}
		return versionsFetchedMsg{php: php, node: node, msg: msg}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), fetchVersionsCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case versionsFetchedMsg:
		m.phpVersions = msg.php
		m.nodeVersions = msg.node
		m.versionMsg = msg.msg
		return m, nil
	case tickMsg:
		if m.step == stepWelcome {
			m.animTick++
			// Repeat name shimmer every ~900 ticks (45s at 50ms/tick)
			// nameDelay(8) + name(11) + 2 = 21 ticks for first shimmer cycle
			// After full intro (50 ticks), keep ticking for periodic shimmer
			return m, tickCmd()
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Key handling (unchanged logic)
// ---------------------------------------------------------------------------

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.result.Cancelled = true
		m.quitting = true
		return m, tea.Quit
	}
	// Allow "q" to quit on non-text-input steps
	if key == "q" && m.step != stepName && m.step != stepSubdomain {
		m.result.Cancelled = true
		m.quitting = true
		return m, tea.Quit
	}
	switch m.step {
	case stepWelcome:
		return m.handleWelcome(key)
	case stepName:
		return m.handleName(key)
	case stepType, stepVersion, stepDatabase:
		return m.handleChoice(key)
	case stepSubdomain:
		return m.handleSubdomain(key)
	case stepConfirm:
		return m.handleConfirm(key)
	case stepGit:
		return m.handleGit(key)
	}
	return m, nil
}

func (m model) handleWelcome(key string) (tea.Model, tea.Cmd) {
	if key == "enter" || key == "tab" {
		m.step = stepName
		m.textInput = m.dirName
		m.textCursor = len(m.textInput)
		m.inputError = ""
	} else if key == "esc" {
		m.result.Cancelled = true
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleName(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "tab":
		name := m.textInput
		if name == "" {
			name = m.dirName
		}
		if err := validate.ProjectName(name); err != nil {
			m.inputError = err.Error()
			return m, nil
		}
		if existing, ok := registry.Get(name); ok && existing.Path != m.cwd {
			m.inputError = fmt.Sprintf("name %q already registered at %s", name, existing.Path)
			return m, nil
		}
		m.result.Name = name
		m.inputError = ""
		m.step = stepType
		m.choices = make([]string, len(projectTypes))
		for i, pt := range projectTypes {
			m.choices[i] = pt.Label
		}
		m.choiceIdx = 0
	case "esc":
		m.step = stepWelcome
	case "backspace":
		if len(m.textInput) > 0 {
			m.textInput = m.textInput[:len(m.textInput)-1]
			m.textCursor = len(m.textInput)
		}
		m.inputError = ""
	default:
		if len(key) == 1 {
			m.textInput += key
			m.textCursor = len(m.textInput)
			m.inputError = ""
		}
	}
	return m, nil
}

func (m model) handleChoice(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "left", "h":
		if m.choiceIdx > 0 {
			m.choiceIdx--
		}
	case "right", "l":
		if m.choiceIdx < len(m.choices)-1 {
			m.choiceIdx++
		}
	case "enter", "tab":
		switch m.step {
		case stepType:
			m.result.Type = projectTypes[m.choiceIdx].Type
			m.result.TypeLabel = projectTypes[m.choiceIdx].Label
			m.step = stepVersion
			if m.result.Type == manifest.TypeWordPress || m.result.Type == manifest.TypePHP {
				m.choices = m.phpVersions
				m.choiceIdx = findIndex(m.choices, "8.4")
			} else {
				m.choices = m.nodeVersions
				m.choiceIdx = findIndex(m.choices, "22")
			}
		case stepVersion:
			m.result.Version = m.choices[m.choiceIdx]
			m.step = stepDatabase
			dbc := dbChoices(m.result.Type)
			m.choices = dbc
			defDB := string(defaultDatabase(m.result.Type))
			m.choiceIdx = findIndex(m.choices, defDB)
			if len(m.choices) == 1 {
				m.result.Database = manifest.DatabaseType(m.choices[0])
				m.step = stepSubdomain
				m.textInput = m.result.Name
				m.textCursor = len(m.textInput)
				m.inputError = ""
			}
		case stepDatabase:
			m.result.Database = manifest.DatabaseType(m.choices[m.choiceIdx])
			m.step = stepSubdomain
			m.textInput = m.result.Name
			m.textCursor = len(m.textInput)
			m.inputError = ""
		}
	case "esc":
		m.goBack()
	}
	return m, nil
}

func (m model) handleSubdomain(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "tab":
		sub := m.textInput
		if sub == "" {
			sub = m.result.Name
		}
		m.result.Subdomain = sub
		if m.hasGitRepo {
			m.result.HasGitRepo = true
			m.step = stepConfirm
			m.confirmIdx = 1 // default to "Create & Start"
		} else {
			m.step = stepGit
			m.gitIdx = 0 // default to "Skip"
		}
	case "esc":
		m.goBack()
	case "backspace":
		if len(m.textInput) > 0 {
			m.textInput = m.textInput[:len(m.textInput)-1]
			m.textCursor = len(m.textInput)
		}
	default:
		if len(key) == 1 {
			m.textInput += key
			m.textCursor = len(m.textInput)
		}
	}
	return m, nil
}

func (m model) handleConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		m.result.StartPod = m.confirmIdx == 1
		m.quitting = true
		return m, tea.Quit
	case "left", "h":
		if m.confirmIdx > 0 {
			m.confirmIdx--
		}
	case "right", "l":
		if m.confirmIdx < 1 {
			m.confirmIdx++
		}
	case "esc":
		m.goBack()
	}
	return m, nil
}

func (m model) handleGit(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		m.result.InitGit = m.gitIdx == 1
		m.step = stepConfirm
		m.confirmIdx = 1 // default to "Create & Start"
	case "left", "h":
		if m.gitIdx > 0 {
			m.gitIdx--
		}
	case "right", "l":
		if m.gitIdx < 1 {
			m.gitIdx++
		}
	case "esc":
		m.goBack()
	}
	return m, nil
}

func (m *model) goBack() {
	switch m.step {
	case stepName:
		m.step = stepWelcome
	case stepType:
		m.step = stepName
		m.textInput = m.result.Name
		if m.textInput == "" {
			m.textInput = m.dirName
		}
		m.textCursor = len(m.textInput)
		m.inputError = ""
	case stepVersion:
		m.step = stepType
		m.choices = make([]string, len(projectTypes))
		for i, pt := range projectTypes {
			m.choices[i] = pt.Label
		}
		m.choiceIdx = 0
		for i, pt := range projectTypes {
			if pt.Type == m.result.Type {
				m.choiceIdx = i
				break
			}
		}
	case stepDatabase:
		m.step = stepVersion
		if m.result.Type == manifest.TypeWordPress || m.result.Type == manifest.TypePHP {
			m.choices = m.phpVersions
			m.choiceIdx = findIndex(m.choices, m.result.Version)
		} else {
			m.choices = m.nodeVersions
			m.choiceIdx = findIndex(m.choices, m.result.Version)
		}
	case stepSubdomain:
		dbc := dbChoices(m.result.Type)
		if len(dbc) == 1 {
			m.step = stepVersion
			if m.result.Type == manifest.TypeWordPress || m.result.Type == manifest.TypePHP {
				m.choices = m.phpVersions
				m.choiceIdx = findIndex(m.choices, m.result.Version)
			} else {
				m.choices = m.nodeVersions
				m.choiceIdx = findIndex(m.choices, m.result.Version)
			}
		} else {
			m.step = stepDatabase
			m.choices = dbc
			m.choiceIdx = findIndex(m.choices, string(m.result.Database))
		}
	case stepGit:
		m.step = stepSubdomain
		m.textInput = m.result.Subdomain
		m.textCursor = len(m.textInput)
	case stepConfirm:
		if m.hasGitRepo {
			m.step = stepSubdomain
			m.textInput = m.result.Subdomain
			m.textCursor = len(m.textInput)
		} else {
			m.step = stepGit
		}
	}
}

// ---------------------------------------------------------------------------
// View — full-screen framed layout
// ---------------------------------------------------------------------------

func (m model) View() tea.View {
	// After quit, BubbleTea exits alt screen — print to original terminal.
	if m.quitting {
		if m.result.Cancelled {
			return tea.NewView(tui.WarningStyle().Render("Cancelled.") + "\n")
		}
		return tea.NewView("")
	}

	c := tui.Colors()

	// Frame fills nearly all the terminal
	frameW := m.width - 4
	if frameW < 40 {
		frameW = 40
	}
	innerW := frameW - 6 // border (2) + padding left/right (2+2)

	// === Build the three sections ===
	var header string
	var body string

	if m.step == stepWelcome {
		header = tui.Banner("", "Create a new project", innerW, m.animTick)
	} else {
		header = m.renderHeader(innerW)
		body = m.renderBody()
	}

	footer := m.renderFooter(innerW)

	// === Assemble ===
	var sections []string
	sections = append(sections, header)

	if body != "" {
		sections = append(sections, "")
		sections = append(sections, body)
	}

	// Pad content to fill available height
	content := strings.Join(sections, "\n")
	contentH := lipgloss.Height(content)
	// Reserve: border top/bottom (2) + padding (0) + sep (1) + footer (1) + margin (4)
	targetH := m.height - 8
	if targetH < 12 {
		targetH = 12
	}
	if contentH < targetH {
		content += strings.Repeat("\n", targetH-contentH)
	}

	// Separator + footer
	sep := tui.Separator(innerW)
	inner := content + "\n" + sep + "\n" + footer

	// Render framed — no Background so terminal native bg shows through
	frame := lipgloss.NewStyle().
		Width(frameW).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(c.Blue).
		Padding(0, 2).
		Render(inner)

	v := tea.NewView(tui.FullScreen(frame, m.width, m.height))
	v.AltScreen = true
	return v
}

// renderHeader builds a breadcrumb trail (left) with small logo (right).
func (m model) renderHeader(width int) string {
	c := tui.Colors()
	chevron := lipgloss.NewStyle().Foreground(c.OrangeDim).Render(" › ")
	val := lipgloss.NewStyle().Foreground(c.Text)

	var crumbs []string
	if m.result.Name != "" {
		crumbs = append(crumbs, val.Render(m.result.Name))
	}
	if m.result.TypeLabel != "" {
		crumbs = append(crumbs, val.Render(m.result.TypeLabel))
	}
	if m.result.Version != "" {
		crumbs = append(crumbs, val.Render(m.result.Version))
	}
	if m.result.Database != "" {
		crumbs = append(crumbs, val.Render(string(m.result.Database)))
	}
	if m.result.Subdomain != "" {
		crumbs = append(crumbs, val.Render(m.result.Subdomain))
	}

	trail := strings.Join(crumbs, chevron)

	logo := tui.LogoSmall()
	logoW := lipgloss.Width(logo)
	gap := 4
	leftW := width - logoW - gap
	if leftW < 20 {
		leftW = 20
	}
	left := lipgloss.NewStyle().Width(leftW).Render("\n" + trail)
	right := lipgloss.NewStyle().Width(logoW).Render(logo)
	header := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)

	return header + "\n" + tui.Separator(width)
}

// renderBody builds the step-specific content.
func (m model) renderBody() string {
	switch m.step {
	case stepName:
		return m.bodyName()
	case stepType:
		return m.bodyChoice("Project Type")
	case stepVersion:
		label := "PHP Version"
		if m.result.Type != manifest.TypeWordPress && m.result.Type != manifest.TypePHP {
			label = "Node Version"
		}
		var prefix string
		if m.versionMsg != "" {
			prefix = "  " + tui.SubtitleStyle().Render(m.versionMsg) + "\n\n"
		}
		return prefix + m.bodyChoice(label)
	case stepDatabase:
		return m.bodyChoice("Database")
	case stepSubdomain:
		return m.bodySubdomain()
	case stepConfirm:
		return m.bodyConfirm()
	case stepGit:
		return m.bodyGit()
	}
	return ""
}

// renderFooter builds the status bar: nav hints (left) + progress dots (right).
func (m model) renderFooter(width int) string {
	nav := m.navText()
	left := tui.SubtitleStyle().Render(nav)

	if m.step == stepWelcome {
		return left
	}

	dots := tui.ProgressDots(int(m.step), userSteps)
	gap := width - lipgloss.Width(left) - lipgloss.Width(dots)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + dots
}

func (m model) navText() string {
	switch m.step {
	case stepWelcome:
		return "↵ begin   q quit"
	case stepName, stepSubdomain:
		return "↵ confirm   esc back"
	case stepType, stepVersion, stepDatabase:
		return "← → select   ↵ confirm   esc back   q quit"
	case stepGit, stepConfirm:
		return "← → select   ↵ confirm   esc back   q quit"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Step body renderers
// ---------------------------------------------------------------------------

func (m model) bodyName() string {
	var b strings.Builder
	b.WriteString("  " + tui.LabelStyle().Render("Project Name"))
	b.WriteString("\n")
	b.WriteString("  " + tui.SubtitleStyle().Render(fmt.Sprintf("default: %s", m.dirName)))
	b.WriteString("\n\n")

	cursor := tui.SuccessStyle().Render("\u2588")
	b.WriteString("  " + tui.SelectedStyle().Render("\u25b8 ") + tui.SuccessStyle().Render(m.textInput) + cursor)
	b.WriteString("\n")

	if m.inputError != "" {
		b.WriteString("\n  " + tui.ErrorStyle().Render(m.inputError) + "\n")
	}

	return b.String()
}

func (m model) bodyChoice(title string) string {
	var b strings.Builder
	b.WriteString("  " + tui.LabelStyle().Render(title))
	b.WriteString("\n\n  ")

	for i, c := range m.choices {
		if i == m.choiceIdx {
			b.WriteString(tui.SelectedStyle().Render("\u25b8 " + c))
		} else {
			b.WriteString(tui.UnselectedStyle().Render("  " + c))
		}
		if i < len(m.choices)-1 {
			b.WriteString("   ")
		}
	}
	b.WriteString("\n")

	return b.String()
}

func (m model) bodySubdomain() string {
	var b strings.Builder
	b.WriteString("  " + tui.LabelStyle().Render("Subdomain"))
	b.WriteString("\n")
	b.WriteString("  " + tui.SubtitleStyle().Render(fmt.Sprintf("default: %s", m.result.Name)))
	b.WriteString("\n\n")

	cursor := tui.SuccessStyle().Render("\u2588")
	suffix := tui.SubtitleStyle().Render(".tainer.me")
	b.WriteString("  " + tui.SelectedStyle().Render("\u25b8 ") + tui.SuccessStyle().Render(m.textInput) + cursor + suffix)
	b.WriteString("\n")

	return b.String()
}

func (m model) bodyConfirm() string {
	var b strings.Builder

	domain := m.result.Subdomain + ".tainer.me"

	b.WriteString("  " + tui.LabelStyle().Render("Summary"))
	b.WriteString("\n\n")

	rows := []struct {
		label string
		value string
		url   bool
	}{
		{"Name", m.result.Name, false},
		{"Type", m.result.TypeLabel, false},
		{"Version", m.result.Version, false},
		{"Database", string(m.result.Database), false},
		{"Domain", domain, true},
	}

	for _, r := range rows {
		lbl := tui.SubtitleStyle().Render(fmt.Sprintf("  %-10s", r.label))
		var val string
		if r.url {
			val = tui.RenderURL(r.value)
		} else {
			val = tui.SuccessStyle().Render(r.value)
		}
		b.WriteString(lbl + " " + val + "\n")
	}

	// Action buttons
	b.WriteString("\n")
	b.WriteString(renderButtons([]string{"  Create  ", "  Create & Start  "}, m.confirmIdx))
	b.WriteString("\n")

	return b.String()
}

func (m model) bodyGit() string {
	var b strings.Builder

	b.WriteString("  " + tui.LabelStyle().Render("Git Repository"))
	b.WriteString("\n\n")
	b.WriteString("  " + tui.SubtitleStyle().Render("No git repository detected in this directory."))
	b.WriteString("\n")
	b.WriteString("  " + tui.SubtitleStyle().Render("tainer can initialise one with a proper .gitignore."))
	b.WriteString("\n\n")

	b.WriteString(renderButtons([]string{"  Skip  ", "  Init Git  "}, m.gitIdx))
	b.WriteString("\n")

	return b.String()
}

// renderButtons renders a horizontal row of styled buttons following the
// huh/v2 button pattern: both states are solid background blocks with
// consistent padding, differentiated by colour.
func renderButtons(labels []string, selectedIdx int) string {
	c := tui.Colors()

	// Find the widest label so all buttons are the same width.
	maxW := 0
	for _, l := range labels {
		if len(l) > maxW {
			maxW = len(l)
		}
	}

	focused := lipgloss.NewStyle().
		Bold(true).
		Width(maxW + 4). // 2 chars padding each side
		Align(lipgloss.Center).
		Foreground(lipgloss.Color("#0C1018")).
		Background(c.Orange)

	blurred := lipgloss.NewStyle().
		Width(maxW + 4).
		Align(lipgloss.Center).
		Foreground(c.Text).
		Background(c.Border)

	// Build 3 lines manually: blank, label, blank (vertical padding).
	var topParts, midParts, botParts []string
	for i, label := range labels {
		style := blurred
		if i == selectedIdx {
			style = focused
		}
		pad := style.Render(strings.Repeat(" ", maxW))
		topParts = append(topParts, pad)
		midParts = append(midParts, style.Render(label))
		botParts = append(botParts, pad)
	}

	gap := "  "
	return "  " + strings.Join(topParts, gap) + "\n" +
		"  " + strings.Join(midParts, gap) + "\n" +
		"  " + strings.Join(botParts, gap)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findIndex(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	if len(items) > 0 {
		return len(items) - 1
	}
	return 0
}

// Run launches the full-screen TUI wizard and returns the user's selections.
func Run(cwd, dirName string) (*Result, error) {
	m := initialModel(cwd, dirName)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running wizard TUI: %w", err)
	}
	result := finalModel.(model).result
	return &result, nil
}
