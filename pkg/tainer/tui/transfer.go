package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const transferBarWidth = 24

// HumanBytes is a short, glanceable size: "743 B", "12.3 KB", "92.0 MB",
// "1.2 GB", in 1024-based units like `ls -lh`.
func HumanBytes(n int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// TransferLine renders one frame of a transfer: the spinner frame, the
// label, and, when the total is known, a bar, the percentage and the
// sizes. Without a total it shows the bytes so far (none before the first).
func TransferLine(frame, label string, done, total int64) string {
	return transferLine(frame, label, done, total, transferBarWidth, true)
}

// FitTransferLine is TransferLine kept to one row of a terminal width
// columns wide (one column is left free so the cursor never wraps). It
// gives things up in order: the sizes, half the bar, the bar, then the end
// of the label. The spinner frame and the percentage always stay.
func FitTransferLine(frame, label string, done, total int64, width int) string {
	budget := width - 1
	for _, v := range []struct {
		bar   int
		sizes bool
	}{{transferBarWidth, true}, {transferBarWidth, false}, {transferBarWidth / 2, false}, {0, false}} {
		if line := transferLine(frame, label, done, total, v.bar, v.sizes); lipgloss.Width(line) <= budget {
			return line
		}
	}
	// Even frame, label and percentage are too wide: shorten the label.
	tail := lipgloss.Width(transferLine(frame, "", done, total, 0, false))
	room := budget - tail
	if room < 2 {
		return ansi.Truncate(transferLine(frame, "", done, total, 0, false), max(budget, 0), "")
	}
	return transferLine(frame, ansi.Truncate(label, room, "…"), done, total, 0, false)
}

func transferLine(frame, label string, done, total int64, barWidth int, sizes bool) string {
	line := frame + " " + label
	if total <= 0 {
		if done > 0 && sizes {
			line += "  " + HumanBytes(done)
		}
		return line
	}
	if done > total {
		done = total
	}
	if barWidth > 0 {
		filled := int(done * int64(barWidth) / total)
		line += "  " + strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	} else {
		line += " "
	}
	line += fmt.Sprintf(" %3d%%", int(done*100/total))
	if sizes {
		line += fmt.Sprintf("  %s / %s", HumanBytes(done), HumanBytes(total))
	}
	return line
}

// Transfer is a live, single-line progress display: the brand spinner
// animates from the moment it starts (no grace delay, so a long
// operation is never silent), and the line redraws in place with
// carriage returns. Use only on a terminal.
type Transfer struct {
	w     io.Writer
	label string
	// width is the terminal's width, read on every redraw.
	width func() int

	mu          sync.Mutex
	done, total int64
	frame       int
	finished    bool

	stop    chan struct{}
	stopped chan struct{}
}

// StartTransfer starts a live transfer line on w.
func StartTransfer(w io.Writer, label string) *Transfer {
	return newTransfer(w, label, 125*time.Millisecond)
}

// newTransfer with tick 0 never redraws by itself (tests call draw).
func newTransfer(w io.Writer, label string, tick time.Duration) *Transfer {
	t := &Transfer{w: w, label: label, total: -1, width: func() int { return GetTermInfo().Width },
		stop: make(chan struct{}), stopped: make(chan struct{})}
	if tick <= 0 {
		close(t.stopped)
		return t
	}
	// The first frame is drawn before returning, so the line shows at
	// once even if the step finishes before the first tick.
	t.draw()
	go func() {
		defer close(t.stopped)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
			}
			t.draw()
		}
	}()
	return t
}

// Update records the bytes so far and the total (-1 when unknown). The
// line redraws at once whenever the whole percentage changes, so the bar
// keeps up with the download between spinner ticks.
func (t *Transfer) Update(done, total int64) {
	t.mu.Lock()
	before := percent(t.done, t.total)
	t.done, t.total = done, total
	changed := total > 0 && percent(done, total) != before
	t.mu.Unlock()
	if changed {
		t.draw()
	}
}

func percent(done, total int64) int64 {
	if total <= 0 {
		return -1
	}
	return min(done, total) * 100 / total
}

func (t *Transfer) draw() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	frames := BrandFrames()
	fmt.Fprint(t.w, "\r\x1b[2K"+FitTransferLine(frames[t.frame%len(frames)], t.label, t.done, t.total, t.width()))
	t.frame++
}

// Finish stops the animation and replaces the line with a success line.
func (t *Transfer) Finish(final string) { t.end(MarkSuccess() + final + "\n") }

// Stop stops the animation and clears the line, leaving nothing behind
// (for failures, which the caller reports itself).
func (t *Transfer) Stop() { t.end("") }

func (t *Transfer) end(final string) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished = true
	t.mu.Unlock()
	select {
	case <-t.stop:
	default:
		close(t.stop)
	}
	<-t.stopped
	fmt.Fprint(t.w, "\r\x1b[2K"+final)
}
