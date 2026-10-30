# Windows Port Recon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `GOOS=windows` a CI-enforceable invariant (seams + `make cross`), and produce the sourced research dossier + portability audit that the Windows engine-architecture decision will be made from.

**Architecture:** Two code tasks extract the only unix-only call sites behind `_unix.go`/`_windows.go` build-tag seams in `pkg/tainer/runtime` (plus one SIGWINCH seam in cmd/tainer) and add a `make cross` target; two document tasks research and write the dossier. No Windows behavior is implemented — windows stubs error honestly.

**Tech Stack:** Go build tags, Make, web research.

**Spec:** `docs/superpowers/specs/2026-10-30-windows-port-recon-design.md`

**Tracker:** TAIN-106. Branch: `TAIN-106-windows-recon` from `main` (tainer repo only; cyber-stack gets audited, not modified).

## Global Constraints

- Hard product constraint: Windows **Home** must work; **8GB RAM** is the design floor; the bar is Docker Desktop on an 8GB machine ("works, hell slow"). Both test laptops are **Intel/AMD (amd64)** — Windows-on-ARM is documented as out of scope for the gate, one paragraph max in the dossier.
- `cmd/tainer-menu` is EXCLUDED from all cross-build gates (darwin-only vendored systray fork; do not touch it, do not shim it).
- Windows stubs must error honestly ("not supported on Windows yet"), never silently no-op — with the one documented exception called out in Task 1.
- Shims live in `_windows.go`/`_unix.go` files; shared code is only touched to extract a seam, never to branch on GOOS inline.
- No behavior change for darwin/linux: `go test ./...` on darwin green at every commit; diff limited to new build-tagged files, seam extractions, Makefile.
- No Co-Authored-By or AI mentions in commits.
- Dossier file: `docs/superpowers/specs/2026-10-31-windows-port-recon.md` — every claim sourced (link or repo file:line); it outlives tonight.

## Review Focus

- `tainer status` on darwin must behave exactly as before the seam extraction (PID probe semantics unchanged) — pinned by the existing runtime tests plus TestProcessAlive in Task 1.
- `tainer network <mode>`'s daemon stop path (SIGTERM → poll → SIGKILL) must keep its 30s deadline semantics through the refactor — pinned by reading networkcmd's existing tests; if none cover stopDaemon, note that in the task's commit message rather than adding engine-dependent tests tonight.
- A future `GOOS=windows` regression must fail CI loudly — `make cross` is that gate; Task 2 verifies it fails when given a deliberately broken file (then reverts it).
- The dossier's decision matrix must compare like for like at the 8GB floor (idle RSS, cold-start, file-I/O class) — not vendor marketing numbers; every cell sourced or marked "needs laptop".
- Exec resize on Windows degrades to attach-time sizing only (no SIGWINCH equivalent wired tonight) — documented in the audit, not silently lost.

---

### Task 1: Process-management seams in pkg/tainer/runtime (+ consumers)

**Files:**
- Create: `pkg/tainer/runtime/proc_unix.go` (build tag `//go:build !windows`)
- Create: `pkg/tainer/runtime/proc_windows.go` (build tag `//go:build windows`)
- Modify: `pkg/tainer/runtime/runtime.go:108-122` (CurrentStatus) and `:169-215` (startDaemon)
- Modify: `pkg/tainer/networkcmd/networkcmd.go:148-170` (stopDaemon)
- Create: `cmd/tainer/resize_unix.go`, `cmd/tainer/resize_windows.go`
- Modify: `cmd/tainer/main.go:1963-1980` (the SIGWINCH block in the exec path)
- Test: `pkg/tainer/runtime/proc_test.go`

**Interfaces:**
- Produces (exported, the Linux plan builds on this package layout):
  - `runtime.ProcessAlive(pid int) bool` — unix: `syscall.Kill(pid, 0) == nil`; windows: always `false` (documented exception to the "error, don't no-op" rule: there is no cyberstackd on Windows yet, so "not running" is the truth; revisit when a Windows engine exists).
  - `runtime.SignalTerm(pid int) error` / `runtime.SignalKill(pid int) error` — unix: `syscall.Kill(pid, SIGTERM/SIGKILL)`; windows: return `ErrUnsupportedOS`.
  - `runtime.ErrUnsupportedOS = errors.New("not supported on Windows yet")` (defined in proc_windows.go's shared sibling or runtime.go — implementer's call, one definition).
  - unexported `detachAttr() *syscall.SysProcAttr` — unix: `&syscall.SysProcAttr{Setsid: true}`; windows: `nil`.
  - In cmd/tainer: `watchResize(doResize func())` — unix: SIGWINCH loop (today's goroutine verbatim); windows: no-op (resize happens once at attach).

- [ ] **Step 1: Write the failing test** `pkg/tainer/runtime/proc_test.go`:

```go
func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("ProcessAlive(self) = false, want true")
	}
	// A PID from the kernel's usual range that cannot be us. Spawn-and-reap
	// a child to get a guaranteed-dead PID instead of guessing.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(cmd.Process.Pid) {
		t.Errorf("ProcessAlive(reaped child %d) = true, want false", cmd.Process.Pid)
	}
}

func TestSignalTermStopsChild(t *testing.T) {
	// Only ever signal a child we own — a reaped PID can be reused by an
	// unrelated process, and TERMing that would be a real casualty.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := SignalTerm(cmd.Process.Pid); err != nil {
		t.Fatalf("SignalTerm(live child) = %v", err)
	}
	err := cmd.Wait()
	if err == nil {
		t.Error("child exited cleanly, want killed by SIGTERM")
	}
}
```

(The ProcessAlive dead-pid assertion carries a tiny PID-reuse flake risk; it is a harmless probe, acceptable. Never add a test that *signals* a non-child PID.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/tainer/runtime/ -run 'TestProcessAlive|TestSignalTerm' -v`
Expected: compile error `undefined: ProcessAlive`.

- [ ] **Step 3: Implement the seams**

`proc_unix.go`: the four functions above over `syscall`. `proc_windows.go`: the stubs above. Then rewire the callers:
- `runtime.go:113`: `if ProcessAlive(st.PID) { st.Running = true }` (drop the inline syscall.Kill and the `syscall` import if now unused).
- `runtime.go:202`: `cmd.SysProcAttr = detachAttr()`.
- `startDaemon` (runtime.go:169): first line gains the honest gate — on windows return `ErrUnsupportedOS` wrapped with context (`"cannot start the tainer engine: %w"`). Implement as a tiny `func canSpawnDaemon() error` in the proc files (unix: nil; windows: the error) so startDaemon itself stays tag-free.
- `networkcmd.go:156-166`: replace the three `syscall.Kill` calls with `runtime.SignalTerm(pid)`, `!runtime.ProcessAlive(pid)` (poll), `runtime.SignalKill(pid)`. networkcmd already imports the runtime package (check; add if not).
- `cmd/tainer/main.go:1973`: replace the `signal.Notify(ch, syscall.SIGWINCH)` + goroutine block with `watchResize(doResize)`; `resize_unix.go` holds today's code verbatim (chan, Notify, goroutine), `resize_windows.go` is `func watchResize(func()) {}`.

- [ ] **Step 4: Verify green on both GOOS**

Run: `go test ./pkg/tainer/runtime/ ./pkg/tainer/networkcmd/ -v 2>&1 | tail -15` — Expected: PASS.
Run: `GOOS=windows GOARCH=amd64 go build ./cmd/tainer ./pkg/...` — Expected: exit 0, no output. (If a further package now surfaces windows errors that runtime was masking, extract the same way — seam file pairs, honest stubs — and list each in the commit message.)
Run: `go test ./... 2>&1 | tail -10` — Expected: green.
Run: `gofmt -l cmd pkg && go vet ./...` — Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/runtime/ pkg/tainer/networkcmd/ cmd/tainer/
git commit -m "feat(runtime): per-OS process seams — GOOS=windows builds (TAIN-106)"
```

### Task 2: `make cross` target

**Files:**
- Modify: `Makefile` (new target + .PHONY entry)

**Interfaces:**
- Consumes: Task 1 (windows build must already be green).
- Produces: `make cross` — the cross-compile gate later CI and the Linux plan reuse. Target list is explicit, NOT `./...`: `./cmd/tainer ./pkg/...` (tainer-menu excluded by design).

- [ ] **Step 1: Add the target** to the Makefile, following its existing style (tabs, comment explaining why the target list is explicit — tainer-menu is darwin-only by design):

```make
# Cross-compilation gate: every supported GOOS/GOARCH must compile.
# Explicit target list — cmd/tainer-menu is darwin-only (vendored systray)
# and excluded by design. Build-only (no artifacts kept): -o /dev/null
# works per-binary, but pkg/... is a compile check, so plain `go build`.
cross:
	GOOS=darwin GOARCH=arm64 go build ./cmd/tainer ./pkg/...
	GOOS=linux GOARCH=amd64 go build ./cmd/tainer ./pkg/...
	GOOS=linux GOARCH=arm64 go build ./cmd/tainer ./pkg/...
	GOOS=windows GOARCH=amd64 go build ./cmd/tainer ./pkg/...
```

Add `cross` to `.PHONY`.

- [ ] **Step 2: Verify it passes, then verify it can fail**

Run: `make cross` — Expected: exit 0.
Negative proof: temporarily add `var _ = syscall.Kill` to any always-built file in pkg/tainer/runtime (e.g. runtime.go), run `make cross`, Expected: FAIL on the windows leg; revert the line, run `make cross` again, Expected: exit 0. This proves the gate actually gates.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: make cross — darwin/linux/windows compile gate (TAIN-106)"
```

### Task 3: Portability audit (tainer + cyber-stack)

**Files:**
- Create: `docs/superpowers/specs/2026-10-31-windows-port-recon.md` (this task creates the file with the audit as its "Part 2 — Portability audit" section plus a stub header; Task 4 fills Part 1 above it)

**Interfaces:**
- Consumes: Task 1's seam layout (cite the new proc_unix/proc_windows files as the house pattern, alongside the pre-existing `pkg/tainer/identity/identity_unix.go`/`identity_windows.go`).
- Produces: the audit section later tasks and the follow-up Windows design doc cite.

- [ ] **Step 1: Sweep tainer.** Every unix assumption with file:line and a one-line porting note. Search systematically, minimum sweeps: `grep -rn "syscall\." pkg/ cmd/ --include="*.go" | grep -v _test | grep -v _unix`, `grep -rn "unix://\|\.sock\|SIGTERM\|SIGHUP\|/etc/\|/opt/\|/usr/\|0o7\|os.Chmod\|exec.Command" pkg/ cmd/ --include="*.go" | grep -v _test`, plus `~/.cyberstack`/XDG dir resolution, `filepath` vs hardcoded `/` joins, launchd/pid-file daemon management, resolver setup (`pkg/tainer/dns/resolver.go` darwin + linux paths — no windows path exists), `pkg/tainer/router` (`:22`/`:2222` bind), cert paths. Each finding: `file:line — what — porting note (seam / NSSM-service / registry / named-pipe / etc.)`.

- [ ] **Step 2: Sweep cyber-stack** (`/Users/lenineto/dev/cyber5-io/cyber-stack`, READ ONLY — audit, no code changes). Windows lens on top of the 2026-10-30 Linux recon (cite it as input, do not re-derive): unix socket serving (httpapi), vsock transport (AF_VSOCK host CID dialing — what replaces it under WSL2/Hyper-V: AF_HYPERV vs localhost TCP vs named pipe), vfkit launcher (darwin-only; the windows analog is the dossier's engine question), path assumptions, signal lifecycle (SIGTERM shutdown), `/opt/tainer/bin` helper exec, data-disk/storage paths. Confirm and state: there are no `_windows.go` files or windows build tags anywhere in cyber-stack today.

- [ ] **Step 3: Write Part 2 into the dossier file** with this structure: `## Part 2 — Portability audit` → `### tainer` (table: location / assumption / porting note), `### cyber-stack` (same), `### Seam inventory` (what exists: identity, proc, resize seams; what's missing), `### Exec/TTY note` (resize degradation on windows, from Task 1).

- [ ] **Step 4: Verify completeness by machine**

Run: `grep -c ':[0-9]' docs/superpowers/specs/2026-10-31-windows-port-recon.md` — Expected: ≥ 30 (file:line specifics, not vibes). Spot-check 5 random cited file:line pairs actually contain what the audit claims (`sed -n '<line>p' <file>`).

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-10-31-windows-port-recon.md
git commit -m "docs: windows portability audit — tainer + cyber-stack (TAIN-106)"
```

### Task 4: Research dossier

**Files:**
- Modify: `docs/superpowers/specs/2026-10-31-windows-port-recon.md` (fill `## Part 1 — Engine research` above the audit)

**Interfaces:**
- Consumes: Task 3's audit (the transport/file-sharing sections should reference which audited seams each engine option would satisfy).

- [ ] **Step 1: Research and write, section by section.** Web research expected (official Microsoft docs, Docker/Podman/Rancher source + issue trackers); every claim linked. Sections, each ending in a one-paragraph "what this means for tainer":
  1. **Engine host options** — WSL2 (custom distro via `wsl --import`, wsl.exe control surface, WSL2 VM lifecycle realities), raw Virtual Machine Platform / Windows Hypervisor Platform APIs from Go, full Hyper-V. Per option: Windows edition requirements (Home MUST work — any Pro-only option is documented then deprioritized), 8GB-machine memory behavior, control surface from a Go host process, installer implications.
  2. **Incumbents** — Docker Desktop, Podman machine, Rancher Desktop: which primitive each uses, and the top user complaints with links (vmmem ballooning, 9P file I/O, clock drift, firewall prompts).
  3. **Socket transport host↔guest** — AF_UNIX on Windows 10 1803+ (and its WSL2 interop limits), named pipes, AF_HYPERV/vsock, localhost TCP: which can carry cyberstackd's Docker-API socket, what Docker Desktop itself ships.
  4. **File sharing** — 9P (`\\wsl$`/drvfs) performance class, virtiofs availability on Windows, "code inside the VM" strategies, Dev Drive — mapped against tainer's bind-mount model and the macOS virtio-fs lessons (cite the filesystem roadmap memory/spec).
  5. **Ports & firewall** — forwarding to host localhost and :80/:443 (router model), WSL2 localhostForwarding, Windows Defender Firewall prompt UX.
  6. **Memory discipline** — .wslconfig limits, autoMemoryReclaim, vs owning VM lifecycle the way cyberstackd does on macOS; what fits in 8GB alongside a browser and an editor.
  7. **Packaging & trust** — MSI vs MSIX vs winget; Authenticode OV vs EV cost and SmartScreen reputation mechanics; auto-update patterns (the analog of our notarized .pkg + postinstall).
  8. **Decision matrix + provisional recommendation** — rows = options, columns = reach (Home?), 8GB behavior, control surface, dev effort, distribution; every cell sourced or marked "needs laptop". Recommendation clearly labeled PROVISIONAL — the decision is made with Leni after the laptop exists.

- [ ] **Step 2: Verify dossier completeness**

Run: `grep -c 'http' docs/superpowers/specs/2026-10-31-windows-port-recon.md` — Expected: ≥ 25 (sourced claims). Check all 8 Part-1 section headings exist: `grep -n '^###' docs/superpowers/specs/2026-10-31-windows-port-recon.md`. The decision matrix renders as a markdown table with ≥ 3 option rows.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-10-31-windows-port-recon.md
git commit -m "docs: windows engine research dossier — WSL2/VMP/Hyper-V, decision matrix (TAIN-106)"
```
