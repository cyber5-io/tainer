package main

import (
	"fmt"
	"io"
	"os"

	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// downloadProgress is what a self-update shows between the y/N and the
// Installer window: the download, then the package check.
type downloadProgress interface {
	start(label string)
	update(done, total int64)
	verifying()
	verified()
	// opening comes once everything before the Installer has succeeded.
	opening()
	// stop clears anything still animating; safe to call more than once.
	stop()
}

// newDownloadProgress picks the display for an output mode: a live bar on
// a terminal in normal output, one line per step in plain output (or when
// out isn't a terminal), nothing with --json.
func newDownloadProgress(mode tui.OutputMode, out io.Writer, tty bool) downloadProgress {
	switch {
	case mode == tui.ModeJSON:
		return noProgress{}
	case mode == tui.ModeStyled && tty:
		return &liveProgress{out: out}
	default:
		return plainProgress{out: out}
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && tui.IsTTY(f.Fd())
}

type noProgress struct{}

func (noProgress) start(string)        {}
func (noProgress) update(int64, int64) {}
func (noProgress) verifying()          {}
func (noProgress) verified()           {}
func (noProgress) opening()            {}
func (noProgress) stop()               {}

type plainProgress struct{ out io.Writer }

func (p plainProgress) start(label string) { fmt.Fprintf(p.out, "downloading %s…\n", label) }
func (plainProgress) update(int64, int64)  {}
func (p plainProgress) verifying()         { fmt.Fprintln(p.out, "verifying…") }
func (plainProgress) verified()            {}
func (p plainProgress) opening()           { fmt.Fprintln(p.out, "opening the installer…") }
func (plainProgress) stop()                {}

// liveProgress animates tainer's spinner from the first moment, draws a
// bar once the size is known, and leaves one line per finished step.
type liveProgress struct {
	out         io.Writer
	label       string
	done, total int64
	line        *tui.Transfer
}

func (p *liveProgress) start(label string) {
	p.label = label
	p.line = tui.StartTransfer(p.out, "downloading "+label)
}

func (p *liveProgress) update(done, total int64) {
	p.done, p.total = done, total
	if p.line != nil {
		p.line.Update(done, total)
	}
}

func (p *liveProgress) verifying() {
	if p.line != nil {
		p.line.Finish(fmt.Sprintf("downloaded %s (%s)", p.label, tui.HumanBytes(p.done)))
	}
	p.line = tui.StartTransfer(p.out, "verifying…")
}

func (p *liveProgress) verified() {
	if p.line != nil {
		p.line.Finish("verified")
		p.line = nil
	}
}

func (p *liveProgress) opening() { fmt.Fprintln(p.out, tui.MarkInfo()+"opening the installer…") }

func (p *liveProgress) stop() {
	if p.line != nil {
		p.line.Stop()
		p.line = nil
	}
}
