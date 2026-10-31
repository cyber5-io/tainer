# Boot-Start Pods + Live-Restore Continuity — Design (TAIN-109)

**Status:** draft for Leni's review — not yet approved. This is the
blenzi-era ticket; it has a **Questions for Leni** section that must be
answered before planning.
**Ticket:** tain-109 — "Linux: boot-start pods + live-restore continuity (blenzi-era)".
**Repos:** cyber-stack (daemon lifecycle) + tainer (packaging, `tainer.yaml` key).

## Intent / why

Phase-1 Linux lifecycle is deliberately dev-semantics (TAIN-105 spec
ruling 12): the engine is socket-activated on first command, nothing
resurrects pods at host boot, and a daemon restart drops the userspace
forwarders and DNS until the next explicit start — reconcile guarantees
correctness *after* the fact, not continuity *through* it. Hosting (blenzi)
needs the Docker-parity pair:

- **boot-start** — designated pods are serving after a host reboot with no
  human involved;
- **live-restore** — a cyberstackd restart or package upgrade does not take
  running pods off the air.

## Goals

- A pod can be marked for boot-start; after host reboot it is serving
  without login or `tainer start` (Linux first-class — blenzi hosts are
  Linux).
- Daemon restart/upgrade on Linux native: running containers keep running
  (they already survive — they are independent processes), and the daemon
  re-establishes forwarders and DNS **eagerly at startup**, not lazily at
  the next CLI command, closing today's blackout window.
- Container exit statuses across the restart follow the TAIN-107 rules
  (unobservable → `-1`, restart policies still honored via reconcile).
- One-operator docs: a blenzi host's unit layout and failure modes fit on
  one page.

## Non-goals

- macOS boot-start/live-restore parity in this ticket (see Questions — on
  macOS the VM is a child of the daemon today, so live-restore means VM
  handover, a much bigger machine).
- Zero-connection-drop upgrades. Established TCP connections through a
  userspace forwarder die with the old process; the target is a sub-second
  re-listen, not FD handover (see Questions if blenzi needs better).
- Multi-tenant isolation, plan tiers, or any other blenzi feature.

## Anti-goals

- Never auto-start pods the user did not mark — a dev laptop must not grow
  surprise boot daemons (8GB floor: nothing preloads at boot unless asked).
- Don't fork the lifecycle into "dev mode" vs "server mode" config
  switches scattered through the code; the difference should be expressed
  by units + restart policies, not if-ladders.

## Constraints

- Builds on tainer `TAIN-105-linux-native` packaging (`packaging/linux/`:
  socket-activated `cyberstack.service` with no `[Install]`, by design) and
  cyber-stack `linux-native-phase1` (reconcile, nativeforwarder, allocator)
  — both unmerged; this work sequences after they land and after laptop-day
  validation of the phase-1 units.
- TAIN-107 (exit codes) should land first: restart policies and
  "survived the restart" reporting depend on honest statuses.
- User-facing copy never says container/docker/engine/VM.

## The how (tentative — pending Questions)

1. **Boot-start = restart policy + enabled service.** Follow Docker's
   model: pods marked for boot-start get engine restart policy
   `unless-stopped` on their containers; a `cyberstack-boot.service`
   (oneshot, `After=network-online.target`, `WantedBy=multi-user.target`,
   enabled by the .deb only when the operator opts in, e.g.
   `systemctl enable`) pokes the socket so the daemon starts and eager
   reconcile brings marked pods up. The base `cyberstack.service` stays
   socket-activated and `[Install]`-less.
2. **tainer surface:** a `tainer.yaml` key (working name `boot: true` under
   the pod, exact name/placement = Question 2) that tainer translates to
   the restart policy at create and reports in `tainer status`.
3. **Eager reconcile:** daemon `Run` on native backend performs the
   existing reconcile + forwarder/DNS re-establishment immediately at
   startup when prior state exists, instead of waiting for the first API
   call. This alone fixes the restart blackout for already-running pods.
4. **Upgrade path:** the .deb postinst restarts the service (systemd
   `Restart=on-failure` already covers crashes); with (3), the blackout is
   the daemon's own startup time. Privileged ports re-bind via the same
   systemd service (CAP_NET_BIND_SERVICE context preserved).
5. **macOS (only if Question 1 says yes):** a LaunchDaemon for cyberstackd
   plus VM auto-boot is the shape; live-restore through a VM restart is
   NOT achievable without VM handover — macOS gets boot-start at best.

## Questions for Leni

1. **Is macOS in scope at all for boot-start?** Blenzi hosts are Linux; is
   "my Mac rebooted and my pods came back" a dev-experience feature you
   want, knowing it means a root LaunchDaemon and the VM booting at login
   or at system start (RAM cost on 8GB machines)?
2. **Opt-in surface:** per-pod key in `tainer.yaml` (travels with the
   project, blenzi-friendly), a `tainer` command (`tainer boot on|off`),
   or operator-level only (systemctl + engine restart policies, no tainer
   UX yet)? Which fits the "Push live" story?
3. **Upgrade blackout budget:** is "listeners back within ~1–2s, established
   connections dropped" acceptable for blenzi v1, or do we need socket
   handover (SO_REUSEPORT/fd-passing — significantly more work) now?
4. **Timing:** ticket says "design when blenzi integration starts". Build
   now (it derisks laptop-day and hosting experiments) or park the plan
   until the blenzi brainstorm settles plan tiers and the hosting host
   image?
5. **Scope of resurrection:** boot-start only re-starts pods that were
   running at shutdown and marked `boot`, or all marked pods regardless of
   their state at shutdown? (Docker's `unless-stopped` = the former.)

## Left to the builder

- Unit/file layout for `cyberstack-boot.service` and the opt-in mechanics
  in nfpm/postinst.
- How eager reconcile coexists with socket activation (activation env
  handling already ledgered in TAIN-105 minors).
- Status reporting ("came back after reboot") in `tainer status` once the
  opt-in surface is chosen.
