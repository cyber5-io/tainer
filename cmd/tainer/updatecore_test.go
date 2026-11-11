package main

import (
	"reflect"
	"strings"
	"testing"
)

// tain-123: `update core` is the self-update; `update tainer` stays an alias.

func TestUpdateCoreSelfUpdatesAndTainerIsAnAlias(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.install(t)
	for _, word := range []string{"core", "tainer"} {
		f.log = nil
		out, exit := failureHarness(t, true, func() { cmdUpdate([]string{word, "--json", "--yes"}) })
		if exit != -1 {
			t.Fatalf("update %s: exit=%d\n%s", word, exit, out)
		}
		if !reflect.DeepEqual(f.log, []string{"tainer"}) {
			t.Fatalf("update %s ran %v, want the tainer install", word, f.log)
		}
		doc := checkDocFrom(t, out)
		// The document keeps its mode: consumers already read "tainer".
		if doc["mode"] != "tainer" || doc["updated"] != true || doc["to"] != "1.5.0" {
			t.Errorf("update %s doc = %v", word, doc)
		}
	}
}

func TestUpdateCoreBeatsAProjectNamedCore(t *testing.T) {
	cwd := selfTestHome(t)
	dir := registerProject(t, cwd, "core")
	f := newFakeSelf()
	f.install(t)

	out := runUpdateCmd(t, []string{"core"})
	if !reflect.DeepEqual(f.log, []string{"tainer"}) {
		t.Fatalf("update core ran %v, want the tainer install", f.log)
	}
	if !strings.Contains(string(out), `(to update the project named "core", run tainer update ./core)`) {
		t.Errorf("missing hint:\n%s", out)
	}

	f.log = nil
	runUpdateCmd(t, []string{"./core"})
	if !reflect.DeepEqual(f.log, []string{"pod:core@" + dir}) {
		t.Fatalf("update ./core ran %v", f.log)
	}

	// The alias names itself in the hint.
	registerProject(t, cwd, "tainer")
	f.log = nil
	out = runUpdateCmd(t, []string{"tainer"})
	if !reflect.DeepEqual(f.log, []string{"tainer"}) || !strings.Contains(string(out), `run tainer update ./tainer)`) {
		t.Errorf("update tainer ran %v\n%s", f.log, out)
	}
}
