//go:build windows

package runtime

import "syscall"

// ProcessAlive always returns false on Windows. This is the one deliberate
// exception to the "stubs must error" rule: there is no cyberstackd on
// Windows yet, so "not running" is the truth. Revisit when a Windows engine
// exists.
func ProcessAlive(pid int) bool {
	return false
}

// SignalTerm is not supported on Windows yet.
func SignalTerm(pid int) error {
	return ErrUnsupportedOS
}

// SignalKill is not supported on Windows yet.
func SignalKill(pid int) error {
	return ErrUnsupportedOS
}

func detachAttr() *syscall.SysProcAttr {
	return nil
}

// canSpawnDaemon reports whether this OS can host the tainer engine.
func canSpawnDaemon() error {
	return ErrUnsupportedOS
}
