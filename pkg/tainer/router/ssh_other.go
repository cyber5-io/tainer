//go:build !darwin && !linux

package router

func sshPortTaken() bool {
	return false
}
