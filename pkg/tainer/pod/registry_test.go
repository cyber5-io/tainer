package pod

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRegistryAllocateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slots.json")
	id1, err := AllocatePodIDAt(path, "demo")
	if err != nil {
		t.Fatalf("Allocate demo: %v", err)
	}
	if id1 < 3001 || id1 > 3999 {
		t.Errorf("first pod id outside 3001-3999: %d", id1)
	}
	if got, _ := LookupPodIDAt(path, "demo"); got != id1 {
		t.Errorf("lookup mismatch: got %d, want %d", got, id1)
	}
	id2, err := AllocatePodIDAt(path, "blog")
	if err != nil {
		t.Fatalf("Allocate blog: %v", err)
	}
	if id2 == id1 {
		t.Errorf("two pods allocated the same id: %d", id1)
	}
}

func TestRegistryAllocateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slots.json")
	id1, _ := AllocatePodIDAt(path, "demo")
	id2, err := AllocatePodIDAt(path, "demo")
	if err != nil {
		t.Fatalf("re-allocate: %v", err)
	}
	if id1 != id2 {
		t.Errorf("re-allocate of same name should return same id: got %d, was %d", id2, id1)
	}
}

func TestRegistryFreeReleasesID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slots.json")
	id1, _ := AllocatePodIDAt(path, "demo")
	if err := FreePodIDAt(path, "demo"); err != nil {
		t.Fatalf("Free: %v", err)
	}
	if got, _ := LookupPodIDAt(path, "demo"); got != 0 {
		t.Errorf("lookup after free: got %d, want 0", got)
	}
	// Next pod should be able to reuse the slot.
	id2, _ := AllocatePodIDAt(path, "demo2")
	if id2 != id1 {
		// Not strictly required — slot reuse policy can pick lowest free.
		// But the lowest-free strategy means yes, reused.
		t.Errorf("freed slot not reused: id1=%d, id2=%d", id1, id2)
	}
}

func TestRegistryPinTCPPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slots.json")
	if _, err := AllocatePodIDAt(path, "demo"); err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if err := PinTCPPortAt(path, "demo", "weird", 45678); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	got, _ := LookupPinnedPortAt(path, "demo", "weird")
	if got != 45678 {
		t.Errorf("lookup pinned: got %d, want 45678", got)
	}
}

func TestRegistryAllocateSkipsPinnedPort(t *testing.T) {
	// Auto-derived pod_id*10 must never collide with a pinned port.
	// The pinned band is 40000-49000, the auto band is 30000-39999, so
	// they're disjoint by manifest validation. This test just exercises
	// the round-trip — collision is impossible by construction.
	dir := t.TempDir()
	path := filepath.Join(dir, "slots.json")
	id, _ := AllocatePodIDAt(path, "demo")
	port := id*10 + 1
	if port < 30000 || port > 39999 {
		t.Errorf("auto-derived port %d outside auto band (id %d)", port, id)
	}
}

// TestHelperSlotAllocator is not a test: it is the body of a separate tainer
// process for TestConcurrentProcessesGetDistinctPodIDs.
func TestHelperSlotAllocator(t *testing.T) {
	path, prefix := os.Getenv("TAINER_TEST_SLOTS"), os.Getenv("TAINER_TEST_SLOTS_PREFIX")
	if path == "" {
		t.Skip("helper process only")
	}
	for i := 0; i < 15; i++ {
		if _, err := AllocatePodIDAt(path, fmt.Sprintf("%s-%d", prefix, i)); err != nil {
			t.Fatal(err)
		}
	}
}

// Two tainer processes starting pods at once (a command and the detached
// control child) never get the same pod ID, and neither loses the other's.
func TestConcurrentProcessesGetDistinctPodIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slots.json")
	const procs = 6
	var cmds []*exec.Cmd
	for p := 0; p < procs; p++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSlotAllocator$")
		cmd.Env = append(os.Environ(), "TAINER_TEST_SLOTS="+path, fmt.Sprintf("TAINER_TEST_SLOTS_PREFIX=p%d", p))
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("allocator: %v", err)
		}
	}
	f, err := loadSlots(path)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for name, e := range f.Pods {
		if other, dup := seen[e.PodID]; dup {
			t.Errorf("pod ID %d given to both %s and %s", e.PodID, other, name)
		}
		seen[e.PodID] = name
	}
	if len(f.Pods) != procs*15 {
		t.Fatalf("slots has %d pods, want %d (a process's allocation was lost)", len(f.Pods), procs*15)
	}
}
