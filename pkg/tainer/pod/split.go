package pod

import (
	"fmt"

	units "github.com/docker/go-units"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// podBudgetTable maps a size preset to its absolute pod total. Smart pods:
// a size names ONE budget shared by every container in the pod — enforced
// on a pod-level cgroup, so any container can burst into budget its
// siblings aren't using. How many containers the pod runs is irrelevant.
var podBudgetTable = map[manifest.PodSize]struct {
	mem string
	cpu float64
}{
	manifest.PodSizeNano:   {"512M", 1},
	manifest.PodSizeSmall:  {"1G", 1},
	manifest.PodSizeMedium: {"2G", 2},
	manifest.PodSizeLarge:  {"4G", 4},
	manifest.PodSizeXLarge: {"8G", 6},
	manifest.PodSizeXXL:    {"16G", 8},
}

// PodBudget returns the pod's aggregate memory (bytes) and CPU (cores).
// Presets come from podBudgetTable; custom reads pod.memory / pod.cpu.
func PodBudget(m *manifest.Manifest) (memBytes int64, cpuCores float64, err error) {
	if m.Pod == nil {
		return 0, 0, fmt.Errorf("pod: PodBudget: manifest has no pod config")
	}
	if m.Pod.Size == manifest.PodSizeCustom {
		b, perr := units.RAMInBytes(m.Pod.Memory)
		if perr != nil {
			return 0, 0, fmt.Errorf("pod.memory %q: %w", m.Pod.Memory, perr)
		}
		return b, m.Pod.CPU, nil
	}
	e, ok := podBudgetTable[m.Pod.Size]
	if !ok {
		return 0, 0, fmt.Errorf("unknown pod size %q", m.Pod.Size)
	}
	b, _ := units.RAMInBytes(e.mem)
	return b, e.cpu, nil
}
