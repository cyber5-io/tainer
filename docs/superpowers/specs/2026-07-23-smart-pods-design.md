# Smart Pods — Aggregate Pod-Level Resource Budget — Design

**Date:** 2026-07-23
**Status:** Approved (design), pending implementation
**Repos:** tainer (product/orchestration) + cyber-stack (engine/agent)
**Target:** tainer 1.2.0 / CyberStack 0.7.0

## Problem

Pod sizes are enforced as **hard per-container** limits from a fixed split
table (`pkg/tainer/pod/split.go`). A container is capped at its slice even
when its siblings sit idle. Concretely: cyber5 (a static SvelteKit site) runs
`vite dev` (esbuild SSR compile), which the `small` preset caps at **640M** —
while the pod's other 384M (web 128M + db 256M) sits unused. The app hit its
slice and the cgroup OOM killer SIGKILLed it (exit 137) even though the 1 GB
pod — and the 32 GB VM — had memory to spare.

The fix, and a genuine tainer differentiator: **a pod size is one absolute
budget, shared by its containers.** Any container may burst up to the whole
pod budget as long as it's available.

## Model

- A size names an **absolute pod total** (memory + CPU), independent of how
  many containers the pod has.
- Enforcement is **one pod-level cgroup** carrying `memory.max` + `cpu.max`.
  Every container in the pod runs as a leaf under that cgroup with **no
  per-container limit**, so they share and burst within the single budget.
- The network namespace model is **unchanged** — followers still join the
  leader's netns via `NetworkMode: container:<leader>`. (netns is shared by
  *joining a container*; a cgroup is shared by a *hierarchy path* — two
  different Linux primitives. No pause/infra container is introduced.)

### Size table (pod totals)

| size | memory | CPU (cores) |
|------|--------|-------------|
| nano | 512M | 1 |
| small | 1G | 1 |
| medium | 2G | 2 |
| large | 4G | 4 |
| xlarge | 8G | 6 |
| xxl | 16G | 8 |

- **Default:** `small` (1 GB shared — enough to run cyber5's dev server, the
  regression that motivated this).
- **Custom:** `pod.size: custom` with pod-level `pod.memory` + `pod.cpu`
  (replaces the old per-container `pod.containers.<role>` fields).

CPU totals are all whole cores, so `cpu.max` quota math stays integral.

## Architecture (cgroup v2)

The guest runs cgroup v2 (crun; `containerd/runtime.go` already emits
`cpu.max` v2 syntax). We build a two-level hierarchy per pod:

```
tainer-<pod>/                 ← parent: memory.max + cpu.max = pod budget
  ├── tainer-<pod>/web        ← leaf: container procs, NO own limit
  ├── tainer-<pod>/app        ← leaf: container procs, NO own limit
  └── tainer-<pod>/db         ← leaf: container procs, NO own limit
```

The cgroup-v2 "no internal processes" rule is satisfied: the parent holds only
child cgroups (no direct processes); each container's processes live in its
own leaf. The parent's `memory.max` caps the **aggregate** of all leaves;
leaves have no `memory.max`, so any one leaf can grow to the parent's limit
while the others are idle. Same for `cpu.max`.

### Contract: tainer → engine

Every pod container's `create` carries:
- `HostConfig.CgroupParent = "tainer-<pod>"` (the shared parent path)
- `HostConfig.Memory` + `HostConfig.NanoCpus` = the **pod** budget

The agent, on create with a non-empty `CgroupParent`:
1. **Ensure** the parent cgroup exists and write `memory.max` / `cpu.max` from
   the request's Memory/NanoCpus (idempotent — every pod container sends the
   same budget, so it's order-independent and safe to re-assert).
2. Set the container's OCI `linux.cgroupsPath = "<parent>/<name>"`.
3. **Do not** set leaf `resources.memory` / `resources.cpu` (leaves inherit
   the parent's effective limit).

With an empty `CgroupParent`, behaviour is unchanged (per-container limit) —
so non-pod / standalone containers are unaffected.

The pod name for the cgroup path derives from the project name (matching the
`tainer-<project>-<role>` container naming), e.g. `tainer-cyber5-io`.

## Components

### tainer

- **`pkg/tainer/pod/split.go` → pod budget.** Replace `splitTable`
  (per-container `Limits`) and `Split()` with `PodBudget(m) (memBytes int64,
  cpuCores float64, err error)` returning the single pod total from a
  size→total table; `custom` reads `pod.memory`/`pod.cpu`. Delete the
  per-role division.
- **`pkg/tainer/manifest` — PodConfig schema.** Add `Memory string` (e.g.
  "3G") and `CPU float64` as the canonical `custom` fields. The old per-role
  `Containers map[string]Limits` is retained **only** so the defensive
  migration below can still parse it; new writes never use it.
  `validate()` requires `memory`+`cpu` when `size: custom` and rejects them on
  non-custom sizes.
- **Manifest migration (defensive).** No project manifest in the wild uses
  `size: custom` today, so this is a safety net, not real cleanup: if a
  manifest with the old `size: custom` + `pod.containers.<role>` is loaded,
  migrate it by **summing** the per-container memory/cpu into
  `pod.memory`/`pod.cpu` (persisted rewrite, same mechanism as
  `PersistV2Migration`). Preset manifests need no change.
- **`pkg/tainer/engine/run.go` — RunSpec.** Add `CgroupParent string`; wire it
  into `hostCfg.CgroupParent` (Docker SDK field) alongside the existing
  `NetworkMode` handling.
- **`pkg/tainer/pod/start.go`.** For every role in the pod set
  `spec.CgroupParent = PodCgroup(m.Project.Name)` and
  `spec.Resources = {Memory: budgetBytes, NanoCPUs: budgetCores*1e9}` (the pod
  budget, same on all containers). Remove the per-role `Split` lookup.
- **`PodCgroup(project) string`** helper returns `"tainer-" + project`.

### cyber-stack (engine)

- **`internal/httpapi/containers.go`.** Parse `body.HostConfig.CgroupParent`
  into `agentpb.HostConfig.CgroupParent`.
- **`internal/proto` (agent.proto + regen).** Add `string cgroup_parent` to
  `HostConfig`.
- **`internal/agent/containerd/runtime.go`.** When `CgroupParent` is set:
  ensure the parent cgroup exists and write `memory.max`/`cpu.max` (from
  MemoryBytes/NanoCPUs) directly to the cgroup2 filesystem; set the container's
  OCI `linux.cgroupsPath = "<parent>/<name>"`; skip the leaf
  `resources.memory`/`resources.cpu`. When unset: current behaviour.
- **Parent cgroup lifecycle.** Create-if-absent + re-assert limit on each pod
  container create (idempotent). On the **last** pod container's removal,
  best-effort `rmdir` the empty parent cgroup (leaving an empty parent is
  harmless but we clean up).
- **Version bump** `CyberStackVersion` 0.6.0 → **0.7.0**.

## OOM behaviour

If a pod genuinely exceeds its budget, the cgroup-v2 OOM killer reclaims
within the pod subtree, killing the highest-`oom_score` process. To protect
data integrity we **bias the victim selection**: the app/web (dev-server)
leaves get a higher `oom_score_adj` and the **db leaf a lower one**, so a
runaway app is killed before the database. This is applied by the agent when
it creates each leaf (role-aware `oom_score_adj`).

## Error handling

- Parent cgroup creation / limit-write failure → fail the container create with
  a clear error naming the pod cgroup path; tainer surfaces it on `start`.
- Idempotent re-assert: if two pod containers race to create the parent, both
  writing the same limit is safe (last write wins, identical value).
- `custom` without `memory`/`cpu`, or a preset with stray `pod.memory` → a
  validation error at parse time, before any container is created.

## Testing

- **tainer unit:** `PodBudget` table (every size → exact total; custom reads
  pod.memory/cpu; custom-missing-fields errors). `start.go` sets identical
  `CgroupParent` + pod-budget Resources on all roles and no per-role split.
  Manifest migration sums old per-container custom → pod total.
- **engine unit:** create with `CgroupParent` writes parent `memory.max`/
  `cpu.max` and leaf `cgroupsPath` with no leaf limit; create without it keeps
  per-container limits; last-removal prunes the parent; role-aware
  `oom_score_adj` applied.
- **integration (the regression):** cyber5 at `small` (1 GB shared) runs
  `vite dev` to a live HTTP 200 with **no OOM** — the app using >640M of the
  shared 1 GB while web/db stay small. Assert via the guest OOM log staying
  clean and the site answering.

## Out of scope (YAGNI)

- Per-container reservations / QoS tiers (guaranteed vs burstable).
- A pause/infra container owning the namespaces (leader-owned netns stays).
- Pods spanning more than one VM.
- Live resize of a running pod's budget (size changes still take effect on
  recreate — see the separate `tainer restart` re-apply follow-up).

## Phasing (for the implementation plan)

1. **Engine:** `CgroupParent` plumbing (API → proto → agent), parent-cgroup
   create/limit/prune, role-aware `oom_score_adj`, version bump + tests.
2. **tainer:** pod-budget model, manifest schema + migration, RunSpec
   `CgroupParent`, `start.go` wiring + tests.
3. **Integration:** cyber5 `small` runs the dev server OOM-free end to end.
