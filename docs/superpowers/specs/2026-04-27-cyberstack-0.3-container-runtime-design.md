# CyberStack 0.3 — Container Runtime Design

> **Status:** brainstormed 2026-04-27. Pending implementation plan (writing-plans) + execution.

## Goal

After CyberStack 0.3 ships, this works end-to-end against `cyberstackd`:

```
docker pull alpine
docker run -d alpine sleep 1d
docker exec <id> echo hi
docker logs <id>
docker run alpine echo hi          # non-detached, with attach
docker stop <id>
docker rm <id>
docker rmi alpine
```

— and beats or matches OrbStack on Apple Silicon across five locked performance budgets.

The milestone targets the "fastest VM in the world for a Docker drop-in replacement" pitch by focusing the container runtime work on raw operation latency rather than feature breadth. Volumes, port publishing, custom networks, persistent VM, and amd64 emulation are explicitly deferred to 0.4 and beyond — sequenced so we learn from 0.3 perf characteristics before attacking the more elaborate features.

## Locked decisions

| Q | Decision | Reasoning |
|---|---|---|
| **Scope tier** | **B** — full `docker run` + exec + logs + inspect, no networks/volumes/ports | Tier A (bench-passing minimum) is too thin to call "Docker drop-in"; tier C dilutes the perf focus into compose-territory features OrbStack has been polishing for years. B is the smallest cut that lets us demo `docker run hello-world` end-to-end. |
| **State persistence** | **C** — A pattern in 0.3 (ephemeral VM + persistent data disk), B pattern (persistent VM via suspend/resume) in 0.4 | A is sufficient to hit the 0.3 numbers. B is the killer "we beat OrbStack on feel" feature but it's a self-contained week of work and deserves its own milestone with its own benchmarks. |
| **Multi-arch** | **C** — arm64-only in 0.3, amd64 emulation (qemu-user-static + binfmt_misc) in 0.4 alongside persistent VM | All five 0.3 benchmark images are arm64-native; emulation isn't on the perf-critical path. Verified `ghcr.io/cyber5-io/tainer-*` images are all amd64+arm64, so the existing tainer workflow is unaffected. |
| **Architecture** | **α** — agent-driven (containers/image, containers/storage, crun all in-VM) | Daemon stays a stateless HTTP→gRPC translator; agent is the engine. Fastest cold pull (no virtio-fs serialization), smallest daemon, single source of truth. Same shape Docker Desktop and OrbStack use. |

## Performance budgets

These are the five locked benchmarks. Each has a pass band, fail band, and OrbStack head-to-head comparison.

| Metric | Pass | Fail | What it measures |
|---|---|---|---|
| Warm `docker run -d hello-world` (daemon up, image cached) | <500ms | >1500ms | THE drop-in feel test |
| `docker pull alpine:latest` (cold: empty store) | <3s | >8s | First-use latency, mostly network-bound |
| `docker exec <running> sh -c "echo hi"` | <200ms | >500ms | Daily-driver path; RPC + crun fork floor |
| Cold-cold `docker run` (cold daemon, cold image, cold VM) | <6s | >12s | Worst-case first impression after install |
| Idle memory (daemon + cold VM, no containers) | <200MB | >500MB | Footprint; OrbStack-tier or the lightweight pitch dies |

Pass column ≈ OrbStack-class on Apple Silicon. Fail column ≈ Docker Desktop. Methodology in Section 8.

---

## Section 1 — Architecture & boundaries

```
host (macOS arm64)                          guest VM (linux/arm64)
─────────────────                           ──────────────────────
docker CLI                                  cyberstack-agent
   │                                          ├── proto/agent.v1
   ▼  HTTP+JSON                               │   ├── Ping, Version  (0.2)
cyberstackd  ──────── gRPC ─────────►         │   ├── Image.{Pull,List,Inspect,Remove}
   ├── httpapi.Server                         │   └── Container.{Create,Start,Stop,
   │     ├── /containers/*                    │           Wait,Delete,Inspect,List,
   │     ├── /images/*       (translator)     │           Logs,Attach,Exec,ExecStart}
   │     └── /info, /ping, /version           ├── containers/image    (registry+pull)
   ├── vm.VFKitLauncher                       ├── containers/storage  (layers+overlay)
   │     └── now also: persistent data disk   ├── crun (subprocess per container)
   │                                          └── cgroups v2
```

**Strict boundaries:**

- `cyberstackd` knows nothing about containers or images. It speaks Docker HTTP API on one side and gRPC on the other, and that's it. No `containers/image` import on host.
- `cyberstack-agent` is the container engine. It owns image pull, layer storage on the data disk, container lifecycle, exec, logs streaming, stats. It just happens to speak gRPC instead of having its own CLI.
- Persistent data disk is new in 0.3. A second virtio-blk device, separate from the boot disk, formatted ext4. Lives at `~/.cyberstack/data-arm64.img`. Contains the containers/storage tree (`/var/lib/cyberstack/storage/`). Survives `cyberstackd` restarts. Boot disk stays read-only.
- gRPC streams for logs/exec/attach. Bidi streaming over the existing single vsock conn — gRPC multiplexes via HTTP/2 frames, no new transport needed.

The 0.2 daemon plumbing (`AgentClient` interface, HTTP→gRPC translator pattern in `httpapi.makeInfoHandler`) is the template — every new endpoint follows the same shape.

## Section 2 — Components & proto extensions

### 2a. Agent proto additions

Two new gRPC services next to the existing `Agent` service:

```proto
service Image {
  rpc Pull(PullRequest) returns (PullResponse);          // blocks until done; no progress stream in 0.3
  rpc List(ListImagesRequest) returns (ListImagesResponse);
  rpc Inspect(InspectImageRequest) returns (ImageInspect);
  rpc Remove(RemoveImageRequest) returns (RemoveImageResponse);
}

service Container {
  rpc Create(CreateRequest)      returns (CreateResponse);
  rpc Start(StartRequest)        returns (StartResponse);
  rpc Stop(StopRequest)          returns (StopResponse);
  rpc Wait(WaitRequest)          returns (WaitResponse);
  rpc Delete(DeleteRequest)      returns (DeleteResponse);
  rpc Inspect(InspectRequest)    returns (ContainerInspect);
  rpc List(ListRequest)          returns (ListResponse);
  rpc Logs(LogsRequest)          returns (stream LogFrame);          // server-stream
  rpc Attach(stream AttachFrame) returns (stream AttachFrame);       // bidi
  rpc Exec(ExecRequest)          returns (ExecResponse);
  rpc ExecStart(stream AttachFrame) returns (stream AttachFrame);    // bidi
}
```

Types model only what tier B needs. Container spec fields: `image, cmd, args, env, workdir, hostname, labels, host_config{memory_bytes, cpu_shares, restart_policy}`. Container summary: `id, names, image, state, status, exit_code, created_at`. Networks/volumes/ports are not modeled — those are 0.4.

### 2b. Daemon HTTP translator routes

| Docker endpoint | Translates to | Notes |
|---|---|---|
| `POST /v1.43/containers/create` | `Container.Create` | Marshal `HostConfig` → trimmed spec |
| `POST /v1.43/containers/{id}/start` | `Container.Start` | |
| `POST /v1.43/containers/{id}/stop` | `Container.Stop` | timeout flag |
| `POST /v1.43/containers/{id}/wait` | `Container.Wait` | |
| `DELETE /v1.43/containers/{id}` | `Container.Delete` | force flag |
| `GET /v1.43/containers/{id}/json` | `Container.Inspect` | |
| `GET /v1.43/containers/json` | `Container.List` | |
| `POST /v1.43/containers/{id}/attach` | `Container.Attach` (bidi) | hijacked HTTP ↔ gRPC stream bridge |
| `GET /v1.43/containers/{id}/logs` | `Container.Logs` (server-stream) | docker frame format on the wire |
| `POST /v1.43/containers/{id}/exec` | `Container.Exec` | returns exec id |
| `POST /v1.43/exec/{id}/start` | `Container.ExecStart` (bidi) | hijacked ↔ gRPC |
| `POST /v1.43/images/create` | `Image.Pull` | blocks; CLI sees no progress in 0.3 |
| `GET /v1.43/images/json` | `Image.List` | |
| `GET /v1.43/images/{id}/json` | `Image.Inspect` | |
| `DELETE /v1.43/images/{id}` | `Image.Remove` | |

The hijacked-HTTP ↔ gRPC bridge for `attach`/`exec` is the only meaty translator code. Logs and pull are simpler.

### 2c. New agent modules

| Package | Responsibility |
|---|---|
| `internal/agent/imagestore` | wraps `containers/image` (registry copy) + `containers/storage` (layers, manifests, image DB) |
| `internal/agent/containerd` | container lifecycle state machine; spawns `crun` as subprocess; tracks PIDs, exit codes |
| `internal/agent/exec` | exec/attach stream multiplexer over gRPC bidi |
| `internal/agent/logs` | per-container stdout/stderr capture to ringbuffer + on-disk log file |
| `internal/agent/network` | bridge + veth + nftables setup (Section 5) |
| `internal/agent/server.go` | grow to register Image + Container services next to the existing Agent service |

### 2d. Daemon-side additions

| File | Responsibility |
|---|---|
| `internal/httpapi/containers.go` | the 11 container handlers; mostly thin gRPC-call wrappers |
| `internal/httpapi/images.go` | the 4 image handlers |
| `internal/httpapi/hijack.go` | shared HTTP-hijack helper for attach/exec |
| `internal/daemon/client.go` | grow client wrappers for Image + Container services |

Ballpark: ~20 new proto messages, ~16 new daemon handlers, ~5 new agent packages. Single biggest piece is `imagestore` (~400 lines wrapping containers/image+storage). Smallest are daemon handler shells (~30 lines each).

## Section 3 — Data flow walkthroughs

### 3a. `docker pull alpine` (cold, image not in store)

1. CLI → `POST /v1.43/images/create?fromImage=alpine&tag=latest`
2. *daemon: parse query → `Image.Pull(ref="docker.io/library/alpine:latest")`*
3. agent: `containers/image` opens registry source over the agent's NAT-side virtio-net → copies blobs straight to `containers/storage` on the data disk → writes manifest + image record
4. agent → `PullResponse{ImageID, RepoTag}`
5. *daemon: emits a single JSON line `{"status":"Downloaded newer image for alpine:latest"}` and closes*
6. CLI prints final status

Network path: agent → guest kernel routes via `eth0` (virtio-net) → vfkit translates → host's NAT → internet. No host-side `containers/image`.

### 3b. `docker run -d alpine echo hi` (warm, image cached)

CLI runs two requests:

1. `POST /v1.43/containers/create` with body `{Image, Cmd, Env, HostConfig{Memory, CpuShares}, ...}`
   - *daemon: marshal subset of body → `Container.Create(spec)`*
   - agent: containers/storage creates overlay rootfs (image's layers as lowerdirs, fresh upperdir), writes OCI bundle (`config.json`), persists container metadata to BoltDB on data disk
   - agent → `CreateResponse{ID: "abc123…"}`
   - daemon → `201 Created` with the ID
2. `POST /v1.43/containers/{id}/start`
   - *daemon → `Container.Start(id)`*
   - agent: `exec.Command("/usr/bin/crun", "run", "-d", "-b", bundlePath, id).Start()` → crun creates cgroup, joins namespaces, execs the container's init, daemonizes
   - agent: store PID, waitpid in a goroutine; logs piped to per-container log file
   - agent → `StartResponse{PID}` after crun confirms running
   - daemon → `204 No Content`

End-to-end budget for warm: <500ms. Time-of-flight: HTTP parse (~ms) + gRPC RTT (~ms) + storage overlay setup (~tens of ms) + crun fork+namespace+exec (~tens of ms). Fat margin.

### 3c. `docker exec abc123 sh -c "echo hi"`

1. `POST /v1.43/containers/abc123/exec` body=`{Cmd:["sh","-c","echo hi"], AttachStdout:true, AttachStderr:true}`
   - *daemon → `Container.Exec(containerID, spec)` → returns `ExecID`*
2. `POST /v1.43/exec/{execID}/start` body=`{}`
   - *daemon: HTTP-hijack — take over the raw TCP conn from the response writer*
   - *daemon: open gRPC bidi `Container.ExecStart`, send `ExecID` as the first frame*
   - agent: `crun exec abc123 sh -c "echo hi"` with stdout/stderr captured
   - agent → bidi: `AttachFrame{stream=STDOUT, data="hi\n"}` → `AttachFrame{stream=EXIT, code=0}`
   - *daemon: re-wrap each frame in Docker's 8-byte stream framing (`[stream_type, 0,0,0, len_be32, payload]`), write to the hijacked TCP*

### 3d. `docker logs -f abc123`

1. CLI → `GET /v1.43/containers/abc123/logs?follow=1&stdout=1&stderr=1`
2. *daemon → `Container.Logs(id, follow=true)` (server-stream gRPC)*
3. agent: open per-container log file, follow with inotify → emit `LogFrame{stream, data, ts}` per chunk
4. *daemon: re-frame to Docker stream-framing, flush per chunk*

### 3e. The hard parts

- Hijacked HTTP for attach/exec/start. Standard `http.Hijacker` interface; bridge code ~150 lines, well-trodden (libpod has it).
- Docker stream framing on the wire. 8 bytes per frame, big-endian length. Easy.
- `Container.Wait` semantics. Docker's wait blocks until container exits. Agent already has a waitpid goroutine; signal a channel.

## Section 4 — Storage model

### 4a. Data disk

| | |
|---|---|
| Host path | `~/.cyberstack/data-$(arch).img` |
| Format | Sparse raw file. Default 50GB (configurable via `--data-disk-size`); APFS sparse so only used blocks cost real disk |
| In-VM device | `/dev/vdb` (boot disk is `vda`) |
| Filesystem | ext4. `containers/storage` uses overlay2 driver — its happy path on ext4 |
| Mount | `/var/lib/cyberstack/storage` on the agent at boot |

### 4b. First-boot bootstrap

Daemon-side, on `cyberstackd` start:

1. Check `~/.cyberstack/data-arm64.img` exists. If not: create sparse file, `truncate -s <data-disk-size>`. No formatting on host.
2. If file exists and `--data-disk-size` is *larger* than current size: grow via `truncate -s NEW_SIZE` (never shrink).
3. Pass disk path to vfkit via `--device virtio-blk,path=…`.

Agent-side, in `/init`:

```sh
if ! blkid /dev/vdb >/dev/null 2>&1; then
    mkfs.ext4 -F -L CSDATA /dev/vdb
fi
mount /dev/vdb /var/lib/cyberstack/storage
resize2fs /dev/vdb 2>/dev/null || true   # no-op if already at correct size
```

`mkfs.ext4` ships in `e2fsprogs` added to the initramfs (~1MB).

### 4c. containers/storage layout (managed by the library)

```
/var/lib/cyberstack/storage/
├── overlay/                    # layer storage (overlay2 driver)
│   └── <layer-id>/             # each layer = lower/upper/work dirs
├── overlay-images/             # image manifests, configs
│   └── images.json             # image-id → repo:tag, layers, etc.
├── overlay-containers/         # per-container metadata (containers/storage's own list)
│   └── containers.json
└── tmp/                        # in-flight pulls
```

This is `containers/storage`'s native layout — configured via `storage.conf`, library owns the directory.

### 4d. Container runtime state (ours, separate from containers/storage)

```
/var/lib/cyberstack/containers/
└── <container-id>/
    ├── config.json             # OCI bundle config (crun reads this)
    ├── rootfs/                 # symlink to overlay merged dir
    ├── log.json                # docker-style log file (one JSON line per frame)
    ├── pid                     # crun init PID (for waitpid)
    └── state.json              # our state (created/running/stopped, exit_code, started_at, ...)
```

Two sources of truth (containers/storage + ours) feels off but it isn't — `containers/storage` only tracks the *existence* of a container's storage tree, not its runtime state. Runtime state is ours. Same split podman uses.

### 4e. Logs

`log.json` format = JSON-lines, one entry per write:

```json
{"t":"2026-04-27T10:00:00.123Z","s":"stdout","b":"hello\n"}
```

`Container.Logs` RPC tails this file with `inotify` for follow=true. **Log rotation is not in 0.3** — unbounded log files are accepted for the milestone (worst case: a benchmark run grows logs by KBs). Rotation lands in 0.4.

### 4f. Path-by-path map

| Data | Location | Persistence |
|---|---|---|
| Image layers, manifests | data disk: `/var/lib/cyberstack/storage/` | survives `cyberstackd` restart, host restart |
| Container OCI bundle, log, state | data disk: `/var/lib/cyberstack/containers/` | same |
| crun PID + cgroup state | live in VM, in cgroupfs / kernel | dies with VM (containers don't survive vfkit kill in 0.3 — fixed in 0.4 with persistent VM) |
| EFI vars, boot disk contents | host: `~/.cyberstack/cyberstack-efivars`, `~/.cyberstack/boot-arm64.img` | recreated each launch (boot disk is built artefact; efivars zeroed each boot) |
| Daemon Docker socket, vsock socket, lockfile | host: `~/.cyberstack/cyberstack.sock`, `~/.cyberstack/vsock.sock`, `~/.cyberstack/cyberstackd.pid` | per-process; cleaned on shutdown |

**Convention**: on the host, runtime data is exclusively under `~/.cyberstack/`. The repo's `guest/` directory is build territory — `make boot-disk` produces an artefact there but never reaches into the user's home. Installation (manual `cp` for dev, installer for packaged release) places the artefact under `~/.cyberstack/`.

## Section 5 — Networking

Tier B excludes port publishing and custom networks. Network scope reduces to: containers can reach the internet.

### 5a. The picture

```
internet
   ▲
   │  Apple Virt NAT (vfkit --device virtio-net,nat)
   │
   eth0 (VM)  192.168.x.x via DHCP from Apple's resolver
   ▲
   │  SNAT masquerade rule (nftables)
   │
   cs0 (linux bridge)  172.17.0.1/16
   ▲   ▲   ▲
   │   │   └─ veth-c3 ── eth0 (container 3, 172.17.0.4)
   │   └──── veth-c2 ── eth0 (container 2, 172.17.0.3)
   └──────── veth-c1 ── eth0 (container 1, 172.17.0.2)
```

### 5b. VM ↔ internet

`vm.Spec` gains one device: `--device virtio-net,nat`. Apple Virt provides DHCP + DNS forwarding through this. VM gets a private IP, default route through Apple's NAT, DNS via the host's resolver. Zero agent-side config.

### 5c. Container ↔ internet

Agent setup on first start (idempotent):

1. Create bridge `cs0` with `172.17.0.1/16`.
2. Enable IPv4 forwarding (`/proc/sys/net/ipv4/ip_forward = 1`).
3. nftables rule: `nft add rule ip nat postrouting oifname "eth0" ip saddr 172.17.0.0/16 masquerade`.

Per-container during `Container.Start` (after crun creates the netns, before container runs):

1. Allocate next IP from `172.17.0.0/16` (in-memory counter, persisted to `state.json`).
2. Create veth pair `veth-<short-id>` ↔ `eth0` inside container netns.
3. Push the container side into the netns (`ip link set eth0 netns <pid>`).
4. Assign IP, set default route (`172.17.0.1`), bring up.
5. Write container's `/etc/resolv.conf` from the VM's resolver (so DNS works).

Subnet `172.17.0.0/16` matches Docker's default `docker0` — least-surprising for users who poke around inside containers. No collision risk: our `cs0` lives inside the VM and is masqueraded behind `eth0`, so the host never sees this range.

### 5d. Tools added to the guest initramfs

| Binary | Source | Approx size | Why |
|---|---|---|---|
| `nft` | Alpine `nftables` APK | ~1.5MB | NAT masquerade rule |
| `iproute2` | Alpine `iproute2` APK | ~3MB | Full `ip link/addr/route` (busybox version is limited) |
| Kernel modules: `bridge`, `veth`, `nf_tables`, `nf_nat`, `nf_conntrack` | Alpine `linux-virt` APK | ~0.5MB | already extracting from this APK for vsock |

Initramfs grows from ~8MB to ~13MB.

### 5e. Implementation: roll our own (not CNI)

Networking handled directly via the `vishvananda/netlink` Go library inside the agent: bridge create, veth, IP assign, nftables rules — all from agent code (~300 lines).

CNI would add value for port publishing and custom networks, neither of which is in 0.3. We adopt CNI in 0.4 when its features are actually needed; until then the simpler in-process implementation wins on footprint and clarity.

### 5f. What's NOT in 0.3

- `docker run -p 8080:80` — daemon translator rejects with clear "port publishing lands in 0.4" error
- `docker network create` — no custom networks; only the implicit `cs0` bridge exists
- Container-to-container DNS resolution by name — no embedded DNS server
- IPv6
- Per-container egress firewall

## Section 6 — crun packaging

### 6a. Source

`crun` upstream releases (`containers/crun` on GitHub) ship official **static** binaries for `linux/arm64`. ~5MB. No library deps. Treated like the systemd-boot binary: pin a known version, fetch + sha256 verify in `guest/Makefile`, drop into the initramfs.

```
guest/Makefile additions:
CRUN_VERSION := 1.20
CRUN_URL     := https://github.com/containers/crun/releases/download/$(CRUN_VERSION)/crun-$(CRUN_VERSION)-linux-arm64-static
# fetch + sha256 verify + drop at /usr/bin/crun in initramfs-staging
```

### 6b. Why static, not Alpine APK

Alpine ships `crun` dynamically linked against musl + libcap + libseccomp + libyajl. We'd need to extract all those libraries. The static binary from upstream is one file, one checksum, one fetch step.

### 6c. Where it sits at runtime

`/usr/bin/crun` inside the running initramfs. Agent invokes by absolute path: `exec.Command("/usr/bin/crun", "run", ...)`.

### 6d. Initramfs size budget

| Artefact | 0.2 size | 0.3 added | Running total |
|---|---|---|---|
| busybox + musl | 1.6MB | — | 1.6MB |
| vsock modules | 0.2MB | — | 0.2MB |
| cyberstack-agent | 14MB | grows ~5MB (+image/storage/runtime) | 19MB |
| crun static | — | 5MB | 5MB |
| iproute2 (full) | — | 3MB | 3MB |
| nftables + libs | — | 2MB | 2MB |
| network kernel modules | — | 0.5MB | 0.5MB |
| **Initramfs (gzipped)** | **8.3MB** | **~+8MB** | **~16MB** |

Boot disk is 256MB FAT32 — plenty of room. ~16MB initramfs decompress is single-digit ms at SSD speeds; doesn't move the boot budget.

## Section 7 — Error handling & lifecycle

### 7a. Failure modes and responses

| What fails | How daemon detects | Response | User-visible |
|---|---|---|---|
| Image pull (network, no manifest, auth) | `Image.Pull` returns gRPC error | translate to Docker error JSON, status 500 | `docker pull` prints error, exits non-zero |
| Container exits | agent's waitpid goroutine fires; `Container.Wait` returns | nothing daemon-side; state recorded by agent | `docker wait` returns exit code; `docker ps -a` shows "exited" |
| Container OOM | crun reports kill signal; agent records OOMKilled=true | same as exit, with flag | `docker inspect` shows `OOMKilled: true`, exit 137 |
| Disk full | gRPC error `ResourceExhausted` | translate to 507 | message: "data disk full — try `docker images -q \| xargs docker rmi` or restart with `--data-disk-size=N` (currently 50G) to grow" |
| Agent process dies | gRPC connection drops on next RPC | mark agent-dependent endpoints unhealthy; `/info` reports `"AgentReachable": false` | `docker ps` etc. fail with 503; `/info` still renders metadata |
| vfkit dies / VM kernel panic | 0.2 reaper goroutine flips state to Stopped; agent-conn drop surfaces it | same as above | container ops return 503 |
| Boot disk missing on `cyberstackd` start | os.Stat fails before VM start | exit 1 with error | "boot disk not found at ~/.cyberstack/boot-arm64.img — run install or pass --boot-disk" |
| Two `cyberstackd` instances on same data disk | lockfile collision | exit 1 with error | "another cyberstackd is using ~/.cyberstack/data-arm64.img (PID N)" |
| Hijacked attach/exec stream loses TCP conn | gRPC bidi sees half-close | cancel underlying exec; agent kills the exec process | `docker exec` exits with stream error |

### 7b. Lifecycle state machines

**VM** (already in 0.2):
```
Stopped → Starting → Running → Stopping → Stopped
```

**Container** (new in 0.3):
```
Creating → Created → Running → Stopping → Stopped
                              ↘                ↗
                                Exited (clean or oom-killed)
```

State persisted to `state.json` per container on the data disk so `docker ps` / `docker inspect` work post-restart (read-only — running containers don't survive cyberstackd restart in 0.3, but records do).

**Image**: just "in store" or "not in store". `containers/storage` is the source of truth.

### 7c. cyberstackd lifecycle

- **Start**: lockfile (`~/.cyberstack/cyberstackd.pid`) → bind vsock unix socket → start vfkit → wait for agent dial-in (10s budget) → start HTTP server on `~/.cyberstack/cyberstack.sock`.
- **Hot path**: stateless HTTP→gRPC translator. No daemon-side state to corrupt.
- **Stop** (SIGTERM/SIGINT): cancel root context → close HTTP listener → close agent gRPC client (5s drain timeout) → kill vfkit → release lockfile. Total clean shutdown <6s.
- **Crash**: lockfile is left dangling but contains stale PID; next start detects via `kill(pid, 0)` and reclaims.

### 7d. Disk-full recovery options

In order of escalation:

1. **Standard Docker cleanup** (tier B supports this):
   ```
   docker images -q | xargs docker rmi
   docker ps -aq  | xargs docker rm
   ```
2. **Grow the data disk**: `cyberstackd --data-disk-size=100G` on next start. Daemon truncates the sparse file (never shrinks), agent runs `resize2fs /dev/vdb` on boot. Online, non-destructive.
3. **Last resort**: `cyberstackd stop && rm ~/.cyberstack/data-arm64.img && cyberstackd`. Loses all images and stopped containers; fresh start. The error message tells users this option exists.

### 7e. Deliberately NOT handled in 0.3

- Reconnecting agent across vsock disconnect (user restarts cyberstackd; persistent-VM 0.4 changes this).
- Container restart policies (`--restart=on-failure` etc.). Accepted but treated as `no` with a warning log — keeps compose files functional.
- Liveness/readiness probes.
- `HEALTHCHECK` directives.
- Log rotation.

## Section 8 — Testing strategy + OrbStack baseline

### 8a. Layered tests

| Layer | Scope | Tool |
|---|---|---|
| Unit — agent | `imagestore`, `containerd` state machine, `exec` multiplexer, `logs` ringbuffer + tail | `go test ./internal/agent/...` |
| Unit — daemon | each HTTP handler with mock `AgentClient`, body marshalling roundtrips, hijack bridge | `go test ./internal/httpapi/...` |
| Integration — Docker compat | `docker` CLI hits real `cyberstackd` over the unix socket. Extends 0.2's `tests/integration/`. | `go test -tags=integration ./tests/integration/...` |
| Benchmarks | the five locked metrics, against `cyberstackd` and OrbStack | new tool: `cmd/cs-bench` |

### 8b. The five locked benchmarks

`tests/integration/bench_test.go`, build-tag `bench`:

```go
//go:build bench

func BenchmarkWarmDockerRunDetached(b *testing.B)    // <500ms / >1500ms
func BenchmarkColdImagePull(b *testing.B)            // <3s / >8s
func BenchmarkExec(b *testing.B)                     // <200ms / >500ms
func BenchmarkColdColdFirstRun(b *testing.B)         // <6s / >12s
func BenchmarkIdleMemory(b *testing.B)               // <200MB / >500MB
```

Each:
1. Sets up state (fresh daemon, cached image, etc.).
2. Times the operation N times (default N=20, configurable).
3. Reports p50, p90, p99 (single-run noise from GC etc.).
4. Asserts p50 against pass/fail bands → red CI on regression.

Output format (JSON to stdout + human table):
```
benchmark           p50      p90      p99      pass<     fail>     verdict
warm-docker-run    420ms    480ms    510ms     500ms     1500ms    PASS
cold-pull          2.7s     2.9s     3.4s      3s        8s        PASS
exec               150ms    180ms    210ms     200ms     500ms     PASS
cold-cold          5.2s     5.6s     6.1s      6s        12s       PASS
idle-memory        145MB    —        —         200MB     500MB     PASS
```

### 8c. OrbStack head-to-head methodology

`cs-bench` speaks plain Docker over `DOCKER_HOST`, so it runs identically against both engines. Procedure:

1. Reinstall OrbStack on the development Mac (was deleted earlier — same machine, same SSD, same OS version).
2. Quiesce host: close noisy apps, plug in power, disable Time Machine for the run.
3. Run cs-bench against OrbStack (`DOCKER_HOST=unix:///var/run/docker.sock`). N=20 per metric. Save results JSON.
4. Stop OrbStack (`orb stop`), confirm no orphan VMs running.
5. Run cs-bench against cyberstackd (`DOCKER_HOST=unix://~/.cyberstack/cyberstack.sock`). N=20 per metric. Save results JSON.
6. Compare via `cs-bench-compare results-cs.json results-orb.json`.

The output gets published in 0.3 release notes:
```
metric              cyberstack    orbstack    delta       verdict
warm-docker-run        420ms        330ms     +90ms       OrbStack wins
...
```
Honest comparisons. If we lose anywhere, we lose visibly. Numbers above are illustrative — actual will be measured.

### 8d. Manual-start methodology

`cs-bench` is purely a client — never starts/stops engines. The user starts each engine in one terminal, runs the bench in another. Reasons:

1. Cleanest separation: harness only speaks Docker.
2. Honest comparison: both engines exercised the way users actually use them.
3. No flakiness from in-test lifecycle.

For the cold-cold benchmark, `cs-bench` issues `docker rmi <image>` (best-effort) before each timing run to evict the image from each engine's store. This isn't bit-perfect cold (deeper caches we can't reach without wiping data dirs), but the same protocol against both engines keeps the comparison fair.

For truest first-impression cold-cold, the procedure doc instructs: "fresh-install the engine, ensure no images, run a single cold-cold measurement."

### 8e. CI strategy

| Test class | Runs where | When |
|---|---|---|
| Unit | GitHub Actions (linux/macos) | every push |
| Integration (`-tags=integration`) | macos-14-arm64 runner (has vfkit) | every push |
| Benchmarks (`-tags=bench`) | macos-14-arm64 runner | nightly + before tag |
| OrbStack baseline | manual on dev Mac | before each minor version |

OrbStack baseline is manual because GitHub runners can't reliably install OrbStack and the comparison only matters at release time.

### 8f. Reproducibility

The bench tooling lives in the repo, the procedure is documented in `docs/benchmarking.md` (new file in 0.3), and every release publishes the raw JSON results alongside the table. Anyone can re-run the same numbers on their own Mac.

## Section 9 — Out of scope (explicit list)

Everything below is deliberately NOT in 0.3.

### Container features deferred

| Feature | Lands in | Why deferred |
|---|---|---|
| Volumes / bind mounts (`-v`, `--mount`) | 0.4 (with virtio-fs) | Needs virtio-fs setup; tied to compose-tier C |
| Port publishing (`-p host:container`) | 0.4 | Needs port forwarder + UDP/TCP NAT rules; tied to networks |
| Custom networks (`docker network *`) | 0.4 | Needs CNI or netavark; multiple bridges; service discovery DNS |
| Container restart policies | 0.4 | Accepted in 0.3 but treated as `no` (logged warning) — keeps compose files functional |
| Healthchecks (`HEALTHCHECK`) | 0.5+ | Liveness/readiness probes, separate concern |
| `docker stats` per-container | 0.4 | Needs cgroups v2 metrics streaming |
| `docker events` live stream | 0.5+ | Bus + subscription model |
| `docker exec -it` PTY resize | 0.4 | The `/containers/{id}/resize` endpoint and TTY size events; non-TTY exec works in 0.3 |

### Image features deferred

| Feature | Lands in | Why |
|---|---|---|
| `docker build` / BuildKit | 0.5+ | A whole separate engine; not on the perf-critical path |
| `docker push` | 0.5+ | Few users push from dev machines |
| `docker save` / `load` | 0.5+ | Niche workflows |
| `docker history` | 0.5+ | Inspect-only; informational |
| amd64-on-arm64 emulation | 0.4 | qemu-user-static + binfmt_misc; locked in Q3 |
| Private registry auth | 0.4 | Anonymous pulls cover the benchmarks; auth needs credential store design |

### System / engine features deferred

| Feature | Lands in | Why |
|---|---|---|
| `docker system prune` | 0.4 | Bulk endpoint; users substitute with `docker images -q \| xargs docker rmi` in 0.3 |
| Persistent VM (suspend/resume) | 0.4 | Locked in Q2 — sequential after we ship 0.3 |
| Cgroups v1 fallback | never | We commit to cgroups v2; Linux 6.12 supports it natively |
| Compose orchestration | 0.5+ | Compose CLI runs *against* our engine; we don't implement compose ourselves |

### Platform features deferred

| Feature | Lands in | Why |
|---|---|---|
| Linux host (no-VM shim mode) | post-1.0 | Per spec, Linux is secondary target |
| Intel Mac (amd64 host) | post-1.0 | Mirror of Apple Silicon — same code, opposite VM arch; will work but untested in 0.3 |
| Windows | post-1.0 | Hyper-V instead of vfkit; whole new launcher |

---

## Acceptance criteria

CyberStack 0.3 is shippable when **all** of the following hold:

1. The seven `docker` commands listed under **Goal** work end-to-end against `cyberstackd` (verified by integration tests in `tests/integration/`).
2. All five performance benchmarks report `PASS` p50 (verified by `cs-bench`).
3. OrbStack head-to-head comparison published alongside the release, with honest deltas — wins, losses, and ties all documented.
4. `~/.cyberstack/` is the sole runtime directory on the host (no flag overrides required for default operation).
5. `cyberstackd --data-disk-size=N` grows the data disk online without data loss.
6. Out-of-scope features (Section 9) return clear error messages directing the user to the milestone where the feature lands.
7. CyberStack 0.3.0 tagged on cyber-stack repo; spec + plan docs updated in tainer repo to reflect shipped status (matching the 0.1 / 0.2 convention).

## References

- 0.1 plan: `docs/superpowers/plans/2026-04-23-cyberstack-0.1-mvp-engine-skeleton.md`
- 0.2 plan: `docs/superpowers/plans/2026-04-25-cyberstack-0.2-vm-bringup-agent-handshake.md`
- Top-level CyberStack design: `docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`
- Implementation plan for 0.3 (this design's downstream): created via writing-plans skill after this spec is approved.
