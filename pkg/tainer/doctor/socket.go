package doctor

import (
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// classifySocketErr turns a failure to reach the engine socket into a
// detail line plus a distinct hint: not installed / not started / no
// permission. Pure so it is testable on any OS.
func classifySocketErr(socket string, err error) (detail, hint string) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "engine socket missing: " + socket,
			"is the tainer package installed? (sudo apt install ./tainer.deb) — then make sure the socket unit is enabled: sudo systemctl enable --now cyberstack.socket"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "engine socket exists but nothing is answering: " + socket,
			"the engine service may have failed — check `systemctl status cyberstack.service` and `journalctl -u cyberstack -e`"
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EACCES):
		return "permission denied on the engine socket: " + socket,
			"you need to be in the 'cyberstack' group: sudo usermod -aG cyberstack $USER, then log out and back in (check with: groups | grep cyberstack)"
	}
	return "cannot reach the engine socket " + socket + ": " + err.Error(),
		"check `systemctl status cyberstack.socket` and `groups | grep cyberstack`"
}

var ssUsersRe = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// parseSSOwner finds the first listener on the given local port in `ss -ltnp`
// output and returns the owning process. ok is false when the port is absent
// or its owner is not visible (processes of other users need root).
func parseSSOwner(out, port string) (name string, pid int, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[3], ":"+port) {
			continue
		}
		m := ssUsersRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pid, _ = strconv.Atoi(m[2])
		return m[1], pid, true
	}
	return "", 0, false
}

// portConflictResult reports a host edge port (80/443) held by a process
// other than tainer's own forwarder. There are deliberately no fallback
// ports: https://proj.tainer.me must work without a port number.
func portConflictResult(port, owner string, pid int) Result {
	if strings.Contains(owner, "cyberstack") {
		return Result{Name: "ports", Status: StatusOK, Detail: "host port " + port + " held by the tainer engine"}
	}
	return Result{Name: "ports", Status: StatusFail,
		Detail: "host port " + port + " is in use by " + owner + " (pid " + strconv.Itoa(pid) + ")",
		Hint:   "tainer needs ports 80 and 443 — stop " + owner + " (e.g. sudo systemctl stop " + owner + ") and re-run; tainer has no fallback ports"}
}
