package wizard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/pkg/tainer/validate"
)

// Result holds the user's choices from the wizard.
type Result struct {
	Name      string
	Type      manifest.ProjectType
	TypeLabel string
	Version   string
	Database  manifest.DatabaseType
	Subdomain string
	Cancelled bool
}

type step int

const (
	stepWelcome step = iota
	stepName
	stepType
	stepVersion
	stepDatabase
	stepSubdomain
	stepConfirm
	totalSteps = 7
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
		{manifest.TypeKompozi, "Kompozi"},
	}
)

type model struct {
	step     step
	cwd      string
	dirName  string
	result   Result
	quitting bool

	// text input state
	textInput  string
	textCursor int
	inputError string

	// horizontal choice state
	choices   []string
	choiceIdx int

	// version data (fetched once)
	phpVersions  []string
	nodeVersions []string
	versionMsg   string // offline notice
}

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
	case manifest.TypeNodeJS:
		return []string{"postgres", "mariadb", "none"}
	default:
		return []string{"postgres", "mariadb"}
	}
}

func initialModel(cwd, dirName string) model {
	php, phpMsg := fetchPHPVersions()
	node, nodeMsg := fetchNodeVersions()
	msg := phpMsg
	if msg == "" {
		msg = nodeMsg
	}
	return model{
		step:         stepWelcome,
		cwd:          cwd,
		dirName:      dirName,
		phpVersions:  php,
		nodeVersions: node,
		versionMsg:   msg,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit
	if key == "ctrl+c" {
		m.result.Cancelled = true
		m.quitting = true
		return m, tea.Quit
	}

	switch m.step {
	case stepWelcome:
		return m.handleWelcome(key)
	case stepName:
		return m.handleName(key)
	case stepType:
		return m.handleChoice(key)
	case stepVersion:
		return m.handleChoice(key)
	case stepDatabase:
		return m.handleChoice(key)
	case stepSubdomain:
		return m.handleSubdomain(key)
	case stepConfirm:
		return m.handleConfirm(key)
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
			// Auto-advance if single choice
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
		m.step = stepConfirm
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
		m.quitting = true
		return m, tea.Quit
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
	case stepConfirm:
		m.step = stepSubdomain
		m.textInput = m.result.Subdomain
		m.textCursor = len(m.textInput)
	}
}

func (m model) View() string {
	if m.quitting {
		if m.result.Cancelled {
			return tui.WarningStyle.Render("Cancelled.") + "\n"
		}
		return ""
	}

	var b strings.Builder

	// Step indicator
	if m.step > stepWelcome {
		stepNum := int(m.step)
		indicator := fmt.Sprintf("Step %d/%d", stepNum, totalSteps-1)
		label := stepLabel(m.step)
		b.WriteString(tui.SubtitleStyle.Render(fmt.Sprintf("%s \u2014 %s", indicator, label)))
		b.WriteString("\n\n")
	}

	switch m.step {
	case stepWelcome:
		b.WriteString(m.viewWelcome())
	case stepName:
		b.WriteString(m.viewName())
	case stepType:
		b.WriteString(m.viewHorizontalChoice("Project Type"))
	case stepVersion:
		label := "PHP Version"
		if m.result.Type != manifest.TypeWordPress && m.result.Type != manifest.TypePHP {
			label = "Node Version"
		}
		if m.versionMsg != "" {
			b.WriteString(tui.SubtitleStyle.Render("  "+m.versionMsg) + "\n")
		}
		b.WriteString(m.viewHorizontalChoice(label))
	case stepDatabase:
		b.WriteString(m.viewHorizontalChoice("Database"))
	case stepSubdomain:
		b.WriteString(m.viewSubdomain())
	case stepConfirm:
		b.WriteString(m.viewConfirm())
	}

	return b.String()
}

func (m model) viewWelcome() string {
	banner := tui.Banner("tainer init", "Create a new tainer project")
	return banner + "\n\n" + tui.SubtitleStyle.Render("Press Enter to begin, Esc to cancel") + "\n"
}

func (m model) viewName() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Project Name"))
	b.WriteString("\n")
	b.WriteString(tui.SubtitleStyle.Render(fmt.Sprintf("(default: %s)", m.dirName)))
	b.WriteString("\n\n")

	input := m.textInput
	cursor := tui.SuccessStyle.Render("\u2588")
	b.WriteString("  \u25b8 " + tui.SuccessStyle.Render(input) + cursor)
	b.WriteString("\n")

	if m.inputError != "" {
		b.WriteString("\n")
		b.WriteString("  " + tui.ErrorStyle.Render(m.inputError))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(tui.SubtitleStyle.Render("Enter to confirm, Esc to go back"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewHorizontalChoice(title string) string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render(title))
	b.WriteString("\n\n  ")

	for i, c := range m.choices {
		if i == m.choiceIdx {
			b.WriteString(tui.SelectedStyle.Render("\u25b8 " + c))
		} else {
			b.WriteString(tui.UnselectedStyle.Render("  " + c))
		}
		if i < len(m.choices)-1 {
			b.WriteString("   ")
		}
	}

	b.WriteString("\n\n")
	b.WriteString(tui.SubtitleStyle.Render("\u2190 \u2192 to select, Enter to confirm, Esc to go back"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewSubdomain() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Subdomain"))
	b.WriteString("\n")
	b.WriteString(tui.SubtitleStyle.Render(fmt.Sprintf("(default: %s)", m.result.Name)))
	b.WriteString("\n\n")

	input := m.textInput
	cursor := tui.SuccessStyle.Render("\u2588")
	suffix := tui.SubtitleStyle.Render(".tainer.me")
	b.WriteString("  \u25b8 " + tui.SuccessStyle.Render(input) + cursor + suffix)
	b.WriteString("\n\n")
	b.WriteString(tui.SubtitleStyle.Render("Enter to confirm, Esc to go back"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewConfirm() string {
	var b strings.Builder

	domain := m.result.Subdomain + ".tainer.me"

	summaryLines := []string{
		fmt.Sprintf("  %s  %s", tui.SubtitleStyle.Render("Name:"), tui.SuccessStyle.Render(m.result.Name)),
		fmt.Sprintf("  %s  %s", tui.SubtitleStyle.Render("Type:"), tui.SuccessStyle.Render(m.result.TypeLabel)),
		fmt.Sprintf("  %s  %s", tui.SubtitleStyle.Render("Version:"), tui.SuccessStyle.Render(m.result.Version)),
		fmt.Sprintf("  %s  %s", tui.SubtitleStyle.Render("Database:"), tui.SuccessStyle.Render(string(m.result.Database))),
		fmt.Sprintf("  %s  %s", tui.SubtitleStyle.Render("Domain:"), tui.URLStyle.Render(domain)),
	}

	panel := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorder).
		Padding(1, 2).
		Render(strings.Join(summaryLines, "\n"))

	b.WriteString(tui.TitleStyle.Render("Summary"))
	b.WriteString("\n\n")
	b.WriteString(panel)
	b.WriteString("\n\n")
	b.WriteString(tui.SuccessStyle.Render("Enter to create project") + "  " + tui.SubtitleStyle.Render("Esc to go back"))
	b.WriteString("\n")
	return b.String()
}

func stepLabel(s step) string {
	switch s {
	case stepName:
		return "Project Name"
	case stepType:
		return "Project Type"
	case stepVersion:
		return "Runtime Version"
	case stepDatabase:
		return "Database"
	case stepSubdomain:
		return "Subdomain"
	case stepConfirm:
		return "Confirm"
	default:
		return ""
	}
}

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

// Run launches the TUI wizard and returns the user's selections.
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
