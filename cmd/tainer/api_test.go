package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The discovery document is part of the public contract: the control webapp
// and the menu app read it to learn which commands accept --json and which
// stream NDJSON. The list mirrors the gateway allowlist in cyber-stack
// (internal/controlapi allowedCommands) — keep the two in step.
const apiGolden = `{"v":1,"commands":[` +
	`{"name":"status","json":true,"stream":false},` +
	`{"name":"list","json":true,"stream":false},` +
	`{"name":"init","json":true,"stream":false},` +
	`{"name":"start","json":true,"stream":false},` +
	`{"name":"stop","json":true,"stream":false},` +
	`{"name":"restart","json":true,"stream":false},` +
	`{"name":"destroy","json":true,"stream":false},` +
	`{"name":"db","json":true,"stream":false},` +
	`{"name":"update","json":true,"stream":false},` +
	`{"name":"network","json":true,"stream":false},` +
	`{"name":"doctor","json":true,"stream":false},` +
	`{"name":"logs","json":true,"stream":true},` +
	`{"name":"exec","json":true,"stream":true},` +
	`{"name":"config","json":true,"stream":false},` +
	`{"name":"api","json":true,"stream":false},` +
	`{"name":"watch","json":true,"stream":true}` +
	`]}` + "\n"

func TestAPIListsAllJSONCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runAPI([]string{"--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != apiGolden {
		t.Errorf("api output drifted from the golden:\n got: %s\nwant: %s", out.String(), apiGolden)
	}
	for _, need := range []string{"watch", "logs", "exec", "config", "doctor"} {
		if !strings.Contains(out.String(), `"name":"`+need+`"`) {
			t.Errorf("missing %s", need)
		}
	}
}

// TestAPIDocMatchesGolden keeps docs/api/cli-json-v1.md honest: the discovery
// document and the error-envelope examples must appear in it verbatim.
func TestAPIDocMatchesGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "cli-json-v1.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if !strings.Contains(doc, strings.TrimSuffix(apiGolden, "\n")) {
		t.Error("docs/api/cli-json-v1.md does not contain the tainer api golden verbatim")
	}
	for _, line := range []string{
		"`status` exits 1 only when `daemon.reachable` is false",
		`{"v":1,"error":{"code":"confirmation-required","message":"destroy --nuke requires --yes in --json mode"}}`,
		`{"v":1,"error":{"code":"update-failed","message":"pull failed"}}`,
	} {
		if !strings.Contains(doc, line) {
			t.Errorf("doc lacks verbatim example %s", line)
		}
	}
}

func TestAPIRequiresJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runAPI(nil, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if out.Len() != 0 || strings.TrimSpace(errOut.String()) != "tainer api requires --json" {
		t.Errorf("stdout %q stderr %q", out.String(), errOut.String())
	}
}
