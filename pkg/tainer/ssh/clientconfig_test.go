package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientConfigContent(t *testing.T) {
	got := ClientConfigContent(ClientIdentityTilde)
	for _, want := range []string{
		"Host ssh.tainer.me",
		"IdentityFile ~/.config/tainer/keys/tainer_rsa",
		"IdentitiesOnly yes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("content missing %q; got:\n%s", want, got)
		}
	}
}

func TestWriteDropIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), DropInName)
	if err := WriteDropIn(p, ClientIdentityTilde); err != nil {
		t.Fatalf("WriteDropIn: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "Host ssh.tainer.me") {
		t.Errorf("drop-in content wrong:\n%s", b)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("drop-in mode = %o, want 0644", info.Mode().Perm())
	}
}

func TestEnsureUserConfigIncludeMissingFileIsNoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatalf("want nil for missing file, got %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("missing config must stay missing (system drop-in covers it)")
	}
}

func TestEnsureUserConfigIncludePrependsWhenAbsent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\n  UseKeychain yes\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines[0] != UserConfigIncludeLine {
		t.Errorf("first line = %q, want include", lines[0])
	}
	if !strings.Contains(string(b), "UseKeychain yes") {
		t.Error("original content must be preserved")
	}
}

func TestEnsureUserConfigIncludeIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte(UserConfigIncludeLine+"\nHost *\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), UserConfigIncludeLine) != 1 {
		t.Errorf("include must appear exactly once, got:\n%s", b)
	}
}

func TestEnsureUserConfigIncludeMovesToFirst(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\n  IdentityFile ~/.ssh/x\n"+UserConfigIncludeLine+"\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines[0] != UserConfigIncludeLine {
		t.Errorf("include must be moved to first line, got first = %q", lines[0])
	}
	if strings.Count(string(b), UserConfigIncludeLine) != 1 {
		t.Errorf("include must appear exactly once, got:\n%s", b)
	}
}

func TestEnsureUserConfigIncludePreservesMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\n"), 0640)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Errorf("mode = %o, want 0640", info.Mode().Perm())
	}
}

func TestEnsureUserConfigIncludeCRLFIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\r\n  UseKeychain yes\r\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if string(first) != string(second) {
		t.Errorf("not idempotent on CRLF:\nfirst=%q\nsecond=%q", first, second)
	}
	if !strings.HasPrefix(string(first), UserConfigIncludeLine) {
		t.Errorf("include not first line: %q", first)
	}
}
