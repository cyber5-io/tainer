package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/doctor"
)

func TestDoctorJSONSkipCarriesReason(t *testing.T) {
	rep := doctor.Report{Results: []doctor.Result{
		{Name: "agent", Status: doctor.StatusSkip, Detail: "daemon is down"},
		{Name: "router", Status: doctor.StatusSkip, Detail: "x", Hint: "restart"},
	}}
	raw := captureStdout(t, func() { emitDoctorJSON(rep, false) })
	var doc struct {
		Checks []struct{ Status, Hint string } `json:"checks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Checks[0].Status != "fail" || doc.Checks[0].Hint != "skipped: daemon is down" {
		t.Errorf("skipped check must explain itself: %+v", doc.Checks[0])
	}
	if doc.Checks[1].Hint != "restart" {
		t.Errorf("existing hint must win: %+v", doc.Checks[1])
	}
}

func TestLogsJSONLineCap(t *testing.T) {
	var buf bytes.Buffer
	src := strings.NewReader("ok\n" + strings.Repeat("x", logLineMax+10) + "\nafter\n")
	err := streamLogLines(src, &buf, "blog", "app")
	if err == nil || errCode(err) != "logs-unavailable" {
		t.Fatalf("oversized line must fail with logs-unavailable, got %v", err)
	}
	if got := decodeLines(t, buf.String()); len(got) != 1 || got[0]["line"] != "ok" {
		t.Errorf("lines before the oversized one must still be emitted: %s", buf.String())
	}
}

func TestLoadManifestAtSlugs(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadManifestAt(filepath.Join(dir, "missing.yaml")); errCode(err) != "project-not-found" {
		t.Errorf("missing file: %v (%s)", err, errCode(err))
	}
	bad := filepath.Join(dir, "tainer.yaml")
	if err := os.WriteFile(bad, []byte("version: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifestAt(bad); errCode(err) != "manifest-invalid" {
		t.Errorf("parse error: %v (%s)", err, errCode(err))
	}
	good := filepath.Join(dir, "ok.yaml")
	if err := os.WriteFile(good, []byte("version: 2\nproject:\n  name: blog\n  type: wordpress\n  domain: blog.tainer.me\nruntime:\n  php: \"8.3\"\n  database: mariadb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifestAt(good); err != nil {
		t.Errorf("valid manifest: %v", err)
	}
}
