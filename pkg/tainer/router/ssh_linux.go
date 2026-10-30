//go:build linux

package router

// sshPortTaken reports whether another process (almost always the host's
// openssh-server) already listens on :22. A router container of ours that
// already publishes :22 settles it first; the probe only decides when no
// router exists.
func sshPortTaken() bool {
	if routerPublishes22() {
		return false
	}
	return chooseSSHPort("22", "2222") != "22"
}
