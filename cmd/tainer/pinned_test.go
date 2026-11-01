package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

const pinnedMessage = "the control dashboard runs whenever tainer runs"

// inProjectDir chdirs into a temp dir holding a minimal tainer.yaml for
// the named project, so cmdStop/cmdDestroy resolve it without a registry.
func inProjectDir(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	yaml := "version: 2\nproject:\n  name: " + name + "\n  type: nodejs\nruntime:\n  node: 22\n  database: none\n"
	if err := os.WriteFile(filepath.Join(dir, "tainer.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func wantPinned(t *testing.T, out []byte, exit int) {
	t.Helper()
	want := `{"v":1,"error":{"code":"pod-pinned","message":"` + pinnedMessage + `"}}` + "\n"
	if string(out) != want || exit != 1 {
		t.Errorf("out=%q exit=%d, want %q exit 1", out, exit, want)
	}
}

func TestStopControlIsPinned(t *testing.T) {
	for name, args := range map[string][]string{
		"json":  {"--json"},
		"plain": {"--plain"},
	} {
		t.Run(name, func(t *testing.T) {
			inProjectDir(t, "control")
			out, exit := failureHarness(t, name == "json", func() { cmdStop(args) })
			if name == "json" {
				wantPinned(t, out, exit)
			} else if exit != 1 {
				t.Errorf("exit = %d, want 1", exit)
			}
		})
	}
}

func TestDestroyControlIsPinned(t *testing.T) {
	for _, extra := range [][]string{nil, {"--clean"}, {"--nuke", "--yes"}} {
		inProjectDir(t, "control")
		out, exit := failureHarness(t, true, func() { cmdDestroy(append([]string{"--json"}, extra...)) })
		wantPinned(t, out, exit)
	}
}

func TestPinnedOverrideBypasses(t *testing.T) {
	override, rest := stripPinnedOverride([]string{"--json", "--pinned-override", "control"})
	if !override || !reflect.DeepEqual(rest, []string{"--json", "control"}) {
		t.Errorf("override=%v rest=%v", override, rest)
	}
	if override, rest := stripPinnedOverride([]string{"control"}); override || len(rest) != 1 {
		t.Errorf("override=%v rest=%v", override, rest)
	}
	if err := guardPinned("control", true); err != nil {
		t.Errorf("override should bypass the guard: %v", err)
	}
	if err := guardPinned("control", false); err == nil || err.Error() != pinnedMessage || errCode(err) != "pod-pinned" {
		t.Errorf("guard = %v (%s)", err, errCode(err))
	}
	if err := guardPinned("blog", false); err != nil {
		t.Errorf("ordinary project guarded: %v", err)
	}
	if err := guardInitName("control", true); err != nil {
		t.Errorf("init override: %v", err)
	}
}

func TestListExcludesControl(t *testing.T) {
	all := map[string]registry.Project{"blog": {Type: "wordpress"}, "control": {Type: "svelte"}}
	got := visibleProjects(all)
	if _, ok := got["control"]; ok || len(got) != 1 {
		t.Errorf("visibleProjects = %v", got)
	}
	if len(all) != 2 {
		t.Error("input map was mutated")
	}
}

func TestStatusAllExcludesControl(t *testing.T) {
	got := visiblePods([]pod.Pod{{Name: "blog"}, {Name: "control"}, {Name: "shop"}})
	if len(got) != 2 || got[0].Name != "blog" || got[1].Name != "shop" {
		t.Errorf("visiblePods = %v", got)
	}
}

func TestInitRefusesReservedName(t *testing.T) {
	err := guardInitName("control", false)
	if err == nil || errCode(err) != "invalid-arguments" || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("guardInitName = %v (%s)", err, errCode(err))
	}
	if err := guardInitName("blog", false); err != nil {
		t.Errorf("ordinary name refused: %v", err)
	}
	// Through the command: explicit name, and name derived from cwd.
	out, exit := failureHarness(t, true, func() { cmdInit([]string{"--json", "node", "control"}) })
	if exit != 1 || !strings.Contains(string(out), `"code":"invalid-arguments"`) {
		t.Errorf("explicit: out=%s exit=%d", out, exit)
	}
	dir := filepath.Join(t.TempDir(), "control")
	_ = os.Mkdir(dir, 0o755)
	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(orig)
	out, exit = failureHarness(t, true, func() { cmdInit([]string{"--json", "node"}) })
	if exit != 1 || !strings.Contains(string(out), `"code":"invalid-arguments"`) {
		t.Errorf("cwd-derived: out=%s exit=%d", out, exit)
	}
}

func TestInitSizeFlag(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    manifest.PodSize
		wantErr bool
	}{
		{"", manifest.PodSizeSmall, false},
		{"nano", manifest.PodSizeNano, false},
		{"xxl", manifest.PodSizeXXL, false},
		{"huge", "", true},
		{"custom", "", true},
	} {
		got, err := parseInitSize(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseInitSize(%q) = %q, %v", c.in, got, err)
		}
		if err != nil {
			if errCode(err) != "invalid-arguments" || !strings.Contains(err.Error(), "nano, small, medium, large, xlarge, xxl") {
				t.Errorf("error = %v (%s)", err, errCode(err))
			}
		}
	}
	for _, args := range [][]string{{"--json", "node", "x", "--size", "huge"}, {"--json", "node", "x", "--size=huge"}} {
		out, exit := failureHarness(t, true, func() { cmdInit(args) })
		if exit != 1 || !strings.Contains(string(out), `"code":"invalid-arguments"`) || !strings.Contains(string(out), "xxl") {
			t.Errorf("%v: out=%s exit=%d", args, out, exit)
		}
	}
}
