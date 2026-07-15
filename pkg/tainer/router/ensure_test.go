package router

import "testing"

func TestSSHHintUsesWrapper(t *testing.T) {
	if got := SSHHint("newnode"); got != "tainer ssh newnode" {
		t.Errorf("SSHHint = %q, want %q", got, "tainer ssh newnode")
	}
}
