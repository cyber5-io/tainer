# Host Bridge Implementation Plan (TAIN-111)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Go-side UI foundation: a daemon event bus with `tainer watch --json`, a versioned v1 JSON envelope across every CLI command, and a token-gated control gateway that lets the control pod drive the host CLI — then prove the stream by migrating tainer-menu onto it.

**Architecture:** cyberstackd grows an in-process event bus fed by a poll-and-diff state sampler (engine + pod states, incl. the recovering/failed timing that today lives in tainer-menu) plus a progress-ingestion route; host clients subscribe over the existing Unix-socket HTTP API (`/cyberstack/events`), the control pod over a new pod-network-only control listener guarded by a per-boot bearer token whose API re-exposes the CLI 1:1 by spawning `tainer <cmd> --json` (argv exec, no shell). tainer gains the envelope (`"v":1`), `watch`, `api`, the missing `--json` modes, and progress reporting; tainer-menu drops its `pkg/tainer` imports and becomes the reference watch client.

**Tech Stack:** Go (both repos); NDJSON over HTTP (chunked) for streams; no new daemon, no new binary.

**Spec:** `docs/superpowers/specs/2026-10-31-host-bridge-design.md` (consumers: `2026-10-31-control-webapp-design.md`, `2026-10-31-menu-app-design.md`).

## Global Constraints

- tainer branch `TAIN-111-host-bridge` cut from `TAIN-105-linux-native` tip `5fb618c179`; worktree under `.claude/worktrees/TAIN-111-host-bridge`. Merge order to main: 106 → 105 → 111.
- cyber-stack branch `host-bridge` cut from `linux-native-phase1` tip `ac11862`, in a new worktree `.claude/worktrees/host-bridge` of the cyber-stack repo. Merge order: `linux-native-phase1` → `host-bridge`. Never write to the main cyber-stack checkout.
- Nothing merged or pushed. Dev/test on macOS: gates are `go test ./...` and `make cross` in each repo; linux-only files additionally compile-gated via `GOOS=linux GOARCH=amd64 go build -tags containers_image_openpgp ./...` (cyber-stack builds always carry `-tags containers_image_openpgp`).
- The v1 envelope and event shapes in this plan's Interfaces blocks are a **public contract** — the control webapp and menu app are written against them verbatim; do not rename fields while implementing.
- Existing `--json` output keys must not change — `"v":1` is additive (Task 4 locks this with snapshot tests).
- No web terminal anywhere. No AI attribution in commits or files. Single-line shell commands in docs/scripts. User-facing copy and event/error strings never say podman/docker/engine/VM/container (event `type` values are `engine`/`pod`/`progress` — "engine" as an event-family identifier is machine-surface, not UX copy, and is the spec's own naming).
- Idle cost: the sampler tick is 2s with no allocations on no-change; no new always-on process.
- **Deliberately out of scope here:** `tainer login` and the host-side session store (spec §c, last bullet). Their API contract is the accounts spec's; they land with the TAIN-112 integration. Nothing in this plan needs them — no CLI command is login-gated yet, and the gateway relays whatever gate the CLI enforces, today and later, without change.

## Review Focus

1. A stalled watch subscriber (never reads) must not block the bus, the daemon loop, or other subscribers — pinned by `TestBusSlowSubscriberDropped` (Task 1).
2. A pod-network request without (or with a wrong) bearer token gets 401 and no CLI spawn — pinned by `TestControlAuthRequired` (Task 8).
3. Every `--json` failure path emits `{"v":1,"error":{…}}` on stdout and exits nonzero — pinned by `TestJSONErrorEnvelopeAndExit` (Task 4) and the per-command tests in Task 6.
4. Daemon restart mid-stream: `tainer watch` ends with an error envelope and nonzero exit (no auto-reconnect in v1; clients respawn) — pinned by `TestWatchStreamEOF` (Task 5).
5. Gateway command injection: argv-array exec, rejected subcommands (`ssh`, `menu`, unknown), no shell interpretation — pinned by `TestGatewayRejectsUnknownAndInteractive` (Task 8).

---

### Task 1: cyber-stack — events package (bus + types + snapshot)

**Files:**
- Create: `internal/events/types.go`, `internal/events/bus.go`
- Test: `internal/events/bus_test.go`

**Interfaces (Produces — the v1 event contract, verbatim):**
- Envelope (one NDJSON line): `{"v":1,"ts":"<RFC3339>","type":"hello|engine|pod|progress","data":{…}}`
- `engine` data: `{"state":"healthy|recovering|failed","since":"<RFC3339>","detail":"<string>"}`
- `pod` data: `{"name":"<string>","domain":"<string>","state":"running|stopped|partial","running":<int>,"total":<int>}`
- `progress` data: `{"op":"init|start|stop|db-export|db-import|update","pod":"<string>","step":"<string>","percent":<int -1..100>,"message":"<string>","done":<bool>}`
- `hello` data: `{"engine":<engine data>,"pods":[<pod data>…]}` — always the first line of any subscription.
- Go API: `type Event struct {V int; TS time.Time; Type string; Data any}` with `MarshalJSON` producing the exact envelope; `type EngineData struct {State string; Since time.Time; Detail string}`, `type PodData struct {Name, Domain, State string; Running, Total int}`, `type ProgressData struct {Op, Pod, Step string; Percent int; Message string; Done bool}` (all with JSON tags matching the wire shapes above); `func NewBus(buffer int) *Bus`; `(*Bus).Publish(Event)`; `(*Bus).Subscribe(ctx) (<-chan Event, func())` — the returned channel is primed with the hello event built from `(*Bus).SetSnapshot(engine EngineData, pods []PodData)` state; `(*Bus).Snapshot()` for tests.
- Slow-consumer policy: per-subscriber buffered channel (size = `buffer`); on overflow the subscriber is dropped (channel closed), never blocking `Publish`.

- [ ] **Step 1: Write failing tests** — `TestEnvelopeShape` (marshal one event of each family, compare against the exact JSON literals above, key-for-key), `TestHelloFirst` (subscribe after `SetSnapshot`; first event is `hello` carrying that state), `TestBusSlowSubscriberDropped` (subscriber that never reads; publish `buffer+2` events; `Publish` returns without blocking — guard with a timeout — and the laggard's channel is closed; a second, draining subscriber received every event).
- [ ] **Step 2: Run tests, confirm FAIL** — `go test ./internal/events/` fails: package does not exist.
- [ ] **Step 3: Implement types.go and bus.go** — plain mutex+slice-of-subscribers bus; no goroutine per publish.
- [ ] **Step 4: Run** — `go test ./internal/events/` PASS; `go vet ./internal/events/` clean.
- [ ] **Step 5: Commit** — `git add internal/events && git commit -m "feat(events): v1 event bus with hello snapshot and drop-on-stall policy"`

### Task 2: cyber-stack — state sampler + daemon wiring

**Files:**
- Create: `internal/daemon/events.go` (sampler), `internal/daemon/events_test.go`
- Modify: `internal/daemon/daemon.go` (own a `*events.Bus`, start/stop the sampler in `Run` for every backend)

**Interfaces:**
- Consumes: Task 1's bus.
- Produces: `func newStateSampler(bus *events.Bus, src stateSource, tick, failAfter time.Duration) *stateSampler` with `Run(ctx)`; `type stateSource interface { EngineHealthy(ctx) (bool, string); ListPodStates(ctx) ([]events.PodData, error) }` — the daemon implements `stateSource` over its existing backend state (container list grouped by the tainer project label; `domain` resolved from the manifest-path label when readable, else `""`). Emits an event only on change (diff against last sample); maintains `recovering` (unhealthy < failAfter) vs `failed` (≥ failAfter, default 60s) daemon-side; calls `bus.SetSnapshot` every tick.

- [ ] **Step 1: Write failing tests** against a fake `stateSource`: `TestSamplerEmitsOnChangeOnly` (two identical samples → one engine + N pod events total), `TestSamplerRecoveringToFailed` (source unhealthy; with `failAfter` 30ms and tick 10ms, events go healthy→recovering→failed exactly once each), `TestSamplerSnapshotKeptCurrent`.
- [ ] **Step 2: Run** — `go test ./internal/daemon/ -run TestSampler` FAIL (undefined).
- [ ] **Step 3: Implement sampler; wire into daemon.go** — bus created in `New`, sampler started in `Run` regardless of backend; the `stateSource` implementation reuses existing daemon container/VM state accessors (read-only; one line of adaptation per backend, no behavior change elsewhere).
- [ ] **Step 4: Run** — `go test ./internal/daemon/` and full `go test ./...` PASS; `GOOS=linux GOARCH=amd64 go build -tags containers_image_openpgp ./...` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(daemon): state sampler feeding the event bus (engine/pod deltas, failed-after timing)"`

### Task 3: cyber-stack — host events + progress routes on the Unix-socket API

**Files:**
- Create: `internal/httpapi/events.go`, `internal/httpapi/events_test.go`
- Modify: `internal/httpapi/server.go` (route registration; server gains access to the daemon's bus)

**Interfaces:**
- Produces: `GET /cyberstack/events` — chunked NDJSON, hello first, one envelope per line, connection held until client close or daemon stop; `POST /cyberstack/progress` — body is one `progress` data document (shape from Task 1), republished on the bus as a `progress` event; 400 on malformed body. Both live on the existing host Unix socket only (trust = socket ownership, same as the engine API).
- Consumed by: tainer Tasks 5 and 7.

- [ ] **Step 1: Write failing tests** with `httptest` over the real handlers: `TestEventsStreamHelloThenDelta` (subscribe, publish, read two NDJSON lines, decode and compare), `TestProgressPostRoundTrip` (POST a progress doc; a subscriber receives it verbatim), `TestProgressRejectsMalformed` (400).
- [ ] **Step 2: Run** — FAIL (routes absent).
- [ ] **Step 3: Implement** — flush after every line (`http.Flusher`); subscriber cancel on request context done.
- [ ] **Step 4: Run** — `go test ./internal/httpapi/` and `go test ./...` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(httpapi): /cyberstack/events NDJSON stream and /cyberstack/progress ingestion"`

### Task 4: tainer — envelope v1 (`"v":1` everywhere + error envelope + exit codes)

**Files:**
- Create: `cmd/tainer/jsonenv.go`, `cmd/tainer/jsonenv_test.go`
- Modify: `cmd/tainer/main.go` (the eight `run*JSON` funcs at lines ~562/762/1185/1375/1592/1777/2748/2885 plus the list/update/network JSON sites)

**Interfaces (Produces — public contract):**
- Every single-shot JSON document gains top-level `"v":1`; all pre-existing keys unchanged.
- Failure with `--json` active: stdout gets exactly `{"v":1,"error":{"code":"<stable-slug>","message":"<human text>"}}`, exit code 1.
- Go: `func jsonDoc(payload any)` (injects `"v":1`, writes stdout), `func jsonFail(code, message string)` (writes error envelope, returns exit code 1 for the caller to pass to `os.Exit` through the existing exit path), `func jsonLine(w io.Writer, payload any) error` for NDJSON writers (also `"v":1`).

- [ ] **Step 1: Write failing tests** — `TestJSONDocCarriesV1` (golden: wrap a sample payload, assert `v==1` and payload keys intact), `TestJSONErrorEnvelopeAndExit` (jsonFail output shape + returned code), plus one snapshot test per existing `run*JSON` family asserting their current keys survive wrapping (build the snapshot from the current output shape before changing it).
- [ ] **Step 2: Run** — `go test ./cmd/tainer/ -run TestJSON` FAIL.
- [ ] **Step 3: Implement jsonenv.go; retrofit each JSON site to route through it** — mechanical; no shape changes beyond adding `v`.
- [ ] **Step 4: Run** — `go test ./...` PASS; `make cross` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(cli): v1 JSON envelope and error contract across all --json output (TAIN-111)"`

### Task 5: tainer — `tainer watch --json`

**Files:**
- Create: `cmd/tainer/watch.go`, `cmd/tainer/watch_test.go`
- Modify: `cmd/tainer/main.go` (dispatch `case "watch"`), `pkg/tainer/engine/client.go` or sibling (new method)

**Interfaces:**
- Consumes: Task 3's `GET /cyberstack/events`; Task 4's `jsonLine`/`jsonFail`.
- Produces: `tainer watch --json` relays the NDJSON stream verbatim to stdout (hello first). Without `--json`: error envelope? No — plain-text usage error on stderr, exit 1 (machine mode is the only mode; message: `tainer watch requires --json`). On stream end (daemon stop/restart): `{"v":1,"error":{"code":"watch-ended","message":…}}` then exit 1 — no auto-reconnect in v1. Engine not running at invocation: watch asks the runtime to ensure the daemon the same way `status` does, then subscribes.
- Go: `(engine) EventsStream(ctx context.Context) (io.ReadCloser, error)` on the engine client (HTTP over the existing Unix socket connection machinery).

- [ ] **Step 1: Write failing tests** — `TestWatchRequiresJSON` (args without --json → exit error path), `TestWatchRelaysNDJSON` (fake Unix-socket server from `httptest` + socket pair serving three envelopes; watch writes them verbatim then the `watch-ended` error on EOF — this is also `TestWatchStreamEOF` from Review Focus, name it so).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — line-copy loop (`bufio.Scanner` with enlarged buffer), flush per line.
- [ ] **Step 4: Run** — `go test ./...` PASS; `make cross` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(cli): tainer watch --json event stream (TAIN-111)"`

### Task 6: tainer — JSON gaps: logs, doctor, config, exec

**Files:**
- Modify: `cmd/tainer/main.go` (the four command paths), with per-command helpers beside their existing code
- Test: `cmd/tainer/json_gaps_test.go`

**Interfaces (Produces — public contract):**
- `logs --json`: NDJSON lines `{"v":1,"pod":"<name>","part":"<role>","ts":"<RFC3339|empty>","line":"<raw>"}`.
- `doctor --json`: one document `{"v":1,"checks":[{"name":"<string>","status":"ok|warn|fail","hint":"<string>"}],"fixed":["<name>"…]}` (fixed present only with `--fix`).
- `config --json`: one document `{"v":1,"project":{<manifest v2 fields as today's config prints them>}}`.
- `exec --json`: non-interactive only (TTY request with --json is an error envelope); NDJSON `{"v":1,"stream":"stdout|stderr","data":"<chunk>"}` lines then a final `{"v":1,"exit":<int>}`.

- [ ] **Step 1: Write failing tests** per shape (drive the formatting helpers directly with fake data — the formatting layer is what the contract pins; daemon round-trips stay out of unit scope): `TestLogsJSONLineShape`, `TestDoctorJSONShape`, `TestConfigJSONShape`, `TestExecJSONStreamShape`, `TestExecJSONRefusesTTY`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement the four, reusing jsonenv helpers; wire `--json` flag parsing to match the house pattern in main.go.**
- [ ] **Step 4: Run** — `go test ./...` PASS; `make cross` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(cli): --json for logs, doctor, config, exec (TAIN-111)"`

### Task 7: tainer — progress reporting from long operations

**Files:**
- Create: `pkg/tainer/progress/progress.go`, `pkg/tainer/progress/progress_test.go`
- Modify: the init/start/db-export/db-import/update code paths (call sites only — one reporter call per existing user-visible step; the current human-readable step prints name the steps)

**Interfaces:**
- Consumes: Task 3's `POST /cyberstack/progress`.
- Produces: `func Reporter(ctx context.Context, op, pod string) func(step, message string, percent int, done bool)` — posts best-effort (50ms timeout, silent no-op when the daemon or route is absent, so an old daemon never breaks the CLI); emits the Task 1 `progress` data shape.

- [ ] **Step 1: Write failing tests** — `TestReporterPostsShape` (httptest server captures the POST; compare document), `TestReporterSilentWhenUnavailable` (no server; returns without error, fast).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement; sprinkle call sites** — init (scaffold/pull/start steps), start (image pull, container start), db export/import (dump/restore), update (pull) — percent `-1` where unknown.
- [ ] **Step 4: Run** — `go test ./...` PASS; `make cross` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(cli): structured progress events from long operations (TAIN-111)"`

### Task 8: cyber-stack — control gateway (pod-network listener + token + CLI relay)

**Files:**
- Create: `internal/controlapi/server.go`, `internal/controlapi/token.go`, `internal/controlapi/server_test.go`
- Modify: `internal/daemon/daemon.go` (start/stop the control listener per backend; write token file)

**Interfaces (Produces — public contract, the control pod's whole API):**
- Bind: darwin → the VM network's host-side address; linux native → `10.113.0.1`; never `0.0.0.0`. Port: one fixed high port (builder picks, document it in the contract doc).
- Auth: `Authorization: Bearer <token>`; token is 32 random bytes hex, minted per daemon boot, written `0600` to `<state-dir>/control.token` (the mount into the control pod is TAIN-113's side); any mismatch → 401, body `{"v":1,"error":{"code":"unauthorized","message":…}}`.
- `POST /v1/cli/{cmd}` body `{"args":["<argv>"…]}` → spawns `<tainer-cli> {cmd} <args…> --json` (argv exec, never a shell; `--json` always appended; CLI path from daemon config, default `/opt/tainer/bin/tainer`, env `CYBERSTACK_TAINER_CLI` override), relays stdout bytes and maps exit!=0 → HTTP 200 with the CLI's own error envelope (the envelope is the contract; HTTP stays transport). Streaming commands (`logs`, `exec`) relay chunked NDJSON as produced.
- Subcommand allowlist = JSON-capable, non-interactive set: `status,list,init,start,stop,restart,destroy,db,update,network,doctor,logs,exec,config,api,watch`; anything else (incl. `ssh`, `menu`) → 400 `{"code":"unsupported-command"}`.
- `GET /v1/watch` → relays `/cyberstack/events` (hello first), same NDJSON.

- [ ] **Step 1: Write failing tests** (listener on 127.0.0.1 in tests; CLI stubbed by a test binary via `CYBERSTACK_TAINER_CLI` pointing at a helper that echoes its argv as JSON): `TestControlAuthRequired` (no/wrong token → 401, helper never ran), `TestGatewayRelaysCLIJSON` (argv passthrough + `--json` appended), `TestGatewayRejectsUnknownAndInteractive` (`ssh`, `frobnicate` → 400), `TestTokenFileWrittenPerBoot` (0600, changes across two mints), `TestWatchRelayHelloFirst`.
- [ ] **Step 2: Run** — FAIL (package absent).
- [ ] **Step 3: Implement; wire into daemon Run** — darwin bind address comes from the same place the VM network setup knows its host address; native from the network params already on this branch; if neither is available (backend none) the listener is skipped with a log line.
- [ ] **Step 4: Run** — `go test ./...` PASS; `GOOS=linux GOARCH=amd64 go build -tags containers_image_openpgp ./...` PASS; `make cross` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(controlapi): pod-network gateway with per-boot token and 1:1 CLI relay"`

### Task 9: tainer — `tainer api --json` + the contract document

**Files:**
- Create: `cmd/tainer/api.go`, `cmd/tainer/api_test.go`, `docs/api/cli-json-v1.md`
- Modify: `cmd/tainer/main.go` (dispatch)

**Interfaces:**
- Produces: `tainer api --json` → `{"v":1,"commands":[{"name":"<cmd>","json":true,"stream":<bool>}…]}` listing every JSON-capable command (stream true for watch/logs/exec). The doc `docs/api/cli-json-v1.md` states: the envelope, the error contract, every event family with one verbatim example line each, every command's document/stream shape (copy the Interfaces blocks of Tasks 1, 4, 5, 6, 8), the gateway routes/auth, and the versioning rule (additive keeps v, breaking bumps with one-minor overlap).
- Consumed by: control webapp and menu app plans — cite this doc, not the code.

- [ ] **Step 1: Write failing test** — `TestAPIListsAllJSONCommands` (golden list; must include watch/logs/exec/config/doctor).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement api.go; write the doc** (examples taken from the tests' golden strings so doc and tests cannot drift apart silently — note in the doc header which test guards each example).
- [ ] **Step 4: Run** — `go test ./...` PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(cli): tainer api discovery + docs/api/cli-json-v1.md contract (TAIN-111)"`

### Task 10: tainer — tainer-menu becomes a watch client

**Files:**
- Modify: `cmd/tainer-menu/main.go` (drop `pkg/tainer/{engine,pod,manifest,runtime}` imports, the 5s `pollLoop`, and local recovering/failed timing)
- Create: `cmd/tainer-menu/watchclient.go`, `cmd/tainer-menu/watchclient_test.go`

**Interfaces:**
- Consumes: `tainer watch --json` (Task 5) as a subprocess; events per Task 1's contract.
- Produces: `func runWatch(ctx context.Context, cli string, onEvent func(Event)) error` — spawns `<cli> watch --json`, decodes envelopes, invokes `onEvent`; on subprocess exit, backs off (1s, 2s, 4s… cap 30s) and respawns, surfacing the gap as an `engine` recovering state to the UI so the bar pulses. App state (icon state, status line, pod rows) now derives purely from hello/engine/pod events; actions still `exec.Command(tainerCLI, verb, name, "--plain")` exactly as today; `TAINER_MENU_ICON` and the `/go/` links unchanged.

- [ ] **Step 1: Write failing tests** — `TestWatchClientDecodesAndDispatches` (fake CLI script emitting hello+pod event; onEvent sees both), `TestWatchClientRespawnBackoff` (fake CLI exits immediately; two spawns observed, delayed; use a counter file).
- [ ] **Step 2: Run** — `go test ./cmd/tainer-menu/` FAIL.
- [ ] **Step 3: Implement watchclient.go; rewire main.go** — delete `poll`/`pollLoop`/`setUnreachable` timing (daemon owns it; `recovering`/`failed` arrive as states), keep all systray rendering.
- [ ] **Step 4: Run** — `go test ./...` PASS; `go build ./cmd/tainer-menu` PASS (menu stays excluded from `make cross` per the existing Makefile); grep proves no `pkg/tainer/` import remains in `cmd/tainer-menu`: `grep -c "cyber5-io/tainer/pkg/tainer" cmd/tainer-menu/*.go` → 0.
- [ ] **Step 5: Commit** — `git commit -am "refactor(menu): watch-stream client, no internal imports, no polling (TAIN-111)"`
