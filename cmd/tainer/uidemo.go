package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// cmdUIDemo is a hidden command (not in usage()) that renders the brand
// surface — status marks, bookends, spinner, sample command shapes — so
// we can iterate on the look without building real flows. Run with:
//
//	tainer ui-demo
//
// Auto-detects color support so output stays sensible when piped.
func cmdUIDemo(args []string) {
	tui.SetVersion(version)

	// --- 1. Marks ---
	tui.Bookend("ui-demo", "marks")
	fmt.Println("  " + tui.MarkBrand() + "brand identity — the `[=]` triad")
	fmt.Println("  " + tui.MarkSuccess() + "success — the [✓] in tainer green")
	fmt.Println("  " + tui.MarkInfo() + "info — the [i] in tainer blue")
	fmt.Println("  " + tui.MarkWarn() + "warning — the [!] in tainer orange")
	fmt.Println("  " + tui.MarkError() + "failure — the [✗] in red (outside the brand triad)")
	fmt.Println()

	// --- 2. A sample `tainer start` shape ---
	tui.Bookend("ui-demo", "start shape")
	fmt.Println("Starting pod")
	fmt.Println("  " + tui.MarkSuccess() + "cyberstackd alive")
	fmt.Println("  " + tui.MarkSuccess() + "vfkit booted in 1.2s")
	fmt.Println("  " + tui.MarkInfo() + "reconciling 5 containers")
	fmt.Println("  " + tui.MarkSuccess() + "caddy:443 forwarded")
	fmt.Println("  " + tui.MarkSuccess() + "caddy:80 forwarded")
	tui.BookendClose(4600*time.Millisecond, "Ready", "https://opp.tainer.me")
	fmt.Println()

	// --- 3. A sample error shape ---
	tui.Bookend("ui-demo", "error shape")
	fmt.Println("  " + tui.MarkSuccess() + "cyberstackd alive")
	fmt.Println("  " + tui.MarkError() + "vfkit failed to start")
	fmt.Println()
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
	fmt.Println("    " + muted.Render(`time="..." level=fatal msg="vfkit: exit code 1"`))
	fmt.Println()
	fmt.Println("  " + tui.MarkInfo() + "Looks like vfkit isn't installed. Try:")
	fmt.Println()
	code := lipgloss.NewStyle().Foreground(tui.Colors().Orange)
	fmt.Println("    " + code.Render("tainer doctor") + "      check what's missing")
	fmt.Println("    " + code.Render("open /opt/tainer") + "   reinstall the cyberstack pkg")
	tui.BookendCloseError(2400*time.Millisecond, "Aborted")
	fmt.Println()

	// --- 4. Prompt example ---
	tui.Bookend("ui-demo", "interactive prompts")
	fmt.Println("  " + tui.PromptMark() + "What kind of project?")
	fmt.Println("  " + tui.PromptMark() + "Project name?")
	fmt.Println("  " + tui.PromptMark() + "Use existing git repo?")
	fmt.Println()

	// --- 5. Static spinner frames (visualisation) ---
	tui.Bookend("ui-demo", "spinner frames")
	frames := tui.BrandFrames()
	fmt.Print("  ")
	for i, f := range frames {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(f)
	}
	fmt.Println()
	fmt.Println()

	// --- 6. Live spinner animation (skipped when piped) ---
	if tui.WantsTUI() {
		tui.Bookend("ui-demo", "live spinner")
		fmt.Println("  Spinning for 3 seconds, then snap to success...")
		fmt.Println()
		runLiveSpinner(3 * time.Second)
		fmt.Println("  " + tui.MarkSuccess() + "Done")
		fmt.Println()
	}

	// --- 7. Detection summary ---
	tui.Bookend("ui-demo", "detection")
	fmt.Printf("  IsInteractive=%v  IsPiped=%v  WantsColor=%v  WantsTUI=%v\n",
		tui.IsInteractive(), tui.IsPiped(), tui.WantsColor(), tui.WantsTUI())
	fmt.Println()

	tui.BookendClose(0, "end of demo")
	_ = args
	_ = strings.Builder{}
}

// runLiveSpinner runs the brand spinner for `duration`, then exits.
// A tiny bubbletea program — keeps the demo self-contained.
type spinnerDemoModel struct {
	sp       spinner.Model
	deadline time.Time
}

func (m spinnerDemoModel) Init() tea.Cmd { return m.sp.Tick }

func (m spinnerDemoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if time.Now().After(m.deadline) {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	_ = msg
	return m, nil
}

func (m spinnerDemoModel) View() tea.View {
	return tea.NewView("  " + m.sp.View() + " working...\n")
}

func runLiveSpinner(d time.Duration) {
	m := spinnerDemoModel{sp: tui.BrandSpinner(), deadline: time.Now().Add(d)}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("  (spinner demo skipped:", err, ")")
	}
}
