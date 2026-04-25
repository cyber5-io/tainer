# CyberStack 0.2 — VM Bring-up + Agent Handshake Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a real Linux VM under `cyberstackd` and prove the daemon↔agent wire by round-tripping a Ping over vsock. After this milestone, `docker info` against `cyberstackd` reports real `NCPU` and `MemTotal` from inside the running VM, and a private admin endpoint exists for proving the agent connection.

**Architecture:** `cyberstackd` constructs a `vm.VM` (the vfkit-based launcher from 0.1), launches it with a minimal Alpine guest, and connects to the in-VM agent via a vsock-over-Unix-socket bridge (vfkit translates AF_VSOCK ↔ Unix socket on the host side). The agent is the Go binary from 0.1 running as PID 1 inside the guest. Daemon adds a process reaper to know when the VM exits.

**Tech Stack:** Same as 0.1 — Go 1.22+, vfkit binary (already installed at `/opt/tainer/bin/vfkit`), Alpine 3.19 minirootfs, mdlayher/vsock, gRPC over vsock, e2fsprogs (`brew install e2fsprogs`).

---

## Spec Reference

This plan implements the following from `docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`:

- **Architecture → Process topology:** `cyberstackd` actually owns the VM process now (not just a stub).
- **Architecture → Wire protocols:** gRPC over vsock between daemon and agent — exercised end-to-end for the first time.
- **Lifecycle → Engine bring-up:** Daemon spawns the VM on first need (in 0.2 we use eager startup; transparent lazy startup is 0.3).

**Out of scope for this plan** (later milestones):
- Image pull, container runtime, container lifecycle endpoints (0.3+)
- Networks, volumes, port publishing (0.3+)
- DDEV / docker-compose integration tests (0.4+)
- Memory ballooning (post-1.0)
- Full Docker API endpoint surface

---

## Prerequisites the engineer must verify before starting

Run these once at the start of the milestone — they're not part of any single task because they're host setup:

```bash
brew install e2fsprogs                 # mkfs.ext4 for rootfs build
ls -la /opt/tainer/bin/vfkit           # confirm vfkit binary exists
which curl tar dd                      # native macOS, all should be present
```

If `vfkit` is missing, install via tainer 0.2.4 reinstall or grab from https://github.com/crc-org/vfkit/releases.

---

## File Structure

Additions and modifications to `/Users/lenineto/dev/cyber5-io/cyber-stack/`:

```
cyber-stack/
├── guest/
│   ├── kernel/
│   │   ├── vmlinuz-virt          # NEW (gitignored, downloaded by Make)
│   │   └── initramfs-virt        # NEW (gitignored, downloaded by Make)
│   ├── Makefile                  # MODIFIED — add fetch-kernel target
│   └── rootfs-arm64.img          # NEW (gitignored, built artefact)
├── internal/
│   ├── vm/
│   │   ├── vfkit_launcher.go     # MODIFIED — add reaper goroutine
│   │   └── vfkit_launcher_test.go # MODIFIED — reaper tests
│   ├── daemon/
│   │   ├── daemon.go             # MODIFIED — owns a vm.VM + AgentClient
│   │   ├── daemon_test.go        # MODIFIED — VM-aware integration test
│   │   └── client.go             # already exists (Task 9)
│   └── httpapi/
│       ├── ping_agent.go         # NEW — admin endpoint /cyberstack/ping-agent
│       ├── info.go               # MODIFIED — query agent for NCPU/MemTotal
│       ├── server.go             # MODIFIED — register ping_agent route
│       └── handlers_test.go      # MODIFIED — covers new endpoint
├── cmd/
│   └── cyberstackd/
│       └── main.go               # MODIFIED — accept --vfkit, --kernel, --initrd, --rootfs flags
└── tests/
    └── integration/
        └── vm_handshake_test.go  # NEW — full VM bring-up + ping-agent round-trip
```

### Responsibility summary (changes from 0.1)

| File | What's new |
|---|---|
| `internal/vm/vfkit_launcher.go` | Reaper goroutine; `cmd.Wait()` no longer held under mutex; `State()` correctly transitions to Stopped on unexpected exit |
| `internal/daemon/daemon.go` | `Daemon` now owns a `vm.VM` and an `*AgentClient`; `Run()` boots the VM, dials agent, then serves HTTP |
| `internal/httpapi/ping_agent.go` | New admin endpoint `/cyberstack/ping-agent` (NOT Docker API) that proxies Ping to the in-VM agent |
| `internal/httpapi/info.go` | `NCPU` and `MemTotal` populated from agent's `Version` response (which we'll extend in 0.2 to include sysinfo) |
| `cmd/cyberstackd/main.go` | New flags: `--vfkit-binary`, `--kernel`, `--initrd`, `--rootfs`. Sensible defaults pointing at `/opt/tainer/bin/vfkit` and `./guest/...` paths |
| `guest/Makefile` | New `fetch-kernel` target downloads vmlinuz-virt + initramfs-virt; `rootfs` target now actually executable end-to-end |

---

## Task 1: vfkit launcher reaper goroutine

**Files:**
- Modify: `internal/vm/vfkit_launcher.go`
- Modify: `internal/vm/vfkit_launcher_test.go`

Resolves the deferred TODO from 0.1: spawn a reaper goroutine after `cmd.Start()` that calls `cmd.Wait()` and transitions state to Stopped on process exit. Removes the double-Wait risk and prevents `State()` from lying.

- [ ] **Step 1.1: Write failing test for the reaper**

Append to `internal/vm/vfkit_launcher_test.go`:

```go
import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestVFKitLauncher_StateReturnsStoppedAfterReap(t *testing.T) {
	// Use /usr/bin/true as a stand-in for vfkit — it exits immediately,
	// which exercises the reaper transitioning state on natural exit.
	l := &vfkitLauncher{
		bin: "/usr/bin/true",
		spec: Spec{
			MemoryMB:   64,
			CPUs:       1,
			KernelPath: "/dev/null",
			InitrdPath: "/dev/null",
			RootfsPath: "/dev/null",
			KernelCmd:  "",
			SocketDir:  t.TempDir(),
		},
	}

	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait up to 1 second for the reaper to flip state.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if l.State() == StateStopped {
			return // Pass.
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("state did not transition to Stopped after process exit; got %s", l.State())
}

func TestVFKitLauncher_StopAfterAlreadyExitedIsNoError(t *testing.T) {
	l := &vfkitLauncher{
		bin: "/usr/bin/true",
		spec: Spec{
			SocketDir: t.TempDir(),
		},
	}
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for natural exit + reaper.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && l.State() != StateStopped {
		time.Sleep(20 * time.Millisecond)
	}
	if err := l.Stop(context.Background()); err != nil {
		t.Errorf("Stop on already-exited: %v", err)
	}
}

// Sanity: ensure exec.ExitError isn't surfaced in a way that masks reaper logic.
var _ = exec.ExitError{}
var _ = errors.New
```

Run: `go test ./internal/vm/ -run TestVFKitLauncher_StateReturnsStoppedAfterReap`
Expected: FAIL because the reaper doesn't exist yet — state stays at `StateRunning`.

- [ ] **Step 1.2: Add reaper goroutine to `Start`, restructure `Stop`**

Modify `internal/vm/vfkit_launcher.go`. Update the struct, `Start`, and `Stop`:

```go
type vfkitLauncher struct {
	bin  string
	spec Spec

	mu    sync.Mutex
	state State
	cmd   *exec.Cmd
	done  chan struct{} // closed by reaper when cmd has exited
}

func (l *vfkitLauncher) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != "" && l.state != StateStopped {
		return fmt.Errorf("vm already in state %s", l.state)
	}
	l.state = StateStarting
	cmd := exec.CommandContext(ctx, l.bin, l.argv()...)
	if err := cmd.Start(); err != nil {
		l.state = StateStopped
		return fmt.Errorf("vfkit start: %w", err)
	}
	l.cmd = cmd
	l.done = make(chan struct{})
	l.state = StateRunning
	go l.reap()
	return nil
}

// reap waits for the vfkit subprocess to exit and transitions state to
// Stopped. Called once per Start.
func (l *vfkitLauncher) reap() {
	_ = l.cmd.Wait()
	l.mu.Lock()
	l.state = StateStopped
	close(l.done)
	l.mu.Unlock()
}

func (l *vfkitLauncher) Stop(ctx context.Context) error {
	l.mu.Lock()
	if l.state != StateRunning {
		l.mu.Unlock()
		return nil
	}
	l.state = StateStopping
	cmd := l.cmd
	done := l.done
	l.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("vfkit kill: %w", err)
		}
	}

	// Wait for reaper to finish, bounded by ctx.
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

Remove the TODO comment on `State()` since reaper now keeps state honest.

Run: `go test ./internal/vm/ -v`
Expected: all four tests PASS (the two pre-existing argv tests + the two new reaper tests).

- [ ] **Step 1.3: Verify gofmt + vet**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
gofmt -d internal/vm/
go vet ./internal/vm/...
```

Both should produce empty output.

- [ ] **Step 1.4: Commit**

```bash
git add internal/vm/
git commit -m "feat(0.2): vfkit launcher reaper goroutine"
```

---

## Task 2: Daemon owns the VM and AgentClient

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/daemon_test.go`

`Daemon` extends to hold a `vm.VM` and `*AgentClient`. `Run` boots the VM, waits for the vsock socket to appear, dials the agent, then serves HTTP. The 0.1 path (no VM) becomes a special case: if `Config.VM` is nil, skip the VM/agent setup.

- [ ] **Step 2.1: Extend `Config` and `Daemon`**

Modify `internal/daemon/daemon.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cyber5-io/cyber-stack/internal/httpapi"
	"github.com/cyber5-io/cyber-stack/internal/vm"
)

type Config struct {
	SocketPath string

	// VM is optional. If nil, the daemon runs without a backing VM
	// (this is the 0.1 model — only Docker API metadata endpoints
	// work, no container ops). If non-nil, the daemon takes ownership
	// of its lifecycle.
	VM vm.VM

	// VsockSocketPath is the host-side Unix socket through which vfkit
	// proxies vsock traffic. Required if VM is set; ignored otherwise.
	VsockSocketPath string

	// AgentDialTimeout caps how long we wait for the in-VM agent to
	// become reachable after VM boot. Default 30s if zero.
	AgentDialTimeout time.Duration
}

type Daemon struct {
	cfg   Config
	agent *AgentClient
}

func New(cfg Config) *Daemon {
	if cfg.AgentDialTimeout == 0 {
		cfg.AgentDialTimeout = 30 * time.Second
	}
	return &Daemon{cfg: cfg}
}

// Run boots the VM (if configured), connects to the agent (if configured),
// then serves the HTTP API. Blocks until ctx is cancelled or a fatal error.
func (d *Daemon) Run(ctx context.Context) error {
	if d.cfg.VM != nil {
		if err := d.cfg.VM.Start(ctx); err != nil {
			return fmt.Errorf("vm start: %w", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = d.cfg.VM.Stop(stopCtx)
		}()

		// Wait for the vsock socket to appear (vfkit creates it lazily).
		if err := waitForSocket(ctx, d.cfg.VsockSocketPath, d.cfg.AgentDialTimeout); err != nil {
			return fmt.Errorf("vsock socket not ready: %w", err)
		}

		dialCtx, cancel := context.WithTimeout(ctx, d.cfg.AgentDialTimeout)
		defer cancel()
		agent, err := DialAgent(dialCtx, d.cfg.VsockSocketPath)
		if err != nil {
			return fmt.Errorf("dial agent: %w", err)
		}
		d.agent = agent
		defer agent.Close()
	}

	lis, err := httpapi.ListenUnix(d.cfg.SocketPath)
	if err != nil {
		return err
	}

	srv := httpapi.NewServer(httpapi.ServerOptions{Agent: d.agent})

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-serveErr:
		return err
	}
}

// waitForSocket polls until the given socket path exists or the timeout expires.
func waitForSocket(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("timeout waiting for vsock socket")
}
```

Note the change to `httpapi.NewServer`: it now accepts `ServerOptions` so the agent client can be threaded into handlers. We'll update `httpapi` next task.

- [ ] **Step 2.2: Update existing daemon test for new constructor signature**

The 0.1 test (`TestDaemon_ServesPingOverUnixSocket`) constructs `Config{SocketPath: socketPath}` — that should still compile because `VM` defaults to nil. Run the existing test:

```bash
go test ./internal/daemon/ -run TestDaemon_ServesPingOverUnixSocket
```

Expected: PASS unchanged. If it fails on `httpapi.NewServer` signature, that's wired in Task 3.

For now (until Task 3 is done) this won't fully build. That's expected — Tasks 2 and 3 must land together. Continue to Task 3 before running.

- [ ] **Step 2.3: Commit (after Task 3 is also done — see below)**

Tasks 2 and 3 form a logical pair; commit together at the end of Task 3.

---

## Task 3: HTTP API gains agent-aware ping endpoint and live `/info`

**Files:**
- Create: `internal/httpapi/ping_agent.go`
- Modify: `internal/httpapi/info.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/handlers_test.go`

- [ ] **Step 3.1: Add `ServerOptions` and dependency injection**

Modify `internal/httpapi/server.go`:

```go
package httpapi

import (
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cyber5-io/cyber-stack/internal/daemon"
)
```

Wait — there's a circular import risk: `daemon` imports `httpapi`, and `httpapi` would now import `daemon` for `*AgentClient`. Resolve by moving `AgentClient` to a small new package OR by using an interface in `httpapi`. The interface approach is cleaner:

```go
// AgentPinger is the subset of agent client behaviour httpapi needs.
// Daemon's *AgentClient satisfies this implicitly.
type AgentPinger interface {
	Ping(ctx context.Context) error
}

// ServerOptions configures the HTTP server.
type ServerOptions struct {
	Agent AgentPinger // optional
}

func NewServer(opts ServerOptions) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", handlePing)
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/info", handleInfo)
	mux.HandleFunc("/v1.43/_ping", handlePing)
	mux.HandleFunc("/v1.43/version", handleVersion)
	mux.HandleFunc("/v1.43/info", handleInfo)

	// Admin endpoints (NOT Docker API)
	mux.HandleFunc("/cyberstack/ping-agent", makePingAgentHandler(opts.Agent))

	return &http.Server{Handler: mux}
}
```

Update the import to include `context` since `AgentPinger` uses it. Remove the unused `daemon` import attempt.

- [ ] **Step 3.2: Implement the ping-agent handler**

Create `internal/httpapi/ping_agent.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// makePingAgentHandler returns an HTTP handler that proxies a Ping to the
// in-VM agent. Returns 503 if no agent is configured (daemon running in
// 0.1-style standalone mode), 502 if the agent call fails, 200 with
// {"ok": true} on success.
func makePingAgentHandler(agent AgentPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if agent == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      false,
				"reason":  "no agent configured",
			})
			return
		}

		ctx, cancel := contextWithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := agent.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     false,
				"reason": err.Error(),
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// contextWithTimeout is a tiny helper for clarity in the handler.
func contextWithTimeout(parent any, d time.Duration) (any, func()) {
	// stdlib context.WithTimeout — wrapped here only to keep the import
	// list local. Inline if you prefer.
	return contextWithTimeoutImpl(parent, d)
}
```

Actually that helper indirection is unnecessary noise — write it inline:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func makePingAgentHandler(agent AgentPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if agent == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     false,
				"reason": "no agent configured",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := agent.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     false,
				"reason": err.Error(),
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}
```

- [ ] **Step 3.3: Test the ping-agent handler — agent absent, agent ok, agent error**

Append to `internal/httpapi/handlers_test.go`:

```go
type fakeAgent struct {
	err error
}

func (f *fakeAgent) Ping(ctx context.Context) error { return f.err }

func TestPingAgent_NoAgentReturns503(t *testing.T) {
	h := makePingAgentHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/cyberstack/ping-agent", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestPingAgent_AgentOKReturns200(t *testing.T) {
	h := makePingAgentHandler(&fakeAgent{err: nil})
	req := httptest.NewRequest(http.MethodGet, "/cyberstack/ping-agent", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true, got %v", payload)
	}
}

func TestPingAgent_AgentErrorReturns502(t *testing.T) {
	h := makePingAgentHandler(&fakeAgent{err: errors.New("vsock down")})
	req := httptest.NewRequest(http.MethodGet, "/cyberstack/ping-agent", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}
```

You'll need to add `"context"` and `"errors"` to the test file's imports.

Run: `go test ./internal/httpapi/`
Expected: all five tests pass (3 new + 2 from 0.1's Ping/Version/Info — wait, that's 6 total).

- [ ] **Step 3.4: Update `info.go` to query agent for live values**

Extend `internal/httpapi/info.go` to optionally query the agent. Easiest: add a new package-level setter function the daemon can call to inject a "VM stats" function. Or — cleaner — make `handleInfo` a method on a struct that holds the agent. Use the closure pattern same as `makePingAgentHandler`:

Refactor `info.go`:

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

// InfoStats provides live VM metrics for /info responses.
// When nil, /info returns zeroes (the 0.1 behaviour).
type InfoStats interface {
	NCPU(ctx context.Context) (int, error)
	MemTotalBytes(ctx context.Context) (int64, error)
}

func makeInfoHandler(stats InfoStats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ncpu := 0
		mem := int64(0)
		if stats != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if v, err := stats.NCPU(ctx); err == nil {
				ncpu = v
			}
			if v, err := stats.MemTotalBytes(ctx); err == nil {
				mem = v
			}
		}
		payload := infoPayload{
			ID:              "cyberstack",
			Name:            "cyberstack",
			ServerVersion:   CyberStackVersion,
			OperatingSystem: "CyberStack Linux",
			OSType:          "linux",
			Architecture:    runtime.GOARCH,
			NCPU:            ncpu,
			MemTotal:        mem,
			Containers:      0,
			Images:          0,
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Api-Version", APIVersion)
		_ = json.NewEncoder(w).Encode(payload)
	}
}
```

Remove the old `handleInfo` function. Update `server.go`:

```go
type ServerOptions struct {
	Agent AgentPinger
	Info  InfoStats
}

func NewServer(opts ServerOptions) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", handlePing)
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/info", makeInfoHandler(opts.Info))
	mux.HandleFunc("/v1.43/_ping", handlePing)
	mux.HandleFunc("/v1.43/version", handleVersion)
	mux.HandleFunc("/v1.43/info", makeInfoHandler(opts.Info))
	mux.HandleFunc("/cyberstack/ping-agent", makePingAgentHandler(opts.Agent))
	return &http.Server{Handler: mux}
}
```

Update the existing `TestInfo_ReturnsDockerShape` test in `handlers_test.go` to use `makeInfoHandler(nil)` instead of the now-deleted `handleInfo`.

- [ ] **Step 3.5: Run full test suite**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go test ./internal/...
gofmt -d internal/
go vet ./...
```

All must pass / be empty.

- [ ] **Step 3.6: Commit Tasks 2 and 3 together**

```bash
git add internal/daemon/ internal/httpapi/
git commit -m "feat(0.2): daemon owns VM + agent client; ping-agent admin endpoint; live /info"
```

---

## Task 4: Daemon agent client implements InfoStats

**Files:**
- Modify: `internal/daemon/client.go`

The agent's existing `Version` RPC currently returns `agent_version`, `kernel_version`, `os_release`. We extend its protobuf in **Task 5** to also return CPU count and memory bytes. For now, add stub implementations that return zero — the wire-up is what we're proving.

- [ ] **Step 4.1: Add `NCPU` and `MemTotalBytes` methods**

Append to `internal/daemon/client.go`:

```go
// NCPU returns the number of CPUs reported by the in-VM agent. Returns
// 0 if the agent does not yet expose this (CyberStack 0.2 includes the
// proto field but values may be 0 until Task 5 is done).
func (c *AgentClient) NCPU(ctx context.Context) (int, error) {
	resp, err := c.client.Version(ctx, &agentpb.VersionRequest{})
	if err != nil {
		return 0, err
	}
	return int(resp.NumCpus), nil
}

// MemTotalBytes returns the total RAM (in bytes) reported by the in-VM agent.
func (c *AgentClient) MemTotalBytes(ctx context.Context) (int64, error) {
	resp, err := c.client.Version(ctx, &agentpb.VersionRequest{})
	if err != nil {
		return 0, err
	}
	return int64(resp.MemTotalBytes), nil
}
```

These reference fields `NumCpus` and `MemTotalBytes` that we add to the proto in Task 5.

- [ ] **Step 4.2: Don't commit yet — Task 5 must land first to make this compile.**

---

## Task 5: Extend agent proto + agent implementation for sysinfo

**Files:**
- Modify: `internal/proto/agent.proto`
- Regenerate: `internal/proto/gen/agent.pb.go`, `agent_grpc.pb.go`
- Modify: `internal/agent/server.go`
- Modify: `internal/agent/server_test.go`

- [ ] **Step 5.1: Extend `VersionResponse` in proto**

Modify `internal/proto/agent.proto`:

```proto
message VersionResponse {
  string agent_version = 1;
  string kernel_version = 2;
  string os_release = 3;
  uint32 num_cpus = 4;
  uint64 mem_total_bytes = 5;
}
```

Regenerate:

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
make proto
```

Verify the new fields appear in `internal/proto/gen/agent.pb.go`.

- [ ] **Step 5.2: Read sysinfo in the agent**

Modify `internal/agent/server.go`. Add to the `Version` handler:

```go
func (s *Server) Version(ctx context.Context, _ *agentpb.VersionRequest) (*agentpb.VersionResponse, error) {
	return &agentpb.VersionResponse{
		AgentVersion:   AgentVersion,
		KernelVersion:  readUnameRelease(),
		OsRelease:      readOSRelease(),
		NumCpus:        uint32(runtime.NumCPU()),
		MemTotalBytes:  readMemTotalBytes(),
	}, nil
}

// readMemTotalBytes parses /proc/meminfo MemTotal (kB). Returns 0 on error.
func readMemTotalBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
```

Add `"strconv"` and `"strings"` to imports.

- [ ] **Step 5.3: Add a unit test for `readMemTotalBytes` against a fixture**

This is tricky on macOS because `/proc/meminfo` doesn't exist. Make `readMemTotalBytes` testable by extracting the parsing into a separate function:

```go
func parseMemTotal(meminfo string) uint64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

func readMemTotalBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	return parseMemTotal(string(data))
}
```

Append to `internal/agent/server_test.go`:

```go
func TestParseMemTotal_Standard(t *testing.T) {
	input := "MemTotal:        4097852 kB\nMemFree:          123456 kB\n"
	got := parseMemTotal(input)
	want := uint64(4097852) * 1024
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestParseMemTotal_MissingReturnsZero(t *testing.T) {
	if got := parseMemTotal("MemFree:  123 kB\n"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestParseMemTotal_MalformedReturnsZero(t *testing.T) {
	if got := parseMemTotal("MemTotal: not_a_number kB"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}
```

Run: `go test ./internal/agent/ -v`
Expected: PASS on all (including pre-existing Ping test).

- [ ] **Step 5.4: Cross-compile the agent**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
GOOS=linux GOARCH=arm64 go build ./cmd/cyberstack-agent
GOOS=linux GOARCH=amd64 go build ./cmd/cyberstack-agent
```

Both must succeed.

- [ ] **Step 5.5: Commit Tasks 4 and 5 together**

```bash
git add internal/proto/ internal/agent/ internal/daemon/
git commit -m "feat(0.2): extend agent proto with NumCpus + MemTotalBytes; AgentClient impl"
```

---

## Task 6: cyberstackd flags for VM paths

**Files:**
- Modify: `cmd/cyberstackd/main.go`

- [ ] **Step 6.1: Add VM-related flags with sensible defaults**

Modify `cmd/cyberstackd/main.go`:

```go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cyber5-io/cyber-stack/internal/daemon"
	"github.com/cyber5-io/cyber-stack/internal/vm"
)

func main() {
	var (
		socketPath  string
		vfkitBin    string
		kernel      string
		initrd      string
		rootfs      string
		memoryMB    int
		cpus        int
		noVM        bool
		socketDir   string
	)
	flag.StringVar(&socketPath, "socket", "/var/run/cyberstack.sock", "Docker-compat Unix socket path")
	flag.StringVar(&vfkitBin, "vfkit-binary", "/opt/tainer/bin/vfkit", "vfkit executable")
	flag.StringVar(&kernel, "kernel", "guest/kernel/vmlinuz-virt", "Linux kernel image")
	flag.StringVar(&initrd, "initrd", "guest/kernel/initramfs-virt", "initramfs image")
	flag.StringVar(&rootfs, "rootfs", "guest/rootfs-arm64.img", "ext4 rootfs image")
	flag.IntVar(&memoryMB, "memory-mb", 4096, "VM memory in MB")
	flag.IntVar(&cpus, "cpus", 4, "VM vCPU count")
	flag.StringVar(&socketDir, "socket-dir", "", "directory for vfkit sockets (default: temp dir)")
	flag.BoolVar(&noVM, "no-vm", false, "skip VM bring-up (0.1 mode — only metadata endpoints)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := daemon.Config{SocketPath: socketPath}

	if !noVM {
		if socketDir == "" {
			tmp, err := os.MkdirTemp("", "cyberstack-")
			if err != nil {
				log.Fatalf("mktemp: %v", err)
			}
			socketDir = tmp
			defer os.RemoveAll(tmp)
		}

		spec := vm.Spec{
			MemoryMB:   memoryMB,
			CPUs:       cpus,
			KernelPath: absPath(kernel),
			InitrdPath: absPath(initrd),
			RootfsPath: absPath(rootfs),
			KernelCmd:  "console=hvc0 root=/dev/vda rw init=/sbin/cyberstack-init",
			SocketDir:  socketDir,
		}

		launcher := vm.NewVFKitLauncher(vfkitBin, spec)
		cfg.VM = launcher
		cfg.VsockSocketPath = filepath.Join(socketDir, "vsock.sock")
	}

	d := daemon.New(cfg)
	if err := d.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("daemon exited: %v", err)
		os.Exit(1)
	}
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
```

- [ ] **Step 6.2: Build and verify flags**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
make build
./bin/cyberstackd --help 2>&1 | head -20
```

Expected: `--help` prints all flags including `--vfkit-binary`, `--kernel`, `--no-vm`.

- [ ] **Step 6.3: Verify `--no-vm` still works (regression test for 0.1 path)**

```bash
./bin/cyberstackd --socket /tmp/cs-test.sock --no-vm &
DAEMON_PID=$!
sleep 0.5
curl --unix-socket /tmp/cs-test.sock http://x/_ping
kill $DAEMON_PID
rm -f /tmp/cs-test.sock
```

Expected: prints `OK`. The 0.1 mode still works via `--no-vm`.

- [ ] **Step 6.4: Commit**

```bash
git add cmd/cyberstackd/
git commit -m "feat(0.2): cyberstackd flags for VM bring-up + --no-vm fallback"
```

---

## Task 7: Build the actual rootfs and kernel artefacts

**Files:**
- Modify: `guest/Makefile` (add fetch-kernel target)

The `make rootfs` target already exists from 0.1 — now we run it for real.

- [ ] **Step 7.1: Add `fetch-kernel` target to `guest/Makefile`**

Append to `guest/Makefile`:

```makefile
.PHONY: fetch-kernel

KERNEL_URL := https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/aarch64/netboot-3.19.0/vmlinuz-virt
INITRD_URL := https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/aarch64/netboot-3.19.0/initramfs-virt

fetch-kernel: kernel/vmlinuz-virt kernel/initramfs-virt

kernel/vmlinuz-virt:
	curl -fL -o $@ $(KERNEL_URL)

kernel/initramfs-virt:
	curl -fL -o $@ $(INITRD_URL)
```

- [ ] **Step 7.2: Build everything end-to-end**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
brew install e2fsprogs                    # if not already
make build                                # produces cyberstack-agent-linux-arm64
cd guest
make fetch-kernel                         # downloads vmlinuz + initramfs
make rootfs ARCH=arm64                    # produces rootfs-arm64.img
ls -la rootfs-arm64.img kernel/
```

Expected: `rootfs-arm64.img` is 512MB ext4 image; `kernel/vmlinuz-virt` and `kernel/initramfs-virt` exist.

- [ ] **Step 7.3: Commit Makefile change (artefacts are gitignored)**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
git add guest/Makefile
git commit -m "feat(0.2): guest Makefile fetch-kernel target"
```

---

## Task 8: End-to-end VM handshake integration test

**Files:**
- Create: `tests/integration/vm_handshake_test.go`

This is the critical integration test that proves 0.2's core capability.

- [ ] **Step 8.1: Write the integration test**

File: `tests/integration/vm_handshake_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyber5-io/cyber-stack/internal/daemon"
	"github.com/cyber5-io/cyber-stack/internal/vm"
)

func TestVMHandshake_AgentPingsBack(t *testing.T) {
	// Skip if vfkit / rootfs / kernel aren't available.
	vfkitBin := os.Getenv("CYBERSTACK_VFKIT")
	if vfkitBin == "" {
		vfkitBin = "/opt/tainer/bin/vfkit"
	}
	if _, err := os.Stat(vfkitBin); err != nil {
		t.Skipf("vfkit not available at %s; set CYBERSTACK_VFKIT to override", vfkitBin)
	}

	repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	root := string(repoRoot[:len(repoRoot)-1]) // strip trailing newline

	rootfs := filepath.Join(root, "guest", "rootfs-arm64.img")
	kernel := filepath.Join(root, "guest", "kernel", "vmlinuz-virt")
	initrd := filepath.Join(root, "guest", "kernel", "initramfs-virt")
	for _, p := range []string{rootfs, kernel, initrd} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("artefact missing: %s (run `make build && cd guest && make fetch-kernel rootfs`)", p)
		}
	}

	socketDir := t.TempDir()
	apiSocket := filepath.Join(socketDir, "cs.sock")
	vsockSocket := filepath.Join(socketDir, "vsock.sock")

	spec := vm.Spec{
		MemoryMB:   1024,
		CPUs:       2,
		KernelPath: kernel,
		InitrdPath: initrd,
		RootfsPath: rootfs,
		KernelCmd:  "console=hvc0 root=/dev/vda rw init=/sbin/cyberstack-init",
		SocketDir:  socketDir,
	}
	launcher := vm.NewVFKitLauncher(vfkitBin, spec)

	d := daemon.New(daemon.Config{
		SocketPath:       apiSocket,
		VM:               launcher,
		VsockSocketPath:  vsockSocket,
		AgentDialTimeout: 60 * time.Second, // generous for VM cold boot
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for the API socket to appear (signals daemon ready).
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(apiSocket); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	httpc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", apiSocket)
			},
		},
	}

	resp, err := httpc.Get("http://x/cyberstack/ping-agent")
	if err != nil {
		t.Fatalf("ping-agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("ping-agent status = %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["ok"] != true {
		t.Errorf("expected ok=true, got %v", payload)
	}

	// Bonus check: /info should now report non-zero NCPU.
	resp2, err := httpc.Get("http://x/info")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	defer resp2.Body.Close()
	var info map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&info)
	if ncpu, _ := info["NCPU"].(float64); ncpu != 2 {
		t.Errorf("expected NCPU=2, got %v", info["NCPU"])
	}

	cancel()
	<-errCh
}
```

- [ ] **Step 8.2: Run the integration test**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
make integration
```

Expected: VM boots, agent connects, ping-agent returns `{"ok": true}`, `/info` reports `NCPU: 2`. **This is the 0.2 milestone gate.** If anything fails here, debug before proceeding.

If it fails, common issues:
- Kernel cmdline wrong (missing `init=/sbin/cyberstack-init`)
- Init script fails inside VM (check Alpine has `mount` and the agent is at `/usr/bin/cyberstack-agent`)
- vfkit args incorrect (check vfkit's own version with `vfkit --version` and update argv if needed)
- vsock device misconfigured

- [ ] **Step 8.3: Commit**

```bash
git add tests/integration/
git commit -m "test(0.2): VM bring-up + agent handshake integration test"
```

---

## Task 9: Tag CyberStack 0.2.0

- [ ] **Step 9.1: Bump version**

Edit `internal/httpapi/version.go`, change `CyberStackVersion = "0.1.0"` to `"0.2.0"`.

- [ ] **Step 9.2: Commit**

```bash
git add internal/httpapi/version.go
git commit -m "chore: Bump version to 0.2.0"
```

- [ ] **Step 9.3: Tag**

```bash
git tag v0.2.0
```

- [ ] **Step 9.4: Update tainer's spec doc**

Edit `/Users/lenineto/dev/cyber5-io/tainer/docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`. Update the development track status note from `v0.1.0` to `v0.2.0` with a one-line description of what 0.2 added.

---

## What 0.2.0 gives you

After this milestone:

```
$ ./bin/cyberstackd --socket /tmp/cs.sock --memory-mb 1024 --cpus 2 &
# (boots a real Alpine VM under vfkit)

$ docker -H unix:///tmp/cs.sock info
...
 CPUs: 2
 Total Memory: 1.0GiB
 Name: cyberstack
...

$ curl --unix-socket /tmp/cs.sock http://x/cyberstack/ping-agent
{"ok": true}
```

The architecture is now proven end-to-end: host daemon → vfkit subprocess → Linux guest → in-VM Go agent → vsock back to host → through the Docker API surface.

## Roadmap — what follows 0.2.0

- **0.3 — Image pull + storage:** integrate `containers/image` + `containers/storage`; `/images/create` works against `docker.io` and `ghcr.io`.
- **0.4 — Container runtime:** `crun` integration in the agent; `/containers/create`, `/start`, `/stop`, `/exec`, `/logs`.
- **0.5 — Networks + port publishing:** bridge networks, port forwarding host → VM → container.
- **0.6 — Volumes + events.**
- **0.7 — docker-compose + DDEV integration tests.**
- **1.0 — Polish, signed installer, version-locked bundles.**

## Self-review findings

Completed by the author after writing this plan:

- **Spec coverage:** Implements process topology, wire protocols, lifecycle, and the live-`/info` requirement from the design spec. Out-of-scope items explicitly noted.
- **Placeholder scan:** No TBDs or implement-later text. Each task has runnable code.
- **Type consistency:** `AgentPinger` interface in httpapi matches `*AgentClient.Ping` signature. `InfoStats` interface matches `*AgentClient.NCPU` and `MemTotalBytes`. Proto field names (`NumCpus`, `MemTotalBytes`) match Go method names after generation.
- **Known interactions:** Tasks 2+3 must commit together (compile dependency). Tasks 4+5 must commit together (proto field references).
- **One known fragile point:** the integration test in Task 8 takes 60s+ to run because of VM cold boot. That's inherent to the milestone, not a fixable issue. Documented in the test itself with `AgentDialTimeout: 60s`.
