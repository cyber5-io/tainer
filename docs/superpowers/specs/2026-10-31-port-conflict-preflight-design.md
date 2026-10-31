# Host-Port Conflict Preflight — Design (TAIN-108)

**Status:** draft for Leni's review — not yet approved.
**Ticket:** tain-108 — "Create-time port-conflict preflight (two pods claiming one host port)".
**Repos:** cyber-stack (authoritative check) + tainer (UX copy).

## Intent / why

Found in the TAIN-105 review and parked by ruling: when a second container
claims a host port another container already holds, every forwarder silently
retargets — the native forwarder replaces the proxy target, the hybrid VM
forwarder replaces, pf overwrites — and the `/start` handler only logs
`Expose` errors. A real conflict never reaches the user: the first pod's
port is stolen and both owners think they are serving. With custom
`host_port` pins in `tainer.yaml` (band 40000–49000) and blenzi multi-pod
hosts coming, "silently steals" graduates from oddity to data-path bug.

## Goals

- **Create-time reject (authoritative, engine-side):** `ContainerCreate`
  fails with HTTP 409 and a named error when a requested host-port binding
  duplicates a binding persisted for any other live (non-deleted) container.
  Error body names the port and the current owner.
- **Start-time re-verify:** `/start` re-checks before `Expose` (the owner
  may predate the rule, or state may have been imported) and fails the start
  with the same named error instead of log-and-continue.
- **Host-listener conflicts:** where the forwarder can already tell the
  port is bound by a foreign process (native `ErrPortBusy`), `/start`
  surfaces that as the same hard error rather than a log line.
- **tainer UX copy** for the mapped error, e.g.:
  `port 8080 is already used by pod "blog" — change host_port in tainer.yaml or stop that pod`
  and for foreign owners:
  `port 8080 is already in use by another app on this Mac` — never says
  container/docker/engine/VM.

## Non-goals

- No automatic reassignment or "next free port" picking — the user chose a
  pin; honor it or fail loudly.
- No change to how tainer allocates the 40000–49000 band or how the router
  owns 80/443/22 (doctor already diagnoses those; TAIN-105 extended it).
- No UDP preflight beyond what bindings already express.
- No VM-forwarder internal rework (pf/hybrid keep their mechanics; they
  just stop being reached when create/start rejects first).

## Anti-goals

- Never let two running containers hold the same host port with both
  believing they own it — that is the bug.
- No TOCTOU theater: the create-time check is a registry check (our own
  persisted bindings), not a bind-probe race against the whole OS. Foreign
  listeners are detected where we actually bind (Expose), not guessed.

## Constraints

- The authoritative check lives in cyberstackd, which sees all containers'
  persisted `PortBindings` (`trackedBindings` + agent state) — tainer-side
  registries (slots `TCPPins`) see only tainer-created pods.
- Builds on unmerged branches: cyber-stack `linux-native-phase1`
  (nativeforwarder, `ErrPortBusy`) and tainer `TAIN-105-linux-native`
  (doctor port attribution). Sequence after those merge or branch from
  their tips.
- Docker API compatibility: 409 with a plain-text/JSON message mirrors
  Docker's own "port is already allocated" shape, so non-tainer clients
  degrade sanely.

## The how (decided)

1. **Named error, one place:** a sentinel in the daemon, e.g.
   `ErrHostPortClaimed{Port, Proto, OwnerID, OwnerName}`, rendered by
   httpapi as 409 `{"message":"host port 8080/tcp is already claimed by container <name>"}`.
2. **Create:** on `ContainerCreate` with host bindings, scan existing
   containers' persisted bindings (all states except deleted — an Exited
   container restarts with its stored bindings, so it still owns them).
   Same-container re-create (tainer's reuse path) is not a conflict.
3. **Start:** before the Expose loop, run the same scan against *running*
   containers; on `Expose` returning `ErrPortBusy` (native) fail the start
   and roll back the container to Created, instead of today's
   log-and-continue.
4. **tainer mapping:** pod start/create code recognizes the 409 message,
   resolves the owner container name to a pod name (names embed the
   project), and prints the copy under Goals. Unknown owner → the
   "another app on this Mac/machine" variant.
5. **Doctor:** add the duplicated-pin case to `tainer doctor` output
   (slots registry cross-pod scan) as a cheap local hint; the engine
   remains the authority.

## Left to the builder

- Whether the tainer-side slots-registry hint runs at create, start, or
  doctor-only (doctor-only is acceptable; engine error is the gate).
- Exact rollback semantics when start fails mid-Expose (some ports bound,
  then conflict) — unwind what this start bound; container back to its
  prior state.
- Test seams: fake binding tables for the create/start scans; native
  Expose conflict covered by existing nativeforwarder tests plus one new
  start-path test; VM-path behavior asserted at the handler level (no VM
  needed).
