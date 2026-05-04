package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// memBytes converts our human shorthand back into raw bytes for the
// add-up assertion. Match the format produced by formatBytes.
func memBytes(s string) int64 {
	switch s {
	case "64M":
		return 64 << 20
	case "128M":
		return 128 << 20
	case "256M":
		return 256 << 20
	case "320M":
		return 320 << 20
	case "384M":
		return 384 << 20
	case "512M":
		return 512 << 20
	case "640M":
		return 640 << 20
	case "768M":
		return 768 << 20
	case "1280M":
		return 1280 << 20
	case "1536M":
		return 1536 << 20
	case "1G":
		return 1 << 30
	case "2G":
		return 2 << 30
	case "2560M":
		return 2560 << 20
	case "3G":
		return 3 << 30
	case "4G":
		return 4 << 30
	case "5G":
		return 5 << 30
	case "6G":
		return 6 << 30
	case "10G":
		return 10 << 30
	case "12G":
		return 12 << 30
	case "16G":
		return 16 << 30
	}
	return 0
}

func TestSplitWordPressSmallSums(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
	}
	split, err := Split(m)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	var totalMem int64
	var totalCPU float64
	for _, c := range split {
		totalMem += memBytes(c.Memory)
		totalCPU += c.CPU
	}
	if want := int64(1 << 30); totalMem != want {
		t.Errorf("WordPress small: total mem %d, want %d", totalMem, want)
	}
	if totalCPU != 1.0 {
		t.Errorf("WordPress small: total cpu %v, want 1.0", totalCPU)
	}
}

func TestSplitReactNanoSums(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeReact},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeNano},
	}
	split, err := Split(m)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(split) != 2 {
		t.Fatalf("React: want 2 containers, got %d", len(split))
	}
	var totalMem int64
	for _, c := range split {
		totalMem += memBytes(c.Memory)
	}
	if want := int64(512 << 20); totalMem != want {
		t.Errorf("React nano: total mem %d, want %d", totalMem, want)
	}
}

func TestSplitCustom(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress},
		Pod: &manifest.PodConfig{
			Size: manifest.PodSizeCustom,
			Containers: map[string]manifest.ContainerLimits{
				"web": {Memory: "200M", CPU: 0.3},
				"app": {Memory: "700M", CPU: 0.6},
				"db":  {Memory: "300M", CPU: 0.3},
			},
		},
	}
	split, err := Split(m)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if split["app"].Memory != "700M" || split["app"].CPU != 0.6 {
		t.Errorf("custom app: got %+v", split["app"])
	}
}

func TestSplitCustomMissingRole(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress},
		Pod: &manifest.PodConfig{
			Size: manifest.PodSizeCustom,
			Containers: map[string]manifest.ContainerLimits{
				"web": {Memory: "200M", CPU: 0.3},
				// missing "app" and "db"
			},
		},
	}
	_, err := Split(m)
	if err == nil {
		t.Fatal("want error for missing custom role, got nil")
	}
}
