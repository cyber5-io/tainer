package runtime

import (
	"strings"
	"testing"
)

func TestEngineManagedCopy(t *testing.T) {
	msg := ErrEngineManaged.Error()
	for _, banned := range []string{"engine", "podman", "docker"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("ErrEngineManaged %q must not mention %q", msg, banned)
		}
	}
	for _, want := range []string{
		"the tainer stack isn't running",
		"sudo apt install ./tainer.deb",
		"'cyberstack' group",
		"groups | grep cyberstack",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrEngineManaged %q missing %q", msg, want)
		}
	}
}

func TestSpawnRefusedVocabulary(t *testing.T) {
	msg := spawnRefused(ErrUnsupportedOS).Error()
	if !strings.Contains(msg, "cannot start the tainer stack") {
		t.Errorf("got %q", msg)
	}
	for _, banned := range []string{"engine", "podman", "docker"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("%q must not mention %q", msg, banned)
		}
	}
}
