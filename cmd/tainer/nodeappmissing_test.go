package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func nodeProject(t *testing.T) (manifestPath, dir string) {
	t.Helper()
	dir = t.TempDir()
	manifestPath = filepath.Join(dir, manifest.FileName)
	body := "version: 2\nproject:\n  name: shop\n  type: nodejs\n  domain: shop.tainer.me\nruntime:\n  node: \"22\"\n  database: none\n"
	if err := os.WriteFile(manifestPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestPath, dir
}

// A project whose app still lives in the legacy html/ is not missing its
// app: the hint checks after the folder is moved to app/.
func TestNodeAppMissingSeesLegacyHtml(t *testing.T) {
	mp, dir := nodeProject(t)
	if err := os.MkdirAll(filepath.Join(dir, "html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "html", "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if nodeAppMissing(mp, dir) {
		t.Error("nodeAppMissing = true for a project with html/package.json")
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "package.json")); err != nil {
		t.Errorf("html/ should have moved to app/: %v", err)
	}
}

// Both folders holding files is refused by the migration; the hint must not
// claim the app is missing either.
func TestNodeAppMissingQuietOnRefusal(t *testing.T) {
	mp, dir := nodeProject(t)
	for _, f := range []string{"html/package.json", "app/README.md"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if nodeAppMissing(mp, dir) {
		t.Error("nodeAppMissing = true while html/ holds package.json")
	}
}

func TestNodeAppMissingEmptyApp(t *testing.T) {
	mp, dir := nodeProject(t)
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !nodeAppMissing(mp, dir) {
		t.Error("nodeAppMissing = false for an empty app/")
	}
}
