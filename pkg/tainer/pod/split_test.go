package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func mkPod(size manifest.PodSize) *manifest.Manifest {
	return &manifest.Manifest{
		Version: 2,
		Project: manifest.ProjectConfig{Name: "demo", Type: manifest.TypeNodeJS},
		Runtime: manifest.RuntimeConfig{Node: "24", Database: manifest.DatabasePostgres},
		Pod:     &manifest.PodConfig{Size: size},
	}
}

func TestPodBudgetPresets(t *testing.T) {
	const miB = 1024 * 1024
	cases := []struct {
		size    manifest.PodSize
		wantMem int64
		wantCPU float64
	}{
		{manifest.PodSizeNano, 512 * miB, 1},
		{manifest.PodSizeSmall, 1024 * miB, 1},
		{manifest.PodSizeMedium, 2048 * miB, 2},
		{manifest.PodSizeLarge, 4096 * miB, 4},
		{manifest.PodSizeXLarge, 8192 * miB, 6},
		{manifest.PodSizeXXL, 16384 * miB, 8},
	}
	for _, c := range cases {
		mem, cpu, err := PodBudget(mkPod(c.size))
		if err != nil {
			t.Fatalf("%s: %v", c.size, err)
		}
		if mem != c.wantMem || cpu != c.wantCPU {
			t.Errorf("%s: got %d/%v, want %d/%v", c.size, mem, cpu, c.wantMem, c.wantCPU)
		}
	}
}

func TestPodBudgetCustom(t *testing.T) {
	m := mkPod(manifest.PodSizeCustom)
	m.Pod.Memory = "3G"
	m.Pod.CPU = 4
	mem, cpu, err := PodBudget(m)
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	if mem != 3*1024*1024*1024 || cpu != 4 {
		t.Errorf("custom: got %d/%v, want %d/4", mem, cpu, int64(3*1024*1024*1024))
	}
}

func TestPodBudgetErrors(t *testing.T) {
	if _, _, err := PodBudget(&manifest.Manifest{}); err == nil {
		t.Errorf("nil pod config should error")
	}
	bad := mkPod(manifest.PodSizeCustom)
	bad.Pod.Memory = "not-a-size"
	bad.Pod.CPU = 1
	if _, _, err := PodBudget(bad); err == nil {
		t.Errorf("unparseable custom memory should error")
	}
}
