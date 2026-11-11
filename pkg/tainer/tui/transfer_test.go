package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 743: "743 B", 12595: "12.3 KB", 96468992: "92.0 MB", 1288490189: "1.2 GB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTransferLineAtZeroHalfAndFull(t *testing.T) {
	const total = 96468992 // 92 MB
	cases := []struct {
		done int64
		want string
	}{
		{0, "[=] downloading tainer 1.5.6  ────────────────────────   0%  0 B / 92.0 MB"},
		{total / 2, "[=] downloading tainer 1.5.6  ━━━━━━━━━━━━────────────  50%  46.0 MB / 92.0 MB"},
		{total, "[=] downloading tainer 1.5.6  ━━━━━━━━━━━━━━━━━━━━━━━━ 100%  92.0 MB / 92.0 MB"},
	}
	for _, c := range cases {
		if got := TransferLine("[=]", "downloading tainer 1.5.6", c.done, total); got != c.want {
			t.Errorf("done=%d\n got %q\nwant %q", c.done, got, c.want)
		}
	}
}

func TestTransferLineWithoutAKnownSize(t *testing.T) {
	if got := TransferLine("[=]", "downloading tainer 1.5.6", 0, -1); got != "[=] downloading tainer 1.5.6" {
		t.Errorf("got %q", got)
	}
	if got := TransferLine("[=]", "downloading tainer 1.5.6", 2048, -1); got != "[=] downloading tainer 1.5.6  2.0 KB" {
		t.Errorf("got %q", got)
	}
	// Overshoot (a wrong Content-Length) never draws past the bar.
	if got := TransferLine("[=]", "x", 150, 100); !strings.Contains(got, "100%") || strings.Contains(got, "─") {
		t.Errorf("got %q", got)
	}
}

func TestTransferRedrawsInPlaceAndFinishes(t *testing.T) {
	var buf bytes.Buffer
	tr := newTransfer(&buf, "downloading tainer 1.5.6", 0)
	tr.Update(0, 100)
	tr.draw()
	tr.Update(50, 100)
	tr.draw()
	tr.Update(100, 100)
	tr.draw()
	tr.Finish("downloaded tainer 1.5.6")
	tr.Finish("ignored: finishing twice is a no-op")
	out := buf.String()
	for _, want := range []string{"\r\x1b[2K", "  0%", " 50%", "100%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if !strings.HasSuffix(out, "\r\x1b[2K"+MarkSuccess()+"downloaded tainer 1.5.6\n") {
		t.Errorf("final line: %q", out)
	}
	if strings.Contains(out, "ignored") {
		t.Errorf("second Finish wrote: %q", out)
	}
}

func TestTransferStopClearsWithoutAFinalLine(t *testing.T) {
	var buf bytes.Buffer
	tr := newTransfer(&buf, "verifying…", 0)
	tr.draw()
	tr.Stop()
	if !strings.HasSuffix(buf.String(), "\r\x1b[2K") {
		t.Errorf("got %q", buf.String())
	}
}

func TestTransferAnimatesOnItsOwn(t *testing.T) {
	var buf bytes.Buffer
	tr := newTransfer(&buf, "downloading", time.Millisecond)
	tr.Update(50, 100)
	time.Sleep(30 * time.Millisecond)
	tr.Finish("downloaded")
	out := buf.String()
	if strings.Count(out, "\r\x1b[2K") < 3 || !strings.Contains(out, " 50%") {
		t.Errorf("expected several redraws at 50%%: %q", out)
	}
}

// The live line must stay on one physical row: a wrapped line leaves a stale
// row behind on every redraw, because the redraw clears only one.
func TestFitTransferLineFitsNarrowTerminals(t *testing.T) {
	const total = 96468992
	frame := BrandFrames()[1]
	for _, label := range []string{"downloading tainer 1.5.6", "downloading the menu app 1.5.6", "verifying…"} {
		for _, width := range []int{80, 60, 40, 24} {
			for _, done := range []int64{0, total / 2, total} {
				line := FitTransferLine(frame, label, done, total, width)
				if w := lipgloss.Width(line); w > width-1 {
					t.Errorf("%q at width %d: %d cells: %q", label, width, w, line)
				}
				pct := fmt.Sprintf("%d%%", done*100/total)
				if !strings.Contains(line, pct) {
					t.Errorf("%q at width %d lost the percentage %s: %q", label, width, pct, line)
				}
			}
		}
	}
}

func TestFitTransferLineDegradesInOrder(t *testing.T) {
	const total = 96468992
	label := "downloading the menu app 1.5.6"
	full := TransferLine("[=]", label, total/2, total)
	if got := FitTransferLine("[=]", label, total/2, total, 200); got != full {
		t.Errorf("wide terminal should get the full line: %q", got)
	}
	// 80 columns: the sizes go first, the bar stays.
	at80 := FitTransferLine("[=]", label, total/2, total, 80)
	if strings.Contains(at80, "MB") || !strings.Contains(at80, "━") || !strings.Contains(at80, label) {
		t.Errorf("80 cols: %q", at80)
	}
	// 40 columns: no bar, the label shortened, the percentage kept.
	at40 := FitTransferLine("[=]", label, total/2, total, 40)
	if strings.Contains(at40, "━") || !strings.Contains(at40, "50%") {
		t.Errorf("40 cols: %q", at40)
	}
	// Unknown size: the byte count goes before the label is cut.
	if got := FitTransferLine("[=]", label, 2048, -1, 36); lipgloss.Width(got) > 35 || strings.Contains(got, "KB") {
		t.Errorf("unknown size at 36: %q", got)
	}
}

func TestTransferDrawsWithinTheTerminalWidth(t *testing.T) {
	var buf bytes.Buffer
	tr := newTransfer(&buf, "downloading the menu app 1.5.6", 0)
	tr.width = func() int { return 60 }
	tr.Update(50, 100)
	tr.Stop()
	for _, row := range strings.Split(buf.String(), "\r\x1b[2K") {
		if lipgloss.Width(row) > 59 {
			t.Errorf("row too wide: %q", row)
		}
	}
	if !strings.Contains(buf.String(), "50%") {
		t.Errorf("no percentage: %q", buf.String())
	}
}
