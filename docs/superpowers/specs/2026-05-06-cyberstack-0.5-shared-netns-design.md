# CyberStack 0.5 + Tainer 0.9.x — shared-netns pivot — design

**Date:** 2026-05-06
**Status:** approved, ready for plan
**Supersedes (partially):** the per-pod /24 + Docker network API design in `2026-05-04-tainer-0.9-pod-lifecycle-network-design.md`

## Why this exists

Tainer 0.9 was built against the assumption that cyberstack would expose a Docker-style `/networks/*` API and that pods would each get their own /24 subnet with router multi-attach. End-to-end testing surfaced two findings:

1. **CyberStack 0.4.2 has no `/networks/*` API.** Adding it (with per-pod Linux bridges, IPAM, multi-attach) is a significant chunk of engine work.
2. **The per-pod /24 design wasn't what tainer 0.2.x had been doing.** 0.2.x used podman's pod abstraction — shared network namespace, one IP per pod, all pods on a single shared bridge. The per-pod /24 model was an over-engineered drift introduced when porting to Docker primitives.

The right model is the 0.2.x one, expressed in standard Docker primitives: each pod has a "leader" container (web) on the shared bridge that owns the published ports; sibling containers (app, db, etc.) join the leader's network namespace via Docker's `NetworkMode: "container:<id>"`. CyberStack stays a generic Docker-compatible engine; tainer composes the pod abstraction client-side.

This spec covers the cyberstack engine extensions needed to support that primitive (cyberstack **0.5**) plus the tainer rework that adopts it (tainer **0.9.x**).

## Goals

- **CyberStack 0.5** ships the minimum container API extension that tainer 0.9 actually needs: shared-netns via `NetworkMode: "container:<id>"`, bind/volume mounts, port bindings, restart policy, fractional CPU.
- **Tainer 0.9.x** drops per-pod subnets, drops router multi-attach, adopts shared-netns pods, replaces "subnet octet" UX with the more direct "pod ID = port prefix" scheme, and fixes two bind-mount regressions vs 0.2.x.
- **CyberStack stays generic.** No "pod" concept enters the engine. Anyone using cyberstack as a Docker drop-in (e.g., DDEV in 0.6+) gets the same primitive.

## Non-goals

- `/networks/*` API (deferred to cyberstack 0.6 with the rest of DDEV-readiness)
- Explicit `/volumes/*` API (deferred to 0.6; named volumes auto-create on first reference in 0.5)
- Pod-aggregate cgroup limits (tainer 1.0 + cyberstack 0.7+ — out-of-tree cgroup work)
- TUI restoration (Step 9, separate milestone)
- Network isolation between pods on the shared bridge (Blenzi multi-tenant production concern, not 0.9 dev concern)
- Replacing tainer-images for the new role layout (that's its own pipeline, parallel work)

## Architecture overview

```
host (macOS)
  cyberstackd ──> vfkit VM ── virtio-fs sharedDir=$HOME, mountTag=host ──> /host inside VM
                              cs0 (Linux bridge) ── containers (per-veth)
                                                        │
                              tainer-router-web (caddy)
                              tainer-router-ssh (sshpiperd)
                              tainer-mywp-web (leader, owns netns + ports)
                              tainer-mywp-app (NetworkMode: container:tainer-mywp-web)
                              tainer-mywp-db  (NetworkMode: container:tainer-mywp-web)
                              tainer-blog-web (leader)
                              tainer-blog-app (joined to blog-web)
                              ...
```

**Pod model:** 1 leader container + N sibling containers in the leader's netns. The leader owns:
- the network identity (one IP on cs0)
- the published TCP host ports (via `PortBindings` on its create call)
- the inbound traffic from the router caddy

**Router model:** caddy (web) and sshpiperd (ssh) live as plain containers on cs0 alongside the pods. Caddy reverse-proxies by Host header to each pod's leader IP. No multi-attach, no per-pod connect/disconnect.

**Data path for an HTTPS request to `https://demo.tainer.me`:**
```
Browser ─HTTPS:443─> tainer-router-web (cs0 IP, terminates TLS)
                  ─HTTP─> tainer-demo-web (cs0 IP, per-type routing rules)
                  ─fastcgi/upstream via 127.0.0.1─> tainer-demo-app (shared netns)
```
The web→app hop traverses the kernel loopback inside the shared netns — strictly faster than the per-pod-bridge route the 2026-05-04 design implied.

## CyberStack 0.5 — engine surface

### Wire additions to `ContainerSpec.HostConfig`

| Field | Type | Behaviour |
|---|---|---|
| `NetworkMode` | string | `""` or `"bridge"` = own veth on cs0 (default). `"container:<name-or-id>"` = share that container's netns. |
| `Mounts` | `[]Mount` | Each `{Type: "bind"\|"volume", Source, Target, ReadOnly}`. Bind sources must be under the virtio-fs shared root ($HOME). Named volumes auto-created. |
| `PortBindings` | `map[int]PortBinding` | Container-port → `{HostIP, HostPort}`. Ignored on shared-netns containers (the leader owns them). |
| `RestartPolicy` | string | `no` (default), `always`, `unless-stopped`, `on-failure[:max-retries]`. |
| `NanoCPUs` | int64 | Fractional cores × 1e9. Translates to cgroup v2 `cpu.max`. |

The HTTP API surface stays at the existing 5 endpoints — only the JSON body of `POST /containers/create` grows. Backwards-compatible: omitted fields use zero defaults.

### Agent-side handling

Inside the VM, the agent translates `ContainerSpec` into a crun OCI bundle:

**NetworkMode `container:<id>`:**
- Look up target container's running PID via the agent's state.json
- Resolve its netns path: `/proc/<pid>/ns/net`
- Pass that path into the OCI spec as `linux.namespaces[].path` for `network`
- Container shares net + hostname + DNS
- If the target container is not running, fail the create with a clear error

**Bind mounts of host paths:**
- VM mounts the host's `$HOME` at `/host` via virtio-fs (`mount -t virtiofs host /host` at boot)
- Agent translates bind-mount sources: `/Users/lenineto/foo` → `/host/Users/lenineto/foo`
- Path translation lives in `internal/agent/network/share.go` (or similar)
- Sources outside `$HOME` are rejected at the agent (no `/etc/passwd` exfil)

**Named volumes:**
- Auto-created at `/var/lib/cyberstack/volumes/<name>/_data` on first reference
- Bind-mount that path into the container at the requested target
- No `/volumes/*` API — name reuse is the only way to address an existing volume in 0.5

**PortBindings:**
- Apply via nftables DNAT rules on cs0 host-side: `ip daddr <vm-ip> tcp dport <hostPort> dnat to <containerIP>:<containerPort>`
- For containers with shared netns: silently no-op the bindings (leader owns them; double-binding the netns is invalid)
- The cyberstack VM bridges these to the macOS host via the existing vmnet/external-gvproxy path

**RestartPolicy:**
- Agent's container reaper consults the policy on container exit
- `no`: do nothing
- `always`: restart unconditionally
- `unless-stopped`: restart unless the container was explicitly stopped via `/containers/<id>/stop`
- `on-failure[:N]`: restart on non-zero exit, optionally capped at N attempts

**NanoCPUs:**
- Translate `1500000000` (= 1.5 cores) into `cpu.max = 150000 100000` in the cgroup
- Existing `CpuShares` field stays for now (unused in 0.9, may be deprecated in 0.6)

### vfkit launch

Add `--device virtio-fs,sharedDir=$HOME,mountTag=host` to the existing argv. Inside the VM, `init.sh` mounts `host` → `/host` early in boot.

### Version

CyberStack `0.4.2 → 0.5.0`. Tag from `main` after the work lands.

## Tainer 0.9.x — rework

### Pod ID & TCP port scheme

**Pod IDs run from 3001 to 3999** (999 concurrent pods max). The pod ID is the literal 4-digit prefix of every TCP port the pod uses.

**Allocation:** persistent registry at `~/.cyberstack/tainer/slots.json`:
```json
{
  "demo": {"pod_id": 3012, "tcp_pins": {}},
  "blog": {"pod_id": 3007, "tcp_pins": {"my-weird-thing": 45678}}
}
```
- `AllocatePodID(name)` picks the lowest free 3001-3999 not in use by any pod
- `FreePodID(name)` releases on `tainer destroy`
- `PinTCPPort(pod, role, port)` records a manifest-pinned port; subsequent allocations skip it

**Service offset table** (fixed across all pods):

| offset | role     | typical container port |
|---|---|---|
| 0 | (reserved) | — |
| 1 | db        | 3306 / 5432 |
| 2 | app       | varies |
| 3 | cache     | 6379 / 11211 |
| 4 | mail      | 1025 (SMTP) |
| 5 | search    | 9200 / 7700 |
| 6 | xdebug    | 9003 |
| 7 | queue     | 5672 / 9092 |
| 8 | storage   | 9000 (minio) |
| 9 | custom    | user-defined |

**Auto-derived host port:** `pod_id * 10 + offset`. E.g., pod 3012's db is `3012 * 10 + 1 = 30121`.

**Two distinct port bands:**
- **30000-39999** = auto-derived only. Manifest `host_port:` values inside this range are rejected at validation time.
- **40000-49000** = user-pinned only. Manifest `host_port:` must fall here. ~9000 ports for the long-tail use case where >10 TCP services or specific port numbers are needed.

### Manifest schema additions

`PortEntry` gains an optional `host_port`:
```yaml
ports:
  - role: db
    container: 3306
    protocol: tcp
    # host_port omitted → auto-derived (offset 1, pod 3012 → 30121)
  - role: weird-thing
    container: 12345
    protocol: tcp
    host_port: 45678   # explicit, must be 40000-49000
```

Validation (in `manifest.validate()`):
- If `host_port` is set, `40000 <= host_port <= 49000`
- If `protocol` is `http`, `host_port` is rejected (HTTP services route through the edge caddy, no host port)

### `pod.Start` orchestration

```
1. Load manifest, allocate pod_id (or reuse existing from labels)
2. Compute host port set:
   - For each TCP role with host_port: pin it
   - For each TCP role without host_port: derive from pod_id + offset table
3. Create + start LEADER (web) on cs0:
   - PortBindings = full pod port set
   - Mounts: <project>/html → /var/www/html
             <project>/data → /var/www/data
             <project>/<extra> → /var/www/<extra> (from manifest mounts:)
   - Labels: tainer.pod, tainer.role=web, tainer.pod-id=<3012>, tainer.manifest-path, tainer.manifest-hash
4. For each non-leader role (app, db, cache, ...):
   - Create + start with NetworkMode = "container:tainer-<pod>-web"
   - app: same html/data mounts as web (so php-fpm can read source)
   - db:  bind mount <project>/db → /var/lib/mysql (or /var/lib/postgresql/data)
   - PortBindings: empty
   - Same labels minus role-specific
5. Inspect leader → get cs0 IP
6. router.Ensure(extraHTTPPorts)
7. router.UpdateConfig(podEndpoints) — Caddyfile points to leader IP
   (No AttachToPod / DetachFromPod calls — router and pods all on cs0)
```

### Bind mount fixes (regression vs 0.2.x)

`buildMounts` in `pod/start.go`:
- Non-DB roles: `<project>/<HostAppDir()>` (= `"html"`) → `<ContainerAppPath()>` (= `/var/www/html`). The current code hardcodes `"/app"` — bug.
- DB role: drop the named volume. Bind mount `<project>/db` → `/var/lib/mysql` (mariadb) or `/var/lib/postgresql/data` (postgres).

`init` scaffold (in `initcmd.Run`): create `<project>/{html,data,db}`. Currently creates only `app/data`.

### Deletes

- `pkg/tainer/network/subnet.go` (AllocateSubnet, FreeSubnet, RouterSubnet)
- `pkg/tainer/network/manager.go` (engine network wrappers no longer used)
- `router.AttachToPod`, `router.DetachFromPod` in `pkg/tainer/router/router.go`
- `LabelSubnetOctet` constant + `Pod.SubnetOctet` field + all reads
- `pod.NetworkName` (no per-pod networks)

### Renames / shape changes

- `Pod.SubnetOctet int` → `Pod.PodID int`
- `tainer.subnet-octet` label → `tainer.pod-id`
- `DerivePort(octet, ports, role)` → `DerivePort(podID, role)` — drops alphabetical sort, uses fixed table
- New roles in `pkg/tainer/pod/labels.go`: `RoleSearch`, `RoleXdebug`, `RoleQueue`, `RoleStorage`, `RoleCustom`

### Display refresh

`tainer start` and `tainer status` output:
```
demo  (pod 3012)
  https://demo.tainer.me
  127.0.0.1:30121   # db
  127.0.0.1:30123   # cache
  ssh demo@ssh.tainer.me
```

The pod ID is the port prefix — no offset arithmetic needed by the user. `pod.FormatStatus` is reworked accordingly.

## Migration

**No live state to migrate.** No tainer 0.9 pod has ever started (cyberstack 0.4.2 didn't have `/networks/*`, so every `pod.Start` errored before creating containers). `slots.json` doesn't exist yet. No labelled containers exist. Migration is a no-op.

**Tainer 0.2.x running pods are independent.** They live in podman, on a separate VM, invisible to tainer 0.9.x. No collision.

**Existing v1 manifests** still parse via the in-memory v1→v2 migration that landed in commit 784ae5b3.

## Dependency order

1. **CyberStack 0.5** lands first. Tainer can't compile against fields cyberstack doesn't accept.
2. **Tainer 0.9.x** — manifest schema + registry + service-offset table + display refresh can land first (no cyberstack dependency). The `pod.Start` orchestration + delete passes wait for 0.5.
3. **End-to-end smoke** runs once both ship and `tainer-images` is republished for the new role layout.

## Testing

### CyberStack 0.5 unit (no VM)

- `internal/httpapi/containers_test.go` — body parsing for new HostConfig fields
- `internal/agent/containerd/spec_test.go` — OCI bundle generation per field (NetworkMode → namespace path, Mounts → bind/volume, etc.)
- `internal/agent/network/share_test.go` — virtio-fs path translation
- `internal/vm/vfkit_launcher_test.go` — argv includes `--device virtio-fs`

### CyberStack 0.5 integration smoke (VM required, build-tagged)

- Shared netns: container A and B with `NetworkMode: container:A` see the same `/proc/self/net/dev`
- Bind mount: file under `$HOME` is visible inside the container at the configured target
- Port binding: container listening on 80 with `80→30000` is reachable via `curl 127.0.0.1:30000` from the host
- Restart policy: `unless-stopped` container relaunched after SIGKILL by the agent

### Tainer 0.9.x unit

- `pkg/tainer/pod/registry_test.go` — `AllocatePodID`/`FreePodID` round-trip, exhaustion at 999, pin reservation
- `pkg/tainer/pod/services_test.go` — `TCPServiceOffset` returns the fixed table values; unknown roles return -1
- `pkg/tainer/pod/ports_test.go` — `DerivePort(podID, role)` produces correct host ports
- `pkg/tainer/manifest/manifest_test.go` — `host_port` round-trip, validation rejects values outside 40000-49000
- `pkg/tainer/pod/start_test.go` — `buildMounts` produces html/data/db bind mounts (no named volume)
- Existing `Pod.SubnetOctet` tests update to `Pod.PodID`

### Tainer 0.9.x integration smoke (live cyberstackd 0.5)

Existing `pkg/tainer/integration/smoke_test.go` updates:
- Init → start → status → stop → destroy round trip
- Asserts: pod ID printed in 4-digit form (`pod 30??`), web container is leader on cs0, app+db share leader's netns (same SandboxKey), bind mounts present in `/var/www/html` and `/var/lib/mysql`, host TCP `30??1` (db) reachable

### End-to-end manual (after tainer-images republished)

- `tainer init wordpress demo && cd demo && tainer start`
- `https://demo.tainer.me` reaches WordPress installer
- `mysql -h 127.0.0.1 -P 30??1 -u tainer -p tainer demo` opens MariaDB shell
- `ssh demo@ssh.tainer.me` opens app shell
- Edit `demo/html/wp-content/themes/foo/style.css` on host → browser hot-reload picks it up (virtio-fs working)
- `tainer destroy demo --clean` → containers gone, pod_id freed, project files survive

### Performance targets

- Container creation latency: <500ms baseline + <50ms for the new fields
- Bind-mount fsync via virtio-fs: ≤3× native disk for small writes (acceptable for dev)
- Pod start (3 sequential creates): <2s for a small pod with locally-cached layers

## Acceptance criteria

CyberStack 0.5 ships when:

- [ ] `NetworkMode: container:<id>` produces a container sharing the target's netns (verified by smoke)
- [ ] Bind mounts under `$HOME` work without manual VM-side mounts
- [ ] Named volumes auto-create on first reference and persist across container recreates
- [ ] PortBindings reachable from the macOS host
- [ ] RestartPolicy `unless-stopped` survives container SIGKILL
- [ ] `cyberstackd -v` reports `0.5.0`
- [ ] All existing 0.4.x integration smokes still pass

Tainer 0.9.x ships when:

- [ ] `tainer init <type> <name>` creates `<project>/{html,data,db}` and a v2 manifest
- [ ] `tainer start` creates a leader on cs0 + N siblings sharing its netns
- [ ] `tainer status` prints `(pod 30??)` and the literal port numbers
- [ ] DB data persists across `tainer stop && tainer start` (bind mount, not volume)
- [ ] HTML edits on host show up in the container immediately (virtio-fs)
- [ ] `tainer destroy` frees the pod ID and the slot is reusable on next create
- [ ] `host_port:` values in 40000-49000 are accepted; values outside the band are rejected
- [ ] All existing tainer unit tests pass; the integration smoke passes against cyberstack 0.5.0

## Open items left for the implementation plan

- Exact wire format for `Mount` and `PortBinding` JSON shapes (mirror Docker's `1.43` schema where reasonable, document deviations)
- Subprocess management for `RestartPolicy` (where in agent-side state machine the watcher hook lives)
- nftables rule cleanup on container delete (avoid leaking DNAT rules)
- virtio-fs read/write performance tuning (cache modes, dax, direct_io flags) — defaults likely fine for 0.5
- `tainer-images` republish — separate workstream; the implementation plan should call out as a dependency for end-to-end testing but not gate code work

---

End of design.
