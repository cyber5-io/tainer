# Linux Native Port, Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `cyberstackd --backend native` runs tainer pods via crun directly on a Linux host (no VM), compile- and unit-verified without hardware; macOS behavior untouched.

**Architecture:** A backend seam in `Daemon.Run` selects `vm` (today's darwin path) or `native` (linux). Native constructs the agent's gRPC services in-process (extracted into `agent.NewStack`) and serves them over an in-memory conn to the existing unchanged `AgentClient`. The network package is parametrized (subnet, uplink, scoped nft) so a real host is never fought; a native userspace PortForwarder binds 127.0.0.1 only. systemd socket activation + a .deb skeleton land the ops story.

**Tech Stack:** Go, gRPC (in-memory conn), netlink/nftables, systemd, nfpm.

**Spec:** `docs/superpowers/specs/2026-10-30-linux-native-port-design.md` (tainer repo; the recon hazards H1–H11 it cites are restated inline below where relevant)

**Tracker:** TAIN-105.

## Global Constraints

- Repos/branches: cyber-stack work on branch `linux-native-phase1` (repo `/Users/lenineto/dev/cyber5-io/cyber-stack`); tainer work on branch `TAIN-105-linux-native` from `main`. **Tasks 11–12 (tainer side) must start from a main that already contains TAIN-106's `pkg/tainer/runtime` proc seams and `make cross`** — if TAIN-106 is unmerged when you get there, branch from its branch and say so in the ledger.
- No Co-Authored-By or AI mentions in commits (both repos).
- gofmt/vet clean; **darwin `go test ./...` green at every commit in both repos** — macOS behavior untouched is the prime directive.
- Linux compile gate from Task 1 onward: `GOOS=linux GOARCH=amd64 go build -tags containers_image_openpgp ./cmd/cyberstackd ./cmd/cyberstack-agent` (and arm64) must stay green once established.
- Rootful phase 1; rootless is out of scope. No .rpm. No live end-to-end (no laptop). VM-mode latent bugs fixed only where the port touches them.
- Anti-goals (test for them, don't just avoid them): no LAN exposure by default (publish to 127.0.0.1 only); never flush/alter shared nftables chains or set global policies; no security downgrades on a real host.
- Linux-only code carries `//go:build linux`; prefer extracting pure functions (nft script builders, allocator seeding, fd-activation logic) so their unit tests run on darwin too.

## Review Focus

- A plain `systemctl restart` under running pods must not reissue a live container's IP to the next-created container (H4) — pinned by TestSeedAllocator in Task 7.
- A host already running Docker (docker0 on 172.17.0.0/16, its own nft/iptables chains) must not conflict: native subnet is 10.113.0.0/16 and all rules live in our own tables (H3) — pinned by the nft-script unit tests in Task 5.
- A host with no default route (airplane mode, exotic VPN) must get an actionable error from uplink detection, never a silent "eth0" guess (H2) — pinned by TestUplinkFromDefaultRoute_NoRoute in Task 5.
- Published pod ports must be unreachable from the LAN: the native forwarder listens on 127.0.0.1 exclusively (H1) — pinned by TestNativeForwarderLoopbackOnly in Task 6.
- A daemon start when another process owns :80/:443 must name the port and owning process and fail the Expose, not pick a random port (spec ruling 15) — pinned by TestExposeConflictError in Task 6.

---

## cyber-stack repo (Tasks 1–10, branch `linux-native-phase1`)

### Task 1: Linux compile baseline — sizing probe + `make cross`

**Files:**
- Create: `internal/vm/sizing/probe_linux.go`
- Create: `internal/vm/sizing/probe_linux_test.go`
- Modify: `Makefile` (new `cross` target, add to .PHONY)

**Interfaces:**
- Produces: `sizing.ProbeHost() (memBytes uint64, cpus int)` on linux (same signature as `probe_darwin.go:15`); Makefile target `cross` that every later task keeps green.

- [ ] **Step 1: Write the failing build** — Run: `GOOS=linux GOARCH=amd64 go build ./cmd/cyberstackd` — Expected: FAIL, undefined `sizing.ProbeHost` (this is the RED; the package has only probe_darwin.go).
- [ ] **Step 2: Implement `probe_linux.go`** — `//go:build linux`; `ProbeHost` via `syscall.Sysinfo` (Totalram × Unit) and `runtime.NumCPU()`. Mirror probe_darwin.go's doc comment and zero-on-error behavior. `probe_linux_test.go` (linux-tagged) asserts mem > 0 and cpus > 0 — it compiles tonight, runs in Task 10's linux runner.
- [ ] **Step 3: Add `cross` to the Makefile** (house style; explicit list, no `./...`):

```make
cross:
	GOOS=darwin GOARCH=arm64 go build ./cmd/cyberstackd ./cmd/cyberstack-pf ./cmd/cs-bench
	GOOS=linux GOARCH=amd64 go build -tags containers_image_openpgp ./cmd/cyberstackd ./cmd/cyberstack-agent
	GOOS=linux GOARCH=arm64 go build -tags containers_image_openpgp ./cmd/cyberstack-agent ./cmd/cyberstackd
```

(cyberstack-pf and cs-bench are darwin tools; the agent is linux-only — `//go:build linux` on its main. The openpgp tag is required the moment cyberstackd links the imagestore (Task 4) and is harmless before; see the project build-tags memory.)

- [ ] **Step 4: Verify** — Run: `make cross` — Expected: exit 0. Run `go test ./... 2>&1 | tail -5` on darwin — Expected: green.
- [ ] **Step 5: Commit** — `git add internal/vm/sizing/ Makefile && git commit -m "build(sizing): linux host probe + make cross gate"`

### Task 2: Backend seam in Daemon.Run + nil-agent fix

**Files:**
- Modify: `internal/daemon/daemon.go` (Config struct ~line 79 area, `Run` at :96-300)
- Modify: `cmd/cyberstackd/main.go` (flag block ~:88-102)
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Produces: `Config.Backend string` with constants `BackendVM = "vm"`, `BackendNative = "native"`, `BackendNone = "none"` (in daemon package); `func DefaultBackend() string` returning `"vm"` on darwin, `"native"` on linux; the contract later tasks rely on: **after backend setup, `d.agent != nil` for vm and native; only `none` runs agentless, and ServerOptions must receive untyped nils then** (today's bug: `daemon.go:269-277` passes a nil `*AgentClient` into interface fields, so `/images/create` panics in `-no-vm` mode instead of 404ing).
- Consumes: nothing; Task 4 fills the native case.

- [ ] **Step 1: Write failing tests** in `backend_test.go` (runs on darwin — pure logic):

```go
func TestDefaultBackendPerOS(t *testing.T)   // darwin→"vm", linux→"native" (table over runtime.GOOS with the exported helper; assert current GOOS value)
func TestServerOptionsNoTypedNil(t *testing.T) // build ServerOptions via the new buildServerOptions(d) helper with d.agent == nil; assert opts.Images == nil (interface nil, not typed nil)
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/daemon/ -run 'TestDefaultBackend|TestServerOptionsNoTypedNil' -v` — Expected: compile error (undefined symbols).
- [ ] **Step 3: Implement.** Add `Backend` to Config (empty → `DefaultBackend()`). In `Run`: wrap today's `if d.cfg.VM != nil` block (daemon.go:111-142) as the `BackendVM` arm of a backend switch; `BackendNative` arm calls `d.setupNative(ctx)` — for now a stub returning `errors.New("native backend: not wired yet (Task 4)")`; `BackendNone` arm is today's no-VM fallthrough. Gate the VM-only pieces on `BackendVM`: the `/opt/tainer/bin/cyberstack-pf` existence check (:152-161), `watchWake`/`syncClockLoop` (:140-141), `pollVMIP`+pf watchdog arms of the NetworkMode switch (:170-256 — the whole switch is VM-only; native gets its own fwd in Task 6, `none` keeps NoopForwarder). Extract `buildServerOptions(d *Daemon, fwd httpapi.PortForwarder, pm *httpapi.PortMap, portRemote func(string, string) string) httpapi.ServerOptions` that assigns `d.agent` to the four interface fields ONLY when `d.agent != nil`.
- [ ] **Step 4: Wire the flag** — `cmd/cyberstackd/main.go`: `flag.StringVar(&backend, "backend", "", "container backend: vm (default on macOS) or native (default on Linux)")`; validate against the constants; on darwin `-backend native` errors out ("native backend requires linux").
- [ ] **Step 5: Verify** — new tests PASS; `go test ./... 2>&1 | tail -5` green on darwin; `make cross` green. The existing daemon tests (daemon_test.go) must pass unmodified — if one breaks, the seam changed behavior: fix the seam, not the test.
- [ ] **Step 6: Commit** — `git commit -m "feat(daemon): backend seam (vm|native|none); fix typed-nil agent in ServerOptions"`

### Task 3: Extract agent.NewStack from the agent main

**Files:**
- Create: `internal/agent/stack.go` (`//go:build linux`)
- Modify: `cmd/cyberstack-agent/main.go` (stages 1–3 + server construction, lines ~60-95, become a NewStack call)
- Test: `internal/agent/stack_test.go` (`//go:build linux` — compiles tonight, runs in Task 10's runner)

**Interfaces:**
- Produces:

```go
type StackConfig struct {
	GraphRoot, RunRoot, ContainersRoot, VolumeRoot, CrunPath string
	ShareRoot, MountTag string // virtio-fs share (VM mode); identity passthrough when HostPathIdentity
	HostPathIdentity bool      // native mode: bind mounts pass through untranslated (Task 8 consumes)
	NoPivot bool               // true = crun --no-pivot (VM mode); native passes false (Task 8 consumes)
	SkipHostNetwork bool       // native: daemon calls network.SetupHostNative itself (Task 5); VM path keeps SetupHost here
}
type Stack struct { /* images *imagestore.Store, rt *containerd.Runtime, ... */ }
func NewStack(cfg StackConfig) (*Stack, error) // stages: network (unless skipped), imagestore.New, containerd.New
func (s *Stack) Register(g *grpc.Server)       // RegisterAgent/Image/ContainerServer with agent.NewServer()/NewImageServer(s.images)/NewContainerServer(s.rt, s.images)
func (s *Stack) Close() error
```

- Consumes: existing `imagestore.New(graphRoot, runRoot)`, `containerd.New(images, containersRoot, crunPath, shareRoot, mountTag, volumeRoot)`, `network.SetupHost()`, `agent.NewServer/NewImageServer/NewContainerServer` exactly as `cmd/cyberstack-agent/main.go:60-95` composes them today. HostPathIdentity and NoPivot are PLUMBED (stored on Stack/passed to containerd.New if its signature grows) but semantics land in Task 8 — tonight VM behavior must be bit-identical (ShareRoot/MountTag defaults preserved, NoPivot=true from the agent main).
- The gRPC serving loop (dial/redial, keepalive, backoff — main.go:95-140) STAYS in the agent main; NewStack is construction only.

- [ ] **Step 1: Write the failing test** (`stack_test.go`, linux-tagged): `TestNewStackConstructs` — NewStack with t.TempDir() roots and `SkipHostNetwork: true`, assert no error, Register onto a fresh `grpc.NewServer()` doesn't panic, Close succeeds. (Imagestore/containerd on tmpdirs must work — the agent does exactly this on a fresh data disk.)
- [ ] **Step 2: RED** — on darwin the test can't run; the RED gate is: `GOOS=linux go vet ./internal/agent/` — Expected: FAIL undefined NewStack. (vet compiles the package including linux-tagged tests' package, but not test files — so ALSO require: `GOOS=linux GOARCH=amd64 go test -c -tags containers_image_openpgp -o /dev/null ./internal/agent/` — Expected: FAIL undefined NewStack. That command is this plan's standing substitute for "watch it fail" on linux-only code; use it in every linux-tagged task.)
- [ ] **Step 3: Implement stack.go; refactor the agent main** to `agent.NewStack(StackConfig{…flags…, NoPivot: true, SkipHostNetwork: false})` + its existing serve loop calling `stack.Register(srv)`.
- [ ] **Step 4: Verify** — `GOOS=linux GOARCH=amd64 go test -c -tags containers_image_openpgp -o /dev/null ./internal/agent/` exit 0; `make cross` green; darwin `go test ./...` green.
- [ ] **Step 5: Commit** — `git commit -m "refactor(agent): extract NewStack — construction reusable in-process"`

### Task 4: Native backend assembly — in-process agent over in-memory conn

**Files:**
- Create: `internal/daemon/backend_native_linux.go` (`//go:build linux`)
- Create: `internal/daemon/backend_native_other.go` (`//go:build !linux` — `setupNative` returns "native backend requires linux")
- Modify: `cmd/cyberstackd/main.go` (native-mode flags: storage roots, crun path)
- Test: `internal/daemon/backend_native_linux_test.go` (linux-tagged)

**Interfaces:**
- Consumes: Task 2's seam (`setupNative(ctx)` hook), Task 3's `agent.NewStack`/`Register`, `NewAgentClient(lis net.Listener, timeout)` from `internal/daemon/client.go:56` — UNCHANGED.
- Produces: a running native backend: `d.agent` is a real `*AgentClient` whose conn terminates in-process. Storage roots (spec decision 9): graph `/var/lib/cyberstack/storage`, run `/run/cyberstack/storage`, containers `/var/lib/cyberstack/containers`, volumes `/var/lib/cyberstack/volumes` — the agent main's existing flag defaults, reused verbatim as the native defaults.

- [ ] **Step 1: Write the failing test** (linux-tagged): `TestNativeInMemoryAgentPing` — build a Stack on tmpdirs (SkipHostNetwork true), serve it over the in-memory listener pair, `NewAgentClient(lis, 5*time.Second)`, call `Ping` — Expected error-free. RED gate: the `go test -c` cross-compile command from Task 3 Step 2.
- [ ] **Step 2: Implement `setupNative`**: construct StackConfig (HostPathIdentity true, NoPivot false, SkipHostNetwork true — network comes in Task 5), `agent.NewStack`, a `grpc.Server` with the same keepalive params the agent main uses, and an in-memory conn pair: reuse the single-conn-listener shape the agent uses for its vsock conn (see `internal/transport` and the agent main's serve loop) over `net.Pipe()` — one end Accept()ed by `NewAgentClient`, the other Serve()d by the grpc.Server. (If net.Pipe trips gRPC's deadline handling, `google.golang.org/grpc/test/bufconn` is the sanctioned fallback — builder's call, note it in the commit.) Set `d.agent`; register a shutdown hook closing stack + server. No reconnect machinery — if the in-process server dies the daemon is dying too.
- [ ] **Step 3: Verify** — test binary compiles (`go test -c` gate); `make cross` green (cyberstackd now links imagestore on linux — the openpgp tag earns its keep; if darwin `go build ./cmd/cyberstackd` breaks because backend_native pulled linux-only imports, the build tags are wrong: fix tags, nothing else); darwin `go test ./...` green.
- [ ] **Step 4: Commit** — `git commit -m "feat(daemon): native backend — in-process agent stack over in-memory gRPC"`

### Task 5: Network nativization — subnet, uplink, scoped nft, resolv.conf

**Files:**
- Modify: `internal/agent/network/host.go` (SetupHost stays VM-exact; new SetupHostNative), `internal/agent/network/container.go` (subnet parametrization, WriteResolvConf), `internal/agent/network/portmap.go` (no-op/guard on native)
- Create: `internal/agent/network/native_linux.go` + `internal/agent/network/nftscript.go` (pure string builders — NO build tag, unit-testable on darwin)
- Test: `internal/agent/network/nftscript_test.go` (darwin-runnable), `internal/agent/network/native_linux_test.go` (linux-tagged)

**Interfaces:**
- Produces:
  - `network.Params{BridgeName, BridgeCIDR, SubnetCIDR, GatewayIP string; NativeMode bool}` with `DefaultVMParams()` (today's constants: cs0, 172.17.0.1/16, 172.17.0.0/16) and `DefaultNativeParams()` (**cs0n, 10.113.0.1/16, 10.113.0.0/16** — off Docker's 172.17 pool, H3; distinct bridge name so a dev box running both VM and native never collides).
  - `network.UplinkFromDefaultRoute() (string, error)` — netlink route lookup for 0.0.0.0/0, ERROR (never "eth0") when absent (H2); used only for outbound masquerade.
  - `network.NativeNFTScript(p Params, uplink string) string` — pure function: all rules in table `ip cyberstack` ONLY (own table, own chains; masquerade scoped `ip saddr <subnet> oifname <uplink>`; forward chain scoped to the bridge with `ct state` accept — **no `policy accept` on any hook chain that could affect non-cyberstack traffic, no flush of any table/chain we didn't create** (H3)).
  - `network.SetupHostNative(p Params) error` — bridge + ip_forward + the native script via `nft -f -`.
  - `network.TeardownNative(p Params) error` — `delete table ip cyberstack` + bridge removal (daemon shutdown politeness; best-effort).
  - Container side: `AllocIP`/`SetupContainer` take Params (today's hardcoded `172.17.0.%d` at container.go:31-33 and gw at :124 come from Params). VM callers pass DefaultVMParams() — byte-identical behavior.
  - `WriteResolvConf` (container.go:131): on native, prefer `/run/systemd/resolve/resolv.conf` (real upstreams) over `/etc/resolv.conf` (often 127.0.0.53 stub, unreachable from a container netns — H9), falling back to `/etc/resolv.conf`, then the current publics.
  - `AddPortMap` (portmap.go:28): native path must NOT install uplink-facing DNAT (H1 — that's LAN exposure). Guard: when Params.NativeMode, AddPortMap is not called (the daemon's forwarder in Task 6 handles publishing); add `RemovePortMaps` no-op guard likewise.
- Consumes: Task 3's StackConfig grows `Net network.Params` passed through containerd.New's network calls — thread it, VM default when zero.

- [ ] **Step 1: Write failing darwin-runnable tests** in `nftscript_test.go`:

```go
func TestNativeNFTScriptScoped(t *testing.T)      // script contains `table ip cyberstack`; does NOT contain `policy accept`, `flush chain ip nat`, `172.17.`; masquerade line contains both `ip saddr 10.113.0.0/16` and `oifname "<uplink>"`
func TestDefaultParams(t *testing.T)              // VM params == today's constants; native params == 10.113.0.0/16, bridge cs0n
func TestUplinkFromDefaultRoute_NoRoute(t *testing.T) // linux-tagged variant in native_linux_test.go; the darwin-runnable part: error message of ErrNoDefaultRoute names the fix ("check your network connection")
```

- [ ] **Step 2: RED** — run them, watch compile failure; implement; GREEN. Then the linux-tagged pieces behind the `go test -c` gate.
- [ ] **Step 3: Rewire** container.go/host.go/portmap.go to Params (mechanical; VM constants preserved via DefaultVMParams). `SetupHost()` keeps its exact current body (VM back-compat) delegating to the parametrized internals.
- [ ] **Step 4: Verify** — darwin `go test ./internal/agent/network/... -v` green (the pure tests), `go test ./...` green, `make cross` green, `go test -c` gate green.
- [ ] **Step 5: Commit** — `git commit -m "feat(network): parametrized subnet/uplink + scoped nft for native hosts"`

### Task 6: Native PortForwarder — loopback userspace proxy + port-conflict errors

**Files:**
- Create: `internal/daemon/nativeforwarder/nativeforwarder.go` (no build tag — pure TCP proxy, testable on darwin)
- Create: `internal/daemon/nativeforwarder/nativeforwarder_test.go`
- Modify: `internal/daemon/backend_native_linux.go` (wire fwd + portRemote)

**Interfaces:**
- Consumes: `httpapi.PortForwarder` interface (`internal/httpapi/containers.go:74`): `Expose(ctx, local, remote string) error`, `Unexpose(ctx, local string) error`. portRemote shape: the hybrid mode's containerIP lookup (`daemon.go:222-233`) — native copies that exact portRemote (agent inspect → `insp.IpAddress + ":" + p`), minus the gRPC tunnel: the proxy dials the container IP directly (bridge-reachable from the host).
- Produces: `nativeforwarder.New() *Forwarder`; Expose("127.0.0.1:80", "10.113.0.5:80") binds **loopback only** and proxies; spec decision 4 — userspace proxy is the phase-1 default (nft OUTPUT DNAT measured later).

- [ ] **Step 1: Write failing tests** (darwin-runnable — it's plain TCP):

```go
func TestExposeProxiesTCP(t *testing.T)           // backend := net.Listen on 127.0.0.1:0 echoing; Expose a 127.0.0.1:0 local to it; dial local, write, read echo
func TestNativeForwarderLoopbackOnly(t *testing.T) // after Expose, the bound address's IP is 127.0.0.1 (assert on the forwarder's reported bound addr); Expose("0.0.0.0:…") or a bare ":80" is rejected with an error naming the loopback-only policy
func TestExposeConflictError(t *testing.T)         // pre-bind a port, Expose it: error is ErrPortBusy-wrapped, names the port, and includes the owner lookup string when resolvable (owner lookup via lsof/ss is best-effort: non-empty on linux, may be empty on darwin tests — assert the port is named, owner only if present)
func TestUnexposeStopsProxy(t *testing.T)
```

- [ ] **Step 2: RED → implement → GREEN.** Owner lookup helper `func portOwner(port string) string`: on linux parse `ss -ltnp` output (exec), on darwin `lsof -iTCP:<port> -sTCP:LISTEN`; best-effort, empty string on any failure. Error copy pattern (spec ruling 15): `port 443 is already in use by <owner> — stop it or see 'tainer doctor'`.
- [ ] **Step 3: Wire into `setupNative`**: `fwd = nativeforwarder.New()`, portRemote = the hybrid-shape containerIP lookup. Reconcile pass: reuse `reconcileForwarders` (daemon.go:327) if its signature permits (it takes the `fwd` interface — check; adapt minimally).
- [ ] **Step 4: Verify** — package tests green on darwin; `go test ./...` green; `make cross` green.
- [ ] **Step 5: Commit** — `git commit -m "feat(daemon): native loopback port forwarder with named-owner conflict errors"`

### Task 7: State reconcile on a real host — allocator seeding + live-state check

**Files:**
- Modify: `internal/agent/network/container.go` (allocator), `internal/agent/containerd/reconcile.go:14-90`, `internal/agent/containerd/restart.go`
- Test: `internal/agent/network/alloc_test.go` (darwin-runnable — pure), `internal/agent/containerd/reconcile_native_test.go` (linux-tagged)

**Interfaces:**
- Produces: `network.SeedAllocator(usedIPs []string)` — marks persisted containers' IPs as taken so a daemon restart under running pods never reissues a live IP (H4; today `nextIP` at container.go:19 restarts at .2 every process start). Allocator becomes a free-list over Params.SubnetCIDR (keep the ≤254 host range semantics). `containerd` calls SeedAllocator during its startup scan with every IP found in persisted container state.
- Produces: reconcile for native (H5): `reconcileOnBoot` (reconcile.go:33) gains a live check — before downgrading Running→Exited, query crun (`crun state <id>`, using the configured crun path and state root): if actually running, KEEP Running, re-arm the reaper for it (the wait goroutines died with the old daemon), and re-register its IP via SeedAllocator. VM mode keeps today's assume-fresh-boot behavior (the VM really did reboot) — switch on StackConfig/params plumbed in Task 3.
- Exit codes always-0 is pre-existing VM behavior: do NOT fix tonight; add it to the bring-up checklist and the Tracker task notes (spec decision 5 says "fix if cheap" — the live-reconcile rework makes it non-cheap; ticket it).

- [ ] **Step 1: Failing darwin tests for the allocator**: `TestSeedAllocator` (seed {".0.2", ".0.4"} → next alloc is .0.3 then .0.5), `TestAllocExhaustion` (unchanged >254 error), `TestSeedAllocatorIdempotent`. RED → implement → GREEN.
- [ ] **Step 2: Linux-tagged reconcile test** `TestReconcileKeepsLiveContainer`: fake crun via a stub script in t.TempDir() that answers `state <id>` with a running JSON; persisted state dir with one Running container; reconcile with native params; assert status stays Running and the ID's IP got seeded. Gate: `go test -c` cross-compile.
- [ ] **Step 3: Verify** — darwin suite green; `make cross`; `go test -c` gate.
- [ ] **Step 4: Commit** — `git commit -m "feat(agent): allocator seeding + live-crun reconcile for native restarts"`

### Task 8: Host-security hardening on the native path

**Files:**
- Modify: `internal/agent/containerd/runtime.go` (:558-565 crun args; :30 shareRoot use), `internal/agent/network/share.go` (identity passthrough), `internal/agent/imagestore/imagestore.go` (SystemContext paths + untar mode)
- Test: `internal/agent/containerd/runtime_native_test.go`, `internal/agent/network/share_test.go` additions (darwin-runnable — TranslateHostPath is pure)

**Interfaces:**
- Consumes: Task 3's `StackConfig.HostPathIdentity` and `.NoPivot` — this task gives them semantics.
- Produces:
  - crun args (runtime.go:565): `--no-pivot` emitted ONLY when NoPivot (VM keeps it; native uses pivot_root — H7).
  - `TranslateHostPath` (share.go): when HostPathIdentity, return the host path unchanged (no /Users gate, no mount-tag prefix — H8). Existing VM translation untouched.
  - imagestore: layer extraction uses chrootarchive (the reexec path — `reexec.Init()` is already called in the agent main; ENSURE cyberstackd's main also calls `reexec.Init()` first thing, since native re-execs cyberstackd itself) when a new `imagestore.Options.ChrootUntar bool` is set; the third_party in-process untar patch stays the VM default (H7). Explicit `types.SystemContext` paths (policy, registries.d, auth) pointed at `/var/lib/cyberstack/` defaults so an existing Podman's `/etc/containers` is never read (H10); signature policy stays insecureAcceptAnything (Docker-default, per spec).
- cyberstackd main change: `reexec.Init()` guard at the very top of `cmd/cyberstackd/main.go` main() — one line + comment copied in spirit from the agent main (:24-33).

- [ ] **Step 1: Failing darwin test**: `TestTranslateHostPathIdentity` — identity mode returns input verbatim for `/home/leni/proj` AND `/Users/x/proj`; VM mode behavior unchanged (existing tests keep passing). RED → implement → GREEN.
- [ ] **Step 2: Linux-tagged test** `TestCrunArgsPivot`: with NoPivot false the arg slice omits `--no-pivot`; with true it matches today's exact slice (runtime.go:565). (Extract the args construction into a pure `crunRunArgs(bundle, id string, noPivot bool) []string` so this test runs on darwin too — prefer that.)
- [ ] **Step 3: imagestore options + SystemContext** — mechanical; verify via `go test -c` gate + darwin suite.
- [ ] **Step 4: Verify** — darwin green, `make cross` green, `go test -c` gates green.
- [ ] **Step 5: Commit** — `git commit -m "feat(agent): pivot_root, chroot untar, identity mounts, scoped image config on native"`

### Task 9: Cgroups under a delegated systemd subtree

**Files:**
- Modify: `internal/agent/containerd/cgroup.go` (cgroup2Root at :11 and its callers)
- Test: `internal/agent/containerd/cgroup_test.go` additions (pure-function parts darwin-runnable)

**Interfaces:**
- Produces: `cgroupBase() string` — VM mode: `/sys/fs/cgroup` (today). Native: the daemon's own delegated subtree, derived from `/proc/self/cgroup` (parse the v2 line `0::<path>` → `/sys/fs/cgroup<path>`), so pod cgroups `tainer-<project>` are created under the service's `Delegate=yes` subtree instead of the root (H6). Extract `func cgroupPathFromProcSelf(line string) (string, error)` pure for darwin-testable parsing. Pod-budget semantics unchanged (one absolute budget per pod).
- Consumes: Params/StackConfig native flag from Task 3/5.

- [ ] **Step 1: Failing darwin test**: `TestCgroupPathFromProcSelf` — `"0::/system.slice/cyberstack.service"` → `/sys/fs/cgroup/system.slice/cyberstack.service`; malformed line → error. RED → implement → GREEN.
- [ ] **Step 2: Rewire callers** of cgroup2Root through cgroupBase with the mode plumbed; VM behavior identical.
- [ ] **Step 3: Verify + commit** — darwin suite, `make cross`, `go test -c` gates. `git commit -m "feat(agent): pod cgroups under the daemon's delegated subtree on native"`

### Task 10: Socket activation, linux test runner, integration-fixture seam

**Files:**
- Modify: `internal/httpapi/server.go` (or wherever `ListenUnix` lives — grep `func ListenUnix`)
- Create: `.github/workflows/ci.yml`
- Modify: the integration fixture (grep `darwin` under `integration/` or `*_integration_test.go` — the recon confirmed only the fixture hard-requires darwin/arm64)
- Test: `internal/httpapi/activation_test.go` (darwin-runnable)

**Interfaces:**
- Produces: `httpapi.ListenMaybeActivated(path string) (net.Listener, error)` — systemd socket activation: when `LISTEN_PID == os.Getpid()` and `LISTEN_FDS >= 1`, adopt fd 3 (`os.NewFile(3, "cyberstack.sock")` → `net.FileListener`); otherwise fall back to `ListenUnix(path)`. No new dependency (no go-systemd). cyberstackd's Run path calls it in place of ListenUnix. Socket perms (0660 root:cyberstack) are the socket UNIT's job (Task 11's files), not Go's.
- Produces: CI — `.github/workflows/ci.yml`: ubuntu-latest, Go from go.mod, `go vet ./...` + `go test -tags containers_image_openpgp ./... 2>&1` + `make cross`. Commit it on the branch; it runs when the branch is pushed (pushing is Leni's/the controller's call — note in ledger, do not push from a task subagent).
- Produces: fixture seam — the integration fixture selects its target daemon via env (`CSTACK_TEST_SOCKET` — if set, run the Docker-API tests against that socket and skip VM boot), so laptop day points the same suite at a native daemon. The darwin/arm64 hard-skip stays the default.
- Produces: the local best-effort unit-test run: cross-compile every linux-tagged package's test binary (`GOOS=linux GOARCH=amd64 go test -c -tags containers_image_openpgp ./internal/...` each to a file) — compile success is tonight's gate. Executing them needs a linux userland; if the local engine's VM is trivially reachable for it, run the pure-Go ones (no root/netlink/nft) there and record results; otherwise record "compiled, not executed" per package in the bring-up checklist. Do not sink the night here.

- [ ] **Step 1: Failing darwin test** `TestListenMaybeActivatedFallsBack` (no LISTEN_FDS → unix listener at tmp path) and `TestListenMaybeActivatedRejectsWrongPID` (LISTEN_PID=1 → fallback, not fd adoption). RED → implement → GREEN.
- [ ] **Step 2: CI workflow + fixture env seam + test-binary cross-compile sweep** (script the sweep into `scripts/crosstest-linux.sh` so laptop day reruns it).
- [ ] **Step 3: Verify + commit** — darwin suite, `make cross`, the sweep script exits 0. `git commit -m "feat(httpapi): systemd socket activation + linux CI and test-runner scaffolding"`

## tainer repo (Tasks 11–12, branch `TAIN-105-linux-native` — AFTER TAIN-106 seams exist on main)

### Task 11: Per-OS socket path, darwin-gated auto-spawn, doctor hints

**Files:**
- Modify: `pkg/tainer/engine/client.go:26-32` (DefaultSocketPath), `pkg/tainer/runtime/runtime.go` (startDaemon gate ~:169, canSpawnDaemon from TAIN-106's proc seams)
- Create: `pkg/tainer/engine/socket_linux.go` / `socket_darwin.go` (or a GOOS switch in one file if <10 lines — follow the dns resolver's existing per-GOOS style)
- Modify: `pkg/tainer/doctor/` (socket check)
- Test: `pkg/tainer/engine/client_test.go` additions

**Interfaces:**
- Consumes: TAIN-106's `canSpawnDaemon()`/proc seams in pkg/tainer/runtime.
- Produces: `DefaultSocketPath()` → darwin: `~/.cyberstack/cyberstack.sock` (unchanged); linux: `/run/cyberstack/cyberstack.sock` (spec decision 10). Auto-spawn (`runtime.Options{AutoStart: true}` path) gates to darwin: on linux, a missing/unanswering socket returns an actionable error — `the tainer engine isn't running — is the tainer package installed? (sudo apt install ./tainer.deb) — and are you in the 'cyberstack' group? (groups | grep cyberstack)` — and doctor gets the matching check (socket exists / connect refused / permission denied → three distinct hints: not installed / not started (socket unit disabled?) / not in group).

- Produces (spec ruling 15, ssh half): the router's `:22` publish finishes its per-OS seam — today's darwin path checks Remote Login (`remoteLoginActive` shape; grep `remoteLoginActive\|:22` under `pkg/tainer/router/`); the linux path replaces the darwin-specific check with a plain bind-probe on `:22` (dial/listen test) and falls back to `:2222` when taken (on Ubuntu openssh-server almost always owns :22). Extract the probe as a pure-ish `func sshPortAvailable(port string) bool` so the fallback decision is unit-testable. Doctor also gains the 80/443 pre-flight: on linux, if another process listens on 80 or 443, report the port and owner (same `ss -ltnp`/`lsof` best-effort lookup pattern as cyber-stack's nativeforwarder) — hard actionable error territory, no fallback ports (they'd break the no-port `https://proj.tainer.me` story).

- [ ] **Step 1: Failing test**: `TestDefaultSocketPathPerOS` asserting the current-GOOS value (darwin asserts the home path; the linux value pinned via the pure helper `socketPathFor(goos, home string) string` so darwin tests cover both). RED → implement → GREEN.
- [ ] **Step 2: Gate auto-spawn + doctor hints**; verify `go test ./...` green, `make cross` green (from TAIN-106).
- [ ] **Step 3: Router :22 probe fallback** — failing test `TestSSHPortFallback` (pre-bind a loopback port in the test, assert the chooser returns the 2222 fallback; free port → returns :22), then the per-OS wiring. Doctor 80/443 check with its hint copy.
- [ ] **Step 4: Commit** — `git commit -m "feat(engine): per-OS socket path, ssh port probe, linux doctor hints (TAIN-105)"`

### Task 12: systemd units, .deb skeleton, make linux-dist, bring-up checklist

**Files:**
- Create: `packaging/linux/cyberstack.socket`, `packaging/linux/cyberstack.service`, `packaging/linux/postinst.sh`, `packaging/linux/nfpm.yaml`
- Modify: `Makefile` (`linux-dist` target)
- Create: `docs/superpowers/specs/2026-10-31-linux-bringup-checklist.md`

**Interfaces:**
- Consumes: cyber-stack `make cross` output (CS_REPO pattern — see how the darwin Makefile pulls cyberstackd from CS_REPO for the .pkg); Task 10's activation contract (fd 3), Task 11's socket path.
- Produces (spec rulings 12/14/15 pinned):
  - `cyberstack.socket`: `ListenStream=/run/cyberstack/cyberstack.sock`, `SocketMode=0660`, `SocketUser=root`, `SocketGroup=cyberstack` — enabled in postinst (`systemctl enable --now cyberstack.socket`).
  - `cyberstack.service`: `ExecStart=/opt/tainer/bin/cyberstackd -backend native`, `Delegate=yes`, `Restart=on-failure`, NO WantedBy multi-user auto-start (socket-activated; pods do NOT resurrect at boot — ruling 12).
  - `postinst.sh`: `groupadd -f cyberstack`, enable socket, print the "add yourself to the group" hint.
  - `nfpm.yaml`: package `tainer`, contents = tainer CLI (`/opt/tainer/bin/tainer` + `/usr/local/bin` symlink or bin dir per the .pkg's layout — mirror it), cyberstackd, both units into `/usr/lib/systemd/system/`, postinst; `depends: [crun, nftables]`; arch from env.
  - `make linux-dist`: cross-build tainer (linux/$(ARCH)), expect `CS_REPO`'s cyberstackd linux build (document `make -C $(CS_REPO) cross` as the prereq, same pattern as pkg-full), run nfpm for amd64 and arm64 into `dist/`.
  - The checklist doc: every assumption tonight could not verify, in test order for laptop day — minimum: socket activation end-to-end; group-permission UX; uplink detection on wifi/ethernet/VPN (NordVPN!); coexistence with a running Docker (nft + subnet); :22/:80/:443 conflicts with openssh/nginx; pod start → page loads; daemon restart under running pods (IPs, forwarders, DNS outage window — ruling 12's accepted semantic); cgroup budgets under Delegate; resolv.conf on systemd-resolved and non-resolved hosts; chrootarchive + pivot_root pulls; every linux test binary from Task 10's sweep marked "compiled, not executed"; exit-codes ticket pointer. The checklist also DOCUMENTS the phase-1 limitations as stated limitations, not bugs: one operator per host (router/caddy/cert state is per-user in $HOME; the group socket governs access, not concurrent use — two users' tainer state would silently fight over the shared router — spec ruling 16), and the daemon-restart outage window (ruling 12).

- [ ] **Step 1: Write units + nfpm.yaml + postinst; add `linux-dist`.**
- [ ] **Step 2: Verify by machine** — `nfpm` (go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest) packages successfully for both arches; inspect: `ar t dist/tainer_*_amd64.deb` lists control.tar + data.tar; `tar tf` of data contains both binaries + both unit files; `systemd-analyze verify` is linux-only — add the two unit files to the checklist for laptop-day verify instead.
- [ ] **Step 3: Write the bring-up checklist** (the deliverable Leni reads on laptop day — plain language, commands included, ordered).
- [ ] **Step 4: Commit** — `git commit -m "build: linux .deb skeleton (systemd socket activation) + bring-up checklist (TAIN-105)"`
