package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/doctor"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func decodeLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line is not a JSON object: %q: %v", l, err)
		}
		if m["v"] != float64(1) {
			t.Errorf("line lacks v:1: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func TestLogsJSONLineShape(t *testing.T) {
	var buf bytes.Buffer
	src := strings.NewReader("2026-10-31T10:11:12.345678Z GET /\nplain line without stamp\n\nlast without newline")
	if err := streamLogLines(src, &buf, "blog", "app"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `{"v":1,"pod":"blog","part":"app","ts":`) {
		t.Errorf("key order wrong: %s", buf.String())
	}
	got := decodeLines(t, buf.String())
	if len(got) != 4 {
		t.Fatalf("want 4 lines (blank preserved), got %d: %s", len(got), buf.String())
	}
	if got[0]["ts"] != "2026-10-31T10:11:12.345Z" || got[0]["line"] != "2026-10-31T10:11:12.345678Z GET /" {
		t.Errorf("stamped line wrong: %v", got[0])
	}
	if got[1]["ts"] != "" || got[1]["line"] != "plain line without stamp" {
		t.Errorf("unstamped line must have empty ts and raw line: %v", got[1])
	}
	if got[2]["line"] != "" || got[3]["line"] != "last without newline" {
		t.Errorf("edge lines wrong: %v %v", got[2], got[3])
	}
	for _, g := range got {
		if g["pod"] != "blog" || g["part"] != "app" {
			t.Errorf("pod/part wrong: %v", g)
		}
	}
}

func TestDoctorJSONShape(t *testing.T) {
	rep := doctor.Report{Results: []doctor.Result{
		{Name: "daemon", Status: doctor.StatusOK, Detail: "up", Fixed: true},
		{Name: "network", Status: doctor.StatusWarn, Hint: "switch mode"},
		{Name: "agent", Status: doctor.StatusSkip, Detail: "daemon is down"},
		{Name: "router", Status: doctor.StatusFail, Hint: "restart"},
	}}
	parse := func(fix bool) map[string]any {
		raw := captureStdout(t, func() { emitDoctorJSON(rep, fix) })
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, raw)
		}
		return doc
	}

	doc := parse(false)
	if doc["v"] != float64(1) {
		t.Errorf("v: %v", doc["v"])
	}
	if _, has := doc["fixed"]; has {
		t.Errorf("fixed must be absent without --fix")
	}
	checks := doc["checks"].([]any)
	if len(checks) != 4 {
		t.Fatalf("checks: %v", checks)
	}
	want := []string{"ok", "warn", "fail", "fail"} // skip is reported as fail
	for i, c := range checks {
		cm := c.(map[string]any)
		if cm["status"] != want[i] {
			t.Errorf("check %d status %v, want %s", i, cm["status"], want[i])
		}
		if _, ok := cm["hint"]; !ok {
			t.Errorf("check %d lacks hint key", i)
		}
	}

	doc = parse(true)
	fixed, has := doc["fixed"].([]any)
	if !has || len(fixed) != 1 || fixed[0] != "daemon" {
		t.Errorf("fixed with --fix: %v", doc["fixed"])
	}
	rep.Results[0].Fixed = false
	doc = parse(true)
	if f, has := doc["fixed"].([]any); !has || len(f) != 0 {
		t.Errorf("fixed must be present and empty when --fix repaired nothing: %v", doc["fixed"])
	}
}

func TestDoctorExitCode(t *testing.T) {
	ok := doctor.Report{Results: []doctor.Result{{Name: "a", Status: doctor.StatusWarn}}}
	bad := doctor.Report{Results: []doctor.Result{{Name: "a", Status: doctor.StatusFail}}}
	if doctorExitCode(ok) != 0 || doctorExitCode(bad) != 1 {
		t.Errorf("exit semantics: warn=0 fail=1")
	}
}

func TestConfigJSONShape(t *testing.T) {
	m := &manifest.Manifest{
		Version: 2,
		Project: manifest.ProjectConfig{Name: "blog", Type: manifest.TypeWordPress, Domain: "blog.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.3", Database: manifest.DatabaseMariaDB},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
	}
	raw := captureStdout(t, func() {
		if err := emitConfigJSON(m); err != nil {
			t.Fatal(err)
		}
	})
	var doc struct {
		V       int            `json:"v"`
		Project map[string]any `json:"project"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if doc.V != 1 {
		t.Errorf("v: %d", doc.V)
	}
	p := doc.Project["project"].(map[string]any)
	if doc.Project["version"] != float64(2) || p["name"] != "blog" || p["type"] != "wordpress" || p["domain"] != "blog.tainer.me" {
		t.Errorf("project fields: %v", doc.Project)
	}
	if doc.Project["runtime"].(map[string]any)["database"] != "mariadb" || doc.Project["pod"].(map[string]any)["size"] != "small" {
		t.Errorf("runtime/pod fields: %v", doc.Project)
	}
}

func TestExecJSONStreamShape(t *testing.T) {
	var buf bytes.Buffer
	em := newExecJSONEmitter(&buf)
	if _, err := em.Stdout().Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := em.Stderr().Write([]byte("oops")); err != nil {
		t.Fatal(err)
	}
	if err := em.Exit(3); err != nil {
		t.Fatal(err)
	}
	lines := decodeLines(t, buf.String())
	if len(lines) != 3 {
		t.Fatalf("lines: %s", buf.String())
	}
	if lines[0]["stream"] != "stdout" || lines[0]["data"] != "hello\n" {
		t.Errorf("stdout line: %v", lines[0])
	}
	if lines[1]["stream"] != "stderr" || lines[1]["data"] != "oops" {
		t.Errorf("stderr line: %v", lines[1])
	}
	if lines[2]["exit"] != float64(3) || len(lines[2]) != 2 {
		t.Errorf("exit line must be exactly v+exit: %v", lines[2])
	}
}

func TestExecJSONRefusesTTY(t *testing.T) {
	for name, p := range map[string]execParsedArgs{
		"tty":         {ttyExplicit: true, ttyForced: true},
		"interactive": {interactive: true},
	} {
		code, msg := execJSONRefusal(p)
		if code != "exec-tty-unsupported" || msg == "" {
			t.Errorf("%s: want exec-tty-unsupported, got %q %q", name, code, msg)
		}
	}
	if code, _ := execJSONRefusal(execParsedArgs{ttyExplicit: true, ttyForced: false}); code != "" {
		t.Errorf("--no-tty must be allowed under --json, got %q", code)
	}
	if code, _ := execJSONRefusal(execParsedArgs{}); code != "" {
		t.Errorf("plain exec must be allowed under --json, got %q", code)
	}
}

func TestSplitExecOutputFlags(t *testing.T) {
	cases := []struct {
		in   []string
		json bool
		out  []string
	}{
		{[]string{"opp", "--json", "--", "ls", "--json"}, true, []string{"opp", "--", "ls", "--json"}},
		{[]string{"opp", "--", "ls", "--json"}, false, []string{"opp", "--", "ls", "--json"}},
	}
	for _, c := range cases {
		j, out := splitExecOutputFlags(c.in)
		if j != c.json || strings.Join(out, " ") != strings.Join(c.out, " ") {
			t.Errorf("%v -> %v %v", c.in, j, out)
		}
	}
}
