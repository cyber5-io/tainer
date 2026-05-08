package pod

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// PodIDMin and PodIDMax bound the auto-allocator pool. The displayed
// pod ID is the literal 4-digit prefix of every TCP host port the pod
// uses (pod 3012 → ports 30120-30129).
const (
	PodIDMin = 3001
	PodIDMax = 3999
)

// SlotsPath returns the canonical state file path under the user's home
// directory. Callers can override via SlotsPathFor for tests.
func SlotsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cyberstack", "tainer", "slots.json")
}

// SlotEntry is the per-pod record persisted in slots.json.
type SlotEntry struct {
	PodID   int            `json:"pod_id"`
	TCPPins map[string]int `json:"tcp_pins,omitempty"`
}

type slotsFile struct {
	Pods map[string]*SlotEntry `json:"pods"`
}

var registryMu sync.Mutex

// AllocatePodID returns the existing pod ID for `name` if one is recorded,
// else allocates the lowest free integer in [PodIDMin, PodIDMax]. Persists
// the assignment to the canonical slots.json. Returns an error if the
// pool is exhausted.
func AllocatePodID(name string) (int, error) {
	return AllocatePodIDAt(SlotsPath(), name)
}

// AllocatePodIDAt is AllocatePodID with an explicit file path (for tests).
func AllocatePodIDAt(path, name string) (int, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := loadSlots(path)
	if err != nil {
		return 0, err
	}
	if entry, ok := f.Pods[name]; ok {
		return entry.PodID, nil
	}
	id, err := pickFreeID(f)
	if err != nil {
		return 0, err
	}
	f.Pods[name] = &SlotEntry{PodID: id}
	if err := saveSlots(path, f); err != nil {
		return 0, err
	}
	return id, nil
}

// FreePodID removes the registry entry for `name`. Idempotent; missing
// entries are ignored. Returns an error only on filesystem I/O failure.
func FreePodID(name string) error { return FreePodIDAt(SlotsPath(), name) }

func FreePodIDAt(path, name string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := loadSlots(path)
	if err != nil {
		return err
	}
	if _, ok := f.Pods[name]; !ok {
		return nil
	}
	delete(f.Pods, name)
	return saveSlots(path, f)
}

// LookupPodID returns the assigned pod ID for `name`, or 0 if unknown.
// Errors only on filesystem I/O failure.
func LookupPodID(name string) (int, error) {
	return LookupPodIDAt(SlotsPath(), name)
}

func LookupPodIDAt(path, name string) (int, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := loadSlots(path)
	if err != nil {
		return 0, err
	}
	if entry, ok := f.Pods[name]; ok {
		return entry.PodID, nil
	}
	return 0, nil
}

// PinTCPPort records a manifest-pinned host port for (pod, role).
// Subsequent allocators across all pods skip this port.
func PinTCPPort(pod, role string, port int) error {
	return PinTCPPortAt(SlotsPath(), pod, role, port)
}

func PinTCPPortAt(path, pod, role string, port int) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := loadSlots(path)
	if err != nil {
		return err
	}
	entry, ok := f.Pods[pod]
	if !ok {
		return fmt.Errorf("pin: pod %q not allocated yet", pod)
	}
	if entry.TCPPins == nil {
		entry.TCPPins = map[string]int{}
	}
	entry.TCPPins[role] = port
	return saveSlots(path, f)
}

// LookupPinnedPort returns the recorded pinned port for (pod, role), or 0.
func LookupPinnedPort(pod, role string) (int, error) {
	return LookupPinnedPortAt(SlotsPath(), pod, role)
}

func LookupPinnedPortAt(path, pod, role string) (int, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := loadSlots(path)
	if err != nil {
		return 0, err
	}
	if entry, ok := f.Pods[pod]; ok && entry.TCPPins != nil {
		return entry.TCPPins[role], nil
	}
	return 0, nil
}

func loadSlots(path string) (*slotsFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &slotsFile{Pods: map[string]*SlotEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f slotsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Pods == nil {
		f.Pods = map[string]*SlotEntry{}
	}
	return &f, nil
}

func saveSlots(path string, f *slotsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pickFreeID(f *slotsFile) (int, error) {
	used := map[int]bool{}
	for _, e := range f.Pods {
		used[e.PodID] = true
	}
	candidates := make([]int, 0, len(used))
	for id := PodIDMin; id <= PodIDMax; id++ {
		if !used[id] {
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return 0, fmt.Errorf("pod id pool exhausted (%d-%d)", PodIDMin, PodIDMax)
	}
	sort.Ints(candidates)
	return candidates[0], nil
}
