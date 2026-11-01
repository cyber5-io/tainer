//go:build darwin

package router

import "os/exec"

// sshPortTaken reports whether macOS Remote Login (the system sshd) is
// enabled, in which case :22 is taken and we must not fight it.
// `launchctl print system/com.openssh.sshd` exits 0 only when the SSH
// service is bootstrapped in the system domain.
func sshPortTaken() bool {
	return exec.Command("launchctl", "print", "system/com.openssh.sshd").Run() == nil
}
