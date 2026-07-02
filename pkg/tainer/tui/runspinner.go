package tui

import (
	"fmt"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// RunSpinner runs fn in a goroutine while displaying the tainer brand
// spinner on a single line with `label` to its right. When fn returns
// the spinner snaps to either [✓] (fn returned nil) or [✗] (fn returned
// an error). The bubbletea program quits at that point, leaving the
// final rendered line in the terminal's scrollback so output that comes
// next sits cleanly underneath it.
//
// Inline only — no alt-screen — so this can be used inside any styled-
// text command without taking over the terminal. Designed for opaque
// long-running calls (image pulls, VM boot, pod start/stop) where we
// have no inner progress visibility.
//
// Returns the error fn produced, or any bubbletea program error.
//
//	err := tui.RunSpinner("starting pod", func() error {
//	    var ferr error
//	    res, ferr = pod.Start(ctx, eng, opts)
//	    return ferr
//	})
//	if err != nil { ... }
//	// use res
//
// fn runs in a goroutine but RunSpinner blocks until it finishes, so the
// caller's stack frame stays valid for the closure.
func RunSpinner(label string, fn func() error) error {
	ch := make(chan error, 1)
	go func() { ch <- fn() }()

	m := runSpinnerModel{sp: BrandSpinner(), label: label}
	p := tea.NewProgram(m)

	// p.Send is goroutine-safe — wire fn's result into the model's
	// Update via a custom message. Model handles tea.Quit itself.
	go func() {
		err := <-ch
		p.Send(runSpinnerDoneMsg{err: err})
	}()

	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("running spinner: %w", err)
	}
	return final.(runSpinnerModel).err
}

// runSpinnerDoneMsg is the internal "fn finished" signal that runSpinnerModel
// listens for to record the final state and quit.
type runSpinnerDoneMsg struct {
	err error
}

// runSpinnerModel is a single-line bubbletea model — brand spinner with
// a label, snaps to [✓]/[✗] on done. Held unexported so callers can't
// instantiate it raw.
type runSpinnerModel struct {
	sp    spinner.Model
	label string
	done  bool
	err   error
}

func (m runSpinnerModel) Init() tea.Cmd { return m.sp.Tick }

func (m runSpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.done {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	case runSpinnerDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	}
	return m, nil
}

func (m runSpinnerModel) View() tea.View {
	if m.done {
		mark := MarkSuccess()
		if m.err != nil {
			mark = MarkError()
		}
		return tea.NewView("  " + mark + m.label + "\n")
	}
	return tea.NewView("  " + m.sp.View() + " " + m.label + "\n")
}
