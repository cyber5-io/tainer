//go:build !windows

package runtime

import "syscall"

// ProcessAlive reports whether pid refers to a live process. Signal 0 is a
// no-op probe on Unix.
func ProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// SignalTerm asks pid to shut down gracefully.
func SignalTerm(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// SignalKill terminates pid immediately.
func SignalKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

// detachAttr puts a spawned daemon in its own session, with no controlling
// terminal.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// canSpawnDaemon reports whether this OS can host the tainer engine.
func canSpawnDaemon() error {
	return nil
}
