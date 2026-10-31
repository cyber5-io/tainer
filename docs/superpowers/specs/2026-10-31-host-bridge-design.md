# Host Bridge — design

Status: DRAFT for Leni's review (2026-10-31). Not yet approved.
Companion spec: `2026-10-31-tainer-accounts-design.md` (the remote identity
this bridge holds sessions for).

## Intent

The Go-side foundation every tainer UI stands on, shipped inside the normal
tainer install packages (.pkg/.deb) — no new binary for users to know about.
Three jobs:

1. **Events out:** `tainer watch` — a machine-readable stream of engine and
   pod state changes plus structured progress for long operations, so UIs
   stop polling.
2. **A versioned JSON contract:** every tainer command usable by a program,
   with stable, versioned shapes — the single API both the control webapp
   and the native menu apps consume. "The UI is a strict client of the CLI"
   is the architecture; this makes the CLI worth being a client of.
3. **Commands in:** the sanctioned channel by which the control pod (which
   runs *inside* the engine) drives tainer on the *host* — because pods,
   manifests, and project directories live on the host filesystem, where
   the control pod cannot reach.

## Goals

- UIs (control pod, menu apps) never import `pkg/tainer` internals and never
  poll on a timer.
- One JSON contract, one version marker, additive evolution.
- The control pod can do everything the CLI can do — through the gateway,
  with the host enforcing the login gate — and nothing the CLI cannot.
- Negligible idle cost: fits the 8GB floor and "UI is a plus, not a
  requirement".

## Non-goals

- Remote (off-machine) access to the gateway. MVP is this-machine-only;
  teams/LAN routing arrive later and will go through their own design.
- A stable *Go API* for third parties. The JSON contract is the public
  surface; the packages stay internal.
- Replacing the Docker-compatible engine socket — that remains the engine's
  own surface, for the CLI's host-side use.

## Anti-goals

- The control pod holding raw engine-socket access. The engine API speaks
  containers; tainer speaks pods and manifests that live on the host. A pod
  with the engine socket could bypass every tainer invariant (budgets,
  registry, login gating) and touch other pods' containers. Never.
- A second long-running host daemon. cyberstackd is the one daemon; the
  bridge is a facet of it, not a sibling.
- A web terminal in the dashboard — ever. Leni's ruling (2026-10-31):
  "tainer itself is a terminal app. we don't need web terminal at all."
  The CLI is the terminal surface; the dashboard never embeds one.
- Breaking `--json` consumers silently. Shapes are versioned; removals are
  a major version.

## Constraints

- CLI stays fully functional with no UI installed and no login (login gates
  only live-world actions, per the accounts spec).
- UX copy never says podman/docker/engine/VM — event types and errors
  included, since UIs will render them.
- 8GB floor: the event bus and gateway add no measurable idle RAM; no new
  always-on process.
- Single-line shell commands in all docs/scripts; no AI attribution.

## Decided hows

### (a) Events: `tainer watch`

- New command: `tainer watch --json` emits NDJSON, one event per line, each
  an envelope: `{"v":1,"ts":…,"type":…,"data":{…}}`.
- Event families (v1): `engine` (reachable/recovering/failed — the same
  states tainer-menu derives today), `pod` (state transitions, with name,
  domain, running counts), `progress` (long ops: init, start image pulls,
  db import/export — step, percent-when-known, message).
- Source of truth: an event bus inside cyberstackd (it already supervises
  containers and the VM); `tainer watch` is a thin subscriber over the
  daemon's control endpoint. Buffered replay of current state on connect
  (a "hello" snapshot) so clients render instantly, then deltas.
- Long-op progress: commands report progress to the daemon as they work, so
  *any* watcher sees `tainer start`'s pull progress, not just the terminal
  that ran it.

### (b) The versioned JSON contract

Today (surveyed in `cmd/tainer/main.go`): status, status-in-project, init,
start, stop, destroy, db export/import, list, update, network have JSON
modes; **exec, logs, doctor, config are human-only** — those are the gaps.

- Every command gets `--json`. Streams (logs, watch, progress) are NDJSON;
  single-shot commands emit one document.
- Envelope version: single-shot documents gain `"v":1`; a new
  `tainer api --json` reports `{"v":1,"commands":[…]}` for capability
  discovery. Additive changes keep `v`; breaking changes bump it and the
  old shape keeps working for one minor release.
- Errors: on failure with `--json`, stdout carries
  `{"v":1,"error":{"code":…,"message":…}}` and the exit code is nonzero.
  (CLI exit codes are currently honest at the top level; container exit
  codes are TAIN-107, a separate ticket.)
- The contract document itself lives in-repo as
  `docs/api/cli-json-v1.md`, generated examples included, so UI work can
  proceed against a written shape.

### (c) Commands in: the control gateway

- cyberstackd grows a **control listener** reachable only from the pod
  network: on macOS the host side of the VM network, on Linux native the
  bridge address (10.113.0.1) — never 0.0.0.0, never a host-public port.
- Authn: at engine start, cyberstackd mints a per-boot random bearer token
  and injects it into the control pod alone (file mount, not env, so
  `inspect` on other pods never shows it). Requests without it are 401; the
  token never leaves the machine. Pod-network neighbours cannot drive the
  gateway without it.
- The gateway's API is **the CLI, re-exposed**: endpoints map 1:1 onto
  `tainer <cmd> --json`, and v1 literally spawns the installed CLI on the
  host (same binary, same code paths, same login gate) and relays the JSON.
  This keeps "zero business logic outside the CLI" literally true;
  in-process linking is a later optimisation if spawn cost ever shows.
- Streams (watch, logs, progress) are relayed over the same connection
  (chunked NDJSON).
- The login session (refresh token, per the accounts spec) is stored and
  used **host-side** (Keychain on macOS, secret service/0600 on Linux):
  `tainer login` establishes it, CLI and gateway both consult it, and the
  control pod only ever receives short-lived access tokens to render
  "logged in as…". A stolen control-pod filesystem yields nothing durable.

### (d) tainer-menu becomes a watch client

- Today `cmd/tainer-menu` imports `pkg/tainer/{engine,pod,manifest,runtime}`
  and polls every 5s. It becomes a `tainer watch --json` subprocess client:
  no internal imports, no polling, instant state flips, and the "recovering/
  failed" logic moves daemon-side where it belongs.
- The future SwiftUI menu app (and the Linux tray app) consume the same
  stream the same way — the Go systray app is the reference client and
  remains the Windows/Linux fallback until native equivalents land.

## Left to the builder

- Event bus internals (buffer sizes, slow-consumer policy), gateway route
  naming, exact progress event granularity per command.
- JSON gap-filling order for exec/logs/doctor/config and their shapes.
- Token file mount mechanics per backend (VM vs native) — the native side
  has prior art in the TAIN-105 branch.

## Questions for Leni

None open — web terminal is ruled out (see anti-goals) and `tainer watch`
is machine-only (`--json` required; no human mode) per Leni, 2026-10-31.
