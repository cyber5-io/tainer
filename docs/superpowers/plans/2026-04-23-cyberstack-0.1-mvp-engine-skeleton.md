# CyberStack 0.1 MVP — Engine Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the CyberStack engine skeleton — a host daemon (`cyberstackd`) that can launch a Linux VM on macOS, communicate with an in-VM agent over vsock, and serve a minimal Docker-compatible API on a Unix socket. No container operations yet; this is the architectural foundation that subsequent milestones (image pull, container runtime, full Docker API) build on.

**Architecture:** Two Go binaries — `cyberstackd` on the host and `cyberstack-agent` inside the VM. The host daemon uses Apple Virtualization.framework (via the existing `vfkit` binary as an external process) to boot a minimal Linux guest, communicates with the in-VM agent over vsock using gRPC, and exposes a Docker-compatible HTTP+JSON API on a Unix socket so that `docker version` and `docker info` commands work against it.

**Tech Stack:**
- Go 1.22+
- [vfkit](https://github.com/crc-org/vfkit) — external binary wrapping Virtualization.framework (reused, not rewritten in 0.1)
- gRPC + protobuf — vsock wire protocol between `cyberstackd` and `cyberstack-agent`
- Alpine Linux 3.19 — guest rootfs base
- Standard `net/http` with `net.UnixListener` — Docker API server
- [mdlayher/vsock](https://github.com/mdlayher/vsock) — vsock Go library

---

## Spec Reference

This plan implements the following from `docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`:

- **Architecture → Process topology:** `cyberstackd` and `cyberstack-agent` as separate binaries
- **Architecture → Wire protocols:** HTTP+JSON over Unix socket (host ↔ daemon), gRPC over vsock (daemon ↔ agent)
- **Architecture → Responsibilities (CyberStack, partial):** VM lifecycle; stubs for container/image/network lifecycle to be filled in by subsequent plans
- **Lifecycle → Engine bring-up:** transparent spawn on first request, explicit controls via API
- **Resource management → VM sizing:** tiered default heuristic from spec (4GB on ≤16GB hosts, 50% on 16–48GB, capped at ~24GB on >48GB)

**Out of scope for this plan** (deferred to later CyberStack milestones):
- Image pull, storage, layer cache
- Container create/start/stop/exec/logs
- Network and volume operations
- Port publishing
- Full Docker Engine API surface (only `/_ping`, `/version`, `/info` in 0.1)
- DDEV / docker-compose integration tests

---

## File Structure

Directory layout for the new `cyber-stack` repo at `/Users/lenineto/dev/cyber5-io/cyber-stack`:

```
cyber-stack/
├── go.mod                          # Module root: github.com/cyber5-io/cyber-stack
├── go.sum
├── LICENSE                         # Proprietary / All rights reserved
├── README.md                       # Internal-facing readme
├── Makefile                        # Build, test, lint targets
├── .gitignore                      # Standard Go ignores + build artefacts
├── cmd/
│   ├── cyberstackd/                # Host daemon binary
│   │   └── main.go                 # Wire-up: config → VM launcher → HTTP server
│   └── cyberstack-agent/           # In-VM agent binary
│       └── main.go                 # Wire-up: vsock listener → gRPC server
├── internal/
│   ├── config/
│   │   ├── config.go               # EngineConfig struct, YAML load/save
│   │   ├── config_test.go
│   │   ├── sizing.go               # Tiered default sizing heuristic
│   │   └── sizing_test.go
│   ├── vm/
│   │   ├── vm.go                   # VM lifecycle interface and state
│   │   ├── vm_test.go
│   │   ├── vfkit_launcher.go       # vfkit subprocess management (macOS)
│   │   └── vfkit_launcher_test.go
│   ├── proto/
│   │   ├── agent.proto             # gRPC service definition
│   │   └── gen/                    # protoc-gen-go output (checked in)
│   │       ├── agent.pb.go
│   │       └── agent_grpc.pb.go
│   ├── transport/
│   │   ├── vsock.go                # vsock dialer (host side)
│   │   ├── vsock_test.go
│   │   └── listener.go             # vsock listener (guest side)
│   ├── agent/
│   │   ├── server.go               # gRPC Ping/Version implementation
│   │   └── server_test.go
│   ├── daemon/
│   │   ├── daemon.go               # cyberstackd main orchestrator
│   │   ├── daemon_test.go
│   │   └── client.go               # gRPC client wrapper for agent calls
│   └── httpapi/
│       ├── server.go               # Unix-socket HTTP server setup
│       ├── ping.go                 # GET /_ping
│       ├── version.go              # GET /version (Docker-compat shape)
│       ├── info.go                 # GET /info (Docker-compat shape)
│       └── handlers_test.go
├── guest/
│   ├── Makefile                    # Builds minimal rootfs + bundles kernel
│   ├── alpine-base/
│   │   ├── Containerfile           # Alpine 3.19 + busybox + cyberstack-agent
│   │   └── init.sh                 # First-boot script: start agent on vsock
│   └── kernel/
│       └── README.md               # Kernel acquisition notes (initial: stock vmlinuz)
└── tests/
    └── integration/
        ├── daemon_boot_test.go     # End-to-end: start daemon, boot VM, ping
        └── docker_version_test.go  # docker version --host works
```

### Responsibility summary

| File/package | Responsibility |
|---|---|
| `cmd/cyberstackd` | Wire config + VM + HTTP API into a running daemon |
| `cmd/cyberstack-agent` | Wire vsock listener + gRPC server into a running in-VM process |
| `internal/config` | Engine configuration struct, YAML persistence, sizing heuristics |
| `internal/vm` | VM lifecycle abstraction; vfkit-specific launcher on macOS |
| `internal/proto` | gRPC service definitions between host and guest |
| `internal/transport` | vsock dial (host) and listen (guest) wrappers |
| `internal/agent` | In-VM gRPC handler implementations (Ping, Version for now) |
| `internal/daemon` | Host daemon orchestrator — owns VM, owns agent client |
| `internal/httpapi` | Docker-compat HTTP endpoints on the Unix socket |
| `guest/*` | Minimal Linux rootfs build inputs |
| `tests/integration/*` | End-to-end tests that spin up a real daemon and VM |

---

## Task 1: Repo bootstrap

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/go.mod`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/README.md`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/.gitignore`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/LICENSE`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/Makefile`

- [ ] **Step 1.1: Create the directory and initialise git**

```bash
mkdir -p /Users/lenineto/dev/cyber5-io/cyber-stack
cd /Users/lenineto/dev/cyber5-io/cyber-stack
git init -b main
```

- [ ] **Step 1.2: Create `go.mod`**

```
module github.com/cyber5-io/cyber-stack

go 1.22
```

- [ ] **Step 1.3: Create `.gitignore`**

```
bin/
dist/
*.test
*.out
coverage.*
.DS_Store
/guest/kernel/*.gz
/guest/kernel/*.img
/guest/rootfs/*.img
```

- [ ] **Step 1.4: Create `LICENSE`** — proprietary, all rights reserved, internal to Cyber5 IO.

```
Copyright (c) 2026 Cyber5 IO Ltd.
All rights reserved.

This software is proprietary and confidential. Unauthorized copying,
modification, distribution, or use is strictly prohibited.
```

- [ ] **Step 1.5: Create `README.md`** — internal-facing only.

```markdown
# CyberStack

Container runtime for macOS (Apple Virtualization.framework), Linux, and Windows (WSL2).

**Status:** Private, pre-release. Internal to Cyber5 IO.

## Layout

- `cmd/cyberstackd` — host daemon
- `cmd/cyberstack-agent` — in-VM agent
- `internal/` — engine internals
- `guest/` — Linux guest image build inputs

## Build

    make build           # both binaries
    make test            # unit tests
    make integration     # integration tests (needs macOS + vfkit)

## Relationship to tainer

CyberStack is the engine powering tainer. The tainer project at
/Users/lenineto/dev/cyber5-io/tainer bundles CyberStack binaries in its
installer. See docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md
in the tainer repo for the architecture spec.
```

- [ ] **Step 1.6: Create `Makefile`**

```makefile
.PHONY: build test integration lint clean proto

BIN_DIR := bin

build:
	go build -o $(BIN_DIR)/cyberstackd ./cmd/cyberstackd
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/cyberstack-agent-linux-arm64 ./cmd/cyberstack-agent
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/cyberstack-agent-linux-amd64 ./cmd/cyberstack-agent

test:
	go test ./internal/...

integration:
	go test -tags=integration ./tests/integration/...

lint:
	go vet ./...

proto:
	protoc --go_out=. --go-grpc_out=. internal/proto/agent.proto

clean:
	rm -rf $(BIN_DIR)
```

- [ ] **Step 1.7: Verify build plumbing**

Run: `go mod tidy && make lint`
Expected: no output, no errors (no code yet, just plumbing).

- [ ] **Step 1.8: Commit**

```bash
git add .
git commit -m "chore: Initial repo scaffold"
```

---

## Task 2: Engine config type and YAML persistence

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/config/config.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/config/config_test.go`

- [ ] **Step 2.1: Write failing test `TestEngineConfig_DefaultsToZeroValues`**

File: `internal/config/config_test.go`

```go
package config

import "testing"

func TestEngineConfig_DefaultsToZeroValues(t *testing.T) {
	var c EngineConfig
	if c.MemoryMB != 0 {
		t.Errorf("expected zero memory, got %d", c.MemoryMB)
	}
	if c.CPUs != 0 {
		t.Errorf("expected zero CPUs, got %d", c.CPUs)
	}
}
```

Run: `go test ./internal/config/ -run TestEngineConfig_DefaultsToZeroValues`
Expected: FAIL — `EngineConfig` undefined.

- [ ] **Step 2.2: Create minimal `EngineConfig` struct**

File: `internal/config/config.go`

```go
package config

// EngineConfig is the on-disk configuration for the cyberstackd engine.
// Persisted at ~/.cyberstack/engine.yaml (or $CYBERSTACK_CONFIG if set).
// CyberStack owns its own data dir; tainer's ~/.tainer/ is separate.
type EngineConfig struct {
	MemoryMB int `yaml:"memory_mb"`
	CPUs     int `yaml:"cpus"`
}
```

Run: `go test ./internal/config/ -run TestEngineConfig_DefaultsToZeroValues`
Expected: PASS.

- [ ] **Step 2.3: Add dependency on `gopkg.in/yaml.v3`**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2.4: Write failing test for load/save round trip**

Append to `internal/config/config_test.go`:

```go
func TestEngineConfig_LoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/engine.yaml"

	original := EngineConfig{MemoryMB: 8192, CPUs: 4}
	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.MemoryMB != 8192 || loaded.CPUs != 4 {
		t.Errorf("round trip mismatch: got %+v", loaded)
	}
}

func TestEngineConfig_LoadMissingFileReturnsZero(t *testing.T) {
	loaded, err := Load("/nonexistent/path/engine.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if loaded.MemoryMB != 0 || loaded.CPUs != 0 {
		t.Errorf("expected zero config for missing file, got %+v", loaded)
	}
}
```

Run: `go test ./internal/config/`
Expected: FAIL — `Load` and `Save` undefined.

- [ ] **Step 2.5: Implement `Load` and `Save`**

Append to `internal/config/config.go`:

```go
import (
	"errors"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads an EngineConfig from the given path. A missing file is
// not an error — it returns a zero-value EngineConfig so callers can
// apply defaults without branching on "first run vs returning user".
func Load(path string) (EngineConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return EngineConfig{}, nil
	}
	if err != nil {
		return EngineConfig{}, err
	}
	var c EngineConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return EngineConfig{}, err
	}
	return c, nil
}

// Save writes an EngineConfig to the given path.
func Save(path string, c EngineConfig) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
```

Run: `go test ./internal/config/`
Expected: PASS on all three tests.

- [ ] **Step 2.6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: EngineConfig YAML load/save"
```

---

## Task 3: Tiered VM sizing heuristic

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/config/sizing.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/config/sizing_test.go`

Implements the tiered heuristic from the spec:
- Host ≤ 16GB: 50%, floor 4GB, leave ≥4GB for macOS
- Host 16–48GB: 50%, no cap
- Host > 48GB: 50%, capped at 24GB
- CPU: 50% of cores, floor 2, no cap

- [ ] **Step 3.1: Write failing table-driven test**

File: `internal/config/sizing_test.go`

```go
package config

import "testing"

func TestDefaultMemoryMB(t *testing.T) {
	cases := []struct {
		name     string
		hostGB   int
		expectGB int
	}{
		{"8GB host", 8, 4},
		{"16GB host", 16, 8},
		{"32GB host", 32, 16},
		{"48GB host", 48, 24},
		{"64GB host capped at 24GB", 64, 24},
		{"128GB host capped at 24GB", 128, 24},
		{"4GB host floors at 4GB", 4, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultMemoryMB(tc.hostGB * 1024)
			want := tc.expectGB * 1024
			if got != want {
				t.Errorf("host %dGB: got %dMB, want %dMB", tc.hostGB, got, want)
			}
		})
	}
}

func TestDefaultCPUs(t *testing.T) {
	cases := []struct {
		hostCores int
		expect    int
	}{
		{4, 2},
		{8, 4},
		{11, 5},  // M3 Pro 11-core -> 5 (integer division)
		{16, 8},  // M3 Max 16-core
		{2, 2},   // floor at 2
		{1, 2},   // floor at 2 even below
	}
	for _, tc := range cases {
		got := DefaultCPUs(tc.hostCores)
		if got != tc.expect {
			t.Errorf("host %d cores: got %d, want %d", tc.hostCores, got, tc.expect)
		}
	}
}
```

Run: `go test ./internal/config/`
Expected: FAIL — `DefaultMemoryMB` and `DefaultCPUs` undefined.

- [ ] **Step 3.2: Implement sizing heuristic**

File: `internal/config/sizing.go`

```go
package config

// DefaultMemoryMB returns the recommended VM memory allocation given
// the host's total RAM in MB. Tiered heuristic from the spec:
//
//	host ≤ 16GB:  50%, floored at 4GB
//	host 16–48GB: 50%, no cap
//	host > 48GB:  50%, capped at 24GB
func DefaultMemoryMB(hostMB int) int {
	const (
		floorMB  = 4 * 1024  // 4 GB
		capMB    = 24 * 1024 // 24 GB
		bigHost  = 48 * 1024 // 48 GB
	)
	half := hostMB / 2
	if hostMB > bigHost && half > capMB {
		return capMB
	}
	if half < floorMB {
		return floorMB
	}
	return half
}

// DefaultCPUs returns the recommended vCPU count given the host's
// reported CPU count. 50% with a floor of 2.
func DefaultCPUs(hostCores int) int {
	half := hostCores / 2
	if half < 2 {
		return 2
	}
	return half
}
```

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 3.3: Commit**

```bash
git add internal/config/sizing.go internal/config/sizing_test.go
git commit -m "feat: tiered VM sizing heuristic"
```

---

## Task 4: VM lifecycle interface and vfkit launcher

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/vm/vm.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/vm/vfkit_launcher.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/vm/vfkit_launcher_test.go`

Uses the existing `vfkit` binary as an external subprocess. In-process Virtualization.framework binding is a post-0.1 decision (spec's "open questions for the implementation plan").

- [ ] **Step 4.1: Define the VM interface**

File: `internal/vm/vm.go`

```go
package vm

import "context"

// State reflects what the VM is doing right now.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
)

// Spec describes what VM to create.
type Spec struct {
	MemoryMB   int
	CPUs       int
	KernelPath string
	InitrdPath string
	RootfsPath string
	KernelCmd  string
	// SocketDir is where vfkit will place its control and vsock sockets.
	SocketDir string
}

// VM is the lifecycle interface the daemon programs against.
// Implementations are platform-specific; vfkit is the darwin default.
type VM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	State() State
	// VsockPath returns the Unix socket path proxying vsock traffic to the VM.
	VsockPath() string
}
```

- [ ] **Step 4.2: Write failing test for vfkit launcher argv generation**

File: `internal/vm/vfkit_launcher_test.go`

```go
package vm

import (
	"strings"
	"testing"
)

func TestVFKitLauncher_ArgvIncludesResources(t *testing.T) {
	l := &vfkitLauncher{
		spec: Spec{
			MemoryMB:   4096,
			CPUs:       4,
			KernelPath: "/tmp/k",
			InitrdPath: "/tmp/i",
			RootfsPath: "/tmp/r",
			KernelCmd:  "console=hvc0",
			SocketDir:  "/tmp/sockets",
		},
	}
	argv := l.argv()
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--memory 4096") {
		t.Errorf("missing --memory: %q", joined)
	}
	if !strings.Contains(joined, "--cpus 4") {
		t.Errorf("missing --cpus: %q", joined)
	}
	if !strings.Contains(joined, "--kernel /tmp/k") {
		t.Errorf("missing --kernel: %q", joined)
	}
}
```

Run: `go test ./internal/vm/`
Expected: FAIL — `vfkitLauncher` undefined.

- [ ] **Step 4.3: Implement vfkit launcher (argv generation and subprocess management)**

File: `internal/vm/vfkit_launcher.go`

```go
package vm

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

// NewVFKitLauncher returns a VM that shells out to the vfkit binary.
// vfkitBinaryPath is typically "/opt/tainer/libexec/vfkit" in production.
func NewVFKitLauncher(vfkitBinaryPath string, spec Spec) VM {
	return &vfkitLauncher{
		bin:  vfkitBinaryPath,
		spec: spec,
	}
}

type vfkitLauncher struct {
	bin  string
	spec Spec

	mu    sync.Mutex
	state State
	cmd   *exec.Cmd
}

func (l *vfkitLauncher) argv() []string {
	return []string{
		"--memory", fmt.Sprintf("%d", l.spec.MemoryMB),
		"--cpus", fmt.Sprintf("%d", l.spec.CPUs),
		"--kernel", l.spec.KernelPath,
		"--initrd", l.spec.InitrdPath,
		"--kernel-cmdline", l.spec.KernelCmd,
		"--device", fmt.Sprintf("virtio-blk,path=%s", l.spec.RootfsPath),
		"--device", fmt.Sprintf("virtio-vsock,socketURL=%s,port=1024", l.VsockPath()),
	}
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
	l.state = StateRunning
	return nil
}

func (l *vfkitLauncher) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != StateRunning {
		return nil
	}
	l.state = StateStopping
	if l.cmd != nil && l.cmd.Process != nil {
		if err := l.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("vfkit kill: %w", err)
		}
		_ = l.cmd.Wait()
	}
	l.state = StateStopped
	return nil
}

func (l *vfkitLauncher) State() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == "" {
		return StateStopped
	}
	return l.state
}

func (l *vfkitLauncher) VsockPath() string {
	return filepath.Join(l.spec.SocketDir, "vsock.sock")
}
```

Run: `go test ./internal/vm/`
Expected: PASS on the argv test.

- [ ] **Step 4.4: Commit**

```bash
git add internal/vm/
git commit -m "feat: VM lifecycle interface and vfkit launcher"
```

**Note on integration testing:** the full VM boot is covered in Task 10, not here, because it requires the guest rootfs produced by Task 5.

---

## Task 5: Minimal guest rootfs

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/guest/Makefile`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/guest/alpine-base/Containerfile`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/guest/alpine-base/init.sh`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/guest/kernel/README.md`

Produces a bootable rootfs image containing Alpine 3.19 + busybox + the `cyberstack-agent` binary, plus an init script that starts the agent on vsock port 1024.

Rootfs is built as an ext4 image using a container-based build pipeline (podman or docker) to stay reproducible on macOS hosts without a Linux build machine.

- [ ] **Step 5.1: Create `guest/alpine-base/Containerfile`**

```dockerfile
FROM alpine:3.19

RUN apk add --no-cache \
    busybox-extras \
    openrc \
    e2fsprogs

COPY init.sh /sbin/cyberstack-init
RUN chmod +x /sbin/cyberstack-init

# The cyberstack-agent binary is copied in at rootfs-assembly time
# from the Go build output (see guest/Makefile), not baked in here.

CMD ["/sbin/cyberstack-init"]
```

- [ ] **Step 5.2: Create `guest/alpine-base/init.sh`**

```sh
#!/bin/sh
# CyberStack guest init: mount pseudo-filesystems, start the agent.
set -e

mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev

# Agent binary is placed at /usr/bin/cyberstack-agent by the rootfs build.
exec /usr/bin/cyberstack-agent --vsock-port 1024
```

- [ ] **Step 5.3: Create `guest/Makefile`**

```makefile
.PHONY: rootfs clean

ARCH ?= arm64
ROOTFS_IMG := rootfs-$(ARCH).img
ROOTFS_DIR := .build/rootfs-$(ARCH)
CONTAINER_IMG := cyberstack-guest:$(ARCH)

# Assumes cyberstack-agent has been built for linux/$(ARCH) via top-level `make build`
AGENT_BIN := ../bin/cyberstack-agent-linux-$(ARCH)

rootfs: $(ROOTFS_IMG)

$(ROOTFS_IMG): alpine-base/Containerfile alpine-base/init.sh $(AGENT_BIN)
	# Build the container image
	podman build --platform=linux/$(ARCH) -t $(CONTAINER_IMG) alpine-base
	# Export its filesystem
	rm -rf $(ROOTFS_DIR) && mkdir -p $(ROOTFS_DIR)
	podman create --name cyberstack-export $(CONTAINER_IMG)
	podman export cyberstack-export | tar -x -C $(ROOTFS_DIR)
	podman rm cyberstack-export
	# Inject the agent binary
	cp $(AGENT_BIN) $(ROOTFS_DIR)/usr/bin/cyberstack-agent
	chmod +x $(ROOTFS_DIR)/usr/bin/cyberstack-agent
	# Pack into an ext4 image (512MB)
	dd if=/dev/zero of=$(ROOTFS_IMG) bs=1M count=512
	mkfs.ext4 -d $(ROOTFS_DIR) $(ROOTFS_IMG)

clean:
	rm -rf .build $(ROOTFS_IMG)
```

- [ ] **Step 5.4: Create `guest/kernel/README.md`**

```markdown
# Guest kernel

For 0.1, use a stock vmlinuz + initramfs from Alpine's aarch64 virt kernel.

## Fetch

    curl -LO https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/aarch64/netboot-3.19.0/vmlinuz-virt
    curl -LO https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/aarch64/netboot-3.19.0/initramfs-virt

Place both in this directory. They are .gitignored.

## Kernel cmdline

    "console=hvc0 root=/dev/vda rw init=/sbin/cyberstack-init"

Post-0.1 milestone: custom kernel config for minimal boot time. Not MVP.
```

- [ ] **Step 5.5: Verify rootfs build plumbing (skip if podman unavailable locally)**

Run: `cd guest && make rootfs ARCH=arm64` (requires podman on macOS and a prior `make build` from the repo root).
Expected: produces `guest/rootfs-arm64.img`.

If `podman` is not available locally, defer execution to CI. This step does not block Task 6+.

- [ ] **Step 5.6: Commit**

```bash
git add guest/
git commit -m "feat: minimal Alpine-based guest rootfs build"
```

---

## Task 6: gRPC proto and code generation

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/proto/agent.proto`
- Create (via codegen): `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/proto/gen/agent.pb.go`
- Create (via codegen): `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/proto/gen/agent_grpc.pb.go`

- [ ] **Step 6.1: Install protoc plugins**

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Verify: `protoc-gen-go --version` prints a version.

- [ ] **Step 6.2: Create `internal/proto/agent.proto`**

```proto
syntax = "proto3";

package cyberstack.agent.v1;

option go_package = "github.com/cyber5-io/cyber-stack/internal/proto/gen;agentpb";

// Agent runs inside the VM and responds to host-side requests.
// 0.1 exposes a minimal surface; container / image / network RPCs
// are added by subsequent milestones.
service Agent {
  rpc Ping(PingRequest) returns (PingResponse);
  rpc Version(VersionRequest) returns (VersionResponse);
}

message PingRequest {}
message PingResponse {
  string message = 1; // always "pong"
}

message VersionRequest {}
message VersionResponse {
  string agent_version = 1;
  string kernel_version = 2;
  string os_release = 3;
}
```

- [ ] **Step 6.3: Add gRPC module dependencies**

```bash
go get google.golang.org/grpc
go get google.golang.org/protobuf
```

- [ ] **Step 6.4: Generate Go bindings**

```bash
mkdir -p internal/proto/gen
protoc \
  --go_out=internal/proto/gen --go_opt=paths=source_relative \
  --go-grpc_out=internal/proto/gen --go-grpc_opt=paths=source_relative \
  --proto_path=internal/proto \
  agent.proto
```

Verify: `internal/proto/gen/agent.pb.go` and `internal/proto/gen/agent_grpc.pb.go` exist.

- [ ] **Step 6.5: Update `Makefile` `proto` target to match the actual command**

Replace the `proto:` target in the root `Makefile` with:

```makefile
proto:
	mkdir -p internal/proto/gen
	protoc \
	  --go_out=internal/proto/gen --go_opt=paths=source_relative \
	  --go-grpc_out=internal/proto/gen --go-grpc_opt=paths=source_relative \
	  --proto_path=internal/proto \
	  agent.proto
```

Run: `make proto`
Expected: regenerates both `.pb.go` files with no diff if already current.

- [ ] **Step 6.6: Commit**

```bash
git add internal/proto/ Makefile go.mod go.sum
git commit -m "feat: gRPC agent service proto and generated bindings"
```

---

## Task 7: vsock transport

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/transport/vsock.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/transport/listener.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/transport/vsock_test.go`

The host side dials into vfkit's vsock Unix-socket proxy; the guest side uses the real AF_VSOCK Linux socket.

- [ ] **Step 7.1: Add the vsock dependency**

```bash
go get github.com/mdlayher/vsock
```

- [ ] **Step 7.2: Write failing test for host-side `VsockDial` error path**

File: `internal/transport/vsock_test.go`

```go
package transport

import (
	"context"
	"testing"
	"time"
)

func TestVsockDial_UnreachableSocketReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := VsockDial(ctx, "/nonexistent/vsock.sock")
	if err == nil {
		t.Fatal("expected error for unreachable socket, got nil")
	}
}
```

Run: `go test ./internal/transport/`
Expected: FAIL — `VsockDial` undefined.

- [ ] **Step 7.3: Implement host-side `VsockDial`**

File: `internal/transport/vsock.go`

```go
package transport

import (
	"context"
	"net"
)

// VsockDial connects to the host-side Unix socket that vfkit exposes for
// its virtio-vsock device. The in-VM peer speaks real AF_VSOCK; on the
// host side vfkit forwards those streams over a Unix socket we dial here.
func VsockDial(ctx context.Context, socketPath string) (net.Conn, error) {
	d := net.Dialer{}
	return d.DialContext(ctx, "unix", socketPath)
}
```

Run: `go test ./internal/transport/`
Expected: PASS.

- [ ] **Step 7.4: Implement guest-side vsock listener (Linux only)**

File: `internal/transport/listener.go`

```go
//go:build linux

package transport

import (
	"net"

	"github.com/mdlayher/vsock"
)

// VsockListen opens an AF_VSOCK server socket on the given port.
// Only compiles on Linux; the host daemon never calls this.
func VsockListen(port uint32) (net.Listener, error) {
	return vsock.Listen(port, nil)
}
```

- [ ] **Step 7.5: Verify Linux cross-compile**

Run: `GOOS=linux GOARCH=arm64 go build ./internal/transport/`
Expected: exit 0, no errors.

- [ ] **Step 7.6: Commit**

```bash
git add internal/transport/ go.mod go.sum
git commit -m "feat: vsock transport (host dial + guest listen)"
```

---

## Task 8: In-VM agent — Ping and Version handlers

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/agent/server.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/agent/server_test.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/cmd/cyberstack-agent/main.go`

- [ ] **Step 8.1: Write failing test for the Ping handler**

File: `internal/agent/server_test.go`

```go
package agent

import (
	"context"
	"testing"

	agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
)

func TestServer_PingReturnsPong(t *testing.T) {
	s := NewServer()
	resp, err := s.Ping(context.Background(), &agentpb.PingRequest{})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if resp.Message != "pong" {
		t.Errorf("got %q, want pong", resp.Message)
	}
}
```

Run: `go test ./internal/agent/`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 8.2: Implement the agent gRPC server**

File: `internal/agent/server.go`

```go
package agent

import (
	"context"
	"os"
	"runtime"

	agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
)

// AgentVersion is stamped by the build at link time (see Makefile).
// Default is "dev" for developer builds.
var AgentVersion = "dev"

// Server implements agentpb.AgentServer.
type Server struct {
	agentpb.UnimplementedAgentServer
}

// NewServer returns a fresh agent Server.
func NewServer() *Server {
	return &Server{}
}

func (s *Server) Ping(ctx context.Context, _ *agentpb.PingRequest) (*agentpb.PingResponse, error) {
	return &agentpb.PingResponse{Message: "pong"}, nil
}

func (s *Server) Version(ctx context.Context, _ *agentpb.VersionRequest) (*agentpb.VersionResponse, error) {
	return &agentpb.VersionResponse{
		AgentVersion:  AgentVersion,
		KernelVersion: readUnameRelease(),
		OsRelease:     readOSRelease(),
	}, nil
}

func readUnameRelease() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return runtime.GOOS + "-unknown"
	}
	return trimNewline(string(data))
}

func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	return trimNewline(string(data))
}

func trimNewline(s string) string {
	if n := len(s); n > 0 && s[n-1] == '\n' {
		return s[:n-1]
	}
	return s
}
```

Run: `go test ./internal/agent/`
Expected: PASS on `TestServer_PingReturnsPong`.

- [ ] **Step 8.3: Write the agent main binary**

File: `cmd/cyberstack-agent/main.go`

```go
package main

import (
	"flag"
	"log"
	"os"

	"google.golang.org/grpc"

	"github.com/cyber5-io/cyber-stack/internal/agent"
	agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
	"github.com/cyber5-io/cyber-stack/internal/transport"
)

func main() {
	var port uint
	flag.UintVar(&port, "vsock-port", 1024, "vsock port to listen on")
	flag.Parse()

	lis, err := transport.VsockListen(uint32(port))
	if err != nil {
		log.Printf("vsock listen failed: %v", err)
		os.Exit(1)
	}
	log.Printf("cyberstack-agent listening on vsock port %d", port)

	srv := grpc.NewServer()
	agentpb.RegisterAgentServer(srv, agent.NewServer())

	if err := srv.Serve(lis); err != nil {
		log.Printf("grpc serve: %v", err)
		os.Exit(1)
	}
}
```

Run: `GOOS=linux GOARCH=arm64 go build ./cmd/cyberstack-agent`
Expected: exit 0.

- [ ] **Step 8.4: Commit**

```bash
git add internal/agent/ cmd/cyberstack-agent/
git commit -m "feat: in-VM agent with Ping/Version gRPC handlers"
```

---

## Task 9: Daemon side — gRPC client and HTTP Unix socket server

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/daemon/client.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/httpapi/server.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/httpapi/ping.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/httpapi/version.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/httpapi/info.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/httpapi/handlers_test.go`

Implements `/_ping`, `/version`, and `/info` — the Docker Engine API subset needed for `docker version --host` and `docker info --host` to work. The full Docker API lives in a later plan; these three endpoints are what the Docker CLI calls before anything else and getting them right proves the wire format.

- [ ] **Step 9.1: Write `AgentClient` — a thin wrapper around the generated gRPC client**

File: `internal/daemon/client.go`

```go
package daemon

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
	"github.com/cyber5-io/cyber-stack/internal/transport"
)

// AgentClient is the daemon's handle to the in-VM agent.
type AgentClient struct {
	conn   *grpc.ClientConn
	client agentpb.AgentClient
}

// DialAgent opens a gRPC connection over the given vsock Unix socket path.
func DialAgent(ctx context.Context, vsockSocketPath string) (*AgentClient, error) {
	conn, err := grpc.DialContext(ctx, vsockSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return transport.VsockDial(ctx, vsockSocketPath)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial vsock: %w", err)
	}
	return &AgentClient{
		conn:   conn,
		client: agentpb.NewAgentClient(conn),
	}, nil
}

func (c *AgentClient) Ping(ctx context.Context) error {
	_, err := c.client.Ping(ctx, &agentpb.PingRequest{})
	return err
}

func (c *AgentClient) Version(ctx context.Context) (*agentpb.VersionResponse, error) {
	return c.client.Version(ctx, &agentpb.VersionRequest{})
}

func (c *AgentClient) Close() error {
	return c.conn.Close()
}
```

- [ ] **Step 9.2: Write failing test for `/_ping` handler**

File: `internal/httpapi/handlers_test.go`

```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPing_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_ping", nil)
	rec := httptest.NewRecorder()

	handlePing(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "OK" {
		t.Errorf("body = %q, want %q", got, "OK")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("content-type = %q", got)
	}
}
```

Run: `go test ./internal/httpapi/`
Expected: FAIL — `handlePing` undefined.

- [ ] **Step 9.3: Implement `/_ping`**

File: `internal/httpapi/ping.go`

```go
package httpapi

import "net/http"

// handlePing answers the Docker Engine `/_ping` probe. It's deliberately
// trivial — the Docker CLI uses it to check server availability before
// anything else.
func handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Api-Version", APIVersion)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
```

- [ ] **Step 9.4: Write failing test for `/version`**

Append to `internal/httpapi/handlers_test.go`:

```go
import (
	"encoding/json"
)

func TestVersion_ReturnsDockerShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()

	handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for _, required := range []string{"Version", "ApiVersion", "Os", "Arch"} {
		if _, ok := payload[required]; !ok {
			t.Errorf("missing field %q in %v", required, payload)
		}
	}
}
```

Run: `go test ./internal/httpapi/`
Expected: FAIL — `handleVersion` undefined.

- [ ] **Step 9.5: Implement `/version`**

File: `internal/httpapi/version.go`

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// APIVersion is the Docker Engine API version we claim to implement.
// 1.43 is the baseline we target; more capabilities are added over time.
const APIVersion = "1.43"

// CyberStackVersion is stamped at link time.
var CyberStackVersion = "0.1.0-dev"

// versionPayload mirrors the Docker /version response shape. Fields are
// the ones the docker CLI prints; additional fields may be added later.
type versionPayload struct {
	Version       string `json:"Version"`
	ApiVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	GitCommit     string `json:"GitCommit"`
	GoVersion     string `json:"GoVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
	KernelVersion string `json:"KernelVersion"`
	BuildTime     string `json:"BuildTime"`
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	payload := versionPayload{
		Version:       CyberStackVersion,
		ApiVersion:    APIVersion,
		MinAPIVersion: "1.24",
		GitCommit:     "unknown",
		GoVersion:     runtime.Version(),
		Os:            "linux", // the engine serves a Linux-native API regardless of host
		Arch:          runtime.GOARCH,
		KernelVersion: "cyberstack-guest",
		BuildTime:     "unknown",
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", APIVersion)
	_ = json.NewEncoder(w).Encode(payload)
}
```

Run: `go test ./internal/httpapi/`
Expected: PASS on both tests.

- [ ] **Step 9.6: Write failing test for `/info`**

Append to `handlers_test.go`:

```go
func TestInfo_ReturnsDockerShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rec := httptest.NewRecorder()

	handleInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for _, required := range []string{"ID", "Name", "ServerVersion", "OperatingSystem", "Architecture"} {
		if _, ok := payload[required]; !ok {
			t.Errorf("missing field %q in %v", required, payload)
		}
	}
}
```

- [ ] **Step 9.7: Implement `/info`**

File: `internal/httpapi/info.go`

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"runtime"
)

type infoPayload struct {
	ID              string `json:"ID"`
	Name            string `json:"Name"`
	ServerVersion   string `json:"ServerVersion"`
	OperatingSystem string `json:"OperatingSystem"`
	OSType          string `json:"OSType"`
	Architecture    string `json:"Architecture"`
	NCPU            int    `json:"NCPU"`
	MemTotal        int64  `json:"MemTotal"`
	Containers      int    `json:"Containers"`
	Images          int    `json:"Images"`
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	payload := infoPayload{
		ID:              "cyberstack",
		Name:            "cyberstack",
		ServerVersion:   CyberStackVersion,
		OperatingSystem: "CyberStack Linux",
		OSType:          "linux",
		Architecture:    runtime.GOARCH,
		NCPU:            0, // populated by daemon when VM is up; 0 pre-boot is correct
		MemTotal:        0,
		Containers:      0,
		Images:          0,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", APIVersion)
	_ = json.NewEncoder(w).Encode(payload)
}
```

Run: `go test ./internal/httpapi/`
Expected: PASS on all three endpoint tests.

- [ ] **Step 9.8: Implement the HTTP server constructor**

File: `internal/httpapi/server.go`

```go
package httpapi

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// NewServer returns an HTTP server wired up with the 0.1 endpoints.
// socketPath is typically "/var/run/cyberstack.sock" or a per-user path.
func NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", handlePing)
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/info", handleInfo)
	// Also under /v{N}/ prefix — Docker CLI negotiates a version and then prefixes.
	mux.HandleFunc("/v1.43/_ping", handlePing)
	mux.HandleFunc("/v1.43/version", handleVersion)
	mux.HandleFunc("/v1.43/info", handleInfo)
	return &http.Server{Handler: mux}
}

// ListenUnix creates a Unix socket listener at the given path, removing
// any stale socket file first. Parent directory must already exist.
func ListenUnix(path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lis, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = lis.Close()
		return nil, err
	}
	return lis, nil
}
```

- [ ] **Step 9.9: Commit**

```bash
git add internal/daemon/ internal/httpapi/
git commit -m "feat: Docker-compat /_ping /version /info + gRPC agent client"
```

---

## Task 10: cyberstackd wire-up

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/cmd/cyberstackd/main.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/daemon/daemon.go`
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/internal/daemon/daemon_test.go`

- [ ] **Step 10.1: Write failing test for `Daemon.Run` lifecycle (without VM)**

File: `internal/daemon/daemon_test.go`

```go
package daemon

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDaemon_ServesPingOverUnixSocket brings up a daemon with no VM
// (nil VM is valid for 0.1 — VM is spawned on first container op,
// which doesn't exist yet) and checks that /_ping responds.
func TestDaemon_ServesPingOverUnixSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "cs.sock")

	d := New(Config{SocketPath: socketPath})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for the socket to appear (up to 2s).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	httpc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	resp, err := httpc.Get("http://unix/_ping")
	if err != nil {
		t.Fatalf("GET _ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := <-errCh; err != nil && err != context.Canceled {
		t.Errorf("daemon exited with: %v", err)
	}
}
```

Run: `go test ./internal/daemon/`
Expected: FAIL — `New`, `Config`, `Run` undefined.

- [ ] **Step 10.2: Implement `Daemon`**

File: `internal/daemon/daemon.go`

```go
package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/cyber5-io/cyber-stack/internal/httpapi"
)

// Config holds runtime parameters for the daemon.
type Config struct {
	// SocketPath is where the Docker-compat Unix socket is served.
	SocketPath string
}

// Daemon is the cyberstackd orchestrator. In 0.1 it only runs the
// HTTP Unix socket server; VM and agent wire-up arrive in later plans.
type Daemon struct {
	cfg Config
}

func New(cfg Config) *Daemon {
	return &Daemon{cfg: cfg}
}

// Run blocks until ctx is cancelled or the HTTP server exits with an error.
func (d *Daemon) Run(ctx context.Context) error {
	lis, err := httpapi.ListenUnix(d.cfg.SocketPath)
	if err != nil {
		return err
	}

	srv := httpapi.NewServer()

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
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-serveErr:
		return err
	}
}
```

Run: `go test ./internal/daemon/`
Expected: PASS.

- [ ] **Step 10.3: Write the `cyberstackd` main binary**

File: `cmd/cyberstackd/main.go`

```go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyber5-io/cyber-stack/internal/daemon"
)

func main() {
	var socketPath string
	flag.StringVar(&socketPath, "socket", "/var/run/cyberstack.sock", "Docker-compat Unix socket path")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	d := daemon.New(daemon.Config{SocketPath: socketPath})
	if err := d.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("daemon exited: %v", err)
		os.Exit(1)
	}
}
```

Run: `make build`
Expected: `bin/cyberstackd` and `bin/cyberstack-agent-linux-*` produced without errors.

- [ ] **Step 10.4: Commit**

```bash
git add cmd/cyberstackd/ internal/daemon/
git commit -m "feat: cyberstackd daemon wire-up with /_ping /version /info"
```

---

## Task 11: End-to-end integration test — `docker version` against cyberstackd

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/cyber-stack/tests/integration/docker_version_test.go`

This task validates the wire format by running the real `docker` CLI against a running `cyberstackd`. It's guarded by a build tag so unit-test runs stay fast.

**Prerequisite:** `docker` CLI available on `PATH`. No Docker daemon needed — the CLI only needs to talk to our socket.

- [ ] **Step 11.1: Write the integration test**

File: `tests/integration/docker_version_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/cyber-stack/internal/daemon"
)

func TestDockerVersion_AgainstCyberStackd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not on PATH")
	}

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "cs.sock")

	d := daemon.New(daemon.Config{SocketPath: socketPath})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for socket.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := exec.Command("test", "-S", socketPath).Output(); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cmd := exec.Command("docker", "-H", "unix://"+socketPath, "version", "--format", "{{.Server.Version}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker version failed: %v\noutput: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got == "" {
		t.Errorf("empty version output")
	}
	t.Logf("docker reported server version: %s", got)
}
```

- [ ] **Step 11.2: Run the integration test**

```bash
make integration
```

Expected: PASS on macOS with `docker` CLI installed. Output prints the CyberStack server version.

- [ ] **Step 11.3: Commit**

```bash
git add tests/integration/
git commit -m "test: docker version integration against cyberstackd"
```

---

## Task 12: Tag 0.1.0

- [ ] **Step 12.1: Stamp version in the binary**

Edit `internal/httpapi/version.go`, change `CyberStackVersion = "0.1.0-dev"` to `"0.1.0"`.

- [ ] **Step 12.2: Commit version bump**

```bash
git add internal/httpapi/version.go
git commit -m "chore: Bump version to 0.1.0"
```

- [ ] **Step 12.3: Tag and push**

```bash
git tag v0.1.0
# Push happens only after the user has pushed the initial `main` to GitHub
# and confirmed remote setup.
```

- [ ] **Step 12.4: Open a short release notes PR / ADR in the tainer repo cross-linking**

Append a note to `/Users/lenineto/dev/cyber5-io/tainer/docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md` under the "Development track" section noting that CyberStack 0.1.0 is live. (A minor doc edit — does not block anything else.)

---

## What 0.1.0 gives you

Running `cyberstackd` on macOS and pointing `docker` at its socket should produce:

```
$ docker -H unix:///var/run/cyberstack.sock version
Client: Docker Engine ...
 Version: ...
Server: CyberStack
 Version: 0.1.0
 API version: 1.43 (minimum version 1.24)
 OS/Arch: linux/arm64
 ...
```

No containers, no images, no networks. But the wire format is correct, the daemon lifecycle works, the socket is at the right place, and the architecture is proven.

## Roadmap — what follows 0.1.0

Each of these is its own plan (not this one):

- **CyberStack 0.2 — VM boot + agent handshake.** Daemon actually boots the VM on first Docker request; `/info` reports real NCPU / MemTotal from the agent; Ping makes the round trip host → daemon → vsock → agent → back.
- **CyberStack 0.3 — Image pull and storage.** Integrate `containers/image` + `containers/storage` in the agent; `/images/create` (pull) works; `/images/json` (list) works.
- **CyberStack 0.4 — Container runtime.** `crun` integration in the agent; `/containers/create`, `/containers/{id}/start`, `/containers/{id}/stop`, `/containers/{id}/logs`.
- **CyberStack 0.5 — Networks and port publishing.** Bridge network, port forwards from host into VM into container.
- **CyberStack 0.6 — Volumes, exec, events.** Remaining core Docker API surface.
- **CyberStack 0.7 — docker-compose and DDEV integration tests.**
- **CyberStack 1.0 — Polish, installer, bundled docker/docker-compose binaries.** Ships alongside tainer 1.0.0.
- **Tainer 1.0.0 rebuild plan.** Written after CyberStack is at 0.5 or so, because tainer's engine calls need a stable API target.

## Self-review findings

Completed by the author after writing this plan:

- **Spec coverage:** Plan implements spec sections: Architecture (process topology, wire protocols partial), Lifecycle (engine bring-up skeleton), Resource management (VM sizing heuristic). Spec sections deferred to later plans: full Docker API, container runtime, networking UX (tainer side), packaging / installer. All explicitly flagged in "Out of scope for this plan."
- **Placeholder scan:** No TBDs, TODOs, or "implement later" text in tasks. The "Post-0.1 roadmap" section is deliberately high-level because each item is its own future plan.
- **Type consistency:** `EngineConfig`, `AgentClient`, `Server`, `Daemon`, `Config` used consistently across tasks. gRPC message names (`PingRequest`, `VersionResponse`) match the proto.
- **One known simplification:** `grpc.DialContext` with a context dialer is used, but 0.1 doesn't actually call gRPC over the dialed connection yet — the agent is only wired up in 0.2 when the daemon boots the VM. That's deliberate scope-control, not an oversight.
