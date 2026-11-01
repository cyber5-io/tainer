package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

func TestJSONDocCarriesV1(t *testing.T) {
	payload := struct {
		Pod       string `json:"pod"`
		Stopped   bool   `json:"stopped"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{"opp", true, 2310}

	out := captureStdout(t, func() { jsonDoc(payload) })

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	want := map[string]any{"v": float64(1), "pod": "opp", "stopped": true, "elapsedMs": float64(2310)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if !strings.HasPrefix(string(out), "{\n  \"v\": 1,\n") {
		t.Errorf("v must lead the document, indented like the other keys:\n%s", out)
	}
	if !strings.HasSuffix(string(out), "}\n") {
		t.Errorf("document must end with a newline:\n%q", out)
	}
}

func TestJSONDocEmptyObject(t *testing.T) {
	out := captureStdout(t, func() { jsonDoc(struct{}{}) })
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, map[string]any{"v": float64(1)}) {
		t.Errorf("got %q (%v)", out, err)
	}
}

func TestJSONDocEmbeddedStructAndEscaping(t *testing.T) {
	type base struct {
		Name string `json:"name"`
	}
	payload := struct {
		base
		Note string `json:"note"`
	}{base{"a"}, "x<y&z"}
	out := captureStdout(t, func() { jsonDoc(payload) })
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "a" || got["note"] != "x<y&z" || got["v"] != float64(1) {
		t.Errorf("got %v", got)
	}
}

func TestJSONDocNonObjectIsAnError(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONDoc(&buf, []int{1, 2}, true); err == nil {
		t.Fatal("a bare array cannot carry v; want an error")
	}
	if buf.Len() != 0 {
		t.Errorf("nothing may be written on error, got %q", buf.String())
	}
}

func TestJSONErrorEnvelopeAndExit(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = jsonFail("project-not-found", `unknown project "x"`) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := `{"v":1,"error":{"code":"project-not-found","message":"unknown project \"x\""}}` + "\n"
	if string(out) != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestJSONLineCarriesV1(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonLine(&buf, map[string]any{"type": "tick", "n": 3}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"v":1,"n":3,"type":"tick"}`+"\n" {
		t.Errorf("got %q", buf.String())
	}
	buf.Reset()
	if err := jsonLine(&buf, struct {
		Type string `json:"type"`
	}{"hello"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"v":1,"type":"hello"}`+"\n" {
		t.Errorf("got %q", buf.String())
	}
	if err := jsonLine(&buf, "scalar"); err == nil {
		t.Error("non-object line must be an error")
	}
}

func TestJSONTimeIsRFC3339Millis(t *testing.T) {
	ts := time.Date(2026, 10, 31, 12, 30, 45, 123456789, time.FixedZone("x", 3600))
	if got, want := jsonTime(ts), "2026-10-31T11:30:45.123Z"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	whole := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if got, want := jsonTime(whole), "2026-10-31T00:00:00.000Z"; got != want {
		t.Errorf("got %q want %q (millisecond precision is always exactly 3 digits)", got, want)
	}
}

func TestJSONListEnvelope(t *testing.T) {
	golden, err := os.ReadFile("testdata/json/list.golden")
	if err != nil {
		t.Fatal(err)
	}
	var legacy []map[string]any
	if err := json.Unmarshal(golden, &legacy); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { emitListJSON(sampleListRows()) })
	var doc struct {
		V        int              `json:"v"`
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.V != 1 || !reflect.DeepEqual(doc.Projects, legacy) {
		t.Errorf("rows must be the old array verbatim under \"projects\"\n got %s", out)
	}
	empty := captureStdout(t, func() { emitListJSON(nil) })
	if !strings.Contains(string(empty), `"projects": []`) {
		t.Errorf("an empty list must be [] not null:\n%s", empty)
	}
}

// failureHarness swaps the process-exit hook and JSON-active flag so the
// must()/failure paths can be asserted without exiting the test binary.
func failureHarness(t *testing.T, active bool, fn func()) (out []byte, exit int) {
	t.Helper()
	origExit, origActive := exitFn, jsonActive
	exit = -1
	exitFn = func(c int) { exit = c; panic(exitSentinel{}) }
	jsonActive = active
	defer func() { exitFn, jsonActive = origExit, origActive }()
	out = captureStdout(t, func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(exitSentinel); !ok {
					panic(r)
				}
			}
		}()
		fn()
	})
	return out, exit
}

type exitSentinel struct{}

func TestMustEmitsEnvelopeInJSONMode(t *testing.T) {
	out, exit := failureHarness(t, true, func() {
		must(withCode("project-not-found", fmt.Errorf("unknown project %q", "x")))
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	want := `{"v":1,"error":{"code":"project-not-found","message":"unknown project \"x\""}}` + "\n"
	if string(out) != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestMustUncodedErrorFallsBackToStableSlug(t *testing.T) {
	out, _ := failureHarness(t, true, func() { must(errors.New("boom")) })
	if !strings.Contains(string(out), `"code":"command-failed"`) || !strings.Contains(string(out), `"message":"boom"`) {
		t.Errorf("got %s", out)
	}
}

func TestMustLeavesStdoutAloneWhenNotJSON(t *testing.T) {
	out, exit := failureHarness(t, false, func() { must(errors.New("boom")) })
	if exit != 1 || len(out) != 0 {
		t.Errorf("exit=%d stdout=%q; human mode reports on stderr only", exit, out)
	}
}

func TestMustNilIsNoop(t *testing.T) {
	out, exit := failureHarness(t, true, func() { must(nil) })
	if exit != -1 || len(out) != 0 {
		t.Errorf("exit=%d stdout=%q", exit, out)
	}
}

func TestEngineErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{runtime.ErrEngineManaged, "stack-unavailable"},
		{fmt.Errorf("wrapped: %w", runtime.ErrEngineManaged), "stack-unavailable"},
		{runtime.ErrUnsupportedOS, "unsupported-os"},
		{errors.New("socket missing"), "stack-unreachable"},
	}
	for _, c := range cases {
		wrapped := engineErr(c.err)
		if got := errCode(wrapped); got != c.code {
			t.Errorf("%v: code %q want %q", c.err, got, c.code)
		}
		if wrapped.Error() != c.err.Error() {
			t.Errorf("human text must be untouched: %q vs %q", wrapped.Error(), c.err.Error())
		}
	}
	if engineErr(nil) != nil {
		t.Error("engineErr(nil) must stay nil")
	}
}

func TestResolveModeActivatesJSON(t *testing.T) {
	orig := jsonActive
	defer func() { jsonActive = orig }()
	jsonActive = false
	if m := resolveMode(tui.OutputFlags{JSON: true}, false); m != tui.ModeJSON || !jsonActive {
		t.Errorf("mode=%v active=%v", m, jsonActive)
	}
	jsonActive = false
	if m := resolveMode(tui.OutputFlags{Plain: true}, false); m != tui.ModePlain || jsonActive {
		t.Errorf("mode=%v active=%v", m, jsonActive)
	}
}

func TestFailedUpdateAndNetworkEmitEnvelope(t *testing.T) {
	out, exit := failureHarness(t, true, func() {
		emitUpdateBaseJSON("wordpress", withCode("update-failed", errors.New("pull failed")), time.Second)
	})
	want := `{"v":1,"error":{"code":"update-failed","message":"pull failed"}}` + "\n"
	if string(out) != want || exit != -1 {
		t.Errorf("out=%q exit=%d", out, exit)
	}
	out, _ = failureHarness(t, true, func() {
		emitUpdateSingleJSON("blog", errors.New("x"), time.Second)
	})
	if !strings.Contains(string(out), `"code":"update-failed"`) {
		t.Errorf("uncoded update error must default to update-failed: %s", out)
	}
	out, _ = failureHarness(t, true, func() {
		emitNetworkSetJSON(nil, errors.New("bad mode"), time.Second)
	})
	if !strings.Contains(string(out), `"code":"network-switch-failed"`) {
		t.Errorf("got %s", out)
	}
}

func TestNukeGuardInJSONMode(t *testing.T) {
	origIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString("blog\n")
	os.Stdin = r
	defer func() { os.Stdin = origIn; r.Close(); w.Close() }()

	out, exit := failureHarness(t, true, func() { requireNukeConfirmation("blog", tui.ModeJSON, pod.DestroyNuke, false) })
	want := `{"v":1,"error":{"code":"confirmation-required","message":"destroy --nuke requires --yes in --json mode"}}` + "\n"
	if string(out) != want || exit != 1 {
		t.Errorf("out=%q exit=%d", out, exit)
	}
	// Nothing was read: the typed confirmation is still pending on stdin.
	_ = w.Close()
	rest, _ := io.ReadAll(r)
	if string(rest) != "blog\n" {
		t.Errorf("stdin was consumed: %q", rest)
	}

	for name, c := range map[string]struct {
		mode tui.OutputMode
		dm   pod.DestroyMode
		yes  bool
	}{
		"yes given":      {tui.ModeJSON, pod.DestroyNuke, true},
		"not a nuke":     {tui.ModeJSON, pod.DestroyClean, false},
		"human mode too": {tui.ModePlain, pod.DestroyDefault, false},
	} {
		out, exit := failureHarness(t, true, func() { requireNukeConfirmation("blog", c.mode, c.dm, c.yes) })
		if exit != -1 || len(out) != 0 {
			t.Errorf("%s: out=%q exit=%d; guard must not fire", name, out, exit)
		}
	}
}
