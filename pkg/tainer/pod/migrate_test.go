package pod

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("%s should exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s should not exist (err=%v)", path, err)
	}
}

func TestMigrateRenamesHtml(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "html", "index.php"), "<?php echo 1;")
	writeFile(t, filepath.Join(p, "html", ".env"), "A=1")
	writeFile(t, filepath.Join(p, "html", "wp-content", "themes", "x", "style.css"), "body{}")
	// A relative link reaching into data/ keeps its depth across the rename.
	if err := os.MkdirAll(filepath.Join(p, "data", "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../data/uploads", filepath.Join(p, "html", "wp-content", "uploads")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "renamed html/ to app/\n" {
		t.Errorf("notice = %q", got)
	}
	mustNotExist(t, filepath.Join(p, "html"))
	for _, f := range []string{"index.php", ".env", "wp-content/themes/x/style.css"} {
		mustExist(t, filepath.Join(p, "app", f))
	}
	b, err := os.ReadFile(filepath.Join(p, "app", "index.php"))
	if err != nil || string(b) != "<?php echo 1;" {
		t.Errorf("index.php contents = %q, %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(p, "app", "wp-content", "uploads")); err != nil || !fi.IsDir() {
		t.Errorf("relative uploads link should still resolve: %v", err)
	}
}

func TestMigrateNoopWhenAppExists(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "app", "index.php"), "x")
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("no output expected, got %q", out.String())
	}
	mustExist(t, filepath.Join(p, "app", "index.php"))
	mustNotExist(t, filepath.Join(p, "html"))
}

func TestMigrateNoopWhenAppFullAndHtmlEmpty(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "app", "index.php"), "x")
	if err := os.Mkdir(filepath.Join(p, "html"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("no output expected, got %q", out.String())
	}
	mustExist(t, filepath.Join(p, "app", "index.php"))
}

func TestMigrateRemovesEmptyAppThenRenames(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "html", "index.php"), "x")
	if err := os.Mkdir(filepath.Join(p, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "renamed html/ to app/\n" {
		t.Errorf("notice = %q", got)
	}
	mustExist(t, filepath.Join(p, "app", "index.php"))
	mustNotExist(t, filepath.Join(p, "html"))
}

func TestMigrateRefusesBothNonEmpty(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "html", "index.php"), "old")
	// A dotfile alone makes app/ non-empty.
	writeFile(t, filepath.Join(p, "app", ".env"), "A=1")
	var out bytes.Buffer
	err := MigrateAppDir(p, &out)
	if err == nil || err.Error() != "both app/ and html/ exist — move your code into app/ and remove html/" {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("no output expected, got %q", out.String())
	}
	mustExist(t, filepath.Join(p, "html", "index.php"))
	mustExist(t, filepath.Join(p, "app", ".env"))
}

func TestMigrateRefusesSymlinkedHtml(t *testing.T) {
	p := realDir(t)
	elsewhere := realDir(t)
	writeFile(t, filepath.Join(elsewhere, "index.php"), "x")
	if err := os.Symlink(elsewhere, filepath.Join(p, "html")); err != nil {
		t.Fatal(err)
	}
	err := MigrateAppDir(p, &bytes.Buffer{})
	if err == nil || err.Error() != "html is a link; tainer only migrates a real folder" {
		t.Fatalf("err = %v", err)
	}
	mustNotExist(t, filepath.Join(p, "app"))
	mustExist(t, filepath.Join(elsewhere, "index.php"))
}

func TestMigrateRefusesSymlinkedAppBesideHtml(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "html", "index.php"), "x")
	if err := os.Symlink(realDir(t), filepath.Join(p, "app")); err != nil {
		t.Fatal(err)
	}
	err := MigrateAppDir(p, &bytes.Buffer{})
	if err == nil || err.Error() != "app is a link; tainer only migrates a real folder" {
		t.Fatalf("err = %v", err)
	}
	mustExist(t, filepath.Join(p, "html", "index.php"))
}

func TestMigrateFreshProjectIsNoop(t *testing.T) {
	p := realDir(t)
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("no output expected, got %q", out.String())
	}
	mustNotExist(t, filepath.Join(p, "app"))
	mustNotExist(t, filepath.Join(p, "html"))
}

// Start's project check runs the migration before any mount is built, so a
// half-migrated project is caught before buildMounts can make app/ or
// data/, and a legacy one reaches the mounts already renamed.
func TestStartMigratesBeforeMounts(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabaseMariaDB},
	}

	t.Run("legacy project", func(t *testing.T) {
		p := realDir(t)
		writeFile(t, filepath.Join(p, "html", "index.php"), "x")
		var out bytes.Buffer
		if err := checkProjectFolders(m, p, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "renamed html/ to app/") {
			t.Errorf("notice missing: %q", out.String())
		}
		mustExist(t, filepath.Join(p, "app", "index.php"))
		mustNotExist(t, filepath.Join(p, "html"))
	})

	t.Run("refusal stops before mounts", func(t *testing.T) {
		p := realDir(t)
		elsewhere := realDir(t)
		if err := os.Symlink(elsewhere, filepath.Join(p, "html")); err != nil {
			t.Fatal(err)
		}
		if err := checkProjectFolders(m, p, &bytes.Buffer{}); err == nil {
			t.Fatal("expected refusal")
		}
		// buildMounts would have created both; neither may appear.
		mustNotExist(t, filepath.Join(p, "app"))
		mustNotExist(t, filepath.Join(p, "data"))
	})

	t.Run("control folder untouched", func(t *testing.T) {
		p := realDir(t)
		writeFile(t, filepath.Join(p, "data", "x"), "y")
		var out bytes.Buffer
		if err := MigrateAppDir(p, &out); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Errorf("no output expected, got %q", out.String())
		}
		mustNotExist(t, filepath.Join(p, "app"))
	})
}

// An old pod still running after the rename recreates html/ cache folders
// (.vite and the like). They hold nothing of the user's, so start says so
// and carries on rather than refusing; nothing is deleted or mounted.
func TestMigrateIgnoresDotOnlyHtmlBesideApp(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "app", "index.php"), "x")
	writeFile(t, filepath.Join(p, "html", ".vite", "deps", "_metadata.json"), "{}")
	writeFile(t, filepath.Join(p, "html", ".cache"), "c")
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatalf("dot-only html/ beside app/: %v", err)
	}
	if got, want := out.String(), "ignoring leftover html/ (old caches) — safe to delete\n"; got != want {
		t.Errorf("notice = %q, want %q", got, want)
	}
	mustExist(t, filepath.Join(p, "html", ".vite", "deps", "_metadata.json"))
	mustExist(t, filepath.Join(p, "app", "index.php"))
}

// One non-dot entry in html/ is real content: still refused.
func TestMigrateRefusesHtmlWithContentBesideApp(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "app", "index.php"), "x")
	writeFile(t, filepath.Join(p, "html", ".vite", "x"), "{}")
	writeFile(t, filepath.Join(p, "html", "wp-config.php"), "<?php")
	var out bytes.Buffer
	err := MigrateAppDir(p, &out)
	if err == nil || !strings.Contains(err.Error(), "both app/ and html/ exist") {
		t.Fatalf("err = %v, want the both-exist refusal", err)
	}
	if out.Len() != 0 {
		t.Errorf("no notice expected, got %q", out.String())
	}
}

// With no app/, a dot-only html/ is still the project's folder and moves.
func TestMigrateRenamesDotOnlyHtmlWithoutApp(t *testing.T) {
	p := realDir(t)
	writeFile(t, filepath.Join(p, "html", ".env"), "A=1")
	var out bytes.Buffer
	if err := MigrateAppDir(p, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "renamed html/ to app/\n" {
		t.Errorf("notice = %q", got)
	}
	mustExist(t, filepath.Join(p, "app", ".env"))
	mustNotExist(t, filepath.Join(p, "html"))
}

// tain-129: a start on a tainer.yaml still carrying a custom budget says
// which named size it runs as, once, before anything else.
func TestStartNotesAMappedCustomSize(t *testing.T) {
	m, err := manifest.ParseBytes([]byte("version: 2\nproject:\n  name: demo\n  type: nodejs\n  domain: demo.tainer.me\nruntime:\n  node: \"22\"\n  database: none\npod:\n  size: custom\n  memory: 3G\n  cpu: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := checkProjectFolders(m, realDir(t), &out); err != nil {
		t.Fatal(err)
	}
	want := "custom pod sizes are no longer supported: 3G, 2 CPU now runs as large (4G, 4 CPU) — choose a size with tainer config set pod.size <size>\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
	m.Pod.MappedFrom = ""
	out.Reset()
	if err := checkProjectFolders(m, realDir(t), &out); err != nil || out.Len() != 0 {
		t.Errorf("a named size says nothing: %q %v", out.String(), err)
	}
}
