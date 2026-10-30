# Linux Native Port, Phase 1 — Design

**Date:** 2026-10-30
**Status:** Draft, pending approval
**Repos:** cyber-stack (the bulk), tainer (socket/lifecycle seams)
**Grounding:** full architectural recon of cyber-stack, 2026-10-30 (this
spec's requirements reference its findings; hazards H1–H11 below).

## Intent

Make tainer run natively on Linux: no VM, cyberstackd drives crun directly
on the host. The rebuild spec always promised this ("native on Linux. No
VM" — 2026-04-23 spec); a Linux laptop is arriving to test on. Phase 1
builds everything that can be built and compile/unit-verified without that
laptop, so hardware day is integration day, not architecture day.

Strategic weight: Linux native is also the **blenzi substrate** — pods on
bare metal with no VM overhead is the hosting pitch. The engine hardening
this port forces (real-host networking, security, multi-daemon hygiene) is
work blenzi needs anyway.

## Goals

- `cyberstackd --backend native` (Linux only) serves the same Docker API
  socket tainer already speaks, with containers running via crun on the
  host. The tainer CLI works unchanged above it: init/start/stop/exec/
  list, shared pod netns, pod cgroup budgets, router pod, *.tainer.me.
- All macOS behavior is untouched: the VM backend remains the darwin path
  and the default; `go test ./...` on darwin stays green.
- Phase 1 exit: compiles for linux/amd64+arm64, linux unit tests pass
  (see Verification), systemd + packaging scaffold exists, and a written
  bring-up checklist for laptop day.

## Non-goals (phase 1)

- Rootless mode. See the process-model decision below.
- .rpm packaging, distro matrix beyond the reference target (Ubuntu
  24.04/systemd; anything with systemd + cgroups v2 + nftables should
  work, but only the reference is promised).
- Live end-to-end verification — impossible until the laptop arrives.
- Windows anything (separate recon spec).
- Fixing VM-mode-only latent bugs beyond what the port touches.

## Anti-goals

- **No LAN exposure by default.** On macOS the VM NAT means published
  ports are host-local. The native port must preserve that: pods publish
  to 127.0.0.1 unless explicitly configured otherwise (H1: the current
  uplink DNAT would expose every pod port to the network).
- **Never fight the host.** No flushing shared nftables chains, no global
  `forward policy accept`, no clobbering an existing Docker/Podman
  install's networks or /etc/containers config (H3, H10). Tainer must be
  a polite guest on a machine that already runs Docker.
- **No security downgrades on the host.** Patches justified by "disposable
  single-tenant VM" do not get that justification on a real host (H7).

## Decisions (mine, flagged for review)

1. **Process model: rootful system daemon, phase 1.** The agent code
   needs root (bridge/veth, nftables, overlay mounts, cgroup writes).
   cyberstackd runs as a **systemd system service** with
   **socket activation**: systemd owns `/run/cyberstack/cyberstack.sock`
   (root:`cyberstack` group, 0660; installer adds the user to the group)
   and starts the daemon on first connection. This is Docker's trust
   model, it keeps the agent code nearly unchanged, and socket activation
   means tainer needs no Linux spawn logic at all. Rootless (userns +
   fuse-overlayfs/pasta) is a documented phase 2 candidate, not phase 1:
   it forks the storage and network stacks and would eat the whole phase.
2. **One process.** The agent's gRPC servers are constructed in-process
   inside cyberstackd and served over an in-memory/socketpair conn, with
   the existing AgentClient reused unchanged — the recon's lowest-churn
   option. No second binary, no vsock, no reconnect machinery (its
   complexity exists for VM sleep/wake semantics that don't apply).
3. **Backend seam.** Introduce a `backend` concept at the point where
   `Daemon.Run` currently hardcodes VM+AgentClient: `vm` (today's path,
   darwin default) and `native` (linux default). The VM-only loops —
   watchWake, syncClockLoop, pollVMIP, pf watchdog, the hard
   /opt/tainer/bin/cyberstack-pf requirement — gate on the backend (H11).
   Fix the latent nil-AgentClient panic at this seam while touching it.
4. **Native networking.** Keep cs0-bridge + veth + shared-netns exactly
   as in the VM (it's plain netlink/nft code). Change the edges:
   - Subnet: native default moves off 172.17.0.0/16 to avoid docker0
     (H3); configurable; VM mode keeps its current value.
   - Uplink: derived from the default route, never the "eth0" literal
     (H2); used only for outbound masquerade.
   - nftables: everything in our own `cyberstack` tables; scoped
     masquerade for the cs0 subnet only; no global policy changes, no
     flushes of shared chains (H3).
   - Port publishing: a native PortForwarder binds 127.0.0.1:<port> on
     the host and proxies straight to containerIP:port (the hybrid shape
     minus the gRPC hop — the recon's recommendation). No uplink DNAT in
     phase 1 (H1). The PortRemote hook already supports per-container
     targets.
   - Container resolv.conf: host's effective upstream resolvers (read
     through systemd-resolved's /run/systemd/resolve/resolv.conf when
     present), falling back to the current publics (H9).
5. **State correctness on a real host** (H4, H5): IP allocation and
   container state reconcile from live crun/netlink state on daemon
   start instead of assuming a fresh boot; a plain daemon restart under
   running containers must not reissue live IPs or orphan the reap
   loops. (Exit codes currently always report 0 — pre-existing in VM
   mode too; fix if the reconcile work makes it cheap, else ticket it.)
6. **Security hardening, native path** (H7): chroot/chrootarchive-based
   layer extraction (the in-process untar patch stays VM-only),
   pivot_root instead of `--no-pivot`, and explicit containers/image
   config paths so an existing Podman's /etc/containers is never picked
   up (H10). Image signature policy stays insecureAcceptAnything —
   that's Docker-default behavior, not a VM shortcut.
7. **Cgroups under systemd** (H6): the daemon's unit delegates
   (`Delegate=yes`) and pod cgroups (`tainer-<project>`) are created
   under the service's delegated subtree, not at the cgroup root. The
   pod-budget semantics (one absolute budget per pod) are unchanged.
8. **Bind mounts:** identity passthrough on native (no /host/ prefix,
   no /Users gate) (H8).
9. **Storage roots on the host:** /var/lib/cyberstack/{storage,
   containers,volumes}, /run/cyberstack/storage. No data-disk image.
10. **tainer CLI seams:** socket path becomes per-OS (`~/.cyberstack/…`
    on darwin, `/run/cyberstack/cyberstack.sock` on linux); the
    auto-spawn path gates to darwin (systemd owns the daemon on linux;
    doctor learns the "socket missing → is the package installed /
    group membership?" hints). The existing GOOS seams (dns resolver's
    systemd-resolved path, machine.go, router ssh check) get exercised
    and finished rather than redesigned. **Sequencing:** the Windows
    recon task owns the base per-OS seam extraction in
    `pkg/tainer/runtime` (`runtime_unix.go`/`runtime_windows.go`) and
    the `make cross` target — this plan's tainer-side tasks start after
    that scaffold lands and build on those seams, so the two efforts
    never extract the same seam twice.
11. **Packaging scaffold:** a .deb skeleton plus `make linux-dist`.
    Signing/repo hosting deferred.

## Rulings from builder review (author's calls, 2026-10-30)

12. **Lifecycle semantics are dev-tool, not hosting.** The socket unit is
    enabled; the service starts on first connection ("first tainer
    command", the macOS semantic) — pods do **not** auto-resurrect at
    host boot in phase 1. A daemon stop/upgrade drops host-side
    forwarders and the DNS responder until the next start; that outage
    is the accepted phase-1 semantic (reconcile guarantees correctness
    after restart, not continuity through it). Docker-style
    live-restore/boot-start continuity is a blenzi-era requirement —
    ticket it, don't build it. `Restart=on-failure` on the service.
13. **`cmd/tainer-menu` is excluded from the linux build gate**, same
    ruling as the Windows recon spec (darwin-only vendored systray; a
    Linux tray is future UI work). "All binaries" in Verification means
    the tainer CLI and all cyber-stack binaries, explicitly listed in
    the Make target, not `./...`.
14. **The .deb lives in the tainer repo and ships the product**: tainer
    CLI + cyberstackd together (mirroring the macOS .pkg, which bundles
    cyberstackd from CS_REPO), with crun as a package dependency, and
    the systemd units + `cyberstack` group creation in postinst.
    cyber-stack's `make cross` provides the daemon binaries; tainer's
    `make linux-dist` assembles the package.
15. **Port conflicts on the host:** finish the `:22` seam with a
    bind-probe → 2222 fallback (the existing darwin `remoteLoginActive`
    shape; on Ubuntu, openssh-server almost always owns :22). For
    80/443: **hard, actionable error** — name the port and the owning
    process, with a doctor check to match. No silent fallback ports (a
    pod on :8080 breaks the no-port `https://proj.tainer.me` story and
    the cert UX); a config knob is deferred until someone real needs it.
16. **One operator per host in phase 1, documented.** Router/caddy/cert
    state stays per-user in $HOME as today; the group socket governs
    access, not concurrent use (two users' tainer state would silently
    fight over the shared router — documented limitation). True
    multi-user/multi-tenant state is the blenzi design's problem, which
    will force the right answer anyway.

## Constraints

- cyber-stack work on a feature branch in that repo; tainer work on a
  branch from `main` named after a Tracker task (create the task first).
- No Co-Authored-By/AI mentions in commits (both repos).
- gofmt/vet clean; darwin suite green at every commit.
- The Makefile's cross targets must build the daemon for linux/amd64 and
  linux/arm64 from day one (today `GOOS=linux go build ./cmd/cyberstackd`
  fails only on the missing sizing probe — that gap closes first).

## Verification without the laptop

- Compile gate: linux/amd64 + linux/arm64 builds of the tainer CLI and
  all cyber-stack binaries (tainer-menu excluded, ruling 13) in CI or
  `make cross`. Note: once the native daemon links the imagestore it
  inherits the agent's build-tag/CGO treatment (`containers_image_openpgp`
  etc.) — Makefile work, planned for.
- Unit tests: the linux-tagged agent tests are currently *silently
  skipped* on darwin. The executor stands up a Linux unit-test runner —
  GitHub Actions on the private repo if credentials allow, else
  cross-compiled `go test -c` binaries executed inside a Linux container
  on the local engine (arm64). Network/root-dependent tests may be
  skipped there; everything skipped on the Mac must be listed in the
  bring-up checklist for laptop day.
- Integration tests: the fixtures currently hard-require darwin/arm64;
  abstract the fixture so the same Docker-API tests can target a native
  daemon later (the recon confirmed only the fixture needs to change).
  Running them is laptop-day work.
- Deliverable: `docs/superpowers/specs/2026-10-31-linux-bringup-checklist.md`
  (in tainer) listing every unverified assumption to test on hardware,
  in order.

## Left to the builder

- Exact backend interface shape and where the in-memory gRPC conn lives.
- nftables table/chain naming and the native subnet default.
- Whether the native PortForwarder uses a userspace proxy (like
  cyberstack-pf's child) or nft OUTPUT DNAT + route_localnet; userspace
  is the safe phase 1 default, measured later.
- .deb layout details (dh vs nfpm vs raw control files — nfpm suggested).
- How much of the reconcile rework lands in shared code vs native-only.

## Open risk, named

The 8GB-floor performance story on Linux is the *easy* half (native crun,
no VM tax). The risk concentrates in networking hygiene on arbitrary
hosts — firewalls, VPNs (NordVPN taught us this on macOS), existing
Docker bridges. Phase 1's "polite guest" rules are designed for that, but
only the laptop proves them. Hence the bring-up checklist as a formal
deliverable.
