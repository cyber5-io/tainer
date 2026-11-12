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

// A retired custom budget runs on its mapped named size's budget.
func TestPodBudgetForAMappedCustomSize(t *testing.T) {
	m, err := manifest.ParseBytes([]byte("version: 2\nproject:\n  name: d\n  type: nodejs\n  domain: d.tainer.me\nruntime:\n  node: \"22\"\n  database: none\npod:\n  size: custom\n  memory: 3G\n  cpu: 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	mem, cpu, err := PodBudget(m)
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	if mem != 4*1024*1024*1024 || cpu != 4 {
		t.Errorf("custom 3G/4 should run on large's 4G/4, got %d/%v", mem, cpu)
	}
}

func TestPodBudgetErrors(t *testing.T) {
	if _, _, err := PodBudget(&manifest.Manifest{}); err == nil {
		t.Errorf("nil pod config should error")
	}
	if _, _, err := PodBudget(mkPod(manifest.PodSizeCustom)); err == nil {
		t.Errorf("an unmapped custom size has no budget")
	}
}
