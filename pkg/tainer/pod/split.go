package pod

import (
	"fmt"

	units "github.com/docker/go-units"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// PodBudget returns the pod's aggregate memory (bytes) and CPU (cores).
// Presets come from the manifest's size table; custom reads pod.memory / pod.cpu.
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
	e, ok := manifest.BudgetFor(m.Pod.Size)
	if !ok {
		return 0, 0, fmt.Errorf("unknown pod size %q", m.Pod.Size)
	}
	b, _ := units.RAMInBytes(e.Memory)
	return b, e.CPU, nil
}
