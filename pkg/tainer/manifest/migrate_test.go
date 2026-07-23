package manifest

import "testing"

// A version:1 manifest that already carried a pod section must keep its size
// through migration. Regression test: the migration used to hardcode small,
// silently downsizing every such project and OOM-killing memory-hungry apps
// (e.g. a SvelteKit/esbuild dev server starved at small's 640M).
func TestMigrateV1PreservesExistingPodSize(t *testing.T) {
	raw := []byte(`version: 1
project:
    name: demo
    type: nodejs
    domain: demo.tainer.me
runtime:
    node: "24"
    database: postgres
pod:
    size: medium
`)
	m, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if m.Version != 2 {
		t.Fatalf("expected migration to v2, got version %d", m.Version)
	}
	got := "nil"
	if m.Pod != nil {
		got = string(m.Pod.Size)
	}
	if got != string(PodSizeMedium) {
		t.Errorf("pod size after migration: got %q, want %q", got, PodSizeMedium)
	}
}

// A true legacy 0.2.x manifest (no pod section) still defaults to small.
func TestMigrateV1DefaultsPodSizeWhenAbsent(t *testing.T) {
	raw := []byte(`version: 1
project:
    name: demo
    type: wordpress
    domain: demo.tainer.me
runtime:
    php: "8.3"
    database: mariadb
`)
	m, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	got := "nil"
	if m.Pod != nil {
		got = string(m.Pod.Size)
	}
	if got != string(PodSizeSmall) {
		t.Errorf("pod size for pod-less v1: got %q, want %q (default)", got, PodSizeSmall)
	}
}
