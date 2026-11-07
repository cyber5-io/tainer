package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pods can only be given files from the user's home folder, and the router
// and every pod mount files from tainer's settings folder. One outside home
// (XDG_CONFIG_HOME elsewhere, or ~/.config linked elsewhere) is caught up
// front with a plain message instead of failing later at start.
func TestSettingsFolderMustBeInsideHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	elsewhere := filepath.Join(base, "elsewhere")
	for _, d := range []string{home, elsewhere, filepath.Join(home, "dotfiles", "config")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linkedOut := filepath.Join(home, "linked-out")
	if err := os.Symlink(elsewhere, linkedOut); err != nil {
		t.Fatal(err)
	}
	linkedIn := filepath.Join(home, "linked-in")
	if err := os.Symlink(filepath.Join(home, "dotfiles", "config"), linkedIn); err != nil {
		t.Fatal(err)
	}
	ok := []string{
		filepath.Join(home, ".config", "tainer"),  // not created yet
		filepath.Join(linkedIn, "tainer"),         // a link that stays inside home
		filepath.Join(home, "dotfiles", "config"), // exists
	}
	for _, dir := range ok {
		if err := checkInsideHome(home, dir); err != nil {
			t.Errorf("%s: refused (%v), want accepted", dir, err)
		}
	}
	bad := []string{
		filepath.Join(elsewhere, "tainer"),
		filepath.Join(linkedOut, "tainer"),
		filepath.Join(base, "home-other", "tainer"), // shares a prefix with home
	}
	for _, dir := range bad {
		err := checkInsideHome(home, dir)
		if err == nil {
			t.Errorf("%s: accepted, want refused", dir)
			continue
		}
		if !strings.Contains(err.Error(), "XDG_CONFIG_HOME") || !strings.Contains(err.Error(), dir) {
			t.Errorf("%s: unhelpful message %q", dir, err)
		}
	}
}
