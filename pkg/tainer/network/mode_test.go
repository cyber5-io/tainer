package network

import (
	"path/filepath"
	"testing"
)

func TestModeRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "network-mode")

	if got, err := ReadMode(path); err != nil || got != DefaultMode {
		t.Errorf("default ReadMode: got (%q,%v), want (%q,nil)", got, err, DefaultMode)
	}

	if err := WriteMode(path, ModeCompatibility); err != nil {
		t.Fatalf("WriteMode: %v", err)
	}
	if got, err := ReadMode(path); err != nil || got != ModeCompatibility {
		t.Errorf("ReadMode after write: got (%q,%v), want (%q,nil)", got, err, ModeCompatibility)
	}
}

func TestParseShortForms(t *testing.T) {
	cases := map[string]Mode{
		"perf":             ModePerformance,
		"performance":      ModePerformance,
		"vmnet-helper":     ModePerformance,
		"compat":           ModeCompatibility,
		"compatibility":    ModeCompatibility,
		"external-gvproxy": ModeCompatibility,
		"hybrid":           ModeHybrid,
		"h":                ModeHybrid,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q): got (%q,%v), want (%q,nil)", in, got, err, want)
		}
	}
	if _, err := ParseMode("garbage"); err == nil {
		t.Error("ParseMode(garbage): want error, got nil")
	}
}
