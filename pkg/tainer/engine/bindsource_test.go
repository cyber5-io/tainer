package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// The runtime refuses any bind source that goes through a link, so tainer
// sends real paths: the user's own links (a dotfiles-managed ~/.config, a
// ~/Sites alias) are resolved here, on the user's side.
func TestHostBindSourceResolvesLinks(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(real, "tainer", "router"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "config")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := hostBindSource(filepath.Join(link, "tainer", "router")); got != filepath.Join(real, "tainer", "router") {
		t.Errorf("got %q, want the real path", got)
	}
	// A missing last element keeps its resolved parent.
	if got := hostBindSource(filepath.Join(link, "nope")); got != filepath.Join(real, "nope") {
		t.Errorf("missing: got %q", got)
	}
	// The last element is never followed: a folder swapped for a link is
	// sent as the link, so the runtime refuses it.
	outside := t.TempDir()
	swapped := filepath.Join(real, "data")
	if err := os.Symlink(outside, swapped); err != nil {
		t.Fatal(err)
	}
	if got := hostBindSource(swapped); got != swapped {
		t.Errorf("swapped: got %q, want the link path %q", got, swapped)
	}
}
