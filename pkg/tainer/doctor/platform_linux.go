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
// port is a hard, actionable error.
func (d *run) checkPorts() {
	d.start("ports")
	reported := false
	for _, port := range []string{"80", "443"} {
		name, pid, ok := listenerOwner(port)
		if !ok {
			continue
		}
		r := portConflictResult(port, name, pid)
		if r.Status == StatusFail {
			d.add(r)
			reported = true
		}
	}
	if !reported {
		d.add(Result{Name: "ports", Status: StatusOK, Detail: "no other process holds host ports 80 / 443"})
	}
}

var lsofLineRe = regexp.MustCompile(`^(\S+)\s+(\d+)\s`)

// listenerOwner finds the process listening on a port: `ss -ltnp` first,
// `lsof` as fallback. Best effort — other users' processes are invisible
// without root.
func listenerOwner(port string) (string, int, bool) {
	if out, err := exec.Command("ss", "-ltnp").Output(); err == nil {
		if name, pid, ok := parseSSOwner(string(out), port); ok {
			return name, pid, true
		}
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return "", 0, false
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return "", 0, false
	}
	m := lsofLineRe.FindStringSubmatch(lines[1])
	if m == nil {
		return "", 0, false
	}
	pid, _ := strconv.Atoi(m[2])
	return m[1], pid, true
}
