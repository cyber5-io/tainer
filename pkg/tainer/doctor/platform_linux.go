//go:build linux

package doctor

import (
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// platformDaemonDiag probes the engine socket and explains why it is
// unreachable. ok is false when the socket looks fine.
func platformDaemonDiag(socket string) (detail, hint string, ok bool) {
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err == nil {
		conn.Close()
		return "", "", false
	}
	detail, hint = classifySocketErr(socket, err)
	return detail, hint, true
}

// checkPorts is the Linux 80/443 pre-flight: another listener on an edge
// port is a hard, actionable error. Detection (is anything listening?) is
// separate from attribution (who?), so an unattributable listener is still
// reported.
func (d *run) checkPorts() {
	d.start("ports")
	reported := false
	for _, port := range []string{"80", "443"} {
		listening, name, pid := listenerOwner(port)
		if !listening {
			continue
		}
		r := portConflictResult(port, name, pid, d.engineUp)
		if r.Status != StatusOK {
			d.add(r)
			reported = true
		}
	}
	if !reported {
		d.add(Result{Name: "ports", Status: StatusOK, Detail: "no other process holds host ports 80 / 443"})
	}
}

var lsofLineRe = regexp.MustCompile(`^(\S+)\s+(\d+)\s`)

// listenerOwner detects a listener with `ss -ltnp` (loopback dial when ss is
// unavailable) and attributes it from ss, then `lsof`. name is empty when the
// listener exists but its owner is not visible without root.
func listenerOwner(port string) (listening bool, name string, pid int) {
	if out, err := exec.Command("ss", "-ltnp").Output(); err == nil {
		listening, name, pid = parseSSListener(string(out), port)
	} else if conn, derr := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); derr == nil {
		conn.Close()
		listening = true
	}
	if !listening || name != "" {
		return listening, name, pid
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return true, "", 0
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return true, "", 0
	}
	if m := lsofLineRe.FindStringSubmatch(lines[1]); m != nil {
		pid, _ = strconv.Atoi(m[2])
		return true, m[1], pid
	}
	return true, "", 0
}
