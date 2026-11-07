package main

import (
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
)

func TestUpdateCheckRejectsCombinedModes(t *testing.T) {
	for _, args := range [][]string{{"--check", "--all"}, {"--check", "--base", "wordpress"}, {"--check", "blog"}} {
		out, exit := failureHarness(t, true, func() { cmdUpdate(append([]string{"--json"}, args...)) })
		if exit != 1 || !strings.Contains(string(out), `"invalid-arguments"`) {
			t.Errorf("%v: exit=%d out=%s", args, exit, out)
		}
	}
}

func TestUpdateCheckDaemonDownEmitsEnvelope(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix://"+t.TempDir()+"/missing.sock")
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"--json", "--check"}) })
	if exit != 1 || !strings.Contains(string(out), `"code":"stack-unreachable"`) {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
}

func TestUpdateCheckShowsHeldControlPod(t *testing.T) {
	rep := imagecheck.Report{UpToDate: 2, Held: 1, Pods: []imagecheck.PodReport{
		{Name: "blog"},
		{Name: "control", Control: &imagecheck.ControlCheck{Running: "1.0.0", Stable: "1.2.0", MinTainer: "1.4.0", State: imagecheck.ControlNeedsNewerTainer}},
	}}
	held := heldPods(rep)
	if len(held) != 1 || held[0].Name != "control" {
		t.Fatalf("heldPods = %+v", held)
	}
	if got := heldDetail(held[0]); got != "dashboard 1.2.0 needs tainer 1.4.0 or later" {
		t.Errorf("heldDetail = %q", got)
	}
	if got := updateCheckSummary(rep); !strings.HasSuffix(got, " · 1 needs a newer tainer") {
		t.Errorf("summary = %q", got)
	}
	rep.Held = 0
	if got := updateCheckSummary(rep); strings.Contains(got, "newer tainer") {
		t.Errorf("summary without held pods = %q", got)
	}
}
