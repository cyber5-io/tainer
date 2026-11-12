package pod

import (
	"fmt"

	units "github.com/docker/go-units"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// PodBudget returns the pod's aggregate memory (bytes) and CPU (cores).
// Budgets come from the manifest's size table (a retired custom budget was
// already mapped to a named size when the manifest loaded).
func PodBudget(m *manifest.Manifest) (memBytes int64, cpuCores float64, err error) {
	if m.Pod == nil {
		return 0, 0, fmt.Errorf("pod: PodBudget: manifest has no pod config")
	}
	e, ok := manifest.BudgetFor(m.Pod.Size)
	if !ok {
		return 0, 0, fmt.Errorf("unknown pod size %q", m.Pod.Size)
	}
	b, _ := units.RAMInBytes(e.Memory)
	return b, e.CPU, nil
}
