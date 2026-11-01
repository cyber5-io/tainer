//go:build windows

package main

// watchResize is a no-op on Windows: the terminal size is sent once at
// attach and live resize events are not propagated yet.
func watchResize(func()) {}
