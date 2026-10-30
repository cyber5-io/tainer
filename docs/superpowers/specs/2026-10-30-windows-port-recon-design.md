# Windows Port Recon — Design

**Date:** 2026-10-30
**Status:** Draft, pending approval
**Repos:** tainer (scaffold + audit), cyber-stack (audit only), docs (the real deliverable)

## Intent

Get ahead on the Windows port before the test laptop arrives, **without
writing significant Windows code nobody can execute**. The deliverable is
knowledge and a compile gate: a thorough research dossier that lets us make
the engine-architecture decision with Leni, a codebase audit that maps
every portability landmine, and a thin scaffold that makes `GOOS=windows`
builds a CI-enforced invariant from today onward.

This is recon, not construction. The actual Windows design doc gets written
*with Leni* after this dossier exists and the laptop is in hand.

## Why now

Two laptops are being sourced (one Linux, one Windows). Research and
compile-cleanliness need no hardware; empirical verification does. We
learned this year (resolver hijack, bind-mount rename semantics) that
assumptions about platform behavior must be verified on the platform —
so tonight we do everything *except* the part that needs the platform.

## Goals

1. **Research dossier** at `docs/superpowers/specs/2026-10-31-windows-port-recon.md`
   (a report, not a design), covering with sources/links:
   - Engine host options: WSL2 (custom distro via `wsl --import`),
     raw Virtual Machine Platform / Windows Hypervisor Platform APIs,
     Hyper-V VMs. For each: Windows edition requirements (**Home must
     work** — hard constraint), memory behavior on an **8GB machine**
     (the floor; Docker Desktop's slowness there is the bar to beat),
     control surface from a Go host process, and distribution/installer
     implications.
   - How the incumbents do it: Docker Desktop, Podman machine on Windows,
     Rancher Desktop, Colima-equivalents. What users complain about
     (vmmem ballooning, 9P file I/O, WSL2 clock drift, firewall prompts).
   - Socket transport host↔guest: AF_UNIX on modern Windows, named pipes,
     AF_HYPERV/vsock, localhost TCP via WSL2's NAT — which can carry the
     Docker-compatible API cyberstackd exposes, and what Docker Desktop
     itself uses.
   - File sharing: 9P (drvfs/`\\wsl$`), virtiofs availability, "keep the
     code inside the VM" strategies, Dev Drive — mapped against tainer's
     bind-mount model (project dir mounted into containers) and our
     macOS virtio-fs lessons.
   - Port forwarding to host localhost and to :80/:443 (our router model),
     including the Windows firewall story.
   - Memory discipline: .wslconfig limits, `autoMemoryReclaim`, vs owning
     the VM lifecycle ourselves the way cyberstackd does on macOS.
   - Packaging & trust: MSI vs MSIX vs winget, Authenticode signing
     (what certs cost / EV vs OV / SmartScreen reputation), auto-update
     patterns — the Windows analog of our notarized .pkg + postinstall.
   - A **decision matrix** (reach × 8GB behavior × control × effort) and a
     provisional recommendation, clearly labeled provisional.
2. **Portability audit** (same dossier, second half): every place tainer
   and cyber-stack assume unix — unix sockets, path separators and
   hardcoded `/` paths, `~/.cyberstack`/XDG dirs, syscall/os.Stat_t usage,
   signals (SIGTERM lifecycle), exec of helper binaries, launchd/pid-file
   daemon management, file permissions/chmod semantics — each with
   file:line and a one-line porting note. The existing
   `identity_windows.go` stub is prior art for the pattern.
3. **Thin scaffold** in tainer: whatever minimal build-tag shims make
   `GOOS=windows GOARCH=amd64 go build ./cmd/tainer ./pkg/...` succeed —
   stubs may return "not supported on windows yet" errors at runtime;
   plus a Makefile `make cross` target that builds darwin/arm64,
   linux/amd64+arm64, windows/amd64 of those same targets so
   cross-compilation can never silently rot. cyber-stack gets the audit
   only, no scaffold.

   Scope rulings (builder-check, settled by the author):
   - **`cmd/tainer-menu` is excluded from all cross-build gates.** It is
     the macOS menu-bar app on a darwin-only vendored systray fork;
     porting it is part of each platform's future UI story, not recon.
     The gate therefore targets `./cmd/tainer ./pkg/...` explicitly, not
     `./...` — do not add no-op shims to the vendored systray fork.
   - **This task owns the shared portability seams in
     `pkg/tainer/runtime`** (e.g. `runtime_unix.go`/`runtime_windows.go`
     extracting the `syscall.Kill` and `SysProcAttr{Setsid}` uses — the
     only current windows compile failures) **and owns `make cross`.**
     The Linux native port's tainer-side work is sequenced after this
     scaffold lands and builds on the same seams (noted in that spec).
   - **The cyber-stack portability audit is this task's own work**, with
     a Windows lens (unix sockets, vsock, paths, signals). The
     2026-10-30 Linux-port recon of cyber-stack is input context to cite,
     not a substitute — it established there are no windows build tags
     anywhere, nothing more.

## Non-goals

- No WSL2 integration code, no VM lifecycle code, no installer. Nothing
  that can only be tested on Windows gets implemented.
- No engine-architecture decision. The dossier recommends; Leni decides
  in the follow-up brainstorm.

## Anti-goals

- Shipping Windows stubs that silently no-op instead of erroring: a
  windows build that pretends to work is worse than none.
- Letting the scaffold distort unix code paths: shims live in
  `_windows.go` files behind the existing seams; shared code is only
  touched to extract a seam, never to branch on GOOS inline.

## Constraints

- Hard product constraint (Leni): tainer must run on as much hardware as
  possible. Windows **Home**, 8GB RAM is the design floor. Any option
  requiring Pro/Enterprise is documented but deprioritized.
- The bar: Docker Desktop on an 8GB Windows machine — works, hell slow.
  Tainer must beat it on footprint and file I/O, not just match it.
- Web research is expected (official Microsoft docs, incumbents' source
  and issue trackers); cite everything — the dossier outlives tonight.

## Acceptance

- Dossier exists, covers every topic above, each section with sources;
  decision matrix present; portability audit has file:line specifics.
- `GOOS=windows GOARCH=amd64 go build ./cmd/tainer ./pkg/...` (tainer)
  exits 0, and `make cross` runs green locally (tainer-menu excluded by
  design); `go test ./...` stays green on darwin.
- No behavior change for darwin/linux builds (diff limited to new
  `_windows.go` files, build tags, seam extractions, Makefile/CI).

## Left to the builder

- Dossier structure/ordering; depth per topic (stop when a question is
  answered, not when a word count is hit).
- Which build-tag seams the audit shows are needed for the scaffold.
