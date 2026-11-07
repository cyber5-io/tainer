package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
)

func listDevCheckout(t *testing.T, marked bool) string {
	t.Helper()
	dir := t.TempDir()
	body := "version: 1\nproject:\n  name: control-dev\n  type: nodejs\n  domain: devmy.tainer.me\n"
	if marked {
		body += "  control-dev: true\n"
	}
	body += "runtime:\n  node: \"22\"\n  database: none\n"
	if err := os.WriteFile(filepath.Join(dir, "tainer.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListControlDevMarksOnlyApprovedMarkedCheckouts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	marked, unmarked := listDevCheckout(t, true), listDevCheckout(t, false)

	if listControlDev(marked) {
		t.Error("marked but unapproved must not be flagged")
	}
	if err := controldev.Allow(marked); err != nil {
		t.Fatal(err)
	}
	if err := controldev.Allow(unmarked); err != nil {
		t.Fatal(err)
	}
	if !listControlDev(marked) {
		t.Error("marked and approved must be flagged")
	}
	if listControlDev(unmarked) {
		t.Error("approved without the marker must not be flagged")
	}
	if listControlDev(filepath.Join(t.TempDir(), "missing")) || listControlDev("") {
		t.Error("an unreadable folder is never flagged")
	}
}

func TestJSONListControlDevField(t *testing.T) {
	out := captureStdout(t, func() {
		emitListJSON([]listRow{
			{Name: "control-dev", Type: "nodejs", Domain: "devmy.tainer.me", Path: "/p/dev", Status: "running", ControlDev: true},
			{Name: "blog", Type: "wordpress", Domain: "blog.tainer.me", Path: "/p/blog", Status: "running"},
		})
	})
	var doc struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Projects[0]["controlDev"] != true {
		t.Errorf("flagged row lacks controlDev: %v", doc.Projects[0])
	}
	if _, ok := doc.Projects[1]["controlDev"]; ok {
		t.Errorf("ordinary rows keep the v1 shape: %v", doc.Projects[1])
	}
}
