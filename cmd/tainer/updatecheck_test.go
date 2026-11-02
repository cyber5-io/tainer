package main

import (
	"strings"
	"testing"
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
