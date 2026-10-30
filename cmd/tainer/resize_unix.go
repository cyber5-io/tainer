//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize invokes doResize on every SIGWINCH.
func watchResize(doResize func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() { // leaks on exit by design — process lifetime
		for range ch {
			doResize()
		}
	}()
}
