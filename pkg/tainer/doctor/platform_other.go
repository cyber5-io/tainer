//go:build !linux

package doctor

func platformDaemonDiag(socket string) (detail, hint string, ok bool) {
	return "", "", false
}

func (d *run) checkPorts() {}
