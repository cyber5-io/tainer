package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tain-129: custom pod sizes are gone; a budget still in tainer.yaml runs as
// the nearest named size.

const gib = 1 << 30

func TestNearestPodSize(t *testing.T) {
	cases := []struct {
		mem  int64
		cpu  float64
		want PodSize
	}{
		{512 << 20, 1, PodSizeNano},
		{1 * gib, 1, PodSizeSmall},
		{3 * gib, 2, PodSizeLarge},     // 3G is nearer 4G (x1.33) than 2G (x1.5)
		{1536 << 20, 1, PodSizeMedium}, // 1.5G: x1.33 to 2G beats x1.5 to 1G
		{6 * gib, 6, PodSizeXLarge},
		{64 * gib, 16, PodSizeXXL},
		{100 << 20, 0.5, PodSizeNano},
		{0, 2, PodSizeMedium}, // no memory: CPU decides
		{0, 1, PodSizeSmall},  // nano and small both have 1 CPU: the larger wins
		{0, 5, PodSizeXLarge}, // 4 and 6 are equally near: the larger wins
	}
	for _, c := range cases {
		if got := NearestPodSize(c.mem, c.cpu); got != c.want {
			t.Errorf("NearestPodSize(%d, %g) = %s, want %s", c.mem, c.cpu, got, c.want)
		}
	}
}

func withPod(pod string) string {
	return strings.Replace(nodeYAML, "  database: none\n", "  database: none\npod:\n"+pod, 1)
}

func TestCustomBudgetLoadsAsTheNearestNamedSize(t *testing.T) {
	m, err := ParseBytes([]byte(withPod("  size: custom\n  memory: 3G\n  cpu: 2\n")))
	if err != nil {
		t.Fatal(err)
	}
	if m.Pod.Size != PodSizeLarge || m.Pod.Memory != "" || m.Pod.CPU != 0 || m.Pod.MappedFrom != "3G, 2 CPU" {
		t.Errorf("pod %+v", m.Pod)
	}
	if got := SizeNotice(m); got != "custom pod sizes are no longer supported: 3G, 2 CPU now runs as large (4G, 4 CPU) — choose a size with tainer config set pod.size <size>" {
		t.Errorf("notice %q", got)
	}
	plain, _ := ParseBytes([]byte(nodeYAML))
	if SizeNotice(plain) != "" || plain.Pod.MappedFrom != "" {
		t.Errorf("a named size has no notice")
	}
}

func TestLeftoverBudgetShapesMap(t *testing.T) {
	cases := []struct {
		name, pod string
		want      PodSize
		from      string
	}{
		{"memory only", "  memory: 2G\n", PodSizeMedium, "2G"},
		{"cpu only", "  size: custom\n  cpu: 8\n", PodSizeXXL, "8 CPU"},
		{"legacy per-container", "  size: custom\n  containers:\n    app:\n      memory: 1G\n      cpu: 1\n    web:\n      memory: 1G\n      cpu: 1\n", PodSizeMedium, "2048M, 2 CPU"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := ParseBytes([]byte(withPod(c.pod)))
			if err != nil {
				t.Fatal(err)
			}
			if m.Pod.Size != c.want || m.Pod.MappedFrom != c.from || m.Pod.Containers != nil {
				t.Errorf("pod %+v", m.Pod)
			}
		})
	}
}

func TestCustomWithoutABudgetAndBadMemoryAreRefused(t *testing.T) {
	_, err := ParseBytes([]byte(withPod("  size: custom\n")))
	if err == nil || err.Error() != `pod.size "custom" is no longer supported (expected nano, small, medium, large, xlarge or xxl)` {
		t.Errorf("custom alone: %v", err)
	}
	if _, err := ParseBytes([]byte(withPod("  size: custom\n  memory: lots\n  cpu: 2\n"))); err == nil || !strings.Contains(err.Error(), `pod.memory "lots"`) {
		t.Errorf("bad memory: %v", err)
	}
	if _, err := ParseBytes([]byte(withPod("  size: huge\n"))); err == nil || err.Error() != `invalid pod size: "huge" (expected nano, small, medium, large, xlarge or xxl)` {
		t.Errorf("unknown size: %v", err)
	}
}

// A named size beside a budget was never valid: the explicit size must not
// silently lose to stray lines, so it is refused rather than mapped.
func TestNamedSizeWithAStrayBudgetIsRefused(t *testing.T) {
	for _, pod := range []string{
		"  size: small\n  memory: 6G\n",
		"  size: large\n  cpu: 2\n",
		"  size: medium\n  containers:\n    app:\n      memory: 1G\n      cpu: 1\n",
	} {
		_, err := ParseBytes([]byte(withPod(pod)))
		if err == nil || !strings.Contains(err.Error(), "pod.memory/pod.cpu can't be combined with pod.size") {
			t.Errorf("%q: %v", pod, err)
		}
	}
}

func TestVersion1CustomPodMaps(t *testing.T) {
	v1 := "version: 1\nproject:\n  name: old\n  type: nodejs\n  domain: old.tainer.me\nruntime:\n  node: \"22\"\n  database: none\npod:\n  size: custom\n  memory: 512M\n  cpu: 1\n"
	m, err := ParseBytes([]byte(v1))
	if err != nil || m.Pod.Size != PodSizeNano {
		t.Errorf("v1: %v %+v", err, m)
	}
}

func TestOverlayBudgetMapsAndANamedOverlayClearsTheNote(t *testing.T) {
	write := func(t *testing.T, base, overlay string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(base), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, LocalOverlayName), []byte(overlay), 0o644); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, FileName)
	}
	m, err := Load(write(t, nodeYAML, "pod:\n  size: custom\n  memory: 8G\n  cpu: 6\n"))
	if err != nil || m.Pod.Size != PodSizeXLarge || m.Pod.MappedFrom != "8G, 6 CPU" {
		t.Errorf("overlay budget: %v %+v", err, m)
	}
	m, err = Load(write(t, withPod("  size: custom\n  memory: 3G\n  cpu: 2\n"), "pod:\n  size: nano\n"))
	if err != nil || m.Pod.Size != PodSizeNano || m.Pod.MappedFrom != "" {
		t.Errorf("named overlay: %v %+v", err, m)
	}
}

func TestOptionsShowTheMappedSize(t *testing.T) {
	o := mustOptions(t, withPod("  size: custom\n  memory: 3G\n  cpu: 2\n"))
	if o.Size.Current != PodSizeLarge || o.Size.MappedFrom != "3G, 2 CPU" {
		t.Errorf("%+v", o.Size)
	}
	if mustOptions(t, nodeYAML).Size.MappedFrom != "" {
		t.Errorf("named size has no mappedFrom")
	}
}

func TestConfigSetRefusesBudgetKeysAndCustom(t *testing.T) {
	for _, key := range []string{"pod.memory", "pod.cpu"} {
		_, _, _, err := ApplySetting([]byte(nodeYAML), key, "4G")
		var ie *InvalidSettingError
		if !errors.As(err, &ie) || err.Error() != key+" can't be set: pods use named sizes (nano 512M, small 1G, medium 2G, large 4G, xlarge 8G or xxl 16G) — set pod.size instead" {
			t.Errorf("%s: %v", key, err)
		}
	}
	_, _, _, err := ApplySetting([]byte(nodeYAML), "pod.size", "custom")
	if err == nil || !strings.Contains(err.Error(), `invalid pod size: "custom" (expected nano, small, medium, large, xlarge or xxl)`) {
		t.Errorf("custom: %v", err)
	}
}

func TestConfigSetOnACustomFileWritesTheNamedSize(t *testing.T) {
	src := withPod("  size: custom\n  memory: 3G\n  cpu: 2\n")
	// Choosing the size it already maps to still rewrites the file: the budget goes, the name stays.
	out, stored, changed, err := ApplySetting([]byte(src), "pod.size", "large")
	if err != nil || !changed || stored != "large" {
		t.Fatalf("changed=%v stored=%q err=%v", changed, stored, err)
	}
	got := string(out)
	if !strings.Contains(got, "  size: large\n") || strings.Contains(got, "memory") || strings.Contains(got, "cpu") {
		t.Errorf("file:\n%s", got)
	}
	// Setting another key leaves the budget lines alone; the file still loads as large.
	out, _, _, err = ApplySetting([]byte(src), "runtime.node", "24")
	if err != nil || !strings.Contains(string(out), "memory: 3G") {
		t.Errorf("other key: %v\n%s", err, out)
	}
}
