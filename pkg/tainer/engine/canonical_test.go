package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// swapCase returns p with every letter's case flipped.
func swapCase(p string) string {
	var b strings.Builder
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// On a case-insensitive disk `cd /users/leni/dev/proj` works, but the
// runtime compares bind sources against the shared home folder by exact
// spelling. Bind sources are sent spelled as they are on disk.
func TestBindSourcesUseTheOnDiskSpelling(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("case-insensitive disks are a macOS concern")
	}
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(real, "Proj")
	if err := os.MkdirAll(filepath.Join(proj, "html"), 0o755); err != nil {
		t.Fatal(err)
	}
	typed := swapCase(proj)
	if _, err := os.Stat(typed); err != nil {
		t.Skip("this disk is case-sensitive")
	}
	got, err := CanonicalDir(typed)
	if err != nil || got != proj {
		t.Errorf("CanonicalDir(%s) = %q, %v; want %q", typed, got, err, proj)
	}
	if got := hostBindSource(filepath.Join(typed, "html")); got != filepath.Join(proj, "html") {
		t.Errorf("hostBindSource = %q, want parent spelled %q", got, proj)
	}
}

func TestCanonicalDirResolvesLinks(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalDir(link); err != nil || got != real {
		t.Errorf("CanonicalDir(link) = %q, %v; want %q", got, err, real)
	}
	if _, err := CanonicalDir(filepath.Join(real, "missing")); err == nil {
		t.Error("missing folder: want an error")
	}
}
