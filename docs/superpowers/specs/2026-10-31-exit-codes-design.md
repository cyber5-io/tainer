# Container Exit Codes — Design (TAIN-107)

**Status:** draft for Leni's review — not yet approved.
**Ticket:** tain-107 — "Container exit codes always report 0".
**Repo:** cyber-stack (engine). No tainer-side code changes required.

## Intent / why

The containerd runtime's reaper cannot observe a container's real exit
status: `crun run -d` detaches the container init, which reparents to PID 1,
so the agent is never its parent and `wait4` is unavailable. `reap()` polls
`kill(pid, 0)` and, on exit, hardcodes `state.ExitCode = 0`
(`internal/agent/containerd/runtime.go`, the comment at the assignment says
exactly this). Consequences:

- `ContainerWait` / inspect always report exit 0 — a crashed app looks like
  a clean stop to tainer, to the UI-to-come, and to blenzi.
- `ShouldRestart`'s `on-failure` policy can never fire: the failure case is
  invisible. The code comment says "address before promising on-failure to
  users" — this spec is that address.

Pre-existing in VM mode; confirmed during TAIN-105. One fix covers both
paths because the same runtime runs in the VM agent and the native backend.

## Goals

- `reap()` records the container init's real exit status (including
  signal deaths as 128+N, Docker convention).
- `ContainerWait`, inspect, and persisted `state.json` carry that status.
- `on-failure` restart policy actually fires on nonzero exits.
- When the status is genuinely unobservable (daemon/agent restarted while
  the container ran; reconcile later finds the pid gone), persist `-1`
  ("unknown") — never a fabricated 0.

## Non-goals

- No tainer CLI/UX changes (a later `tainer status` polish can show
  "stopped (exit N)"; separate ticket if wanted).
- No shim/conmon-style per-container supervisor process. That is the
  live-restore-era answer (see TAIN-109 spec) because it also preserves
  status across daemon restarts; it is not needed to fix this bug.
- No change to OOM detection.

## Anti-goals

- Never report exit 0 unless the init genuinely exited 0. An honest `-1`
  beats a comforting lie.
- No `Wait4(-1, …)` reaping loop: a wait-any steals statuses from
  `os/exec`'s own children (every `crun` invocation, `nft`, etc.) and
  corrupts unrelated `cmd.Run()` calls.

## Constraints

- Linux-only mechanics are fine: both the VM guest and the native backend
  are Linux. Keep darwin builds green via build tags (house pattern:
  `*_linux.go` + stub).
- Tests that need a real subreaper/pid lifecycle gate on the standing
  substitute (`GOOS=linux go test -c -tags containers_image_openpgp`) and
  get a bring-up checklist entry (stage 16 already exists for this ticket).
- cyber-stack work builds on the `linux-native-phase1` branch (unmerged):
  the reconcile rework there is why the cheap fix stopped being cheap.
  Sequence this after that branch merges, or branch from its tip.

## The how (decided)

1. **Subreaper + targeted waits.** At runtime construction (agent main and
   native backend share this), call
   `prctl(PR_SET_CHILD_SUBREAPER, 1)`. Detached container inits now
   reparent to our process instead of PID 1.
2. **SIGCHLD-driven collector.** One goroutine owns status collection:
   `signal.Notify(SIGCHLD)`; on each signal it calls
   `wait4(pid, WNOHANG)` **only for pids registered as container inits**
   (registered by `Start` after the pidfile read, deregistered on
   collection). Unknown pids are never waited — `os/exec` children keep
   their statuses. Collected statuses go in a map the reaper reads.
3. **`reap()` consumes the status.** The existing kill-poll remains the
   liveness signal; on exit, `reap()` takes the collected status
   (`WEXITSTATUS`, or `128+signal` if signalled) instead of hardcoding 0.
   If no status was collected (we were not the subreaper for this init —
   i.e. the daemon restarted after the container started), persist `-1`.
4. **Reconcile paths** keep their existing `-1` markers (they already
   write `ExitCode = -1` for zombies) — now documented as "unknown".
5. **`ShouldRestart`**: `on-failure` fires on exit != 0, including `-1`.
   Rationale: a container that died while we weren't looking is a failure
   as far as a restart policy is concerned; Docker behaves the same after
   a non-live-restore daemon bounce.
6. **Wire format**: `WaitResponse.ExitCode` / `ContainerInspect.ExitCode`
   already exist and are plumbed (`internal/agent/server.go`) — they just
   start carrying truth. `-1` crosses the wire as `-1`.

## Left to the builder

- Exact placement of the pid registry (runtime struct vs package-level) and
  the collector's shutdown semantics.
- Test harness for "init we didn't parent" (e.g. start via a helper that
  double-forks) vs plain child — on-Linux tests; compile-gated off-Linux.
- Whether `Stop()`'s kill path needs to consume the collected status too
  (it should — SIGKILL'd containers report 137).
