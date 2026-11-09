package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmAcceptsOnlyYes(t *testing.T) {
	for in, want := range map[string]bool{
		"y": true, "Yes\n": true, "  YES  \n": true, "y\n": true,
		"": false, "n\n": false, "yep\n": false, "\n": false,
	} {
		var out bytes.Buffer
		if got := confirm(strings.NewReader(in), &out, "Go?"); got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
		if out.String() != "Go? [y/N] " {
			t.Errorf("prompt = %q", out.String())
		}
	}
}
