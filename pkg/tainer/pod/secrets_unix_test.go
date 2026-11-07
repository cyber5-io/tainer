//go:build unix

package pod

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A hardlinked .env is another project's file under a second name, and a
// FIFO would block start forever waiting for a writer: both are refused,
// promptly, naming the file.
func TestLoadOrCreateSecrets_RefusesHardlinkAndFIFO(t *testing.T) {
	other := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(other, []byte("DB_PASSWORD=other\nDB_ROOT_PASSWORD=other-root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(p string) error{
		"hardlink": func(p string) error { return os.Link(other, p) },
		"fifo":     func(p string) error { return syscall.Mkfifo(p, 0o600) },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			envPath := filepath.Join(dir, ".env")
			if err := mk(envPath); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := LoadOrCreateSecretsAt(filepath.Join(t.TempDir(), "site.env"), "site", dir)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), envPath) {
					t.Errorf("got %v, want a refusal naming %s", err, envPath)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("start hung on the .env")
			}
		})
	}
}

// openProjectFile itself never blocks on a FIFO, even one that appears
// after any earlier check.
func TestOpenProjectFileDoesNotBlockOnFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := openNoFollow(p)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("FIFO accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("open blocked on a FIFO")
	}
}
