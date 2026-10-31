package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// The --json envelope is a public contract consumed by the control webapp and
// the menu app. Every single-shot document is a JSON object whose first key is
// "v":1; every failure under --json is exactly
//
//	{"v":1,"error":{"code":"<stable-slug>","message":"<human text>"}}
//
// on stdout with exit status 1. Codes are machine identifiers: kebab-case,
// stable across releases, and free of engine vocabulary. Timestamps in new
// payloads use jsonTime (RFC 3339, exactly millisecond precision).

const jsonEnvelopeVersion = 1

// jsonActive is true once the current command resolved to JSON output; it
// routes must() failures to the error envelope instead of stderr.
var jsonActive bool

// exitFn is the process-exit hook; tests replace it.
var exitFn = os.Exit

// resolveMode resolves the output mode for a command and arms the JSON error
// path when that mode is JSON.
func resolveMode(flags tui.OutputFlags, commandSupportsTUI bool) tui.OutputMode {
	mode := flags.Resolve(commandSupportsTUI)
	if mode == tui.ModeJSON {
		jsonActive = true
	}
	return mode
}

// writeJSONDoc renders payload (which must marshal to a JSON object) with "v"
// injected as the first key. indent selects the human-diffable two-space layout
// used for single-shot documents; otherwise the document is one compact line.
func writeJSONDoc(w io.Writer, payload any, indent bool) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) < 2 || raw[0] != '{' {
		return fmt.Errorf("json envelope: payload must be an object, got %.20s", raw)
	}
	var doc bytes.Buffer
	fmt.Fprintf(&doc, `{"v":%d`, jsonEnvelopeVersion)
	if len(raw) > 2 {
		doc.WriteByte(',')
	}
	doc.Write(raw[1:])

	if indent {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, doc.Bytes(), "", "  "); err != nil {
			return err
		}
		pretty.WriteByte('\n')
		_, err = w.Write(pretty.Bytes())
		return err
	}
	doc.WriteByte('\n')
	_, err = w.Write(doc.Bytes())
	return err
}

// jsonDoc writes one single-shot result document to stdout.
func jsonDoc(payload any) {
	if err := writeJSONDoc(os.Stdout, payload, true); err != nil {
		exitFn(jsonFail("internal-error", err.Error()))
	}
}

// jsonLine writes one NDJSON record (compact, newline-terminated) to w.
func jsonLine(w io.Writer, payload any) error {
	return writeJSONDoc(w, payload, false)
}

// jsonFail writes the error envelope to stdout and returns the exit status the
// caller must pass to its exit path.
func jsonFail(code, message string) int {
	env := struct {
		V     int `json:"v"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{V: jsonEnvelopeVersion}
	env.Error.Code = code
	env.Error.Message = message
	b, _ := json.Marshal(env)
	fmt.Fprintf(os.Stdout, "%s\n", b)
	return 1
}

// jsonTime formats t for new payloads: RFC 3339, UTC, exactly three fractional
// digits.
func jsonTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// codedError carries a stable error-envelope slug while leaving the human
// message untouched.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

// withCode tags err with an envelope slug. An error that already carries a
// slug keeps it (the innermost, most specific one wins).
func withCode(code string, err error) error {
	if err == nil {
		return nil
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return err
	}
	return &codedError{code: code, err: err}
}

// errCode returns err's envelope slug, deriving one for the known sentinel
// errors and falling back to "command-failed".
func errCode(err error) string {
	var ce *codedError
	switch {
	case errors.As(err, &ce):
		return ce.code
	case errors.Is(err, runtime.ErrEngineManaged):
		return "stack-unavailable"
	case errors.Is(err, runtime.ErrUnsupportedOS):
		return "unsupported-os"
	}
	return "command-failed"
}

// engineErr tags a failure to reach the tainer stack.
func engineErr(err error) error {
	if err == nil {
		return nil
	}
	return withCode(errCodeOr(err, "stack-unreachable"), err)
}

// errCodeOr is errCode with a caller-chosen fallback for uncoded, non-sentinel
// errors.
func errCodeOr(err error, fallback string) string {
	if c := errCode(err); c != "command-failed" {
		return c
	}
	return fallback
}
