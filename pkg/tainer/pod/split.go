package pod

import (
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// Limits is the per-container resource budget produced by Split.
// Memory is the human-shorthand string (Docker SDK accepts it via
// units.RAMInBytes); CPU is fractional cores (Docker NanoCPUs / 1e9).
type Limits struct {
	Memory string
	CPU    float64
}

// Split returns a role -> Limits map computed from the manifest's
// pod.size preset and project type. Custom sizes pass through the
// user's per-container overrides verbatim.
func Split(m *manifest.Manifest) (map[string]Limits, error) {
	if m.Pod == nil {
		return nil, fmt.Errorf("pod: Split: manifest has no pod config")
	}
	roles := RolesForType(m.Project.Type)

	if m.Pod.Size == manifest.PodSizeCustom {
		out := make(map[string]Limits, len(roles))
		for _, r := range roles {
			lim, ok := m.Pod.Containers[r]
			if !ok {
				return nil, fmt.Errorf("pod.size=custom but pod.containers.%s is missing", r)
			}
			out[r] = Limits{Memory: lim.Memory, CPU: lim.CPU}
		}
		return out, nil
	}

	tbl, ok := splitTable[len(roles)][m.Pod.Size]
	if !ok {
		return nil, fmt.Errorf("no split for %d-container type at size %q", len(roles), m.Pod.Size)
	}
	out := make(map[string]Limits, len(roles))
	for i, r := range roles {
		out[r] = tbl[i]
	}
	return out, nil
}

// splitTable[role_count][size] = []Limits indexed in role order
// (web, app, db) for 3-container types or (web, db) for 2-container.
var splitTable = map[int]map[manifest.PodSize][]Limits{
	3: {
		manifest.PodSizeNano:   {{"64M", 0.1}, {"320M", 0.25}, {"128M", 0.15}},
		manifest.PodSizeSmall:  {{"128M", 0.25}, {"640M", 0.5}, {"256M", 0.25}},
		manifest.PodSizeMedium: {{"256M", 0.5}, {"1280M", 1.0}, {"512M", 0.5}},
		manifest.PodSizeLarge:  {{"512M", 1.0}, {"2560M", 2.0}, {"1G", 1.0}},
		manifest.PodSizeXLarge: {{"1G", 1.0}, {"5G", 2.0}, {"2G", 1.0}},
		manifest.PodSizeXXL:    {{"2G", 1.0}, {"10G", 6.0}, {"4G", 1.0}},
	},
	2: {
		manifest.PodSizeNano:   {{"384M", 0.35}, {"128M", 0.15}},
		manifest.PodSizeSmall:  {{"768M", 0.75}, {"256M", 0.25}},
		manifest.PodSizeMedium: {{"1536M", 1.5}, {"512M", 0.5}},
		manifest.PodSizeLarge:  {{"3G", 3.0}, {"1G", 1.0}},
		manifest.PodSizeXLarge: {{"6G", 3.0}, {"2G", 1.0}},
		manifest.PodSizeXXL:    {{"12G", 7.0}, {"4G", 1.0}},
	},
}
