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
}
