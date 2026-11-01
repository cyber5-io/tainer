# Native Menu App — macOS (TAIN-114) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The SwiftUI MenuBarExtra replacement for the Go tray app on macOS: live watch-stream state, quick pod actions, Manage deep-links into my.tainer.me.

**Architecture:** A SwiftPM executable package at `apps/menu-macos/` in the tainer repo, bundled into a `.app` by a script (no `.xcodeproj`). Strict client of the CLI JSON contract: state in via a `tainer watch --json` subprocess, actions out via short-lived CLI spawns. Zero `pkg/tainer` knowledge; the contract doc is the only interface.

**Tech Stack:** Swift 6.2 / SwiftUI `MenuBarExtra`, macOS 14+ deployment target, swift-testing (or XCTest if the toolchain's swift-testing is unavailable headless) via `swift test`.

**Spec:** docs/superpowers/specs/2026-10-31-menu-app-design.md. Wire contract: docs/api/cli-json-v1.md (millisecond timestamps are contractual — Swift's `ISO8601DateFormatter` with fractional seconds parses them).

## Global Constraints

- Branch: `TAIN-114-menu-app` cut from `0d624fae63` (TAIN-111 tip, for the dev CLI used in smoke). All work inside `apps/menu-macos/` plus a packaging note — no Go code changes.
- UX copy never says podman/docker/engine/VM/container. The stack is "tainer" or "the stack".
- No login, no account awareness, no settings UI beyond what the spec names. No dock icon (`LSUIElement`).
- Brand: per-theme logo lockups from `packaging/brand/` (light wordmark on dark menus, dark on light), never the white-pill fallback. Links go through `tainer.dev/go/tainerapp` / `cyber5.io/go/tainerapp`.
- Consumers must tolerate unknown slugs, unknown event types, unknown fields, and one invalid final stream line (contract §3–4).
- CLI discovery order: `TAINER_CLI` env override → `/opt/tainer/bin/tainer` → `tainer` on PATH (mirrors the Go app's resolution; verify its exact order in `cmd/tainer-menu` and match it).
- `swift build`/`swift test` must pass headless (no Xcode GUI, no signing) — signing/notarisation is explicitly a human step, documented not automated.
- No Co-Authored-By or AI mentions in commits.

## Review Focus

1. Old installed CLI without `watch` (pre-1.4): the spawn prints plain text usage on stderr and exits 1 with no JSON — the app must land in the unreachable state with a "update tainer" hint, not crash or spin (pinned by T3 `oldCLIUsageTextLandsUnreachableWithHint`).
2. The one-truncated-line death: daemon dies mid-write → invalid final JSON line then EOF — must be skipped, then `watch-ended` handling respawns with backoff (pinned by T2/T3 tolerance tests).
3. Backoff discipline: a daemon that stays down must not fork a subprocess storm — capped exponential backoff, and the escalation to the red ✗ only after sustained unreachability (pinned by T3 timing tests with injected clock).
4. Menu bar theme flips (light↔dark wallpaper changes) must swap logo lockups and keep the state icon legible — template rendering verified by hand in the smoke, lockup selection unit-tested.
5. An action that fails (e.g. `stop` on an already-stopped pod) must revert the row and post one notification with the envelope's `message` — never a dialog, never a stuck spinner (pinned by T5 failure-path test).

---

### Task 1: Package scaffold + app bundle script

**Files:**
- Create: `apps/menu-macos/Package.swift`, `apps/menu-macos/Sources/TainerMenu/TainerMenuApp.swift`, `apps/menu-macos/Resources/Info.plist`, `apps/menu-macos/scripts/make-app.sh`, `apps/menu-macos/README.md`
- Test: `apps/menu-macos/Tests/TainerMenuTests/SmokeTest.swift` (a trivially-true test proving the test target runs)

**Interfaces:**
- Produces: `swift build` and `swift test` green in `apps/menu-macos/`; `scripts/make-app.sh` assembles `build/Tainer Menu.app` (executable + Info.plist with `LSUIElement=true`, `CFBundleIdentifier=io.cyber5.tainer.menu`, min macOS 14) that launches showing a placeholder MenuBarExtra. Brand SVG→PNG assets generated with the existing `packaging/brand/svg2png.swift` into `Sources/TainerMenu/Resources/` (both theme lockups, pod glyph, bar-icon mark) and loaded via SwiftPM resources.

- [ ] **Step 1:** Scaffold Package.swift (executable target + test target, platforms `[.macOS(.v14)]`, resources processed) and the placeholder `MenuBarExtra("Tainer") { Text("…") }`.
- [ ] **Step 2:** `swift build && swift test` — PASS. `scripts/make-app.sh && open` the bundle manually only in the final smoke; here assert the bundle structure (plist keys) in the script's own `--check` mode.
- [ ] **Step 3:** Commit `feat(menu-macos): SwiftPM scaffold and app bundle script (TAIN-114)`

### Task 2: Contract decoding layer

**Files:**
- Create: `Sources/TainerMenu/Contract/Envelope.swift`, `Contract/Event.swift`
- Test: `Tests/TainerMenuTests/ContractTests.swift`

**Interfaces:**
- Produces:
  - `enum Event: Equatable { case hello(engine: EngineState, pods: [Pod]); case engine(EngineState); case pod(Pod); case progress(Progress); case unknown }`
  - `struct EngineState: Equatable { var state: HealthState; var since: Date; var detail: String }`, `enum HealthState: String { case healthy, recovering, failed }` (unknown raw values decode to a stored string, not a crash — use a wrapper that keeps the raw value).
  - `struct Pod: Equatable, Identifiable { var name: String; var domain: String; var state: PodState; var running: Int; var total: Int }`, `enum PodState` with the same unknown tolerance.
  - `struct Progress: Equatable { var op, pod, step, message: String; var percent: Int; var done: Bool }`
  - `enum StreamLine { case event(Event); case errorEnvelope(code: String, message: String); case invalid }` with `static func parse(_ line: String) -> StreamLine` — the single entry point; top-level `error` object ⇒ `errorEnvelope` (discriminator rule); unparseable ⇒ `invalid`.
  - Millisecond RFC3339 date decoding helper used by every payload.

- [ ] **Step 1: Write failing tests** with golden lines copied verbatim from cli-json-v1.md §3–4: hello with pods, hello with `"pods":[]`, engine, pod, progress with `percent:-1`, the `watch-ended` envelope, an unknown `type` ⇒ `.event(.unknown)`, an unknown pod `state` string decodes without throwing, a truncated line ⇒ `.invalid`, millisecond timestamp parses to the exact Date.
- [ ] **Step 2:** `swift test` — FAIL, implement, PASS.
- [ ] **Step 3:** Commit `feat(menu-macos): JSON contract layer (TAIN-114)`

### Task 3: Watch subprocess client + stack state

**Files:**
- Create: `Sources/TainerMenu/WatchClient.swift`, `Sources/TainerMenu/StackState.swift`, `Sources/TainerMenu/CLILocator.swift`
- Test: `Tests/TainerMenuTests/WatchClientTests.swift`, `StackStateTests.swift`

**Interfaces:**
- Consumes: Task 2's `StreamLine.parse`.
- Produces:
  - `CLILocator.find() -> String?` per the Global Constraints order.
  - `@Observable final class StackState`: `var engine: BarHealth` (`.healthy(runningPods: Int)`, `.idle`, `.recovering`, `.unreachable` — the four bar-icon states), `var pods: [Pod]` (stable order: running first, then name), `var statusLine: (pill: String, counts: String)`, `var busy: Set<String>` (pod names with an in-flight action).
  - `final class WatchClient` — protocol-injected process runner + clock for tests: spawns `tainer watch --json`, feeds lines to a reducer closure, respawns on exit with capped exponential backoff (1s doubling to 30s), resets backoff after a stream that delivered a hello. Reducer rules: hello replaces; `engine` events map healthy→(running count derived)/recovering→`.recovering`; spawn failure, `stack-*` first-line envelope, or old-CLI usage-text exit escalate to `.recovering` immediately and `.unreachable` after 60s of sustained failure (injected clock); a successful hello clears escalation. One `.invalid` line is skipped silently.

- [ ] **Step 1: Write failing tests** — `helloPopulatesState`, `podEventUpserts`, `backoffCapsAtThirtySeconds`, `backoffResetsAfterHello`, `oldCLIUsageTextLandsUnreachableWithHint` (runner yields exit 1, stderr "usage:", no stdout ⇒ state `.recovering` with hint "update tainer", `.unreachable` after 60s fake-clock), `invalidFinalLineSkippedThenEndedRespawns`, `sigintDuringQuitDoesNotRespawn` (stop() terminates child and halts the loop).
- [ ] **Step 2:** `swift test` — FAIL, implement, PASS.
- [ ] **Step 3:** Commit `feat(menu-macos): watch client and stack state (TAIN-114)`

### Task 4: Dropdown UI + bar icon

**Files:**
- Create: `Sources/TainerMenu/Views/MenuContent.swift`, `Views/PodRow.swift`, `Views/StatusHeader.swift`, `Views/BarIcon.swift`; Modify: `TainerMenuApp.swift`
- Test: `Tests/TainerMenuTests/ViewModelTests.swift` (presentation logic only — row icon/action sets per state; no snapshot tests)

**Interfaces:**
- Consumes: `StackState`.
- Produces: the approved canvas layout top-to-bottom — brand header (per-theme lockup + version chip from the app bundle version), status line (state-coloured pill HEALTHY/RECOVERING/UNREACHABLE + "N of M pods running" with emphasised counts), PODS section (pod box-icon teal running / grey stopped, name over domain, inline icon buttons: open-in-browser + restart + stop for running, start for stopped, always a teal Manage icon → `https://my.tainer.me/pods/<name>` via `NSWorkspace.shared.open`), then Open Control Dashboard (→ `https://my.tainer.me`), Run Doctor — check & heal, Quit Tainer Menu (quits the app only), footer Cyber5 lockup + "tainer is a Cyber5 product" → `/go/` link. Bar icon: template-rendered mark with the four states (full colour, dimmed, pulsing recovering, red ✗ unreachable). Pod list beyond 12 rows scrolls (`ScrollView` with max height). `MenuBarExtra` uses `.menuBarExtraStyle(.window)` (inline action icons need a window-style dropdown).

- [ ] **Step 1: Write failing presentation tests** — `runningRowActions == [open, restart, stop, manage]`, `stoppedRowActions == [start, manage]`, `busyRowShowsSpinnerAndDisablesActions`, `statusPillTextAndToneForEachHealthState`, `manageURLForPod`.
- [ ] **Step 2:** `swift test` — FAIL, implement views + view-model helpers, PASS. `swift build` clean.
- [ ] **Step 3:** Commit `feat(menu-macos): dropdown UI and bar icon states (TAIN-114)`

### Task 5: Actions

**Files:**
- Create: `Sources/TainerMenu/ActionRunner.swift`; Modify: `Views/PodRow.swift`, `Views/MenuContent.swift`
- Test: `Tests/TainerMenuTests/ActionRunnerTests.swift`

**Interfaces:**
- Consumes: `CLILocator`, `StackState.busy`, Task 2's `StreamLine.parse` for result parsing.
- Produces: `ActionRunner.run(_ action: PodAction, pod: String?) async` where `PodAction ∈ {start, stop, restart, doctorFix}` — spawns `tainer <cmd> [pod] --json` (doctor: `doctor --fix --json`), marks the pod busy for the duration, on success relies on the watch stream to flip the row (no local state mutation beyond clearing busy); on an error envelope or spawn failure clears busy and posts a `UserNotifications` banner "〈pod〉: 〈message〉" (one per failure, no dialogs). Stop/restart on the `control` pod never appear (it is absent from the stream by contract).

- [ ] **Step 1: Write failing tests** with an injected runner — `successClearsBusyWithoutMutatingPods`, `envelopeFailurePostsNotificationAndReverts` (notification sink injected), `doctorFixSpawnsCorrectArgv`, `concurrentActionOnSamePodRefused`.
- [ ] **Step 2:** `swift test` — FAIL, implement, PASS.
- [ ] **Step 3:** Commit `feat(menu-macos): pod actions with watch-confirmed state (TAIN-114)`

### Task 6: Packaging notes, parity checklist, live smoke

**Files:**
- Modify: `apps/menu-macos/README.md` (packaging/signing section, parity table vs `cmd/tainer-menu`); Create: nothing else committed
- Test: full `swift test` + `go test ./...` (prove the Go tree is untouched)

**Steps:**
- [ ] **Step 1:** README: how `make-app.sh` output slots into the existing .pkg pipeline (locate the pkg payload assembly under `packaging/` and name the exact insertion point), signing + notarisation as explicit human steps, and the parity checklist: every feature of today's `cmd/tainer-menu` mapped to its SwiftUI equivalent or an explicit "webapp's job" line.
- [ ] **Step 2:** Build the .app via `make-app.sh`. Launch it against the REAL running stack (read-only: watch stream + rendering only — do not exercise start/stop/doctor against production pods). Verify: bar icon appears, dropdown lists the real pods with correct states, Manage opens the browser. Capture a screenshot for the report. Quit the app.
- [ ] **Step 3:** `swift test` and repo-root `go test ./...` both green. Commit `docs(menu-macos): packaging and parity notes (TAIN-114)`
