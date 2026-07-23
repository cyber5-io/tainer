package main

import (
	"reflect"
	"testing"
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
