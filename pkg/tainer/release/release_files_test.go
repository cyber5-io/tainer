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
	os.WriteFile(tp, []byte("fake tainer pkg"), 0644)
	out := filepath.Join(dir, "out")

	base := append(os.Environ(),
		"VERSION=2.0.0",
		"TAINER_DIST_PKG="+tp,
		"RELEASE_NOTES=Tainer notes.",
	)

	t.Run("refuses unsigned", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = base
		if b, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected refusal for unsigned fixtures, got: %s", b)
		}
		if _, err := os.Stat(filepath.Join(out, "2.0.0")); err == nil {
			t.Fatal("version folder exists after refused run")
		}
	})

	t.Run("refuses SKIP_SIGNATURE_CHECK other than 1", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=yes")
		if b, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected refusal, got: %s", b)
		}
	})

	t.Run("empty notes are summarised from git", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "RELEASE_NOTES=", "SKIP_SIGNATURE_CHECK=1")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, b)
		}
		data, _ := os.ReadFile(filepath.Join(out, "latest.json"))
		var l Latest
		if err := json.Unmarshal(data, &l); err != nil {
			t.Fatalf("does not parse: %v\n%s", err, data)
		}
		if !strings.HasPrefix(l.Tainer.Notes, "New in 2.0.0: ") || len(l.Tainer.Notes) < 20 {
			t.Errorf("auto notes: %q", l.Tainer.Notes)
		}
	})

	t.Run("hostile notes", func(t *testing.T) {
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=1",
			"RELEASE_NOTES=He said \"hi\" \\ path\nsecond line\x01")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, b)
		}
		if strings.Contains(string(b), "upload to get.tainer.dev") || !strings.Contains(string(b), "DO NOT UPLOAD") {
			t.Errorf("test run must say do-not-upload:\n%s", b)
		}
		data, _ := os.ReadFile(filepath.Join(out, "latest.json"))
		var l Latest
		if err := json.Unmarshal(data, &l); err != nil {
			t.Fatalf("does not parse: %v\n%s", err, data)
		}
		if l.Tainer.Notes != "He said \"hi\" \\ path second line" {
			t.Errorf("notes: %q", l.Tainer.Notes)
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
		if _, err := os.Stat(filepath.Join(out, "2.0.0", "tainer-dist-2.0.0.pkg")); err != nil {
			t.Errorf("missing 2.0.0/tainer-dist-2.0.0.pkg")
		}
		data, err := os.ReadFile(filepath.Join(out, "latest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var l Latest
		if err := json.Unmarshal(data, &l); err != nil {
			t.Fatalf("latest.json does not parse: %v\n%s", err, data)
		}
		if l.Tainer.Version != "2.0.0" {
			t.Errorf("version: %+v", l.Tainer)
		}
		if l.Tainer.URL != "https://get.tainer.dev/releases/2.0.0/tainer-dist-2.0.0.pkg" {
			t.Errorf("tainer url: %s", l.Tainer.URL)
		}
		if l.Tainer.SHA256 != fileSum(t, tp) {
			t.Errorf("sha mismatch: %+v", l.Tainer)
		}
		if l.Tainer.Notes != "Tainer notes." {
			t.Errorf("notes: %q", l.Tainer.Notes)
		}
		if l.Menu.Version != "" {
			t.Errorf("menu section should be gone, got %+v", l.Menu)
		}
		if l.Control.Tag == "" {
			t.Error("control tag empty")
		}
	})

	t.Run("keeps other versions in the channel", func(t *testing.T) {
		old := filepath.Join(out, "1.9.0")
		os.MkdirAll(old, 0755)
		os.WriteFile(filepath.Join(old, "tainer-dist-1.9.0.pkg"), []byte("old"), 0644)
		cmd := exec.Command("bash", script, out)
		cmd.Env = append(base, "SKIP_SIGNATURE_CHECK=1")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("script failed: %v\n%s", err, b)
		}
		if _, err := os.Stat(filepath.Join(old, "tainer-dist-1.9.0.pkg")); err != nil {
			t.Error("older version was removed from the channel mirror")
		}
	})
}
