package runtime

import (
	"strings"
	"testing"
)

func TestEngineManagedCopy(t *testing.T) {
	msg := ErrEngineManaged.Error()
	for _, want := range []string{
		"the tainer engine isn't running",
		"sudo apt install ./tainer.deb",
		"'cyberstack' group",
		"groups | grep cyberstack",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrEngineManaged %q missing %q", msg, want)
		}
	}
}
