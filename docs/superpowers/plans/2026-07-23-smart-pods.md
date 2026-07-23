# Smart Pods Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a pod size one absolute budget (memory + CPU) enforced on a single pod-level cgroup that all the pod's containers share and burst within, replacing today's hard per-container limits.

**Architecture:** cgroup v2 hierarchy — a parent cgroup `tainer-<pod>` carries `memory.max`/`cpu.max` = the pod budget; each container is a leaf `tainer-<pod>/<role>` with no own limit (so any leaf can grow to the whole budget while siblings idle). tainer sends `CgroupParent` + the pod budget on every container's create; the cyberstack agent asserts the parent's limits idempotently and places the container as a limitless leaf. The network namespace model (leader-owned, `NetworkMode: container:<leader>`) is unchanged.

**Tech Stack:** Go, cgroup v2, crun/containerd (guest agent), protobuf (agent RPC), Docker-compat HTTP API. Two repos: `cyber-stack` (engine, `~/dev/cyber5-io/cyber-stack`) and `tainer` (`~/dev/cyber5-io/tainer`).

## Global Constraints

- Guest is **cgroup v2** (unified `0::/`); crun emits `cpu.max = "<quota> <period>"` with period 100000µs.
- cgroup-v2 "no internal processes": the parent `tainer-<pod>` holds only child cgroups; container processes live only in leaves.
- Pod cgroup path derives from the project name: `PodCgroup(project) = "tainer-" + project` (matches `tainer-<project>-<role>` container naming).
- Pod totals: nano 512M/1 · small 1G/1 · medium 2G/2 · large 4G/4 · xlarge 8G/6 · xxl 16G/8. Default `small`.
- Empty `CgroupParent` ⇒ current per-container behaviour (non-pod containers unaffected).
- Commit messages: **no** Co-Authored-By / AI mentions. User-facing text never says podman/docker/engine.
- CyberStack version bumps: engine to **0.7.0**. tainer stays 1.1.0 until its own release cut.
- Work on branches: `feat/smart-pods` in each repo (create before first commit; both repos are on their default branch otherwise).

---

## Phase 1 — Engine (cyber-stack)

### Task 1: Plumb `CgroupParent` + `OomScoreAdj` across the API→proto→agent boundary

**Files:**
- Modify: `internal/proto/container.proto:46-55` (HostConfig message)
- Regenerate: `internal/proto/gen/container.pb.go` (via the repo's proto gen)
- Modify: `internal/httpapi/containers.go:162-216` (parse HostConfig)
- Modify: `internal/agent/containerd/runtime.go:70-92` (Spec struct)
- Modify: `internal/agent/server.go` (map proto HostConfig → Spec — find where MemoryBytes is copied)

**Interfaces:**
- Produces: `agentpb.HostConfig.CgroupParent string`, `agentpb.HostConfig.OomScoreAdj int64`; `runtime.Spec.CgroupParent string`, `runtime.Spec.OomScoreAdj int64`. Both flow create-request → agent Spec. No behaviour change yet (values carried, not acted on).

- [ ] **Step 1: Add proto fields**

In `internal/proto/container.proto`, extend `HostConfig` (after `port_bindings = 7`):

```proto
  string cgroup_parent = 8;   // smart pods: shared pod cgroup path, e.g. "tainer-cyber5-io"
  int64  oom_score_adj = 9;   // smart pods: OOM victim bias (db negative, app 0/positive)
```

- [ ] **Step 2: Regenerate proto**

Run the repo's generator (check `Makefile` for the `proto` target):
`cd ~/dev/cyber5-io/cyber-stack && make proto` (or the documented `protoc` invocation).
Expected: `internal/proto/gen/container.pb.go` now has `GetCgroupParent()` and `GetOomScoreAdj()`.

- [ ] **Step 3: Carry the fields through the API parser**

In `internal/httpapi/containers.go`, the `HostConfig struct` (line ~162) add:

```go
		CgroupParent string `json:"CgroupParent"`
		OomScoreAdj  int64  `json:"OomScoreAdj"`
```

and where it builds `&agentpb.HostConfig{...}` (line ~211) add:

```go
				CgroupParent: body.HostConfig.CgroupParent,
				OomScoreAdj:  body.HostConfig.OomScoreAdj,
```

- [ ] **Step 4: Add Spec fields + map from proto**

In `internal/agent/containerd/runtime.go` `Spec` struct add:

```go
	// smart pods: when non-empty, this container is a leaf under the shared
	// pod cgroup <CgroupParent>/<name>; the parent carries the pod's
	// memory.max/cpu.max and this leaf gets no own limit.
	CgroupParent string
	OomScoreAdj  int64
```

In `internal/agent/server.go`, where the incoming `HostConfig` is copied into the `Spec` (search for `MemoryBytes:`), add:

```go
		CgroupParent: hc.GetCgroupParent(),
		OomScoreAdj:  hc.GetOomScoreAdj(),
```

- [ ] **Step 5: Build + commit**

Run: `cd ~/dev/cyber5-io/cyber-stack && go build ./...`
Expected: builds clean.

```bash
git checkout -b feat/smart-pods
git add internal/proto/container.proto internal/proto/gen/container.pb.go internal/httpapi/containers.go internal/agent/containerd/runtime.go internal/agent/server.go
git commit -m "feat(agent): plumb CgroupParent + OomScoreAdj through API/proto/spec"
```

---

### Task 2: Pod cgroup helpers (create, limit, prune)

**Files:**
- Create: `internal/agent/containerd/cgroup.go`
- Create: `internal/agent/containerd/cgroup_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func cgroup2Root() string` — returns `"/sys/fs/cgroup"` (unified mount).
  - `func ensurePodCgroup(parent string, memBytes uint64, nanoCPUs int64) error` — mkdir `<root>/<parent>`, enable `+memory +cpu` in the root's `cgroup.subtree_control`, write `memory.max` and `cpu.max` on the parent. Idempotent.
  - `func prunePodCgroup(parent string) error` — best-effort `rmdir <root>/<parent>` (only succeeds when empty).
  - `func cpuMaxString(nanoCPUs int64) string` — `"<quota> 100000"` (quota = nanoCPUs*100000/1e9), or `"max 100000"` when nanoCPUs==0.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/containerd/cgroup_test.go`:

```go
package containerd

import "testing"

func TestCPUMaxString(t *testing.T) {
	cases := []struct {
		nano int64
		want string
	}{
		{1_000_000_000, "100000 100000"}, // 1 core
		{2_000_000_000, "200000 100000"}, // 2 cores
		{6_000_000_000, "600000 100000"}, // 6 cores
		{0, "max 100000"},                // unlimited
	}
	for _, c := range cases {
		if got := cpuMaxString(c.nano); got != c.want {
			t.Errorf("cpuMaxString(%d) = %q, want %q", c.nano, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/dev/cyber5-io/cyber-stack && go test ./internal/agent/containerd/ -run TestCPUMaxString -v`
Expected: FAIL — `undefined: cpuMaxString`.

- [ ] **Step 3: Implement the helpers**

Create `internal/agent/containerd/cgroup.go`:

```go
package containerd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// cgroup2Root is the unified cgroup v2 mountpoint inside the guest.
func cgroup2Root() string { return "/sys/fs/cgroup" }

// cpuMaxString renders cgroup v2 cpu.max ("<quota> <period>"). Period is
// Docker's default 100ms. nanoCPUs==0 means unlimited ("max").
func cpuMaxString(nanoCPUs int64) string {
	const period = 100000
	if nanoCPUs <= 0 {
		return fmt.Sprintf("max %d", period)
	}
	quota := nanoCPUs * period / 1_000_000_000
	return fmt.Sprintf("%d %d", quota, period)
}

// ensurePodCgroup creates the pod's parent cgroup and writes its aggregate
// memory.max / cpu.max. Idempotent: every pod container calls this with the
// same budget, so re-asserting is safe. The parent caps the total usage of
// all its leaf containers; leaves carry no own limit.
func ensurePodCgroup(parent string, memBytes uint64, nanoCPUs int64) error {
	if parent == "" {
		return nil
	}
	root := cgroup2Root()
	// Make memory + cpu controllers available to new children of the root.
	// Best-effort: on a normally-configured host they are already enabled.
	_ = os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+memory +cpu"), 0o644)

	dir := filepath.Join(root, parent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pod cgroup %s: %w", dir, err)
	}
	if memBytes > 0 {
		if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatUint(memBytes, 10)), 0o644); err != nil {
			return fmt.Errorf("set memory.max on %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(cpuMaxString(nanoCPUs)), 0o644); err != nil {
		return fmt.Errorf("set cpu.max on %s: %w", dir, err)
	}
	return nil
}

// prunePodCgroup removes the pod's parent cgroup once its last leaf is gone.
// Best-effort: rmdir fails (harmlessly) while any leaf remains.
func prunePodCgroup(parent string) error {
	if parent == "" {
		return nil
	}
	return os.Remove(filepath.Join(cgroup2Root(), parent))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/dev/cyber5-io/cyber-stack && go test ./internal/agent/containerd/ -run TestCPUMaxString -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/containerd/cgroup.go internal/agent/containerd/cgroup_test.go
git commit -m "feat(agent): pod cgroup helpers (ensure/limit/prune, cgroup v2)"
```

---

### Task 3: Place containers as leaves under the pod cgroup

**Files:**
- Modify: `internal/agent/containerd/runtime.go` (`buildOCIConfig` — resources + a new `cgroupsPath` + `oomScoreAdj`; the create path calls `ensurePodCgroup`)

**Interfaces:**
- Consumes: `Spec.CgroupParent`, `Spec.OomScoreAdj`, `ensurePodCgroup`, from Tasks 1–2.
- Produces: containers created with `CgroupParent` set become limitless leaves under `<parent>/<name>`; the parent holds the budget.

- [ ] **Step 1: Gate leaf resources + set cgroupsPath in buildOCIConfig**

In `internal/agent/containerd/runtime.go`, the `// Resources` block (line ~255): wrap the per-container limit so it is **skipped** when the container is a pod leaf, and add `cgroupsPath` + `oomScoreAdj`. Replace the resources block with:

```go
	// Resources. Pod leaves (CgroupParent set) inherit the pod's aggregate
	// limit from the parent cgroup, so they carry no own memory/cpu cap.
	resources := map[string]interface{}{}
	if spec.CgroupParent == "" {
		if spec.MemoryBytes > 0 {
			resources["memory"] = map[string]interface{}{"limit": int64(spec.MemoryBytes)}
		}
		if spec.NanoCPUs > 0 {
			const period = 100000
			quota := spec.NanoCPUs * period / 1_000_000_000
			resources["cpu"] = map[string]interface{}{"period": uint64(period), "quota": quota}
		} else if spec.CPUShares > 0 {
			resources["cpu"] = map[string]interface{}{"shares": uint64(spec.CPUShares)}
		}
	}
```

Then, where `linuxConfig` is assembled (after `linuxConfig["namespaces"] = nsList`), add the pod-leaf cgroup path:

```go
	if spec.CgroupParent != "" {
		// Leaf under the shared pod cgroup: <parent>/<container-name>.
		linuxConfig["cgroupsPath"] = spec.CgroupParent + "/" + spec.Hostname
	}
```

(Use the container's name; `spec.Hostname` is set to the container name at create — verify against the caller and use the field that holds `tainer-<project>-<role>`.)

In the returned `process` map, add OOM bias:

```go
		"oomScoreAdj": int(spec.OomScoreAdj),
```

- [ ] **Step 2: Call ensurePodCgroup before container create**

Find the function that starts a container from a `Spec` (the caller of `buildOCIConfig`, e.g. `Create`/`Run` in runtime.go). Immediately before the container is created, add:

```go
	if spec.CgroupParent != "" {
		if err := ensurePodCgroup(spec.CgroupParent, spec.MemoryBytes, spec.NanoCPUs); err != nil {
			return fmt.Errorf("pod cgroup: %w", err)
		}
	}
```

- [ ] **Step 3: Build**

Run: `cd ~/dev/cyber5-io/cyber-stack && go build ./...`
Expected: builds clean. (Behavioural verification happens in the Phase 3 integration task on a live VM — unit-testing OCI-map assembly here would just restate the code.)

- [ ] **Step 4: Commit**

```bash
git add internal/agent/containerd/runtime.go
git commit -m "feat(agent): place pod containers as limitless leaves under shared cgroup"
```

---

### Task 4: Prune the pod cgroup on last-container removal + version bump

**Files:**
- Modify: `internal/agent/containerd/runtime.go` (container remove path — call `prunePodCgroup`)
- Modify: `internal/httpapi/version.go:14` and `Makefile:8` (version 0.7.0)

**Interfaces:**
- Consumes: `prunePodCgroup` (Task 2).

- [ ] **Step 1: Prune on remove**

In the container-removal function (search `Remove`/`Delete` in runtime.go), after the container's own cgroup/rootfs is torn down, add:

```go
	// Best-effort: remove the shared pod cgroup once its last leaf is gone.
	// rmdir fails harmlessly while other pod containers remain.
	if spec.CgroupParent != "" {
		_ = prunePodCgroup(spec.CgroupParent)
	}
```

(If the removal path doesn't have the `Spec`, derive the parent from stored container metadata, or attempt prune of the parent inferred from the container name prefix `tainer-<project>`. Use whichever the removal path already has in scope.)

- [ ] **Step 2: Bump engine version to 0.7.0**

`internal/httpapi/version.go:14`: `var CyberStackVersion = "0.7.0"`
`Makefile:8`: `VERSION := 0.7.0`

- [ ] **Step 3: Build + commit**

Run: `cd ~/dev/cyber5-io/cyber-stack && go build ./cmd/cyberstackd && go test ./internal/agent/containerd/`
Expected: builds clean, cgroup tests pass.

```bash
git add internal/agent/containerd/runtime.go internal/httpapi/version.go Makefile
git commit -m "feat(agent): prune pod cgroup on teardown; bump CyberStackVersion 0.7.0"
```

---

## Phase 2 — tainer

### Task 5: Pod budget model (replace the per-container split)

**Files:**
- Modify: `pkg/tainer/pod/split.go` (replace `Split`/`splitTable` with `PodBudget` + budget table)
- Modify/replace: `pkg/tainer/pod/split_test.go`

**Interfaces:**
- Consumes: `manifest.Manifest`, `manifest.PodSize*`, `manifest.PodConfig.Memory/CPU` (Task 6 adds these — this task uses them, so land Task 6's struct fields first OR add them here; do Task 6 before Task 5 if the compiler complains).
- Produces: `func PodBudget(m *manifest.Manifest) (memBytes int64, cpuCores float64, err error)` — the single pod total. Presets from `podBudgetTable`; `custom` from `pod.memory`/`pod.cpu`.

- [ ] **Step 1: Write the failing test**

Replace `pkg/tainer/pod/split_test.go` with:

```go
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
	cases := []struct {
		size    manifest.PodSize
		wantMem int64
		wantCPU float64
	}{
		{manifest.PodSizeNano, 512 * 1024 * 1024, 1},
		{manifest.PodSizeSmall, 1024 * 1024 * 1024, 1},
		{manifest.PodSizeMedium, 2048 * 1024 * 1024, 2},
		{manifest.PodSizeLarge, 4096 * 1024 * 1024, 4},
		{manifest.PodSizeXLarge, 8192 * 1024 * 1024, 6},
		{manifest.PodSizeXXL, 16384 * 1024 * 1024, 8},
	}
	for _, c := range cases {
		m := mkPod(c.size)
		mem, cpu, err := PodBudget(m)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/pod/ -run TestPodBudget -v`
Expected: FAIL — `undefined: PodBudget` (and split.go still has the old table).

- [ ] **Step 3: Replace split.go with the budget model**

Replace the body of `pkg/tainer/pod/split.go` with:

```go
package pod

import (
	"fmt"

	"github.com/docker/go-units"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// podBudgetTable maps a size preset to its absolute pod total.
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
```

(Delete the old `Limits` type, `Split`, and `splitTable`. If anything else imports `Split`/`Limits`, update those call sites — Task 7 rewires `start.go`, the main consumer.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/pod/ -run TestPodBudget -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dev/cyber5-io/tainer && git checkout -b feat/smart-pods
git add pkg/tainer/pod/split.go pkg/tainer/pod/split_test.go
git commit -m "feat(pod): pod-total budget model, replacing per-container split"
```

---

### Task 6: Manifest schema — custom = pod.memory/pod.cpu

**Files:**
- Modify: `pkg/tainer/manifest/manifest.go` (`PodConfig` struct + `validate()` at ~357-365)
- Modify: `pkg/tainer/manifest/manifest_v2_test.go` (custom validation cases)

**Interfaces:**
- Produces: `PodConfig.Memory string` (yaml `memory`), `PodConfig.CPU float64` (yaml `cpu`). Validation: custom requires both; non-custom rejects them.

- [ ] **Step 1: Write the failing test**

Add to `pkg/tainer/manifest/manifest_v2_test.go`:

```go
func TestPodCustomValidation(t *testing.T) {
	ok := []byte("version: 2\nproject:\n  name: d\n  type: nodejs\n  domain: d.tainer.me\nruntime:\n  node: \"24\"\n  database: postgres\npod:\n  size: custom\n  memory: 3G\n  cpu: 4\n")
	if _, err := ParseBytes(ok); err != nil {
		t.Fatalf("valid custom rejected: %v", err)
	}
	missing := []byte("version: 2\nproject:\n  name: d\n  type: nodejs\n  domain: d.tainer.me\nruntime:\n  node: \"24\"\n  database: postgres\npod:\n  size: custom\n")
	if _, err := ParseBytes(missing); err == nil {
		t.Errorf("custom without memory/cpu should fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/manifest/ -run TestPodCustomValidation -v`
Expected: FAIL (either compile error on `Memory`/`CPU`, or the missing-case doesn't error yet).

- [ ] **Step 3: Add fields + validation**

In `pkg/tainer/manifest/manifest.go` `PodConfig` (around line 131) add:

```go
	Memory string  `yaml:"memory,omitempty"` // custom: pod-total memory, e.g. "3G"
	CPU    float64 `yaml:"cpu,omitempty"`    // custom: pod-total CPU cores
```

In `validate()`, inside the `if m.Version == 2 { if m.Pod != nil {` block, after the size switch, add:

```go
			if m.Pod.Size == PodSizeCustom {
				if m.Pod.Memory == "" || m.Pod.CPU <= 0 {
					return fmt.Errorf("pod.size=custom requires pod.memory and pod.cpu")
				}
			} else if m.Pod.Memory != "" || m.Pod.CPU != 0 {
				return fmt.Errorf("pod.memory/pod.cpu only valid with size: custom")
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/manifest/ -v`
Expected: PASS (new test + existing manifest tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/manifest/manifest.go pkg/tainer/manifest/manifest_v2_test.go
git commit -m "feat(manifest): custom pod size via pod.memory/pod.cpu totals"
```

---

### Task 7: Wire the pod cgroup into container creation

**Files:**
- Modify: `pkg/tainer/engine/run.go:36-63` (`RunSpec` + `hostCfg`)
- Modify: `pkg/tainer/pod/start.go` (buildMounts area ~333-375: set CgroupParent + pod budget on every role; remove Split)
- Create helper: `PodCgroup` in `pkg/tainer/pod/labels.go`
- Modify: `pkg/tainer/pod/start_test.go` (assert every role gets the same CgroupParent + budget, no per-role split)

**Interfaces:**
- Consumes: `PodBudget` (Task 5), `RunSpec.CgroupParent`.
- Produces: `func PodCgroup(project string) string` → `"tainer-" + project`; and `func podCgroupSettings(project, role string, memBytes int64, cpuCores float64) (cgroupParent string, oomScoreAdj int64, res container.Resources)` — the three pod-cgroup values applied to every container's `RunSpec` inline in `Start`. Role-aware `oomScoreAdj`: db −500, others 0.

- [ ] **Step 1: Add RunSpec.CgroupParent + OomScoreAdj and wire to hostCfg**

In `pkg/tainer/engine/run.go`, add to `RunSpec`:

```go
	CgroupParent string // smart pods: shared pod cgroup path
	OomScoreAdj  int64  // smart pods: OOM victim bias
```

Where `hostCfg` is built (near `hostCfg.Resources = spec.Resources`), add:

```go
	hostCfg.CgroupParent = spec.CgroupParent
	hostCfg.OomScoreAdj = int(spec.OomScoreAdj)
```

- [ ] **Step 2: Add the PodCgroup helper**

In `pkg/tainer/pod/labels.go` add:

```go
// PodCgroup is the shared cgroup path for a project's pod. Every container in
// the pod runs as a leaf under it; the parent carries the pod's aggregate
// memory/CPU budget.
func PodCgroup(project string) string { return "tainer-" + project }
```

- [ ] **Step 3: Write the failing test**

In `pkg/tainer/pod/start_test.go` add:

```go
func TestPodCgroupSettings(t *testing.T) {
	m := mkPod(manifest.PodSizeSmall) // helper from split_test.go (same package)
	memBytes, cpu, err := PodBudget(m)
	if err != nil {
		t.Fatal(err)
	}
	want := PodCgroup("demo")
	for _, role := range []string{RoleWeb, RoleApp, RoleDB} {
		parent, oom, res := podCgroupSettings("demo", role, memBytes, cpu)
		if parent != want {
			t.Errorf("%s CgroupParent = %q, want %q", role, parent, want)
		}
		if res.Memory != memBytes || res.NanoCPUs != int64(cpu*1e9) {
			t.Errorf("%s resources = %d/%d, want %d/%d (pod budget)", role, res.Memory, res.NanoCPUs, memBytes, int64(cpu*1e9))
		}
		if role == RoleDB && oom >= 0 {
			t.Errorf("db oomScoreAdj = %d, want negative (protect db)", oom)
		}
		if role != RoleDB && oom != 0 {
			t.Errorf("%s oomScoreAdj = %d, want 0", role, oom)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/pod/ -run TestPodCgroupSettings -v`
Expected: FAIL — `undefined: podCgroupSettings`.

- [ ] **Step 5: Add the helper + apply it inline in start.go**

In `pkg/tainer/pod/start.go` add the focused helper:

```go
// podCgroupSettings returns the shared-pod-cgroup values for a container:
// the parent cgroup path, an OOM bias (db protected so a runaway app dies
// first), and the pod-aggregate Resources (identical on every container —
// the parent enforces the real cap; these just convey the budget to the
// agent, which applies them to the parent, not the leaf).
func podCgroupSettings(project, role string, memBytes int64, cpuCores float64) (string, int64, container.Resources) {
	var oom int64
	if role == RoleDB {
		oom = -500
	}
	return PodCgroup(project), oom, container.Resources{
		Memory:   memBytes,
		NanoCPUs: int64(cpuCores * 1e9),
	}
}
```

Then in `Start`: compute `memBytes, cpuCores, err := PodBudget(m)` once (replacing the `split, err := Split(m)` call ~line 194), and in the per-role `engine.RunSpec` assembly (~line 363-375) set the three fields from the helper, replacing the old per-role `Resources`:

```go
	parent, oom, res := podCgroupSettings(m.Project.Name, role, memBytes, cpuCores)
	spec := engine.RunSpec{
		Image:        ImageRef(m, role),
		Name:         ContainerName(m.Project.Name, role),
		NetworkMode:  netMode,
		Cmd:          smokeCmd(role),
		Env:          envs,
		Mounts:       mounts,
		Ports:        bindings,
		CgroupParent: parent,
		OomScoreAdj:  oom,
		Resources:    res,
	}
```

(Keep any other existing `RunSpec` fields already present in that assembly — copy them verbatim; only `Resources` is being replaced and `CgroupParent`/`OomScoreAdj` added. Delete the old `memBytes, _ := units.RAMInBytes(lim.Memory)` per-role math and the `Split` result.)

- [ ] **Step 6: Run test + build**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/pod/ -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add pkg/tainer/engine/run.go pkg/tainer/pod/labels.go pkg/tainer/pod/start.go pkg/tainer/pod/start_test.go
git commit -m "feat(pod): create every pod container under the shared pod cgroup"
```

---

### Task 8: Defensive migration for legacy custom manifests

**Files:**
- Modify: `pkg/tainer/manifest/manifest.go` (in the v2 path, fold legacy `pod.containers` into `pod.memory`/`pod.cpu`)
- Modify: `pkg/tainer/manifest/manifest_v2_test.go`

**Interfaces:**
- Consumes: `PodConfig.Containers` (retained for parse), `PodConfig.Memory/CPU`.
- Produces: a manifest loaded with legacy `size: custom` + `pod.containers.<role>` ends up with `pod.memory`/`pod.cpu` = the summed totals and `pod.containers` cleared.

- [ ] **Step 1: Write the failing test**

Add to `manifest_v2_test.go`:

```go
func TestLegacyCustomContainersSummed(t *testing.T) {
	raw := []byte("version: 2\nproject:\n  name: d\n  type: nodejs\n  domain: d.tainer.me\nruntime:\n  node: \"24\"\n  database: postgres\npod:\n  size: custom\n  containers:\n    web: {memory: 256M, cpu: 0.5}\n    app: {memory: 1G, cpu: 1}\n    db:  {memory: 512M, cpu: 0.5}\n")
	m, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("legacy custom rejected: %v", err)
	}
	if m.Pod.Memory != "1792M" && m.Pod.CPU != 2 {
		t.Errorf("summed budget = %q/%v, want 1792M/2", m.Pod.Memory, m.Pod.CPU)
	}
}
```

(1792M = 256+1024+512. Assert whatever exact string the summary produces — compute bytes then render; simplest is to assert `m.Pod.CPU == 2` and the byte-parsed `m.Pod.Memory` equals 1792*1024*1024.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/manifest/ -run TestLegacyCustom -v`
Expected: FAIL (validation rejects custom-without-memory, or containers ignored).

- [ ] **Step 3: Implement the fold in the v2 path**

In `manifest.go`, before `validate()` runs (in `ParseBytes`, after migration/defaults), add:

```go
	// Defensive: no manifest in the wild uses the old per-container custom
	// shape, but if one is loaded, sum it into the pod total so it satisfies
	// the current custom schema.
	if m.Pod != nil && m.Pod.Size == PodSizeCustom && m.Pod.Memory == "" && len(m.Pod.Containers) > 0 {
		var totMem int64
		var totCPU float64
		for _, lim := range m.Pod.Containers {
			b, _ := units.RAMInBytes(lim.Memory)
			totMem += b
			totCPU += lim.CPU
		}
		m.Pod.Memory = fmt.Sprintf("%dM", totMem/(1024*1024))
		m.Pod.CPU = totCPU
		m.Pod.Containers = nil
	}
```

(Add `units "github.com/docker/go-units"` to imports if not present.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/dev/cyber5-io/tainer && go test ./pkg/tainer/manifest/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/manifest/manifest.go pkg/tainer/manifest/manifest_v2_test.go
git commit -m "feat(manifest): fold legacy per-container custom into pod total"
```

---

## Phase 3 — Integration

### Task 9: End-to-end — cyber5 at `small` runs the dev server OOM-free

**Files:** none (verification on a live VM after both repos are built + installed).

**Interfaces:** consumes the whole feature.

- [ ] **Step 1: Build + install both sides**

Rebuild the signed pkg (user's terminal — needs keychain) with the smart-pods branches merged, or for a dev loop: `make -C ~/dev/cyber5-io/cyber-stack boot-disk` (agent → boot disk), install cyberstackd, `rm ~/.cyberstack/boot-arm64.img`, bounce the stack.

- [ ] **Step 2: Set cyber5 back to small and start**

```bash
# edit tainer.yaml: pod.size: small
cd ~/dev/websites/cyber5.io/website && tainer destroy && tainer start
```
Expected: `site answering`.

- [ ] **Step 3: Verify the shared budget + no OOM**

```bash
curl -sk -o /dev/null -w "%{http_code}\n" https://cyber5io.tainer.me/   # want 200
tainer exec cyber5-io app -- cat /sys/fs/cgroup/../memory.max           # pod parent = 1G (1073741824)
grep -c "oom-kill" ~/.cyberstack/console.log                            # want 0 new since start
```
Expected: HTTP 200; the app's usage exceeds the old 640M slice (up to ~1 GB shared) with **no** OOM kill in the guest log — the regression that started this is gone.

- [ ] **Step 4: No commit** — verification only.

---

## Notes for the executor

- **Task order:** do Task 6 (manifest fields) before Task 5 (PodBudget) if the compiler needs `PodConfig.Memory/CPU` first; they're listed engine-first / model-first but the compile dependency is Task 6 → Task 5 → Task 7.
- **Cross-repo:** Phase 1 lands in `cyber-stack`, Phase 2 in `tainer`. Phase 3 needs both built together.
- **cgroup v2 gotcha:** if `ensurePodCgroup` can't write `memory.max` (controller not enabled on the root), the `cgroup.subtree_control` write in Task 2 is what enables it; verify on the live guest in Task 9 and adjust the controller-enable path if the guest uses a containerd sub-scope rather than the cgroup root.
