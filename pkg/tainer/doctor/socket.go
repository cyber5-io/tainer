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

// parseSSListener finds a listener on the given local port in `ss -ltnp`
// output. listening is true whenever the port has a LISTEN line; name and pid
// are set only when ss could attribute it (processes of other users need
// root, so a listening port with an empty name is "owner unknown", not free).
func parseSSListener(out, port string) (listening bool, name string, pid int) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[3], ":"+port) {
			continue
		}
		listening = true
		if m := ssUsersRe.FindStringSubmatch(line); m != nil {
			pid, _ = strconv.Atoi(m[2])
			return true, m[1], pid
		}
	}
	return listening, "", 0
}

// portConflictResult reports a host edge port (80/443) in use by something
// other than tainer's own forwarder. There are deliberately no fallback
// ports: https://proj.tainer.me must work without a port number. An unnamed
// owner is still a finding: with the engine down it is a hard fail; with the
// engine up the listener is most likely our own forwarder (a foreign one
// would have stopped it binding), so it is only a warning.
func portConflictResult(port, owner string, pid int, engineUp bool) Result {
	if owner == "" {
		r := Result{Name: "ports", Status: StatusFail,
			Detail: "host port " + port + " is in use (owner not visible — re-run with sudo to identify it)",
			Hint:   "tainer needs ports 80 and 443 and has no fallback ports — free the port and re-run"}
		if engineUp {
			r.Status = StatusWarn
		}
		return r
	}
	if strings.Contains(owner, "cyberstack") {
		return Result{Name: "ports", Status: StatusOK, Detail: "host port " + port + " held by the tainer engine"}
	}
	return Result{Name: "ports", Status: StatusFail,
		Detail: "host port " + port + " is in use by " + owner + " (pid " + strconv.Itoa(pid) + ")",
		Hint:   "tainer needs ports 80 and 443 — stop " + owner + " (e.g. sudo systemctl stop " + owner + ") and re-run; tainer has no fallback ports"}
}
