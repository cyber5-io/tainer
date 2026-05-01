# CyberStack 0.4 — Pull Pipeline Performance

> **Status:** brainstormed 2026-05-01, post-0.3.0 release. Pending implementation plan + execution.

## Goal

Close the cold-pull gap to OrbStack. Specifically:

> **cold-pull P50 ≤ 520ms** against `mirror.gcr.io/library/alpine:latest`, n=100, NordVPN off — within 50ms of OrbStack's measured 467ms baseline on the same hardware.

CyberStack 0.3.0 ships with cold-pull P50 = 860ms (mean 873ms). The other four locked benchmarks (warm-run, exec, cold-cold, idle) all hit budget and are not in scope for this milestone. We instrumented the pull pipeline thoroughly during the 0.3 endgame, so the 0.4 plan sits on real evidence rather than guesswork.

## Evidence we're building on

From the 0.3 phase tracer (`internal/agent/imagestore/store.go`, since reverted) over n=42 pulls:

| phase | mean | content |
|---|---|---|
| pre-phase | 571ms | `copy.Image()` entry → first ReportWriter line: registry ping, auth challenge, bearer-token GET, manifest GET, signature policy setup |
| blob copy | 343ms | blob download + gunzip + tar + chrootarchive subprocess fork + overlay write |
| config write | 0.2ms | metadata persist |
| (network handshakes only) | 62ms | DNS + TCP + TLS, summed |

A `runtime/pprof` CPU profile during the same workload showed the agent at **98% idle** (1.43s on-CPU over 90s wall) — pull latency is in I/O wait, not compute. `tmpfs` over the storage path was *slower* than ext4, so it's not disk I/O either.

The 571ms pre-phase is dominated by sequential dependent HTTP round-trips (each ~80ms TTFB to mirror.gcr.io); the 343ms blob-copy phase contains both body download and `containers/storage`'s `chrootarchive` re-exec for layer apply.

Full diagnosis lives in `cyber-stack/docs/benchmarking.md` "0.3.0 results".

## Locked decisions

| Q | Decision | Reasoning |
|---|---|---|
| **What stays from 0.3** | `containers/storage` for the local image store; the daemon ↔ agent vsock split; the boot-disk pipeline; cs-bench. | The store, transport and benchmarking infrastructure are working well. The pull pipeline is the only piece changing. |
| **What replaces `containers/image` for pull** | A custom `types.ImageSource` adapter backed by `go-containerregistry`'s `remote` package. The destination remains `containers/image`'s `is.Transport.NewImageDestination()` writing into `containers/storage`. | g-c-r exposes `remote.WithTransport(http.RoundTripper)` and uses `ForceAttemptHTTP2: true` + `MaxIdleConnsPerHost: 50` by default. `containers/image` builds its transport in private code and offers no hook. We write the bridge once; both halves keep working with their respective sweet spots. |
| **What replaces `chrootarchive`** | A fork of the overlay graph driver (or a `replace` directive in `go.mod`) that calls in-process `archive/tar` + decompress directly into the upper-dir, skipping the `reexec.Command` fork for `ApplyDiff`. | `chrootarchive` exists for security isolation when extracting tar (zip-slip / device-node attacks). In a single-tenant arm64 VM running as root with no untrusted multi-image input, the threat model doesn't justify a per-pull fork. We give up some defense-in-depth in exchange for ~100–150ms saved per pull. |
| **Connection sharing scope** | Process-wide `*http.Transport` in the agent, shared across all pulls + RPC calls. | Once the transport is owned by us (via the g-c-r adapter), keepalive + connection pooling become free. The TLS handshake amortises across pulls instead of being paid every time. |

## Estimated savings

Sized from the 0.3 phase tracer evidence:

| change | targets | est. saving |
|---|---|---|
| g-c-r adapter (shared transport, HTTP/2, connection reuse) | pre-phase round-trip cost | 100–200ms |
| In-process untar replacing chrootarchive | blob-copy fork overhead | 100–150ms |
| **Combined** | both | **200–350ms** |

860ms − 250ms (midpoint) ≈ 610ms. To hit ≤520ms, both changes need to land near the upper end of their estimate and not regress each other.

## Architecture

### Layer 1: pull source adapter

```
              copy.Image (containers/image)
                          ↓
                  PullSource interface
                  (types.ImageSource)
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
     gcrImageSource (NEW)      registry destination
     (wraps remote.Image)      (unchanged: is.Transport)
              ↓
     remote.WithTransport(sharedTransport)
              ↓
     *http.Transport (process singleton)
       ForceAttemptHTTP2: true
       MaxIdleConnsPerHost: 50
       IdleConnTimeout: 90s
```

The adapter implements ~10 methods from `types.ImageSource` (`Reference`, `Close`, `GetManifest`, `GetBlob`, `HasThreadSafeGetBlob`, `GetSignaturesWithFormat`, `LayerInfosForCopy`). Most delegate to `remote.Image` / `v1.Image`. The new file lives at `internal/agent/imagestore/gcrsource.go`.

### Layer 2: storage driver fork

`containers/storage`'s overlay driver hard-codes `var untar = chrootarchive.UntarUncompressed`. We need to swap it for an in-process implementation. Two paths:

- **Vendor a fork** of `containers/storage` and replace the `untar` package var with `archive.UntarUncompressed` (the non-chroot version that lives in the same vendor tree). Maintained via `go.mod replace`.
- **Build our own graph driver** registered alongside overlay/vfs, named e.g. `cs-overlay`. More code, fewer surprises later.

The fork-via-`replace` route is the cheaper start; we revisit if the maintenance burden gets ugly.

## Build sequencing

Two independent halves; each shippable + benchable on its own. Land them in either order, bench between, get cumulative numbers.

1. **g-c-r pull source** (~1–2 days). Lands behind a feature-flag env (`CYBERSTACK_PULL_SOURCE=gcr` defaults off). Once it benches well, becomes default and the flag is removed in the same PR.
2. **In-process untar via storage fork** (~2–3 days). Lands as a `go.mod replace` plus the relevant patch file checked into `third_party/`.
3. **Final 100-rep bench** with both shipping. If P50 ≤ 520ms, tag `v0.4.0`. If not, the phase tracer goes back in (we kept the harness in git history) and we do another diagnosis pass.

Total: ~3–5 days of focused work + 1 day of bench/cleanup. One implementation milestone.

## Risks

| risk | likelihood | mitigation |
|---|---|---|
| g-c-r adapter doesn't preserve some `containers/image` semantics (manifest validation, schema 1 fallback, multi-platform selection) | **medium** | Smoke-test against the full set of `ghcr.io/cyber5-io/tainer-*` images (WordPress, PHP, Next.js, Nuxt.js, Kompozi) before landing. Don't ship until they all pull successfully. |
| In-process untar regresses security posture in a way we don't notice | **medium** | Document the trade-off explicitly in the storage fork's README. Re-add chrootarchive behind a build tag for the rare paranoid user. |
| Estimated savings don't materialise; we close 100ms instead of 250ms | **medium** | If the bench number after both changes is still >700ms, we re-instrument and look at the next layer (probably `containers/image`'s manifest parsing or signature policy). The 0.4 spec doesn't promise parity — it promises a serious attempt with measurable progress. |
| `go.mod replace` against `containers/storage` breaks on upstream version bumps | **low** | Pin to a specific upstream tag; only bump deliberately. The patch is small enough to forward-port. |

## Out of scope for 0.4

- Persistent VM (suspend/resume) — that's the killer feature for warm-start latency and deserves its own milestone with its own benchmarks. Was originally locked for 0.4 in the 0.3 spec; deferring to 0.5 because the pull-pipeline work crowds out the VM-state-machine work.
- amd64 emulation — same reasoning.
- `docker network`, `docker volume`, port publishing — still in tier-C scope, deferred.
- Vendored gvproxy install (Task #65) — packaging concern, lands when we package cyberstackd standalone for distribution. Not blocking the perf milestone.

## Acceptance criteria

1. `make build && make boot-disk` succeeds on a clean checkout.
2. `cs-bench -only cold-pull -cold-pull-runs 100 -image mirror.gcr.io/library/alpine` reports **P50 ≤ 520ms** on the reference machine, NordVPN off.
3. The other four budgets (warm-run, exec, cold-cold, idle) remain green — no regression.
4. Smoke pull + run of `ghcr.io/cyber5-io/tainer-wordpress`, `tainer-nextjs`, `tainer-kompozi` succeeds end-to-end via the cyberstackd socket.
5. `internal/agent/imagestore/store.go` no longer references `chrootarchive` (transitively via overlay driver, via the `go.mod replace`).
