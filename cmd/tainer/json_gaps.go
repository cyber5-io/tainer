package main

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cyber5-io/tainer/pkg/tainer/doctor"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// Formatting layer for the --json output of logs, doctor, config and exec.
// Everything here is a pure function of its inputs (or a writer-backed
// emitter) so the wire shapes are unit-testable without a daemon.

// ---- logs ------------------------------------------------------------------

type logJSONLine struct {
	Pod  string `json:"pod"`
	Part string `json:"part"`
	TS   string `json:"ts"`
	Line string `json:"line"`
}

// logLineTS returns the RFC 3339 millisecond form of a leading timestamp on
// line, or "" when the line does not start with one.
func logLineTS(line string) string {
	tok := line
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		tok = line[:i]
	}
	t, err := time.Parse(time.RFC3339Nano, tok)
	if err != nil {
		return ""
	}
	return jsonTime(t)
}

// streamLogLines reads r line by line and writes one NDJSON record per line to
// w. Lines are passed through raw; blank lines are preserved.
func streamLogLines(r io.Reader, w io.Writer, pod, part string) error {
	br := bufio.NewReader(r)
	for {
		raw, err := br.ReadString('\n')
		if raw != "" {
			line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
			if werr := jsonLine(w, logJSONLine{Pod: pod, Part: part, TS: logLineTS(line), Line: line}); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// lockedWriter serialises whole-record writes from concurrent log streams.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// ---- doctor ----------------------------------------------------------------

type doctorJSONCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint"`
	Fixed  bool   `json:"fixed,omitempty"`
}

// doctorJSONStatus maps the internal status onto the public ok|warn|fail
// vocabulary. A skipped check means an upstream layer failed (Healthy() already
// counts it as unhealthy), so it is reported as fail.
func doctorJSONStatus(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "ok"
	case doctor.StatusWarn:
		return "warn"
	default:
		return "fail"
	}
}

// emitDoctorJSON writes the doctor report document. fix is true when the run
// was started with --fix; only then is the top-level "fixed" array present.
func emitDoctorJSON(rep doctor.Report, fix bool) {
	checks := make([]doctorJSONCheck, 0, len(rep.Results))
	fixed := []string{}
	for _, r := range rep.Results {
		checks = append(checks, doctorJSONCheck{
			Name: r.Name, Status: doctorJSONStatus(r.Status),
			Detail: r.Detail, Hint: r.Hint, Fixed: r.Fixed,
		})
		if r.Fixed {
			fixed = append(fixed, r.Name)
		}
	}
	out := struct {
		Checks      []doctorJSONCheck `json:"checks"`
		Fixed       *[]string         `json:"fixed,omitempty"`
		StoppedPods int               `json:"stoppedPods,omitempty"`
		Healthy     bool              `json:"healthy"`
	}{Checks: checks, StoppedPods: rep.StoppedPods, Healthy: rep.Healthy()}
	if fix {
		out.Fixed = &fixed
	}
	jsonDoc(out)
}

// doctorExitCode is the exit status for a completed doctor run. Doctor is a
// report command: a run that completes prints its document (never the error
// envelope) and exits 1 when any check failed, so scripts can gate on it. The
// error envelope is reserved for runs that could not execute at all.
func doctorExitCode(rep doctor.Report) int {
	if rep.Healthy() {
		return 0
	}
	return 1
}

// ---- config ----------------------------------------------------------------

// emitConfigJSON writes {"v":1,"project":{<manifest fields>}} using the
// manifest's own (yaml) field names, so the keys match tainer.yaml.
func emitConfigJSON(m *manifest.Manifest) error {
	raw, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	var fields map[string]any
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		return err
	}
	jsonDoc(struct {
		Project map[string]any `json:"project"`
	}{fields})
	return nil
}

// ---- exec ------------------------------------------------------------------

// execJSONRefusal reports why exec cannot run under --json: the stream is
// non-interactive by contract, so any TTY or stdin-attach request is refused.
// An empty code means the invocation is allowed.
func execJSONRefusal(p execParsedArgs) (code, message string) {
	if (p.ttyExplicit && p.ttyForced) || p.interactive {
		return "exec-tty-unsupported", "exec --json is non-interactive: drop --tty / --interactive"
	}
	return "", ""
}

// splitExecOutputFlags strips the output flags (--json etc.) from the part of
// args before the "--" sentinel; everything after belongs to the inner command.
func splitExecOutputFlags(args []string) (jsonOut bool, rest []string) {
	pre, post := args, []string(nil)
	for i, a := range args {
		if a == "--" {
			pre, post = args[:i], args[i:]
			break
		}
	}
	for _, a := range pre {
		if a == "--json" {
			jsonOut = true
			continue
		}
		rest = append(rest, a)
	}
	return jsonOut, append(rest, post...)
}

type execJSONLine struct {
	Stream string `json:"stream"`
	Data   string `json:"data"`
}

type execJSONEmitter struct {
	mu sync.Mutex
	w  io.Writer
}

func newExecJSONEmitter(w io.Writer) *execJSONEmitter { return &execJSONEmitter{w: w} }

type execStreamWriter struct {
	e      *execJSONEmitter
	stream string
}

func (s execStreamWriter) Write(p []byte) (int, error) {
	s.e.mu.Lock()
	defer s.e.mu.Unlock()
	if err := jsonLine(s.e.w, execJSONLine{Stream: s.stream, Data: string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (e *execJSONEmitter) Stdout() io.Writer { return execStreamWriter{e, "stdout"} }
func (e *execJSONEmitter) Stderr() io.Writer { return execStreamWriter{e, "stderr"} }

// Exit writes the terminating record.
func (e *execJSONEmitter) Exit(code int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return jsonLine(e.w, struct {
		Exit int `json:"exit"`
	}{code})
}
