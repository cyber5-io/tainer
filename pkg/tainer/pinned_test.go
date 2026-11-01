package tainer

import "testing"

func TestIsPinnedProject(t *testing.T) {
	for name, want := range map[string]bool{
		"control":  true,
		"Control":  false,
		"controls": false,
		"blog":     false,
		"":         false,
	} {
		if got := IsPinnedProject(name); got != want {
			t.Errorf("IsPinnedProject(%q) = %v, want %v", name, got, want)
		}
	}
}
