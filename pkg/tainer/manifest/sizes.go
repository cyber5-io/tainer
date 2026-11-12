package manifest

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	units "github.com/docker/go-units"
)

// NearestPodSize is the named size a leftover custom budget runs as. Memory
// decides, by ratio (3G is nearer 4G than 2G); without memory, CPU decides.
// Ties go to the larger size. This is the one place the mapping rule lives.
func NearestPodSize(memBytes int64, cpu float64) PodSize {
	best, bestDist := DefaultPodSize, math.Inf(1)
	for _, s := range NamedPodSizes() {
		b := podBudgets[s]
		var dist float64
		if memBytes > 0 {
			m, _ := units.RAMInBytes(b.Memory)
			dist = math.Abs(math.Log(float64(memBytes) / float64(m)))
		} else {
			dist = math.Abs(cpu - b.CPU)
		}
		// Sizes run smallest first, so <= lets a tie go to the larger.
		if dist <= bestDist+1e-9 {
			best, bestDist = s, dist
		}
	}
	return best
}

// mapLegacyPodBudget turns a pod.memory / pod.cpu budget (the retired
// custom size) into the nearest named size, noting what it was in
// MappedFrom. Only the legacy shapes map: `size: custom` with a budget, a
// budget with no size, and the per-container shape. A bare `size: custom`
// has nothing to map from, and a named size beside a budget was never valid;
// both are refused.
func mapLegacyPodBudget(m *Manifest) error {
	p := m.Pod
	if p == nil {
		return nil
	}
	hasBudget := p.Memory != "" || p.CPU != 0 || len(p.Containers) > 0
	if hasBudget && p.Size != "" && p.Size != PodSizeCustom {
		return fmt.Errorf("pod.memory/pod.cpu can't be combined with pod.size %q — remove them (pods use named sizes)", p.Size)
	}
	if p.Memory == "" && len(p.Containers) > 0 {
		// The older per-container shape: its total is the pod's budget.
		var total int64
		var cpu float64
		for _, lim := range p.Containers {
			b, err := units.RAMInBytes(lim.Memory)
			if err != nil {
				return fmt.Errorf("pod.containers memory %q: %w", lim.Memory, err)
			}
			total += b
			cpu += lim.CPU
		}
		p.Memory, p.CPU = fmt.Sprintf("%dM", total/(1024*1024)), cpu
	}
	p.Containers = nil
	if p.Memory == "" && p.CPU == 0 {
		if p.Size == PodSizeCustom {
			return fmt.Errorf("pod.size %q is no longer supported (expected %s)", PodSizeCustom, namedSizeList())
		}
		return nil
	}
	var mem int64
	if p.Memory != "" {
		b, err := units.RAMInBytes(p.Memory)
		if err != nil {
			return fmt.Errorf("pod.memory %q: %w", p.Memory, err)
		}
		mem = b
	}
	var from []string
	if p.Memory != "" {
		from = append(from, p.Memory)
	}
	if p.CPU != 0 {
		from = append(from, strconv.FormatFloat(p.CPU, 'f', -1, 64)+" CPU")
	}
	p.Size = NearestPodSize(mem, p.CPU)
	p.MappedFrom = strings.Join(from, ", ")
	p.Memory, p.CPU = "", 0
	return nil
}

// SizeNotice is the one-line note start prints when a leftover budget was
// mapped to a named size; "" otherwise.
func SizeNotice(m *Manifest) string {
	if m.Pod == nil || m.Pod.MappedFrom == "" {
		return ""
	}
	b := podBudgets[m.Pod.Size]
	return fmt.Sprintf("custom pod sizes are no longer supported: %s now runs as %s (%s, %s CPU) — choose a size with tainer config set pod.size <size>",
		m.Pod.MappedFrom, m.Pod.Size, b.Memory, strconv.FormatFloat(b.CPU, 'f', -1, 64))
}

func namedSizeList() string {
	names := make([]string, 0, 6)
	for _, s := range NamedPodSizes() {
		names = append(names, string(s))
	}
	return orList(names)
}
