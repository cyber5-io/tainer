package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fileSum(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestReleaseFilesScript(t *testing.T) {
	script, err := filepath.Abs("../../../scripts/release-files.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tp := filepath.Join(dir, "tainer-dist-2.0.0.pkg")
	mp := filepath.Join(dir, "tainer-menu-dist-2.1.0.pkg")
	os.WriteFile(tp, []byte("fake tainer pkg"), 0644)
	os.WriteFile(mp, []byte("fake menu pkg"), 0644)
	out := filepath.Join(dir, "out")

	base := append(os.Environ(),
		"VERSION=2.0.0", "MENU_VERSION=2.1.0",
		"TAINER_DIST_PKG="+tp, "MENU_DIST_PKG="+mp,
		"RELEASE_NOTES=Tainer notes.", "MENU_NOTES=Menu notes.",
		"MENU_MIN_TAINER=1.9.0",
	)

	t.Run("refuses unsigned", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = base
		if b, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected refusal for unsigned fixtures, got: %s", b)
		}
		if _, err := os.Stat(out); err == nil {
			t.Fatal("output folder exists after refused run")
		}
	})

	t.Run("refuses SKIP_SIGNATURE_CHECK other than 1", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=yes")
		if b, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected refusal, got: %s", b)
		}
	})

	t.Run("requires notes", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "RELEASE_NOTES=", "SKIP_SIGNATURE_CHECK=1")
		if b, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected failure without notes, got: %s", b)
		}
	})

	t.Run("hostile notes", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=1",
			"RELEASE_NOTES=He said \"hi\" \\ path\nsecond line\x01",
			"MENU_NOTES=\"q\" back\\slash\nnew")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, b)
		}
		if strings.Contains(string(b), "upload this folder") || !strings.Contains(string(b), "DO NOT UPLOAD") {
			t.Errorf("test run must say do-not-upload:\n%s", b)
		}
		data, _ := os.ReadFile(filepath.Join(out, "latest.json"))
		var l Latest
		if err := json.Unmarshal(data, &l); err != nil {
			t.Fatalf("does not parse: %v\n%s", err, data)
		}
		if l.Tainer.Notes != "He said \"hi\" \\ path second line" || l.Menu.Notes != "\"q\" back\\slash new" {
			t.Errorf("notes: %q %q", l.Tainer.Notes, l.Menu.Notes)
		}
	})

	t.Run("layout and latest.json", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=1")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, b)
		}
		if !strings.Contains(string(b), "WARNING") || !strings.Contains(string(b), "DO NOT UPLOAD") {
			t.Errorf("output missing warning or upload hint:\n%s", b)
		}
		for _, p := range []string{"releases/2.0.0/tainer-dist-2.0.0.pkg", "releases/2.1.0/tainer-menu-dist-2.1.0.pkg"} {
			if _, err := os.Stat(filepath.Join(out, p)); err != nil {
				t.Errorf("missing %s", p)
			}
		}
		data, err := os.ReadFile(filepath.Join(out, "latest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var l Latest
		if err := json.Unmarshal(data, &l); err != nil {
			t.Fatalf("latest.json does not parse: %v\n%s", err, data)
		}
		if l.Tainer.Version != "2.0.0" || l.Menu.Version != "2.1.0" {
			t.Errorf("versions: %+v %+v", l.Tainer, l.Menu)
		}
		if l.Tainer.URL != "https://get.tainer.dev/releases/2.0.0/tainer-dist-2.0.0.pkg" {
			t.Errorf("tainer url: %s", l.Tainer.URL)
		}
		if l.Menu.URL != "https://get.tainer.dev/releases/2.1.0/tainer-menu-dist-2.1.0.pkg" {
			t.Errorf("menu url: %s", l.Menu.URL)
		}
		if l.Tainer.SHA256 != fileSum(t, tp) || l.Menu.SHA256 != fileSum(t, mp) {
			t.Errorf("sha mismatch: %+v %+v", l.Tainer, l.Menu)
		}
		if l.Tainer.Notes != "Tainer notes." || l.Menu.Notes != "Menu notes." {
			t.Errorf("notes: %q %q", l.Tainer.Notes, l.Menu.Notes)
		}
		if l.Menu.MinTainer != "1.9.0" {
			t.Errorf("minTainer: %q", l.Menu.MinTainer)
		}
		if l.Control.Tag == "" {
			t.Error("control tag empty")
		}
	})
}
