//go:build linux

package router

// sshPortTaken reports whether another process (almost always the host's
// openssh-server) already listens on :22.
func sshPortTaken() bool {
	return chooseSSHPort("22", "2222") != "22"
}
