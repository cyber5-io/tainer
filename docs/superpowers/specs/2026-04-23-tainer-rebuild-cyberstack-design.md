# Tainer Rebuild: CyberStack + tainer 1.0.0

**Date:** 2026-04-23
**Status:** Approved (design phase)
**Target version:** tainer 1.0.0
**Author:** Design conversation, Leni Neto + Claude

## Context and motivation

Tainer is a podman fork. The current tree carries ~765K LOC of upstream podman Go code to expose ~12K LOC of actual tainer product (TUI, manifest, project lifecycle, scaffolds). Almost none of what podman brings is on tainer's hot path: rootless Linux specifics, systemd quadlets, Windows/WSL, HyperV, libkrun, QEMU, Docker compat API, Kubernetes YAML, remote clients. The tail is dragging the dog.

The Kompozi project — Cyber5's CMS — recently demonstrated that a clean-slate rebuild, reusing trusted libraries but owning the layering from scratch, is dramatically faster than incrementally unwinding a fork. Two days from nothing to a working CMS, now days away from MVP. That pattern applies here.

Two concrete stress points justify the rebuild:

1. **VM layer.** macOS and Windows both need a VM. Podman ships krunkit (Hypervisor.framework-based, known file-ownership issues on macOS). Tainer already replaced it with vfkit (Virtualization.framework-based) as its own work. What remains is the orchestration around the VM: guest kernel, minimal userspace, virtio-fs tuning, Rosetta wiring, idle/resume behaviour, lifecycle control. This is exactly the layer where OrbStack gets its "feels magical" advantage over Docker Desktop, and it's exactly the layer podman weighs tainer down on.
2. **Container and pod operations.** Tainer exposes a small, opinionated command surface — not docker compat, not kube play, not quadlet, not rootless variants. Current code calls into podman for these ops. A purpose-built replacement with just what tainer needs is both simpler and faster.

Timing is favourable. Current tainer is 0.2.4, dogfooded internally, not yet public. The kickstarter campaign is in preparation. A rebuild gives the campaign a cleaner story ("built from scratch for macOS, no podman relics") and the product a cleaner long-term foundation.

## Strategic decisions

Made and locked in during the brainstorming phase:

- **Fresh build, not incremental unwind.** Move the current tree to `legacy/` as read-only reference, rebuild with a clean module graph.
- **Reuse trusted libraries.** `containers/image`, `containers/storage`, `crun`, `charmbracelet/bubbletea`, `lipgloss` — no reinventing wheels.
- **Language stays Go.** Decisively the right call: the container ecosystem libraries are Go, cross-compilation is trivial, single-binary deploy, TUI investment is Go. C++ and Rust were considered and rejected.
- **Hypervisor: Apple Virtualization.framework on macOS, WSL2 on Windows, native on Linux.** No VM on Linux.
- **Two repositories with a clean API boundary.**
  - `tainer` (public, BSL 1.1, Go) — CLI, TUI, manifest, project lifecycle, scaffolds, networking UX.
  - `cyber-stack` (private, closed-source, Go) — VM + container runtime + Docker-compatible API.
- **CyberStack is a real product, not a byproduct.** Designed as a true Docker drop-in: DDEV, docker CLI, docker-compose all work transparently against it. Future possibility of shipping standalone as an OrbStack competitor.
- **Tainer uses CyberStack as a transparent engine.** Users never see "VM" or "engine" in normal UX. `tainer start` spawns the daemon on first run if not already up. No user-facing name for the VM.
- **Internal codename: CyberStack (with "5" stylised as `Cyber5tack` for public-facing branding later).**
- **MVP scope: parity with current tainer.** All 9 project types. Existing container images already run against Virtualization.framework via vfkit; image-side changes expected to be minimal.
- **Target version: tainer 1.0.0 on ship.**
- **No timeline pressure.** Current tainer stays shippable on `main` throughout; rebuild happens on a branch.

## Architecture

### Repositories

| Repo | Path | License | Scope |
|------|------|---------|-------|
| tainer | `/Users/lenineto/dev/cyber5-io/tainer` | BSL 1.1 (public) | CLI, TUI, project orchestration, scaffolds, networking UX |
| cyber-stack | `/Users/lenineto/dev/cyber5-io/cyber-stack` | Proprietary (private) | VM, container runtime, Docker Engine API |

### Process topology

```
Host:
  tainer CLI           (short-lived, per command)
  cyberstackd          (long-lived daemon, spawned by tainer on first use)
    |
    +-- VM (managed by cyberstackd)
          cyberstack-agent  (long-lived, in-guest)
          crun              (short-lived, per container)
```

- `tainer` CLI talks to `cyberstackd` over a Unix socket.
- `cyberstackd` owns the VM process, spawns it on first container request.
- `cyberstack-agent` runs inside the VM, called by `cyberstackd` over vsock, drives container ops via `crun` and `containers/storage`.
- External Docker-compatible tools (docker CLI, docker-compose, DDEV) talk to `cyberstackd` on the same canonical Docker socket path.

### Wire protocols

| Boundary | Format | Reasoning |
|----------|--------|-----------|
| tainer CLI ↔ cyberstackd | HTTP+JSON over Unix socket, **Docker Engine API compatible** | Debuggable with `curl --unix-socket`, no codegen deps, and preserves the Docker drop-in option from day one |
| cyberstackd ↔ cyberstack-agent | gRPC over vsock | Private contract between two halves of the same product; tight typing, codegen acceptable, no debuggability cost to users |
| External docker/compose/DDEV ↔ cyberstackd | Same Docker Engine API socket | Drop-in compatibility is a first-class requirement, not a nice-to-have |

The canonical Docker socket path (`/var/run/docker.sock` on macOS/Linux, `//./pipe/docker_engine` on Windows) is the default; `DOCKER_HOST` can be pointed at an alternate path for side-by-side installs.

### Responsibilities

**CyberStack owns:**

- VM lifecycle: create, start, stop, destroy, suspend, resume.
- Minimal Linux guest: tuned kernel config, minimal userspace (Debian-slim or Alpine base — not bespoke kernel; YAGNI for MVP).
- Virtio-fs mount plumbing from host to guest, and bind-mounting into containers.
- Rosetta integration on Apple Silicon for amd64 images.
- Image pull / store / layer cache (via `containers/image` + `containers/storage`).
- Container lifecycle: create, start, stop, kill, rm, exec, logs, stats.
- Events and logs streaming.
- Networks and volumes.
- Port publishing (plain Docker-style, no magic).
- Docker Engine API surface.

**Tainer owns:**

- CLI and TUI (Bubbletea/Lipgloss).
- `tainer.yaml` parsing and validation.
- Scaffolds for the 9 project types.
- Auto-hostname DNS responder (see Networking UX).
- Auto-port-publish logic and collision remapping.
- Per-machine `.tainer.local.yaml` overrides.
- Caddy config generation, TLS cert management, SSH host keys, DB export/import.
- `tainer claude-skill` installer.
- Project lifecycle orchestration (compose the right sequence of Docker API calls for each project type).
- Transparent spawn of `cyberstackd` on first use.
- Update mechanism (both tainer and the bundled CyberStack binaries).

### Libraries

- `containers/image` — registry auth, pull, manifest, signatures.
- `containers/storage` — overlay snapshots, layer cache.
- `crun` — OCI runtime (subprocess per container).
- `charmbracelet/bubbletea`, `lipgloss`, `bubbles` — TUI (tainer only).
- Standard Go net/http for the Docker-compatible API.

Nothing from `libpod`, `cmd/podman`, `pkg/machine/*`, ignition, cloud-init, quadlet, kube, remote bindings.

## Networking UX

### Tainer-side (where the magic lives)

- **Hostnames.** Tainer runs a small host-side DNS responder. Every running project gets `<project>.test` (RFC 2606 reserved TLD — guaranteed never to clash with real DNS, dodges `.dev` which Google owns as a real gTLD, dodges `.local` mDNS ambiguity). The project name *is* the hostname. No per-project DNS config.
- **Port publishing.** Auto-derived from the Containerfile `EXPOSE` lines or the scaffold's declared ports. Published to matching host ports where possible; remapped with a clear log message on host-side collision.
- **Multi-project on same port.** Fine. `foo.test:80` and `bar.test:80` both resolve because their hostnames point at different container IPs inside the VM.
- **tainer.yaml stays network-agnostic by default.** Ports are derived; explicit declarations are optional.
- **Per-machine override file: `.tainer.local.yaml`** (gitignored). A developer can override ports or disable auto-publish for specific projects on their own machine without touching the team-shared `tainer.yaml`.

### CyberStack-side (generic and boring)

- Plain Docker-style port publishing via the API. No hostname magic, no mDNS.
- This keeps CyberStack a clean generic runtime suitable for the Docker drop-in use case.

## Resource management

### Per-container and per-pod limits

Exposed via `tainer.yaml`:

```yaml
resources:
  memory: 2GB
  cpu: 2
  containers:
    db:
      memory: 512MB
      cpu: 0.5
```

- Enforced inside the VM via cgroups v2 by `crun`.
- Pod-level limit acts as a ceiling; individual containers can have their own sub-limits summing ≤ pod limit.
- Default: unlimited within the VM's total budget.
- Constraint: total active limits cannot exceed the VM's allocated resources; the VM OOM-kills on breach.

### VM sizing

Smart defaults + explicit override. The VM is the ceiling for every container running inside it — cgroups v2 in the guest can only partition what the VM itself is allocated — so defaults need to be generous enough that multi-project dev sessions don't self-contend, while still leaving headroom for macOS.

**Install default for RAM** (tiered by host capacity):

| Host RAM | Default VM RAM | Reasoning |
|----------|----------------|-----------|
| ≤ 16GB | 50%, floor 4GB, never less than leaving 4GB for macOS | Small Macs: protect the host, still usable |
| 16–48GB | 50% | Typical dev machines: straightforward split |
| > 48GB | 50%, capped at ~24GB | Big workstations: diminishing returns past this for dev containers; power users override |

Examples: 8GB MBP → 4GB VM; 16GB MBP → 8GB VM; 32GB Mac → 16GB VM; 64GB Mac → 24GB VM; 128GB Mac Studio → 24GB VM (override as needed).

**Install default for CPU:** 50% of available P+E cores, floor 2, no hard cap.

**Configurable override:** `tainer engine config --memory 32GB --cpus 12`, persisted to `~/.cyberstack/engine.yaml` (CyberStack's data directory; separate from `~/.tainer/` which holds tainer-specific state like project lists and certs). Applied on next engine restart.

**Apple Silicon topology note:** `hw.ncpu` reports P+E cores only (GPU and Neural Engine are not exposed as CPUs). The guest sees a unified SMP CPU count with no P/E distinction — macOS schedules vCPUs across physical cores heterogeneously based on workload and thermal state. Users don't need to think about P vs E; they ask for a CPU count and macOS does the rest.

**Post-1.0.0: dynamic memory ballooning.** Start small, grow on demand, shrink when idle — what OrbStack uses to feel "free." Solves the 64GB-Mac-idle-with-24GB-allocated problem elegantly. Deferred from MVP because static generous defaults are good enough to ship.

## Lifecycle

### Engine bring-up (transparent)

- First `tainer <anything>` that needs a container: tainer checks if `cyberstackd` is up via its socket. If not, spawns it.
- `cyberstackd` brings the VM up on first container request (subsequent requests are fast).
- Cold-start cost on very first command of a session; subsequent commands land on a warm engine.
- **Auto-start at login** offered by the installer: launchd user agent on macOS, scheduled task on Windows, systemd user unit on Linux. Opt-in.
- Explicit controls: `tainer engine status`, `tainer engine stop`, `tainer engine restart`. Power users only; default UX never mentions "engine."

### Upgrades

- Bundled releases: tainer N.N.N always ships matched CyberStack N.N.N. No cross-version matrix to support in v1.
- On upgrade, installer stops the running daemon, replaces binaries, restarts (if auto-start was enabled).

## Packaging and distribution

| Platform | Bundle | Notes |
|----------|--------|-------|
| macOS (arm64 + amd64) | `.pkg` | Signed + notarised. Ships `tainer` CLI, `cyberstackd`, `cyberstack-agent`, guest kernel/initramfs, `docker` + `docker-compose` wrappers. **Primary target** — the 1.0.0 ship gate is macOS parity. |
| Linux | `.deb`, `.rpm` | No VM. `cyberstackd` is mostly a thin shim; container ops hit the host's runtime (crun) directly. **Secondary target** — easiest to port because the VM layer largely drops out; sequenced immediately after macOS. |
| Windows | `.msi` | `cyberstackd` orchestrates WSL2 rather than owning a hypervisor. **Tertiary target** — sequenced last due to the WSL2 integration surface. |

Docker and docker-compose binary wrappers are bundled so that standalone CyberStack users (and tainer users who want to use `docker` CLI directly against their tainer engine) don't need separate installs. Same move OrbStack makes.

## What carries forward, what goes to `legacy/`

### Carries forward (ported to new tree)

- `pkg/tainer/tui/*` — wizard, picker, list, progress, status, home. Pure TUI, no engine coupling.
- `pkg/tainer/manifest` — `tainer.yaml` schema and parser.
- `pkg/tainer/project` — high-level project lifecycle (rewired from podman calls to CyberStack Docker-API calls).
- `pkg/tainer/router`, `pkg/tainer/tls`, `pkg/tainer/dns`, `pkg/tainer/ssh` — application-level infrastructure.
- `pkg/tainer/wizard`, `pkg/tainer/update`, `pkg/tainer/identity`, `pkg/tainer/config`, `pkg/tainer/registry` — supporting packages.
- `cmd/podman/tainer/*` — command implementations (re-homed to `cmd/tainer/`).

Roughly the 11.7K LOC that is actually tainer survives the move, with engine calls rewired.

### Goes to `legacy/`

- Everything else in the current tree: `libpod/`, `cmd/podman/` (non-tainer), upstream `pkg/*` (non-tainer), `docker/`, `vendor/`, `internal/`, all podman docs/man pages, all build/test scaffolding not directly used by the new tree.
- `legacy/` is reference-only. Not compiled. Not in the Go module graph of the new tree.
- After 1.0.0 merges to `main`, `legacy/` is removed from `main` but retained on a `legacy-archive` branch indefinitely.

## Development track

> **Status note (2026-04-24):** CyberStack `v0.1.0` is live — engine skeleton shipped, `docker` CLI verified talking to `cyberstackd` over Unix socket. See `docs/superpowers/plans/2026-04-23-cyberstack-0.1-mvp-engine-skeleton.md` for the completed milestone. VM lifecycle and container runtime work begins next.
>
> **Status note (2026-04-26):** CyberStack `v0.2.0` is live — VM bring-up + agent handshake shipped. Apple Virt UEFI → systemd-boot → Linux 6.12.83 → custom initramfs → `cyberstack-agent` dials home over vsock; daemon accepts and exposes live VM stats through Docker `/info`. End-to-end handshake measured at ~470ms (budget was <3s pass). See `docs/superpowers/plans/2026-04-25-cyberstack-0.2-vm-bringup-agent-handshake.md` for the completed milestone. Container runtime work begins next (0.3).

1. **Set up CyberStack repo.** Create `/Users/lenineto/dev/cyber5-io/cyber-stack` as a private repo. Bootstrap with Go module, repo layout, CI skeleton. ✅ **Done — v0.1.0**
2. **CyberStack MVP — VM lifecycle.** Embedded Virtualization.framework or vfkit wrapper, minimal guest boot, virtio-fs mount, vsock to agent, lifecycle commands. ✅ **Done — v0.2.0** (agent handshake + live `/info`; virtio-fs mount lands with the container runtime in 0.3)
3. **CyberStack MVP — container runtime.** `containers/image` + `containers/storage` integration, `crun` subprocess, basic container lifecycle (create/start/stop/rm/exec/logs), networks, volumes, port publishing.
4. **CyberStack MVP — Docker Engine API.** Implement the subset of endpoints that `docker` CLI, `docker-compose`, and DDEV actually use. Drive completeness via integration tests against these tools.
5. **Tainer rebuild branch.** Create `dev/v1` branch on the tainer repo. Move current tree to `legacy/`. Initialise a new Go module at repo root with a clean module path (proposed: `github.com/cyber5-io/tainer`, dropping the `containers/podman/v6` inheritance). Drop the `RawVersion` indirection in `version/rawversion/version.go` — `TainerVersion` becomes the sole version constant.
6. **Port tainer's survivors.** TUI, manifest, project orchestration, router, TLS, DNS, SSH, update, scaffolds — re-home into the new tree, rewire engine calls from podman to CyberStack Docker API.
7. **Wire transparent engine bring-up.** tainer detects `cyberstackd` presence, spawns if needed, proxies through for all container ops.
8. **Networking UX.** Host-side DNS responder, auto-port-publish, `.tainer.local.yaml` overrides.
9. **Parity validation.** All 9 project types start/stop/exec/db-import/db-export/status/list successfully. `tainer claude-skill` works.
10. **DDEV integration test.** DDEV running transparently against CyberStack as a published test case.
11. **Installer builds.** `release.sh` variant that produces the matched tainer + CyberStack bundles per platform.
12. **Ship 1.0.0.** Merge `dev/v1` → `main` on tainer, cut 1.0.0 releases on both repos, remove `legacy/` from `main` on tainer, retain on `legacy-archive` branch.

## MVP acceptance criteria

Ship 1.0.0 only when all of the following hold:

- All 9 current project types start, stop, exec, status, and list end-to-end.
- `tainer db export` and `tainer db import` work for WordPress, PHP, and Kompozi.
- `docker`, `docker-compose`, and DDEV all work transparently against a running CyberStack daemon.
- Cold-start for a first project is no slower than current tainer's baseline (target: faster).
- Idle memory footprint is no higher than current tainer's baseline (target: lower).
- Installer ships signed, notarised, version-locked bundles for macOS (arm64 + amd64).

Linux and Windows parity are explicitly post-1.0.0 milestones, sequenced in that order.

## Out of scope for 1.0.0

- Dynamic memory ballooning for the VM.
- CyberStack standalone product launch (site, docs, install flow independent of tainer).
- mDNS/Bonjour integration (`.test` hostnames resolve via tainer's own DNS responder, not OS-level mDNS).
- Kubernetes integration.
- Full Windows/Linux feature parity (tracked as separate milestones).

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Docker Engine API surface is vast; implementing it all is prohibitive | Implement only the subset required by `docker` CLI, `docker-compose`, DDEV, and tainer itself. Drive completeness via integration tests against these consumers. |
| Minimal guest kernel carries long-term maintenance burden | Use a tuned stock base (Debian-slim / Alpine) with kernel config adjustments, not a bespoke kernel. Only revisit if memory/performance demands it post-1.0.0. |
| Two parallel codebases stress team bandwidth | CyberStack work precedes tainer rewrite; tainer catches up once CyberStack has working container ops. Sequential-with-overlap rather than two greenfields in parallel. |
| Dogfooding users disrupted during rebuild | `main` stays on current tainer throughout. All rebuild work on `dev/v1`. Ship 1.0.0 only when parity is validated. |
| Docker socket path conflicts with existing Docker Desktop or OrbStack installs | Honour `DOCKER_HOST` for side-by-side installs. Document clearly. |
| Private closed-source engine raises eyebrows in the kickstarter audience | Be explicit in campaign copy: tainer is open-source (BSL 1.1); the high-performance runtime is proprietary, mirroring the industry norm (Docker Desktop, OrbStack). |

## Open questions for the implementation plan

These are *not* strategic questions — those are settled. These are implementation-detail choices for the plan to resolve:

- **Guest distro choice:** Debian-slim vs Alpine vs something else. Pick during CyberStack MVP step 2.
- **VM API binding on macOS:** keep `vfkit` as external binary, or embed Virtualization.framework calls via a Swift sidecar called from Go? Evaluate during CyberStack MVP step 2.
- **Docker API endpoint prioritisation:** which endpoints first? Driven by what DDEV and docker-compose actually call. Instrument during integration work.
- **In-VM agent protocol buffer schema:** drafted during step 3.
- **Release bundling mechanics:** whether to check in CyberStack as a git submodule of tainer, vendor its binaries into tainer's release pipeline, or use a separate release flow that composes the two. Decide during step 11.

## Dual-product roadmap (post-1.0.0)

Not part of 1.0.0, but banked for the roadmap:

- CyberStack ships its own site (`cyber5tack.io` or similar), docs, installer, and support channel.
- Positioned as an open (or freely-distributed) OrbStack competitor.
- Same binaries as bundled in tainer, separate install flow.
- Creates a second product line for Cyber5 IO, alongside tainer (dev tool), Kompozi (CMS), and Blenzi (hosting).
