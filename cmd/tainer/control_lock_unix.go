//go:build !windows

package main

import "syscall"

// detachedProcAttr puts the child in its own session so it outlives the
// command (and its terminal) that launched it.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
