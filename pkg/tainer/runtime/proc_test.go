//go:build !windows

package runtime

import (
	"os"
	"os/exec"
	"testing"
)

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("ProcessAlive(self) = false, want true")
	}
	// A PID from the kernel's usual range that cannot be us. Spawn-and-reap
	// a child to get a guaranteed-dead PID instead of guessing.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(cmd.Process.Pid) {
		t.Errorf("ProcessAlive(reaped child %d) = true, want false", cmd.Process.Pid)
	}
}

func TestSignalTermStopsChild(t *testing.T) {
	// Only ever signal a child we own — a reaped PID can be reused by an
	// unrelated process, and TERMing that would be a real casualty.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := SignalTerm(cmd.Process.Pid); err != nil {
		t.Fatalf("SignalTerm(live child) = %v", err)
	}
	err := cmd.Wait()
	if err == nil {
		t.Error("child exited cleanly, want killed by SIGTERM")
	}
}
