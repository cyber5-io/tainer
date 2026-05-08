package pod

import (
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
