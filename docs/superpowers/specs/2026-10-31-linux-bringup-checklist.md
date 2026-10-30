# Linux bring-up checklist (TAIN-105, laptop day)

Everything below was written, compiled and unit-tested on macOS only. Nothing in the Linux native path has ever run on a Linux kernel. This is the ordered list of what to verify, with the commands. Work top to bottom: each stage assumes the previous one passed. When something fails, note the stage number and stop; later stages will give misleading results.

Spec: `2026-10-30-linux-native-port-design.md` (rulings 12, 14, 15, 16 are referenced below).

## Stated limitations (not bugs)

These are accepted phase-1 behaviours. Do not file them as defects.

- **One operator per host (ruling 16).** The group-owned socket controls who may talk to the engine; it does not make sharing safe. Router, caddy and certificate state live per-user in `$HOME`, so two users running tainer on one machine would silently fight over the one shared router. Treat the host as single-user.
- **Daemon restart is an outage window (ruling 12).** The service is socket-activated and is not enabled at boot, so pods do not come back after a reboot. If `cyberstackd` restarts while pods run (crash, upgrade, `systemctl restart`), forwarded ports and `*.tainer.me` DNS are down until the daemon is back and has reconciled. Existing containers keep running but traffic does not reach them in the gap.
- **Uplink is pinned at daemon start.** Switching between wifi, ethernet or a VPN while the daemon runs breaks outbound traffic from pods until the daemon restarts. (Verified-as-intended in design; stage 7 measures how bad it is.)
- **`ip_forward=1` is left on after teardown.** Host-wide, not reverted. Same as Docker.
- **Foreign firewalls win.** If another nftables table (Docker's FORWARD policy, ufw) drops forwarded packets, our accept rules cannot override it. Stage 8 tests this.
- **Exit codes** from container processes are not propagated faithfully. Deliberately not fixed in phase 1; tracked in a ticket (see stage 16).
- **Same host port claimed by two pods:** the second silently steals the forward. A create-time port-conflict preflight is ticketed, not built.

## Setup

Test host: an Ubuntu or Debian laptop, systemd, root available. Get the packages onto it:

```
make -C ../cyber-stack cross
make linux-dist CS_REPO=../cyber-stack
scp dist/tainer_1.3.0_amd64.deb laptop:
```

(`linux-dist` builds both `amd64` and `arm64`; use the one matching the laptop.)

## Stage 1. Package installs and units are valid

1. `sudo apt install ./tainer_1.3.0_amd64.deb` pulls `crun` and `nftables`. Expect: installs cleanly, prints the "add yourself to the cyberstack group" hint.
2. `systemd-analyze verify /usr/lib/systemd/system/cyberstack.socket /usr/lib/systemd/system/cyberstack.service` Expect: no output. (Never run on macOS.)
3. `systemctl is-enabled cyberstack.socket` Expect: `enabled`. `systemctl is-active cyberstack.socket` Expect: `active (listening)`.
4. `systemctl is-enabled cyberstack.service` Expect: `static` or `disabled`, never `enabled`. The service must have no boot install.
5. `getent group cyberstack` Expect: group exists. `ls -l /run/cyberstack/cyberstack.sock` Expect: `srw-rw---- root cyberstack`.
6. `which tainer; ls -l /usr/local/bin/tainer` Expect: symlink to `/opt/tainer/bin/tainer`. Check `/usr/local/bin` is on `PATH` for a non-login shell too.

## Stage 2. Socket activation end to end

1. Confirm the daemon is NOT running yet: `systemctl is-active cyberstack.service` Expect: `inactive`.
2. `sudo tainer doctor` (or `curl --unix-socket /run/cyberstack/cyberstack.sock http://d/_ping` as root). Expect: `OK`, and the service is now `active`.
3. `journalctl -u cyberstack.service -n 50` Expect: daemon started with the backend `native`, no "listener" errors. This is the first real test of the fd 3 activation contract (`LISTEN_PID`/`LISTEN_FDS` handling from the daemon listener).
4. Activation environment leak: `sudo cat /proc/$(pgrep -o cyberstackd)/environ | tr '\0' '\n' | grep LISTEN` then inspect a spawned child (`crun`, `nft`) with `ls -l /proc/<childpid>/fd`. Known minor: `LISTEN_PID`/`LISTEN_FDS` are not unset after takeover and fd 3 is not close-on-exec, so children may inherit them. Record whether anything misbehaves; fix is small if it does.
5. Kill the daemon with `sudo systemctl stop cyberstack.service`, then run `tainer` again. Expect: socket re-activates it without a manual start. Then `sudo kill -SEGV $(pgrep cyberstackd)` and confirm `Restart=on-failure` brings it back (note `RestartSec=2`).

## Stage 3. Group permission UX

1. As a normal user not in the group: `tainer doctor`. Expect: a clear, actionable message about the `cyberstack` group, not a raw "permission denied" on a socket path. Record the exact text.
2. `sudo usermod -aG cyberstack $USER`, then without re-login `tainer doctor` again (still denied is expected), then `newgrp cyberstack` and retry. Expect: works.
3. Log out and in; `id` shows `cyberstack`; `tainer doctor` works with no sudo.

## Stage 4. Port conflicts (:22, :80, :443)

1. Baseline: `sudo ss -ltnp '( sport = :22 or sport = :80 or sport = :443 )'`.
2. With openssh-server running on :22 (the Ubuntu default): `tainer start` in a project. Expect: the router falls back to host port 2222 for ssh, and `tainer doctor` reports the port in use and by whom. (The :22 dial-probe is checked router-state-first; an already-published :22 owned by our own router must be kept, not flipped to 2222 on the second start.)
3. With nginx or apache holding :80/:443: `tainer doctor` Expect: Fail when the engine is down, Warn when the engine is up (ruling: a healthy daemon under another user is itself an "unnamed owner"). Record whether the listener is named or reported as unknown.
4. Privileged ports: native binds :80/:443/:22 assume the rootful systemd service (it has the capability). Confirm a bind failure surfaces as a readable error, not a stack trace. Known gap: unprivileged runs show a plain listen error, not the friendly "port busy" error.
5. sshpiper banner sniff: with a stray non-ssh service on :22, check whether the banner-prefix guard classifies it correctly. Documented as unverified.

## Stage 5. First pod: start to page loads

1. Create a small project (`tainer init` with the nodejs or php type), then `tainer start`.
2. Image pull works through the in-process image store: this exercises the chrootarchive unpack plus `pivot_root` path for the first time. Watch for `operation not permitted` or `invalid cross-device link` in `journalctl -u cyberstack.service -f`. If pivot_root fails, the `NoPivot` fallback is the first suspect.
3. `curl -I https://<project>.tainer.me` Expect: 200 with a valid cert. Also `curl -I http://127.0.0.1:<hostport>`.
4. `tainer ssh` / `ssh <pod>@ssh.tainer.me` reaches the app container (sshpiper route through the router).
5. `sudo nft list ruleset | grep -i -A20 cyberstack` shows our table and nothing of ours inside foreign tables.
6. `tainer stop`, `tainer destroy`. Expect: nft rules for the pod gone, container dirs removed. Check `findmnt | grep cyberstack` for leaked mounts (the overlay EBUSY risk from `docs/handoff/2026-09-08-dev-pod-resilience.md`).

## Stage 6. cgroups, budgets and Delegate

1. While a pod runs: `systemd-cgls /system.slice/cyberstack.service` and `cat /sys/fs/cgroup/system.slice/cyberstack.service/cgroup.controllers`. Expect: the delegated subtree exists and `memory cpu pids` are listed.
2. Smart-pod budget: `cat /sys/fs/cgroup/.../tainer-<project>/memory.max` matches the pod size (e.g. nano = 512M). Run a memory hog inside the container and confirm it is OOM-killed at the budget, not the host.
3. EBUSY self-move: the daemon moves its own pid into a leaf cgroup so controllers can be enabled on the parent. Verify there is no "device or resource busy" in the journal at first container start. Known risk: child pids left in the service cgroup could keep EBUSY alive.
4. crun absolute `cgroupsPath`: confirm crun accepts the absolute path form (`crun list`, `cat /proc/<container-pid>/cgroup`). Both this and the EBUSY point are marked UNVERIFIED in the code.

## Stage 7. Uplink detection (wifi, ethernet, VPN)

The daemon picks the host uplink once, at start, for masquerade. Test each, restarting the daemon between runs (`sudo systemctl restart cyberstack.service`) and running `tainer exec <pod> curl -sI https://example.com`:

1. Wifi only. Expect: outbound works.
2. Ethernet only, and ethernet plus wifi at once. Expect: outbound works; record which interface was chosen (journal).
3. **VPN (NordVPN, WireGuard/NordLynx).** This broke vfkit's NAT on macOS, so it is the highest-risk case. With the VPN up before the daemon starts, then up after. Expect: pods reach the internet through the tunnel, or at least do not black-hole silently. Check `ip route get 1.1.1.1` against what we chose.
4. Switch network while a pod runs (unplug ethernet, go to wifi) WITHOUT restarting the daemon. Expected today: outbound from pods stops until restart (stated limitation). Measure how it fails and how doctor reports it.

## Stage 8. Docker, ufw and other firewalls

1. Docker installed and running alongside: start Docker, then a tainer pod. Check the pod subnet does not collide with `docker network inspect bridge` (the nft table and subnet allocator both). Both products must keep working.
2. Docker's FORWARD chain defaults to DROP for traffic it does not own. Expect our container traffic to be affected: our accept rules live in our own table and cannot override a drop in another. Record exactly what breaks (outbound from pods? published ports?). This is the "polite guest" trade-off; the finding decides whether a doc note or an opt-in rule is needed.
3. `sudo ufw enable` with default policy. Same questions as step 2.
4. With ufw or iptables-nft present, confirm teardown (`tainer destroy`, then `sudo systemctl stop cyberstack.service`) leaves the foreign tables untouched. Known minor: teardown of our table logs an error if the table is already absent; confirm that is only a log line.

## Stage 9. Daemon restart under running pods (ruling 12)

1. Start two pods. Note container IPs (`tainer ps`/inspect), the forwarded host ports, and curl each pod URL.
2. `sudo systemctl restart cyberstack.service`.
3. Immediately and then every second, `curl -sI https://<pod>.tainer.me`. Measure the outage window (seconds until both pods answer again).
4. After recovery: did containers keep their IPs? The IP allocator reseeds only from containers it believes are Running (a ruling that deviated from the brief); look for IP collisions or duplicate assignments when a third pod is created post-restart.
5. Are the port forwarders re-established, and does `*.tainer.me` DNS resolve while the daemon was down (expected: no)? Record the failure mode honestly; this is the accepted semantic, the measurement is what matters.
6. Create ~250 containers in a loop in one daemon lifetime if cheap (`for i in $(seq 260); do ...`). The old allocator was up-only and capped at 253 per process; the rework should lift the cap. Confirm.

## Stage 10. DNS and resolv.conf variants

Container DNS is generated from the host's resolver configuration. Test on each:

1. systemd-resolved host (`/etc/resolv.conf` -> `127.0.0.53`). Expect: containers get a usable upstream, not the loopback stub, which is unreachable from a container netns.
2. Non-resolved host: plain static `/etc/resolv.conf` with real nameservers.
3. Host with NetworkManager dnsmasq, and with the VPN rewriting resolv.conf.
4. In each: `tainer exec <pod> getent hosts example.com` and `getent hosts <project>.tainer.me`.

## Stage 11. Image store and registries

1. Pull from a private/auth registry if available. `registries.conf` is created if missing, and falls back to unset with a log line on failure: check that log line does not appear on a normal host.
2. The `policy.json` and auth pins are documented as dead in phase 1 (no signature verification, no credentials plumbing). Confirm that matches reality.
3. Identity-mode bind mounts (host path == container path) are scoped as per the threat-model note; confirm a project bind mount appears at the same path inside the container.

## Stage 12. Reboot

1. `sudo reboot`. After login: `systemctl is-active cyberstack.socket` is `active`, the service is `inactive`, no pods are running (ruling 12: no resurrection at boot).
2. `tainer start` brings the project up again through socket activation.

## Stage 13. Uninstall and upgrade

1. `sudo apt install ./tainer_<next>.deb` over a running install. Documented to be a manual `systemctl restart cyberstack.service` today (postinst only reloads and enables the socket). Confirm the old daemon keeps serving until restarted, and note the outage.
2. `sudo apt remove tainer`. Confirm what is left behind: units (no prerm exists yet), `/run/cyberstack`, the `cyberstack` group, per-user state. A prerm that stops the socket is a likely follow-up.

## Stage 14. CI and tests that have never executed

1. **Every Linux test binary was compiled but never executed.** The compile gate was `GOOS=linux go test -c -tags containers_image_openpgp -o /dev/null` per package, on macOS. A test that compiles can still fail at runtime. On the laptop: `cd cyber-stack && go test -tags containers_image_openpgp ./...` and `cd tainer && go test ./...`; triage each failure as test bug vs product bug. Run as root and non-root; several tests assume `/usr/bin/crun` exists.
2. `TestNewStackConstructs` needs `/usr/bin/crun` on the runner; it will fail on a bare CI image without `crun` installed.
3. Day-one ubuntu CI run: the new workflow runs the `cmd` and `benchmarks` packages, which the Makefile `test` target has never run. Expect surprises; first CI run is red-by-default until proven otherwise.
4. The test fixture seam (fake daemon for the sweep) has never been exercised against a real linux daemon.
5. `go vet ./...` and `gofmt -l .` on Linux.

## Stage 15. Cleanup sanity

1. After all stages: `sudo nft list tables` shows no leftover cyberstack table once the daemon is stopped.
2. `ip link`, `ip netns list`, `findmnt`, `ls /run/cyberstack`: nothing orphaned.
3. `sudo systemctl stop cyberstack.service` is a SIGTERM stop, not a kill (killing hard corrupts overlay state).

## Stage 16. Pointers

- Exit-code propagation ticket: find it in the Cyber5 Tracker under project TAIN (filter "exit code"); the T7 note in the ledger records the decision not to fix it in phase 1.
- Cross-container same-host-port steal: follow-up ticket for a create-time preflight.
- Handoff for dev-pod resilience: `docs/handoff/2026-09-08-dev-pod-resilience.md` (tain-99, tain-100, tain-101).

## Ledger cross-reference

Where each deferred or unverified item from the build ledger landed:

| Ledger item | Stage |
|---|---|
| Uplink pinned at daemon start; ip_forward not reverted | Limitations, 7 |
| Foreign FORWARD drops (Docker, ufw); teardown logs on absent table | Limitations, 8 |
| Privileged port binds assume rootful service | 4 |
| Same host port stolen across pods | Limitations, 16 |
| EBUSY self-move; crun absolute cgroupsPath; stray child pids | 6 |
| Activation env / fd 3 CLOEXEC | 2 |
| CI cmd+benchmarks packages; fixture seam unexecuted | 14 |
| Smoke test needs /usr/bin/crun | 14 |
| IP allocator seeding / 253 cap | 9 |
| registries.conf, dead policy.json, identity bind scope | 11 |
| :22 probe, unnamed 80/443 owners, banner sniff | 4 |
| Linux test binaries compiled not executed | 14 |
| Exit codes | Limitations, 16 |
| One operator per host; restart outage window | Limitations, 9 |
