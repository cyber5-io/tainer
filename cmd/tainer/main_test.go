package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestSSHArgsDefaultPort(t *testing.T) {
	got := sshArgs("newnode", "/keys/tainer_rsa", 22, []string{"ls", "-la"})
	want := []string{
		"-F", "/dev/null",
		"-o", "IdentitiesOnly=yes",
		"-o", "UpdateHostKeys=no",
		"-i", "/keys/tainer_rsa",
		"newnode@ssh.tainer.me",
		"ls", "-la",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSSHArgsPort2222(t *testing.T) {
	got := sshArgs("newnode", "/k", 2222, nil)
	if got[0] != "-p" || got[1] != "2222" {
		t.Errorf("expected -p 2222 first, got %v", got)
	}
}

func TestScaffoldHint(t *testing.T) {
	kit := scaffoldHint(manifest.TypeSvelteKit)
	if !strings.Contains(kit, "sv create") {
		t.Errorf("sveltekit hint = %q, want the sv create scaffold", kit)
	}
	if !strings.Contains(kit, "tainer restart") {
		t.Errorf("sveltekit hint = %q, want the restart tail", kit)
	}
	generic := scaffoldHint(manifest.TypeNextJS)
	if !strings.Contains(generic, "create vite@latest") {
		t.Errorf("nextjs hint = %q, want the generic vite copy", generic)
	}
	if scaffoldHint(manifest.TypeNodeJS) != generic {
		t.Error("nodejs hint differs from the generic copy")
	}
}
