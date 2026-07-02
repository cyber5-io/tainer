package tui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// StartLineSpinner shows the brand spinner + label on the current
// terminal line — but only after a short grace delay, so operations
// that finish quickly never flash a spinner at all. The returned stop
// function erases the spinner line (if it ever appeared) and blocks
// until the line is clean, so the caller can immediately print the
// final result in its place.
//
//	stop := tui.StartLineSpinner("dns")
//	res := slowCheck()
//	stop()
//	fmt.Println(renderResult(res))
//
// Unlike RunSpinner this doesn't spin up a bubbletea program — it's a
// plain goroutine with carriage-return redraws, cheap enough to wrap
// around every step of a multi-step command. No-op when stdout isn't
// a terminal.
func StartLineSpinner(label string) (stop func()) {
	if !IsTTY(os.Stdout.Fd()) {
		return func() {}
	}
	done := make(chan struct{})
	cleaned := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(cleaned)

		// Grace period: fast operations come and go without any
		// spinner. 200ms is above human "instant" perception but well
		// below "is it stuck?".
		grace := time.NewTimer(200 * time.Millisecond)
		defer grace.Stop()
		select {
		case <-done:
			return
		case <-grace.C:
		}

		frames := BrandFrames()
		tick := time.NewTicker(125 * time.Millisecond)
		defer tick.Stop()
		i := 0
		for {
			fmt.Fprintf(os.Stdout, "\r  %s %s", frames[i%len(frames)], label)
			i++
			select {
			case <-done:
				// \r + EL2 wipes the spinner so the caller's result
				// line prints onto a clean row.
				fmt.Fprint(os.Stdout, "\r\x1b[2K")
				return
			case <-tick.C:
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
		<-cleaned
	}
}
