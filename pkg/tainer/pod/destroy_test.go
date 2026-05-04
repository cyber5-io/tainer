package pod

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNukeRequiresExactName(t *testing.T) {
	prompt := func() string { return "wrongname" }
	err := confirmNuke("mywp", prompt)
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Errorf("want match error, got %v", err)
	}
}

func TestNukeAcceptsExactName(t *testing.T) {
	prompt := func() string { return "mywp" }
	if err := confirmNuke("mywp", prompt); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestCleanFilesAllowlist(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir+"/.tainer-authorized_keys", "key")
	mustWrite(t, dir+"/.tainer.local.staging", "x")
	mustWrite(t, dir+"/keep-this.txt", "user file")
	mustWrite(t, dir+"/data/db.sql", "user data")

	if err := cleanProjectDir(dir); err != nil {
		t.Fatalf("cleanProjectDir: %v", err)
	}
	if exists(dir + "/.tainer-authorized_keys") {
		t.Error("authorized_keys should be removed")
	}
	if !exists(dir + "/keep-this.txt") {
		t.Error("user file must survive --clean")
	}
	if !exists(dir + "/data/db.sql") {
		t.Error("data/ must survive --clean")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
