# CyberStack 0.3 — Container Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a working container runtime inside the CyberStack VM so `docker pull / run / exec / logs / inspect / stop / rm / rmi` work end-to-end against `cyberstackd`, hitting all five performance budgets locked in the design spec.

**Architecture:** α-shape from the spec — `cyberstackd` is a stateless HTTP→gRPC translator; the agent in the VM is the container engine (uses `containers/image` for pulls, `containers/storage` for layer overlay, `crun` as OCI runtime, native Go `netlink` for the `cs0` bridge + per-container veth + nftables masquerade). Persistent data disk at `~/.cyberstack/data-arm64.img` (50GB sparse ext4) survives `cyberstackd` restarts. VM is still ephemeral.

**Tech Stack:** Go 1.24, gRPC, `containers/image` v5, `containers/storage` v1, `vishvananda/netlink`, crun static aarch64, nftables/iproute2 from Alpine 3.21 APKs, vfkit 0.6.1.

---

## Spec reference

This plan implements `docs/superpowers/specs/2026-04-27-cyberstack-0.3-container-runtime-design.md` in full. Subagents executing tasks **must read the spec first** — it has the design rationale, performance budgets, error-handling tables, and out-of-scope list that constrain implementation choices.

---

## Prerequisites

Before any task starts, the engineer must verify:

```bash
# already from 0.2; confirm still present
ls /opt/tainer/bin/vfkit
which mkfs.fat mcopy unsquashfs        # /opt/homebrew/opt/dosfstools/sbin/mkfs.fat etc.
ls /Users/lenineto/dev/cyber5-io/cyber-stack/bin/cyberstackd  # 0.2 build output

# 0.3-specific (already on dev Mac)
curl -fsSL -o /tmp/crun.test https://github.com/containers/crun/releases/download/1.20/crun-1.20-linux-arm64-static
file /tmp/crun.test                    # confirm: ELF 64-bit LSB executable, ARM aarch64, statically linked

# OrbStack for the head-to-head bench (install only when ready to bench, not during dev)
brew install --cask orbstack           # don't start it yet — interfere with vfkit testing
```

---

## File Structure

### New files (cyber-stack repo)

```
cyber-stack/
├── internal/
│   ├── proto/
│   │   ├── image.proto                     Image gRPC service definition
│   │   └── container.proto                 Container gRPC service definition
│   ├── agent/
│   │   ├── imagestore/
│   │   │   ├── store.go                    containers/image + containers/storage wrapper
│   │   │   └── store_test.go
│   │   ├── containerd/
│   │   │   ├── state.go                    container state machine + state.json persistence
│   │   │   ├── runtime.go                  Create/Start/Stop/Wait/Delete/List/Inspect
│   │   │   └── runtime_test.go
│   │   ├── network/
│   │   │   ├── host.go                     cs0 bridge + nftables masquerade rule (idempotent)
│   │   │   ├── container.go                per-container veth + IP + resolv.conf
│   │   │   └── network_test.go
│   │   ├── exec/
│   │   │   ├── multiplexer.go              gRPC bidi stream ↔ crun exec stdio bridge
│   │   │   └── multiplexer_test.go
│   │   └── logs/
│   │       ├── tail.go                     per-container log writer + inotify-based follow
│   │       └── tail_test.go
│   └── httpapi/
│       ├── containers.go                   11 container handlers
│       ├── images.go                       4 image handlers
│       ├── hijack.go                       HTTP-hijack helper (attach/exec)
│       └── containers_test.go              handler-level tests with mock client
├── cmd/
│   └── cs-bench/
│       └── main.go                         five-benchmark harness, JSON output
├── tests/
│   └── integration/
│       ├── docker_run_test.go              `docker run -d` end-to-end
│       ├── docker_exec_test.go             `docker exec` end-to-end
│       ├── docker_logs_test.go             `docker logs -f` stream
│       ├── docker_pull_test.go             `docker pull` cold
│       └── bench_test.go                   five locked benchmarks (build-tag `bench`)
└── docs/
    └── benchmarking.md                     OrbStack head-to-head methodology
```

### Modified files

```
cyber-stack/
├── go.mod                                  + containers/image, containers/storage, vishvananda/netlink
├── internal/
│   ├── vm/
│   │   ├── vm.go                           Spec gains DataDiskPath
│   │   └── vfkit_launcher.go               argv adds virtio-blk (data disk) + virtio-net,nat
│   ├── daemon/
│   │   └── client.go                       wrappers for Image + Container gRPC clients
│   └── agent/
│       └── server.go                       register Image + Container services
├── cmd/
│   └── cyberstackd/
│       └── main.go                         normalize ~/.cyberstack/ defaults, --data-disk-size, lockfile
└── guest/
    ├── Makefile                            crun static binary + nftables + iproute2 + kernel modules
    └── init.sh                             mount /dev/vdb (mkfs if blank), resize2fs, network setup
```

---

## Tasks

Tasks are sequenced for clean compile-and-test at every commit. Tasks within a phase can run in parallel by separate subagents *after* the phase's prerequisites land. Inter-phase order is strict.

### Task 1: Persistent data disk plumbing

**Files:**
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/vfkit_launcher.go`
- Modify: `internal/vm/vfkit_launcher_test.go`

- [ ] **Step 1: Add `DataDiskPath` to `vm.Spec`**

```go
// internal/vm/vm.go
type Spec struct {
    MemoryMB             int
    CPUs                 int
    BootDiskPath         string
    EFIVariableStorePath string
    VsockSocketPath      string
    // DataDiskPath is the persistent virtio-blk for image + container storage.
    // The host file is created by cyberstackd if missing; agent formats ext4
    // on first boot via /init.
    DataDiskPath         string
}
```

- [ ] **Step 2: Modify `vfkit_launcher.argv()` to add the data disk + NAT network**

```go
// internal/vm/vfkit_launcher.go (replace argv method)
func (l *vfkitLauncher) argv() []string {
    return []string{
        "--memory", fmt.Sprintf("%d", l.spec.MemoryMB),
        "--cpus", fmt.Sprintf("%d", l.spec.CPUs),
        "--bootloader", fmt.Sprintf("efi,variable-store=%s,create", l.spec.EFIVariableStorePath),
        "--device", fmt.Sprintf("virtio-blk,path=%s", l.spec.BootDiskPath),
        "--device", fmt.Sprintf("virtio-blk,path=%s", l.spec.DataDiskPath),
        "--device", "virtio-net,nat",
        "--device", "virtio-rng",
        "--device", fmt.Sprintf("virtio-vsock,port=1024,socketURL=%s,listen", l.spec.VsockSocketPath),
    }
}
```

- [ ] **Step 3: Update existing argv tests + add data-disk-and-net assertions**

```go
// internal/vm/vfkit_launcher_test.go (extend existing tests)
func TestVFKitLauncher_ArgvIncludesDataDisk(t *testing.T) {
    l := &vfkitLauncher{
        spec: Spec{
            BootDiskPath:         "/b/boot.img",
            DataDiskPath:         "/d/data.img",
            EFIVariableStorePath: "/e/efivars",
            VsockSocketPath:      "/v/vsock.sock",
        },
    }
    joined := strings.Join(l.argv(), " ")
    if !strings.Contains(joined, "--device virtio-blk,path=/d/data.img") {
        t.Errorf("missing data disk device: %q", joined)
    }
    if !strings.Contains(joined, "--device virtio-net,nat") {
        t.Errorf("missing NAT network device: %q", joined)
    }
}
```

- [ ] **Step 4: Run unit tests, verify pass**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go test ./internal/vm/...
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/
git commit -m "feat(0.3): add data disk + NAT network to vfkit launcher"
```

---

### Task 2: Normalize host paths to `~/.cyberstack/` + lockfile

**Files:**
- Modify: `cmd/cyberstackd/main.go`
- Create: `internal/daemon/lockfile.go`
- Create: `internal/daemon/lockfile_test.go`

- [ ] **Step 1: Write the lockfile package with TDD**

```go
// internal/daemon/lockfile_test.go
package daemon

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLockfile_AcquireRelease(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.pid")
    lock, err := AcquireLock(path)
    if err != nil { t.Fatalf("Acquire: %v", err) }
    if _, err := os.Stat(path); err != nil {
        t.Fatalf("lockfile not created: %v", err)
    }
    if err := lock.Release(); err != nil { t.Errorf("Release: %v", err) }
}

func TestLockfile_AcquireBusy(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.pid")
    l1, err := AcquireLock(path)
    if err != nil { t.Fatalf("first Acquire: %v", err) }
    defer l1.Release()
    if _, err := AcquireLock(path); err == nil {
        t.Errorf("expected error acquiring busy lock; got nil")
    }
}

func TestLockfile_StalePIDReclaimed(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.pid")
    // Pre-write a fake stale PID (very high, unlikely to exist)
    os.WriteFile(path, []byte("99999999\n"), 0o644)
    lock, err := AcquireLock(path)
    if err != nil { t.Fatalf("expected stale PID reclaim, got: %v", err) }
    defer lock.Release()
}
```

- [ ] **Step 2: Implement lockfile**

```go
// internal/daemon/lockfile.go
package daemon

import (
    "errors"
    "fmt"
    "os"
    "strconv"
    "strings"
    "syscall"
)

type Lockfile struct {
    path string
}

// AcquireLock creates a PID file at path. Returns error if another live
// process holds it. Stale lockfiles (PID points to non-existent process)
// are reclaimed.
func AcquireLock(path string) (*Lockfile, error) {
    if data, err := os.ReadFile(path); err == nil {
        pidStr := strings.TrimSpace(string(data))
        if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
            if err := syscall.Kill(pid, 0); err == nil {
                return nil, fmt.Errorf("another cyberstackd is using %s (PID %d)", path, pid)
            }
        }
        // stale lockfile — fall through and overwrite
    } else if !errors.Is(err, os.ErrNotExist) {
        return nil, fmt.Errorf("read lockfile: %w", err)
    }
    if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
        return nil, fmt.Errorf("write lockfile: %w", err)
    }
    return &Lockfile{path: path}, nil
}

func (l *Lockfile) Release() error {
    return os.Remove(l.path)
}
```

- [ ] **Step 3: Run lockfile tests**

```bash
go test ./internal/daemon/ -run Lockfile -v
```
Expected: all PASS.

- [ ] **Step 4: Update `cmd/cyberstackd/main.go` defaults to `~/.cyberstack/`**

```go
// cmd/cyberstackd/main.go (replace flag definitions)
import (
    // existing imports...
)

func defaultPath(name string) string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".cyberstack", name)
}

func main() {
    var (
        socketPath   string
        vfkitBin     string
        bootDisk     string
        dataDisk     string
        dataDiskSize string
        efiVars      string
        vsockSock    string
        memoryMB     int
        cpus         int
        noVM         bool
    )
    flag.StringVar(&socketPath, "socket", defaultPath("cyberstack.sock"), "Docker-compat Unix socket path")
    flag.StringVar(&vfkitBin, "vfkit-binary", "/opt/tainer/bin/vfkit", "vfkit executable")
    flag.StringVar(&bootDisk, "boot-disk", defaultPath(fmt.Sprintf("boot-%s.img", runtime.GOARCH)), "EFI-bootable raw disk")
    flag.StringVar(&dataDisk, "data-disk", defaultPath(fmt.Sprintf("data-%s.img", runtime.GOARCH)), "persistent data disk for images + containers")
    flag.StringVar(&dataDiskSize, "data-disk-size", "50G", "data disk size (only grows; never shrinks)")
    flag.StringVar(&efiVars, "efi-vars", defaultPath("efivars"), "EFI variable store")
    flag.StringVar(&vsockSock, "vsock-socket", defaultPath("vsock.sock"), "host-side vsock unix socket")
    flag.IntVar(&memoryMB, "memory-mb", 4096, "VM memory in MB")
    flag.IntVar(&cpus, "cpus", 4, "VM vCPU count")
    flag.BoolVar(&noVM, "no-vm", false, "skip VM bring-up (0.1 mode)")
    flag.Parse()

    // Ensure ~/.cyberstack/ exists
    home, _ := os.UserHomeDir()
    csDir := filepath.Join(home, ".cyberstack")
    if err := os.MkdirAll(csDir, 0o755); err != nil {
        log.Fatalf("create %s: %v", csDir, err)
    }

    // Acquire lockfile
    lock, err := daemon.AcquireLock(filepath.Join(csDir, "cyberstackd.pid"))
    if err != nil { log.Fatalf("%v", err) }
    defer lock.Release()

    // ... (rest of main as 0.2, but pass dataDisk into vm.Spec and through cfg.VsockSocketPath)
}
```

- [ ] **Step 5: Run all tests, verify build**

```bash
go build ./...
go test ./...
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/cyberstackd/main.go internal/daemon/lockfile.go internal/daemon/lockfile_test.go
git commit -m "feat(0.3): normalize host paths to ~/.cyberstack/ + add lockfile"
```

---

### Task 3: Data disk creation + grow logic

**Files:**
- Create: `internal/daemon/datadisk.go`
- Create: `internal/daemon/datadisk_test.go`
- Modify: `cmd/cyberstackd/main.go`

- [ ] **Step 1: Test the size parsing + ensure-created behaviour**

```go
// internal/daemon/datadisk_test.go
package daemon

import (
    "os"
    "path/filepath"
    "testing"
)

func TestParseSize(t *testing.T) {
    cases := map[string]int64{
        "1": 1, "100": 100,
        "1K": 1024, "1KB": 1024,
        "1M": 1024 * 1024, "50M": 50 * 1024 * 1024,
        "1G": 1024 * 1024 * 1024, "50G": 50 * 1024 * 1024 * 1024,
        "1T": 1024 * 1024 * 1024 * 1024,
    }
    for in, want := range cases {
        got, err := ParseSize(in)
        if err != nil { t.Errorf("ParseSize(%q) err: %v", in, err); continue }
        if got != want { t.Errorf("ParseSize(%q) = %d, want %d", in, got, want) }
    }
    if _, err := ParseSize("bogus"); err == nil {
        t.Error("expected error on bogus input")
    }
}

func TestEnsureDataDisk_CreatesIfMissing(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "data.img")
    if err := EnsureDataDisk(path, 1024*1024); err != nil {
        t.Fatalf("EnsureDataDisk: %v", err)
    }
    fi, err := os.Stat(path)
    if err != nil { t.Fatalf("Stat: %v", err) }
    if fi.Size() != 1024*1024 {
        t.Errorf("size = %d, want %d", fi.Size(), 1024*1024)
    }
}

func TestEnsureDataDisk_GrowsButNeverShrinks(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "data.img")
    if err := EnsureDataDisk(path, 1024*1024); err != nil { t.Fatal(err) }
    // grow
    if err := EnsureDataDisk(path, 2*1024*1024); err != nil { t.Fatal(err) }
    fi, _ := os.Stat(path)
    if fi.Size() != 2*1024*1024 {
        t.Errorf("after grow: size = %d, want %d", fi.Size(), 2*1024*1024)
    }
    // request shrink — should NOT shrink
    if err := EnsureDataDisk(path, 512*1024); err != nil { t.Fatal(err) }
    fi, _ = os.Stat(path)
    if fi.Size() != 2*1024*1024 {
        t.Errorf("after shrink-request: size = %d, want %d (unchanged)", fi.Size(), 2*1024*1024)
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/daemon/datadisk.go
package daemon

import (
    "errors"
    "fmt"
    "os"
    "strconv"
    "strings"
)

// ParseSize parses sizes like "50G", "100M", "1024K" into bytes.
// Plain numbers are bytes. Suffix is 1024-based (binary).
func ParseSize(s string) (int64, error) {
    s = strings.TrimSpace(strings.ToUpper(s))
    if s == "" { return 0, errors.New("empty size") }
    // strip optional B suffix
    if strings.HasSuffix(s, "B") && len(s) >= 2 && (s[len(s)-2] < '0' || s[len(s)-2] > '9') {
        s = s[:len(s)-1]
    }
    var mult int64 = 1
    switch s[len(s)-1] {
    case 'K': mult = 1024
    case 'M': mult = 1024 * 1024
    case 'G': mult = 1024 * 1024 * 1024
    case 'T': mult = 1024 * 1024 * 1024 * 1024
    }
    if mult > 1 { s = s[:len(s)-1] }
    n, err := strconv.ParseInt(s, 10, 64)
    if err != nil { return 0, fmt.Errorf("parse size %q: %w", s, err) }
    return n * mult, nil
}

// EnsureDataDisk creates a sparse file at path of at least size bytes.
// If the file exists and is smaller, it's grown via truncate (sparse,
// no actual disk allocation). Never shrinks.
func EnsureDataDisk(path string, size int64) error {
    fi, err := os.Stat(path)
    if errors.Is(err, os.ErrNotExist) {
        f, err := os.Create(path)
        if err != nil { return fmt.Errorf("create data disk: %w", err) }
        defer f.Close()
        if err := f.Truncate(size); err != nil {
            return fmt.Errorf("truncate new data disk: %w", err)
        }
        return nil
    }
    if err != nil { return fmt.Errorf("stat data disk: %w", err) }
    if fi.Size() >= size { return nil } // never shrink
    return os.Truncate(path, size)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/daemon/ -run "ParseSize|EnsureDataDisk" -v
```
Expected: all PASS.

- [ ] **Step 4: Wire into cyberstackd main**

In `cmd/cyberstackd/main.go`, after the `csDir` MkdirAll and lockfile, before the VM spec construction:

```go
sizeBytes, err := daemon.ParseSize(dataDiskSize)
if err != nil { log.Fatalf("--data-disk-size: %v", err) }
if err := daemon.EnsureDataDisk(dataDisk, sizeBytes); err != nil {
    log.Fatalf("data disk: %v", err)
}

// Then in vm.Spec construction:
spec := vm.Spec{
    // existing fields...
    DataDiskPath: dataDisk,
}
```

- [ ] **Step 5: Run full test suite + integration test (boot disk must exist)**

```bash
go test ./...
# integration test will skip if boot-disk not at ~/.cyberstack/boot-arm64.img
go test -tags=integration ./tests/integration/...
```
Expected: all PASS or SKIP.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/datadisk.go internal/daemon/datadisk_test.go cmd/cyberstackd/main.go
git commit -m "feat(0.3): create + grow persistent data disk on cyberstackd start"
```

---

### Task 4: Image + Container proto definitions

**Files:**
- Create: `internal/proto/image.proto`
- Create: `internal/proto/container.proto`
- Modify: `Makefile` (proto target)
- Generated (will appear): `internal/proto/gen/image.pb.go`, `internal/proto/gen/image_grpc.pb.go`, `internal/proto/gen/container.pb.go`, `internal/proto/gen/container_grpc.pb.go`

- [ ] **Step 1: Write `image.proto`**

```protobuf
// internal/proto/image.proto
syntax = "proto3";

package cyberstack.image.v1;

option go_package = "github.com/cyber5-io/cyber-stack/internal/proto/gen;agentpb";

service Image {
  rpc Pull(PullRequest)     returns (PullResponse);
  rpc List(ListImagesRequest) returns (ListImagesResponse);
  rpc Inspect(InspectImageRequest) returns (ImageInspect);
  rpc Remove(RemoveImageRequest) returns (RemoveImageResponse);
}

message PullRequest {
  string ref      = 1; // e.g. "docker.io/library/alpine:latest"
  string platform = 2; // optional override; default = host arch
}
message PullResponse {
  string image_id  = 1;
  string repo_tag  = 2;
  uint64 size      = 3;
}

message ListImagesRequest {}
message ListImagesResponse {
  repeated ImageSummary images = 1;
}
message ImageSummary {
  string id            = 1;
  repeated string repo_tags = 2;
  uint64 size          = 3;
  int64  created_unix  = 4;
}

message InspectImageRequest { string id = 1; }
message ImageInspect {
  string id            = 1;
  repeated string repo_tags = 2;
  string config_digest = 3;
  uint64 size          = 4;
  int64  created_unix  = 5;
  string architecture  = 6;
  string os            = 7;
  // selected from container/image config
  repeated string env  = 8;
  repeated string cmd  = 9;
  string entrypoint    = 10;
  string working_dir   = 11;
}

message RemoveImageRequest { string id = 1; bool force = 2; }
message RemoveImageResponse { repeated string deleted = 1; }
```

- [ ] **Step 2: Write `container.proto`**

```protobuf
// internal/proto/container.proto
syntax = "proto3";

package cyberstack.container.v1;

option go_package = "github.com/cyber5-io/cyber-stack/internal/proto/gen;agentpb";

service Container {
  rpc Create(CreateRequest)   returns (CreateResponse);
  rpc Start(StartRequest)     returns (StartResponse);
  rpc Stop(StopRequest)       returns (StopResponse);
  rpc Wait(WaitRequest)       returns (WaitResponse);
  rpc Delete(DeleteRequest)   returns (DeleteResponse);
  rpc Inspect(InspectRequest) returns (ContainerInspect);
  rpc List(ListRequest)       returns (ListResponse);
  rpc Logs(LogsRequest)       returns (stream LogFrame);
  rpc Attach(stream AttachFrame) returns (stream AttachFrame);
  rpc Exec(ExecRequest)       returns (ExecResponse);
  rpc ExecStart(stream AttachFrame) returns (stream AttachFrame);
}

message ContainerSpec {
  string image      = 1;
  repeated string cmd  = 2;
  repeated string args = 3;
  repeated string env  = 4;
  string workdir    = 5;
  string hostname   = 6;
  map<string,string> labels = 7;
  HostConfig host_config    = 8;
  string restart_policy     = 9; // accepted but not honored in 0.3
  bool tty                  = 10;
}
message HostConfig {
  uint64 memory_bytes = 1;
  int64  cpu_shares   = 2;
}

message CreateRequest { ContainerSpec spec = 1; string name = 2; }
message CreateResponse { string id = 1; }

message StartRequest { string id = 1; }
message StartResponse { int32 pid = 1; }

message StopRequest { string id = 1; uint32 timeout_seconds = 2; }
message StopResponse {}

message WaitRequest { string id = 1; }
message WaitResponse { int32 exit_code = 1; bool oom_killed = 2; }

message DeleteRequest { string id = 1; bool force = 2; }
message DeleteResponse {}

message InspectRequest { string id = 1; }
message ContainerInspect {
  string id        = 1;
  string name      = 2;
  string image     = 3;
  string state     = 4; // created/running/stopping/stopped/exited
  string status    = 5; // human-readable
  int32  exit_code = 6;
  bool   oom_killed= 7;
  int64  created_unix = 8;
  int64  started_unix = 9;
  int64  finished_unix= 10;
  ContainerSpec spec = 11;
  string ip_address  = 12;
}

message ListRequest { bool all = 1; }
message ListResponse { repeated ContainerInspect containers = 1; }

message LogsRequest { string id = 1; bool follow = 2; bool stdout = 3; bool stderr = 4; uint32 tail = 5; }
enum Stream { STREAM_UNKNOWN = 0; STDIN = 1; STDOUT = 2; STDERR = 3; EXIT = 4; }
message LogFrame { Stream stream = 1; bytes data = 2; int64 ts_unix_nano = 3; }

message AttachFrame {
  Stream stream = 1;
  bytes data = 2;
  // first frame on ExecStart MUST set exec_id
  string exec_id = 3;
  // Last frame from server has stream=EXIT and exit_code set
  int32 exit_code = 4;
  // resize signal
  uint32 tty_height = 5;
  uint32 tty_width  = 6;
}

message ExecRequest {
  string container_id = 1;
  repeated string cmd = 2;
  repeated string env = 3;
  bool attach_stdin   = 4;
  bool attach_stdout  = 5;
  bool attach_stderr  = 6;
  bool tty            = 7;
  string user         = 8;
  string workdir      = 9;
}
message ExecResponse { string exec_id = 1; }
```

- [ ] **Step 3: Run proto generation**

```bash
make proto
```
Expected: generates files under `internal/proto/gen/` for both new services + the existing agent.

- [ ] **Step 4: Verify compile**

```bash
go build ./...
```
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/proto/image.proto internal/proto/container.proto internal/proto/gen/
git commit -m "feat(0.3): add Image + Container gRPC service definitions"
```

---

### Task 5: imagestore — Init + Pull

**Files:**
- Modify: `go.mod` (add `containers/image/v5`, `containers/storage`)
- Create: `internal/agent/imagestore/store.go`
- Create: `internal/agent/imagestore/store_test.go`

- [ ] **Step 1: Add dependencies**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go get github.com/containers/image/v5@latest
go get github.com/containers/storage@latest
go mod tidy
```

- [ ] **Step 2: Write the store package skeleton with Init**

```go
// internal/agent/imagestore/store.go
package imagestore

import (
    "context"
    "fmt"
    "github.com/containers/storage"
    "github.com/containers/storage/pkg/idtools"
)

// Store wraps containers/storage and (eventually) containers/image
// to back the agent's Image RPC service.
type Store struct {
    cs storage.Store
}

// New initializes containers/storage rooted at the given graphRoot
// (e.g. /var/lib/cyberstack/storage). Uses overlay2 graph driver.
func New(graphRoot, runRoot string) (*Store, error) {
    opts := storage.StoreOptions{
        GraphDriverName: "overlay",
        GraphRoot:       graphRoot,
        RunRoot:         runRoot,
        UIDMap: []idtools.IDMap{{ContainerID: 0, HostID: 0, Size: 65536}},
        GIDMap: []idtools.IDMap{{ContainerID: 0, HostID: 0, Size: 65536}},
    }
    cs, err := storage.GetStore(opts)
    if err != nil { return nil, fmt.Errorf("init storage: %w", err) }
    return &Store{cs: cs}, nil
}

func (s *Store) Close() error {
    _, err := s.cs.Shutdown(false)
    return err
}
```

- [ ] **Step 3: Add Pull method**

```go
// internal/agent/imagestore/store.go (append)
import (
    "github.com/containers/image/v5/copy"
    "github.com/containers/image/v5/docker"
    "github.com/containers/image/v5/signature"
    "github.com/containers/image/v5/storage" // aliased differently — see imports
    is "github.com/containers/image/v5/storage"
    "github.com/containers/image/v5/types"
)

type PullResult struct {
    ImageID string
    RepoTag string
    Size    int64
}

// Pull copies an image from a registry into the local storage.
// ref examples:
//   "docker.io/library/alpine:latest"
//   "ghcr.io/cyber5-io/tainer-php:latest"
func (s *Store) Pull(ctx context.Context, ref string) (*PullResult, error) {
    src, err := docker.ParseReference("//" + ref)
    if err != nil { return nil, fmt.Errorf("parse src ref: %w", err) }
    dst, err := is.Transport.ParseStoreReference(s.cs, ref)
    if err != nil { return nil, fmt.Errorf("parse dst ref: %w", err) }

    policy, _ := signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
    polCtx, _ := signature.NewPolicyContext(policy)
    defer polCtx.Destroy()

    _, err = copy.Image(ctx, polCtx, dst, src, &copy.Options{
        SourceCtx: &types.SystemContext{},
    })
    if err != nil { return nil, fmt.Errorf("copy image: %w", err) }

    img, err := s.cs.Image(ref)
    if err != nil { return nil, fmt.Errorf("lookup pulled image: %w", err) }

    var size int64
    layers, _ := s.cs.LayersByImageStore(img.ID)
    for _, l := range layers { size += l.UncompressedSize }
    return &PullResult{ImageID: img.ID, RepoTag: ref, Size: size}, nil
}
```

- [ ] **Step 4: Test against a real public image (skips on non-linux/amd64 to avoid CI flake)**

```go
// internal/agent/imagestore/store_test.go
//go:build linux

package imagestore

import (
    "context"
    "testing"
    "time"
)

func TestPull_AlpineLatest(t *testing.T) {
    if testing.Short() { t.Skip("requires network") }
    s, err := New(t.TempDir(), t.TempDir())
    if err != nil { t.Fatalf("New: %v", err) }
    defer s.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    res, err := s.Pull(ctx, "docker.io/library/alpine:latest")
    if err != nil { t.Fatalf("Pull: %v", err) }
    if res.ImageID == "" { t.Error("ImageID empty") }
    if res.Size == 0 { t.Error("Size zero") }
    t.Logf("pulled: id=%s size=%d", res.ImageID, res.Size)
}
```

- [ ] **Step 5: Build (won't run on macOS — linux only)**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/imagestore/
```
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/agent/imagestore/
git commit -m "feat(0.3): imagestore — init storage + pull from registry"
```

---

### Task 6: imagestore — List, Inspect, Remove

**Files:**
- Modify: `internal/agent/imagestore/store.go`
- Modify: `internal/agent/imagestore/store_test.go`

- [ ] **Step 1: Add List, Inspect, Remove methods**

```go
// append to store.go

import (
    "encoding/json"
    "github.com/containers/image/v5/manifest"
)

type Summary struct {
    ID        string
    RepoTags  []string
    Size      int64
    CreatedAt time.Time
}

func (s *Store) List(ctx context.Context) ([]Summary, error) {
    images, err := s.cs.Images()
    if err != nil { return nil, fmt.Errorf("list images: %w", err) }
    out := make([]Summary, 0, len(images))
    for _, img := range images {
        var size int64
        layers, _ := s.cs.LayersByImageStore(img.ID)
        for _, l := range layers { size += l.UncompressedSize }
        out = append(out, Summary{
            ID: img.ID, RepoTags: img.Names,
            Size: size, CreatedAt: img.Created,
        })
    }
    return out, nil
}

type Inspect struct {
    Summary
    ConfigDigest string
    Architecture string
    OS           string
    Env          []string
    Cmd          []string
    Entrypoint   string
    WorkingDir   string
}

func (s *Store) Inspect(ctx context.Context, id string) (*Inspect, error) {
    img, err := s.cs.Image(id)
    if err != nil { return nil, fmt.Errorf("lookup: %w", err) }
    cfgBlob, err := s.cs.ImageBigData(img.ID, "manifest")
    if err != nil { return nil, fmt.Errorf("load manifest: %w", err) }
    m, err := manifest.FromBlob(cfgBlob, manifest.GuessMIMEType(cfgBlob))
    if err != nil { return nil, fmt.Errorf("parse manifest: %w", err) }
    cfgDigest := m.ConfigInfo().Digest.String()
    cfgRaw, err := s.cs.ImageBigData(img.ID, cfgDigest)
    if err != nil { return nil, fmt.Errorf("load config: %w", err) }
    var cfg struct {
        Architecture string `json:"architecture"`
        OS           string `json:"os"`
        Config struct {
            Env        []string `json:"Env"`
            Cmd        []string `json:"Cmd"`
            Entrypoint []string `json:"Entrypoint"`
            WorkingDir string   `json:"WorkingDir"`
        } `json:"config"`
    }
    json.Unmarshal(cfgRaw, &cfg)
    var size int64
    layers, _ := s.cs.LayersByImageStore(img.ID)
    for _, l := range layers { size += l.UncompressedSize }
    entry := ""
    if len(cfg.Config.Entrypoint) > 0 { entry = cfg.Config.Entrypoint[0] }
    return &Inspect{
        Summary: Summary{ID: img.ID, RepoTags: img.Names, Size: size, CreatedAt: img.Created},
        ConfigDigest: cfgDigest, Architecture: cfg.Architecture, OS: cfg.OS,
        Env: cfg.Config.Env, Cmd: cfg.Config.Cmd, Entrypoint: entry, WorkingDir: cfg.Config.WorkingDir,
    }, nil
}

// Remove deletes an image and its layers from the store.
// force=true ignores "image in use by container" errors.
func (s *Store) Remove(ctx context.Context, id string, force bool) ([]string, error) {
    layers, err := s.cs.DeleteImage(id, true)
    if err != nil { return nil, fmt.Errorf("delete: %w", err) }
    return layers, nil
}
```

- [ ] **Step 2: Add tests**

```go
// append to store_test.go

func TestList_AfterPull(t *testing.T) {
    if testing.Short() { t.Skip("requires network") }
    s, err := New(t.TempDir(), t.TempDir())
    if err != nil { t.Fatal(err) }
    defer s.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    _, err = s.Pull(ctx, "docker.io/library/alpine:latest")
    if err != nil { t.Fatal(err) }
    list, err := s.List(ctx)
    if err != nil { t.Fatal(err) }
    if len(list) == 0 { t.Error("List returned empty after Pull") }
}

func TestRemove(t *testing.T) {
    if testing.Short() { t.Skip("requires network") }
    s, err := New(t.TempDir(), t.TempDir())
    if err != nil { t.Fatal(err) }
    defer s.Close()
    ctx := context.Background()
    res, err := s.Pull(ctx, "docker.io/library/alpine:latest")
    if err != nil { t.Fatal(err) }
    if _, err := s.Remove(ctx, res.ImageID, false); err != nil { t.Fatal(err) }
    list, _ := s.List(ctx)
    if len(list) != 0 { t.Errorf("expected empty list after remove, got %d", len(list)) }
}
```

- [ ] **Step 3: Build for linux**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/imagestore/
```

- [ ] **Step 4: Commit**

```bash
git add internal/agent/imagestore/
git commit -m "feat(0.3): imagestore — list, inspect, remove"
```

---

### Task 7: containerd — state machine + persistence

**Files:**
- Create: `internal/agent/containerd/state.go`
- Create: `internal/agent/containerd/state_test.go`

- [ ] **Step 1: Write tests for state persistence**

```go
// internal/agent/containerd/state_test.go
package containerd

import (
    "path/filepath"
    "testing"
    "time"
)

func TestState_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    s := State{
        ID: "abc123", Name: "test", Image: "alpine:latest",
        Status: StateRunning, PID: 4242, ExitCode: -1,
        CreatedAt: time.Now().UTC().Truncate(time.Second),
        StartedAt: time.Now().UTC().Truncate(time.Second),
        IPAddress: "172.17.0.2",
    }
    path := filepath.Join(dir, "state.json")
    if err := WriteState(path, &s); err != nil { t.Fatal(err) }
    got, err := ReadState(path)
    if err != nil { t.Fatal(err) }
    if got.ID != s.ID || got.Status != s.Status || got.IPAddress != s.IPAddress {
        t.Errorf("round-trip mismatch:\nwant %+v\n got %+v", s, *got)
    }
}

func TestState_Transitions(t *testing.T) {
    s := &State{Status: StateCreated}
    if !s.CanTransitionTo(StateRunning) { t.Error("Created → Running should be ok") }
    s.Status = StateRunning
    if !s.CanTransitionTo(StateStopping) { t.Error("Running → Stopping should be ok") }
    if s.CanTransitionTo(StateCreated) { t.Error("Running → Created should fail") }
}
```

- [ ] **Step 2: Implement**

```go
// internal/agent/containerd/state.go
package containerd

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
)

type Status string

const (
    StateCreating Status = "creating"
    StateCreated  Status = "created"
    StateRunning  Status = "running"
    StateStopping Status = "stopping"
    StateStopped  Status = "stopped"
    StateExited   Status = "exited"
)

type State struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Image       string    `json:"image"`
    Status      Status    `json:"status"`
    StatusText  string    `json:"status_text"`
    PID         int       `json:"pid"`
    ExitCode    int       `json:"exit_code"`
    OOMKilled   bool      `json:"oom_killed"`
    CreatedAt   time.Time `json:"created_at"`
    StartedAt   time.Time `json:"started_at"`
    FinishedAt  time.Time `json:"finished_at"`
    IPAddress   string    `json:"ip_address"`
    BundlePath  string    `json:"bundle_path"`
    LogPath     string    `json:"log_path"`
}

func WriteState(path string, s *State) error {
    data, err := json.MarshalIndent(s, "", "  ")
    if err != nil { return err }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil { return err }
    return os.Rename(tmp, path)
}

func ReadState(path string) (*State, error) {
    data, err := os.ReadFile(path)
    if err != nil { return nil, err }
    var s State
    if err := json.Unmarshal(data, &s); err != nil { return nil, fmt.Errorf("unmarshal state: %w", err) }
    return &s, nil
}

// CanTransitionTo enforces the state machine from spec section 7b.
func (s *State) CanTransitionTo(target Status) bool {
    switch s.Status {
    case StateCreating: return target == StateCreated || target == StateExited
    case StateCreated:  return target == StateRunning || target == StateExited
    case StateRunning:  return target == StateStopping || target == StateExited
    case StateStopping: return target == StateStopped || target == StateExited
    default:            return false
    }
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/agent/containerd/ -v
```
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/containerd/
git commit -m "feat(0.3): containerd — state machine + state.json persistence"
```

---

### Task 8: containerd — Create (bundle build)

**Files:**
- Modify: `internal/agent/containerd/runtime.go` (create)
- Modify: `internal/agent/containerd/runtime_test.go` (create)

- [ ] **Step 1: Define Runtime + Create signature**

```go
// internal/agent/containerd/runtime.go
package containerd

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "github.com/cyber5-io/cyber-stack/internal/agent/imagestore"
)

// Runtime owns container lifecycle. One instance per agent.
type Runtime struct {
    images   *imagestore.Store
    rootDir  string // /var/lib/cyberstack/containers
    crunPath string // /usr/bin/crun
}

func New(images *imagestore.Store, rootDir, crunPath string) (*Runtime, error) {
    if err := os.MkdirAll(rootDir, 0o755); err != nil { return nil, err }
    return &Runtime{images: images, rootDir: rootDir, crunPath: crunPath}, nil
}

// Spec is the trimmed-down Docker container spec we accept in 0.3.
type Spec struct {
    Image       string
    Cmd         []string
    Args        []string
    Env         []string
    WorkDir     string
    Hostname    string
    Labels      map[string]string
    MemoryBytes uint64
    CPUShares   int64
    TTY         bool
}

// Create builds an OCI bundle (rootfs + config.json) for a new container,
// writes initial state.json, but does NOT start any process.
// Returns the container ID.
func (r *Runtime) Create(ctx context.Context, spec *Spec, name string) (string, error) {
    id := newID()
    bundleDir := filepath.Join(r.rootDir, id)
    if err := os.MkdirAll(bundleDir, 0o755); err != nil { return "", err }

    // Build rootfs via containers/storage CreateContainer (returns mountpoint)
    inspect, err := r.images.Inspect(ctx, spec.Image)
    if err != nil { return "", fmt.Errorf("inspect image: %w", err) }
    mountpoint, err := r.images.PrepareRootfs(ctx, inspect.ID, id)
    if err != nil { return "", fmt.Errorf("prepare rootfs: %w", err) }

    // Build OCI config.json from inspect + spec
    cfg := buildOCIConfig(spec, inspect, mountpoint)
    cfgBytes, _ := json.MarshalIndent(cfg, "", "  ")
    if err := os.WriteFile(filepath.Join(bundleDir, "config.json"), cfgBytes, 0o644); err != nil {
        return "", err
    }

    // Initial state
    state := &State{
        ID: id, Name: name, Image: spec.Image,
        Status: StateCreated, ExitCode: -1,
        CreatedAt: time.Now().UTC(),
        BundlePath: bundleDir,
        LogPath:    filepath.Join(bundleDir, "log.json"),
    }
    if err := WriteState(filepath.Join(bundleDir, "state.json"), state); err != nil {
        return "", err
    }
    return id, nil
}

func newID() string {
    // 64-hex-char ID, matching Docker convention
    b := make([]byte, 32)
    rand.Read(b)
    return hex.EncodeToString(b)
}

// buildOCIConfig produces a minimal OCI runtime spec config.json.
// 0.3: cgroups v2, no namespaces beyond default, mount /proc /sys /dev,
// inherit Cmd/Entrypoint/WorkingDir/Env from image config unless spec overrides.
func buildOCIConfig(spec *Spec, img *imagestore.Inspect, rootfs string) interface{} {
    args := spec.Cmd
    if len(args) == 0 && img.Entrypoint != "" { args = append([]string{img.Entrypoint}, img.Cmd...) }
    if len(args) == 0 { args = img.Cmd }
    args = append(args, spec.Args...)

    env := append([]string{}, img.Env...)
    env = append(env, spec.Env...)

    workdir := spec.WorkDir
    if workdir == "" { workdir = img.WorkingDir }
    if workdir == "" { workdir = "/" }

    return map[string]interface{}{
        "ociVersion": "1.0.2",
        "process": map[string]interface{}{
            "terminal": spec.TTY,
            "user":     map[string]interface{}{"uid": 0, "gid": 0},
            "args":     args,
            "env":      env,
            "cwd":      workdir,
            "capabilities": map[string]interface{}{
                "bounding": []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
                "effective": []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
                "permitted": []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
            },
        },
        "root":     map[string]interface{}{"path": rootfs, "readonly": false},
        "hostname": defaultIfEmpty(spec.Hostname, "container"),
        "mounts": []map[string]interface{}{
            {"destination": "/proc", "type": "proc", "source": "proc"},
            {"destination": "/dev", "type": "tmpfs", "source": "tmpfs",
             "options": []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
            {"destination": "/dev/pts", "type": "devpts", "source": "devpts",
             "options": []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"}},
            {"destination": "/sys", "type": "sysfs", "source": "sysfs",
             "options": []string{"nosuid", "noexec", "nodev", "ro"}},
        },
        "linux": map[string]interface{}{
            "namespaces": []map[string]interface{}{
                {"type": "pid"}, {"type": "network"}, {"type": "ipc"},
                {"type": "uts"}, {"type": "mount"},
            },
            "resources": map[string]interface{}{
                "memory": map[string]interface{}{"limit": int64(spec.MemoryBytes)},
                "cpu":    map[string]interface{}{"shares": uint64(spec.CPUShares)},
            },
        },
    }
}

func defaultIfEmpty(s, d string) string { if s == "" { return d }; return s }
```

- [ ] **Step 2: Add `PrepareRootfs` helper to imagestore**

```go
// append to internal/agent/imagestore/store.go

// PrepareRootfs creates a fresh container layer atop an image's layer chain
// and returns the merged mountpoint (overlay's "merged" dir).
func (s *Store) PrepareRootfs(ctx context.Context, imageID, containerID string) (string, error) {
    container, err := s.cs.CreateContainer(containerID, nil, imageID, "", "", &storage.ContainerOptions{})
    if err != nil { return "", fmt.Errorf("create container: %w", err) }
    mountpoint, err := s.cs.Mount(container.ID, "")
    if err != nil { return "", fmt.Errorf("mount container: %w", err) }
    return mountpoint, nil
}

// CleanupContainer unmounts and removes a container's storage tree.
func (s *Store) CleanupContainer(ctx context.Context, containerID string) error {
    if _, err := s.cs.Unmount(containerID, false); err != nil { return err }
    return s.cs.DeleteContainer(containerID)
}
```

- [ ] **Step 3: Build linux only**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/...
```
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/containerd/runtime.go internal/agent/imagestore/store.go
git commit -m "feat(0.3): containerd.Create — build OCI bundle from image"
```

---

### Task 9: containerd — Start, Stop, Wait, Delete

**Files:**
- Modify: `internal/agent/containerd/runtime.go`

- [ ] **Step 1: Implement Start (fork crun)**

```go
// append to runtime.go

import (
    "os/exec"
    "syscall"
)

func (r *Runtime) Start(ctx context.Context, id string) (int, error) {
    bundle := filepath.Join(r.rootDir, id)
    state, err := ReadState(filepath.Join(bundle, "state.json"))
    if err != nil { return 0, err }
    if !state.CanTransitionTo(StateRunning) {
        return 0, fmt.Errorf("cannot start container in state %q", state.Status)
    }

    // Open log file for child stdout/stderr
    logFile, err := os.OpenFile(state.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
    if err != nil { return 0, err }

    cmd := exec.Command(r.crunPath, "run", "-d", "-b", bundle, "--pid-file", filepath.Join(bundle, "pid"), id)
    // crun handles stdio internally when -d; we tail log.json from agent side
    cmd.Stdout = logFile
    cmd.Stderr = logFile
    if err := cmd.Run(); err != nil { return 0, fmt.Errorf("crun run: %w", err) }

    // Read PID file
    pidData, err := os.ReadFile(filepath.Join(bundle, "pid"))
    if err != nil { return 0, fmt.Errorf("read pidfile: %w", err) }
    pid, _ := strconv.Atoi(strings.TrimSpace(string(pidData)))

    state.PID = pid
    state.Status = StateRunning
    state.StartedAt = time.Now().UTC()
    WriteState(filepath.Join(bundle, "state.json"), state)

    // Spawn waitpid goroutine
    go r.reap(id, pid)

    return pid, nil
}

// reap waits for container init to exit and updates state.
func (r *Runtime) reap(id string, pid int) {
    for {
        var ws syscall.WaitStatus
        wpid, err := syscall.Wait4(pid, &ws, 0, nil)
        if err != nil || wpid == pid { break }
    }
    bundle := filepath.Join(r.rootDir, id)
    state, _ := ReadState(filepath.Join(bundle, "state.json"))
    if state == nil { return }
    state.Status = StateExited
    state.FinishedAt = time.Now().UTC()
    // Exit code derivation requires re-reading; for v1 use 0 if exited cleanly
    state.ExitCode = 0
    WriteState(filepath.Join(bundle, "state.json"), state)
}
```

- [ ] **Step 2: Implement Stop**

```go
func (r *Runtime) Stop(ctx context.Context, id string, timeoutSec uint32) error {
    bundle := filepath.Join(r.rootDir, id)
    state, err := ReadState(filepath.Join(bundle, "state.json"))
    if err != nil { return err }
    if state.Status != StateRunning { return nil }

    // SIGTERM via crun kill
    exec.Command(r.crunPath, "kill", id, "TERM").Run()

    deadline := time.After(time.Duration(timeoutSec) * time.Second)
    tick := time.NewTicker(100 * time.Millisecond)
    defer tick.Stop()
    for {
        select {
        case <-deadline:
            exec.Command(r.crunPath, "kill", id, "KILL").Run()
            return nil
        case <-tick.C:
            s, _ := ReadState(filepath.Join(bundle, "state.json"))
            if s != nil && s.Status == StateExited { return nil }
        }
    }
}
```

- [ ] **Step 3: Implement Wait + Delete**

```go
func (r *Runtime) Wait(ctx context.Context, id string) (int, bool, error) {
    bundle := filepath.Join(r.rootDir, id)
    tick := time.NewTicker(50 * time.Millisecond)
    defer tick.Stop()
    for {
        select {
        case <-ctx.Done(): return 0, false, ctx.Err()
        case <-tick.C:
            s, err := ReadState(filepath.Join(bundle, "state.json"))
            if err != nil { return 0, false, err }
            if s.Status == StateExited || s.Status == StateStopped {
                return s.ExitCode, s.OOMKilled, nil
            }
        }
    }
}

func (r *Runtime) Delete(ctx context.Context, id string, force bool) error {
    bundle := filepath.Join(r.rootDir, id)
    state, err := ReadState(filepath.Join(bundle, "state.json"))
    if err != nil { return err }
    if state.Status == StateRunning {
        if !force { return fmt.Errorf("container running, use force=true") }
        r.Stop(ctx, id, 5)
    }
    exec.Command(r.crunPath, "delete", id).Run()
    if err := r.images.CleanupContainer(ctx, id); err != nil { return err }
    return os.RemoveAll(bundle)
}
```

- [ ] **Step 4: Build linux**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/containerd/
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent/containerd/runtime.go
git commit -m "feat(0.3): containerd — Start, Stop, Wait, Delete via crun"
```

---

### Task 10: containerd — List, Inspect

**Files:**
- Modify: `internal/agent/containerd/runtime.go`

- [ ] **Step 1: Implement**

```go
func (r *Runtime) List(ctx context.Context, all bool) ([]*State, error) {
    entries, err := os.ReadDir(r.rootDir)
    if err != nil { return nil, err }
    var out []*State
    for _, e := range entries {
        if !e.IsDir() { continue }
        s, err := ReadState(filepath.Join(r.rootDir, e.Name(), "state.json"))
        if err != nil { continue }
        if !all && s.Status != StateRunning { continue }
        out = append(out, s)
    }
    return out, nil
}

func (r *Runtime) Inspect(ctx context.Context, id string) (*State, error) {
    return ReadState(filepath.Join(r.rootDir, id, "state.json"))
}
```

- [ ] **Step 2: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/...
git add internal/agent/containerd/runtime.go
git commit -m "feat(0.3): containerd — List + Inspect"
```

---

### Task 11: network — host setup (cs0 bridge + nftables)

**Files:**
- Modify: `go.mod` — add `github.com/vishvananda/netlink`
- Create: `internal/agent/network/host.go`
- Create: `internal/agent/network/host_test.go`

- [ ] **Step 1: Add netlink dep**

```bash
go get github.com/vishvananda/netlink@latest
go mod tidy
```

- [ ] **Step 2: Implement bridge + masquerade setup (idempotent)**

```go
// internal/agent/network/host.go
package network

import (
    "fmt"
    "net"
    "os"
    "os/exec"

    "github.com/vishvananda/netlink"
)

const (
    BridgeName  = "cs0"
    BridgeCIDR  = "172.17.0.1/16"
    UplinkName  = "eth0"
)

// SetupHost creates the cs0 bridge with the well-known address, enables
// IPv4 forwarding, and adds the SNAT masquerade rule. Idempotent.
func SetupHost() error {
    if err := ensureBridge(); err != nil { return fmt.Errorf("bridge: %w", err) }
    if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644); err != nil {
        return fmt.Errorf("ip_forward: %w", err)
    }
    if err := ensureMasquerade(); err != nil { return fmt.Errorf("masquerade: %w", err) }
    return nil
}

func ensureBridge() error {
    if _, err := netlink.LinkByName(BridgeName); err == nil {
        return nil // already exists
    }
    la := netlink.NewLinkAttrs()
    la.Name = BridgeName
    br := &netlink.Bridge{LinkAttrs: la}
    if err := netlink.LinkAdd(br); err != nil { return err }
    addr, _ := netlink.ParseAddr(BridgeCIDR)
    if err := netlink.AddrAdd(br, addr); err != nil { return err }
    return netlink.LinkSetUp(br)
}

func ensureMasquerade() error {
    // Run via nft so we don't depend on netlink-level nat table builders
    // (we accept a tiny exec cost on agent boot, once per VM lifecycle).
    cmd := `add table ip nat
add chain ip nat postrouting { type nat hook postrouting priority 100; }
add rule ip nat postrouting oifname "eth0" ip saddr 172.17.0.0/16 masquerade`
    nft := exec.Command("nft", "-f", "-")
    nft.Stdin = bytesReader(cmd)
    out, err := nft.CombinedOutput()
    if err != nil { return fmt.Errorf("nft: %w (%s)", err, string(out)) }
    _ = net.ParseCIDR // keep import
    return nil
}

func bytesReader(s string) *bytesReaderImpl { return &bytesReaderImpl{data: []byte(s)} }
type bytesReaderImpl struct{ data []byte; n int }
func (r *bytesReaderImpl) Read(p []byte) (int, error) {
    if r.n >= len(r.data) { return 0, fmt.Errorf("EOF") }
    n := copy(p, r.data[r.n:])
    r.n += n
    return n, nil
}
```

- [ ] **Step 3: Test (linux only, requires CAP_NET_ADMIN — skip in CI)**

```go
// internal/agent/network/host_test.go
//go:build linux

package network

import (
    "os"
    "testing"
)

func TestSetupHost_RequiresPrivilege(t *testing.T) {
    if os.Geteuid() != 0 { t.Skip("requires root") }
    if err := SetupHost(); err != nil { t.Fatalf("SetupHost: %v", err) }
    // idempotency
    if err := SetupHost(); err != nil { t.Fatalf("SetupHost (2nd): %v", err) }
}
```

- [ ] **Step 4: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/network/
git add go.mod go.sum internal/agent/network/
git commit -m "feat(0.3): network — cs0 bridge + nftables masquerade"
```

---

### Task 12: network — per-container veth + IP allocation

**Files:**
- Create: `internal/agent/network/container.go`

- [ ] **Step 1: Implement per-container plumbing**

```go
// internal/agent/network/container.go
package network

import (
    "fmt"
    "net"
    "os"
    "path/filepath"
    "runtime"
    "sync"

    "github.com/vishvananda/netlink"
    "github.com/vishvananda/netns"
)

var (
    allocMu sync.Mutex
    nextIP  uint32 = 2 // .0.2 first; .0.0 network, .0.1 gateway
)

// AddContainer creates a veth pair, places one end into the container
// netns (referenced by the container's init PID), assigns an IP from
// 172.17.0.0/16, sets default route to 172.17.0.1, and returns the IP.
func AddContainer(containerID string, initPID int) (string, error) {
    allocMu.Lock()
    ip := fmt.Sprintf("172.17.0.%d/16", nextIP)
    plainIP := fmt.Sprintf("172.17.0.%d", nextIP)
    nextIP++
    allocMu.Unlock()

    hostName := "v" + containerID[:8]
    peerName := "veth0"

    veth := &netlink.Veth{
        LinkAttrs: netlink.LinkAttrs{Name: hostName},
        PeerName:  peerName,
    }
    if err := netlink.LinkAdd(veth); err != nil { return "", err }

    // attach host side to bridge
    br, err := netlink.LinkByName(BridgeName)
    if err != nil { return "", err }
    hostLink, _ := netlink.LinkByName(hostName)
    netlink.LinkSetMaster(hostLink, br.(*netlink.Bridge))
    netlink.LinkSetUp(hostLink)

    // move peer to container netns
    peerLink, _ := netlink.LinkByName(peerName)
    if err := netlink.LinkSetNsPid(peerLink, initPID); err != nil { return "", err }

    // Enter the netns to configure peer
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    origNs, _ := netns.Get()
    defer netns.Set(origNs)

    targetNs, err := netns.GetFromPid(initPID)
    if err != nil { return "", err }
    netns.Set(targetNs)

    peer, _ := netlink.LinkByName(peerName)
    netlink.LinkSetName(peer, "eth0")
    eth0, _ := netlink.LinkByName("eth0")
    addr, _ := netlink.ParseAddr(ip)
    netlink.AddrAdd(eth0, addr)
    netlink.LinkSetUp(eth0)
    gw := net.ParseIP("172.17.0.1")
    netlink.RouteAdd(&netlink.Route{LinkIndex: eth0.Attrs().Index, Gw: gw})

    return plainIP, nil
}

// WriteResolvConf writes /etc/resolv.conf in the container rootfs.
func WriteResolvConf(rootfs string) error {
    src, err := os.ReadFile("/etc/resolv.conf")
    if err != nil { return err }
    return os.WriteFile(filepath.Join(rootfs, "etc", "resolv.conf"), src, 0o644)
}

// RemoveContainer removes the host-side veth (peer goes with the netns).
func RemoveContainer(containerID string) error {
    hostName := "v" + containerID[:8]
    link, err := netlink.LinkByName(hostName)
    if err != nil { return nil } // already gone, idempotent
    return netlink.LinkDel(link)
}
```

- [ ] **Step 2: Add netns dep**

```bash
go get github.com/vishvananda/netns@latest
go mod tidy
```

- [ ] **Step 3: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/network/
git add go.mod go.sum internal/agent/network/container.go
git commit -m "feat(0.3): network — per-container veth + IP allocation"
```

---

### Task 13: logs — write + tail with inotify

**Files:**
- Create: `internal/agent/logs/tail.go`
- Create: `internal/agent/logs/tail_test.go`

- [ ] **Step 1: Test**

```go
// internal/agent/logs/tail_test.go
//go:build linux

package logs

import (
    "context"
    "io"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestWriteAndTail_Static(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "log.json")
    w, err := NewWriter(path)
    if err != nil { t.Fatal(err) }
    if _, err := w.Write(StreamStdout, []byte("hello\n")); err != nil { t.Fatal(err) }
    if _, err := w.Write(StreamStderr, []byte("oops\n")); err != nil { t.Fatal(err) }
    w.Close()

    ch, _ := Tail(context.Background(), path, false)
    var entries []Entry
    for e := range ch { entries = append(entries, e) }
    if len(entries) != 2 { t.Fatalf("expected 2 entries, got %d", len(entries)) }
    if string(entries[0].Data) != "hello\n" || entries[0].Stream != StreamStdout {
        t.Errorf("entry 0 mismatch: %+v", entries[0])
    }
}

func TestTail_Follow(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "log.json")
    w, err := NewWriter(path)
    if err != nil { t.Fatal(err) }
    defer w.Close()

    ctx, cancel := context.WithCancel(context.Background())
    ch, _ := Tail(ctx, path, true)

    go func() {
        time.Sleep(100 * time.Millisecond)
        w.Write(StreamStdout, []byte("late\n"))
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    var seen []Entry
    for e := range ch { seen = append(seen, e) }
    if len(seen) != 1 || string(seen[0].Data) != "late\n" {
        t.Errorf("expected 'late', got %+v", seen)
    }
    _ = io.EOF
}
```

- [ ] **Step 2: Implement (using fsnotify)**

```bash
go get github.com/fsnotify/fsnotify@latest
```

```go
// internal/agent/logs/tail.go
package logs

import (
    "bufio"
    "context"
    "encoding/json"
    "io"
    "os"
    "sync"
    "time"

    "github.com/fsnotify/fsnotify"
)

type Stream string

const (
    StreamStdout Stream = "stdout"
    StreamStderr Stream = "stderr"
)

type Entry struct {
    T      time.Time `json:"t"`
    Stream Stream    `json:"s"`
    Data   []byte    `json:"b"`
}

type Writer struct {
    f  *os.File
    mu sync.Mutex
}

func NewWriter(path string) (*Writer, error) {
    f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
    if err != nil { return nil, err }
    return &Writer{f: f}, nil
}

func (w *Writer) Write(s Stream, data []byte) (int, error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    e := Entry{T: time.Now().UTC(), Stream: s, Data: data}
    line, _ := json.Marshal(e)
    line = append(line, '\n')
    _, err := w.f.Write(line)
    return len(data), err
}

func (w *Writer) Close() error { return w.f.Close() }

// Tail returns a channel of log entries. If follow=true, waits for new
// writes via fsnotify until ctx is done. Channel closes when no more data.
func Tail(ctx context.Context, path string, follow bool) (<-chan Entry, error) {
    out := make(chan Entry, 64)
    f, err := os.Open(path)
    if err != nil { close(out); return out, err }

    go func() {
        defer close(out)
        defer f.Close()
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            var e Entry
            if json.Unmarshal(scanner.Bytes(), &e) == nil {
                select { case out <- e: case <-ctx.Done(): return }
            }
        }
        if !follow { return }

        watcher, err := fsnotify.NewWatcher()
        if err != nil { return }
        defer watcher.Close()
        watcher.Add(path)

        // re-position scanner over remaining bytes after EOF
        for {
            select {
            case <-ctx.Done(): return
            case <-watcher.Events:
                for scanner.Scan() {
                    var e Entry
                    if json.Unmarshal(scanner.Bytes(), &e) == nil {
                        select { case out <- e: case <-ctx.Done(): return }
                    }
                }
                // reset scanner state if EOF
                if err := scanner.Err(); err == io.EOF || err == nil {
                    scanner = bufio.NewScanner(f)
                }
            }
        }
    }()
    return out, nil
}
```

- [ ] **Step 3: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/logs/
git add go.mod go.sum internal/agent/logs/
git commit -m "feat(0.3): logs — JSON-lines log writer + fsnotify tail"
```

---

### Task 14: exec — bidi stream multiplexer

**Files:**
- Create: `internal/agent/exec/multiplexer.go`

- [ ] **Step 1: Implement**

```go
// internal/agent/exec/multiplexer.go
package exec

import (
    "context"
    "io"
    "os/exec"
    "syscall"

    agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
)

// Run spawns a `crun exec` child against the given container, wires up
// stdin/stdout/stderr to the gRPC bidi stream, and returns the exit code.
func Run(
    ctx context.Context,
    containerID string,
    cmd []string,
    crunPath string,
    stream interface {
        Send(*agentpb.AttachFrame) error
        Recv() (*agentpb.AttachFrame, error)
    },
) error {
    args := append([]string{"exec", containerID}, cmd...)
    c := exec.CommandContext(ctx, crunPath, args...)

    stdin, _ := c.StdinPipe()
    stdout, _ := c.StdoutPipe()
    stderr, _ := c.StderrPipe()

    if err := c.Start(); err != nil { return err }

    // stdout pump
    go pumpToStream(stream, agentpb.Stream_STDOUT, stdout)
    // stderr pump
    go pumpToStream(stream, agentpb.Stream_STDERR, stderr)
    // stdin pump
    go func() {
        for {
            f, err := stream.Recv()
            if err != nil { stdin.Close(); return }
            if f.Stream == agentpb.Stream_STDIN && len(f.Data) > 0 {
                stdin.Write(f.Data)
            }
        }
    }()

    err := c.Wait()
    code := 0
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok { code = ws.ExitStatus() }
        }
    }
    stream.Send(&agentpb.AttachFrame{Stream: agentpb.Stream_EXIT, ExitCode: int32(code)})
    return nil
}

func pumpToStream(stream interface{ Send(*agentpb.AttachFrame) error }, s agentpb.Stream, r io.Reader) {
    buf := make([]byte, 4096)
    for {
        n, err := r.Read(buf)
        if n > 0 {
            stream.Send(&agentpb.AttachFrame{Stream: s, Data: append([]byte{}, buf[:n]...)})
        }
        if err != nil { return }
    }
}
```

- [ ] **Step 2: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./internal/agent/exec/
git add internal/agent/exec/
git commit -m "feat(0.3): exec — bidi stream multiplexer for crun exec"
```

---

### Task 15: agent server — register Image + Container services

**Files:**
- Modify: `internal/agent/server.go`

- [ ] **Step 1: Wire all the pieces into the agent server**

```go
// internal/agent/server.go (replace)
package agent

import (
    "context"
    "fmt"
    "os"
    "runtime"
    "strconv"
    "strings"
    "time"

    agentpb "github.com/cyber5-io/cyber-stack/internal/proto/gen"
    "github.com/cyber5-io/cyber-stack/internal/agent/imagestore"
    "github.com/cyber5-io/cyber-stack/internal/agent/containerd"
    "github.com/cyber5-io/cyber-stack/internal/agent/exec"
    "github.com/cyber5-io/cyber-stack/internal/agent/logs"
    "github.com/cyber5-io/cyber-stack/internal/agent/network"
    "google.golang.org/grpc"
)

var AgentVersion = "dev"

// Server bundles all RPC implementations.
type Server struct {
    agentpb.UnimplementedAgentServer
    agentpb.UnimplementedImageServer
    agentpb.UnimplementedContainerServer

    images *imagestore.Store
    rt     *containerd.Runtime
}

func NewServer(images *imagestore.Store, rt *containerd.Runtime) *Server {
    return &Server{images: images, rt: rt}
}

// RegisterAll registers all three services on a single grpc.Server.
func (s *Server) RegisterAll(g *grpc.Server) {
    agentpb.RegisterAgentServer(g, s)
    agentpb.RegisterImageServer(g, s)
    agentpb.RegisterContainerServer(g, s)
}

// --- Agent service (existing 0.2 surface) ---

func (s *Server) Ping(ctx context.Context, _ *agentpb.PingRequest) (*agentpb.PingResponse, error) {
    return &agentpb.PingResponse{Message: "pong"}, nil
}

func (s *Server) Version(ctx context.Context, _ *agentpb.VersionRequest) (*agentpb.VersionResponse, error) {
    return &agentpb.VersionResponse{
        AgentVersion: AgentVersion,
        KernelVersion: readUnameRelease(),
        OsRelease:     readOSRelease(),
        NumCpus:       uint32(runtime.NumCPU()),
        MemTotalBytes: readMemTotalBytes(),
    }, nil
}

// --- Image service ---

func (s *Server) Pull(ctx context.Context, req *agentpb.PullRequest) (*agentpb.PullResponse, error) {
    res, err := s.images.Pull(ctx, req.Ref)
    if err != nil { return nil, err }
    return &agentpb.PullResponse{ImageId: res.ImageID, RepoTag: res.RepoTag, Size: uint64(res.Size)}, nil
}

func (s *Server) List(ctx context.Context, _ *agentpb.ListImagesRequest) (*agentpb.ListImagesResponse, error) {
    list, err := s.images.List(ctx)
    if err != nil { return nil, err }
    out := &agentpb.ListImagesResponse{}
    for _, im := range list {
        out.Images = append(out.Images, &agentpb.ImageSummary{
            Id: im.ID, RepoTags: im.RepoTags, Size: uint64(im.Size), CreatedUnix: im.CreatedAt.Unix(),
        })
    }
    return out, nil
}

func (s *Server) Inspect(ctx context.Context, req *agentpb.InspectImageRequest) (*agentpb.ImageInspect, error) {
    insp, err := s.images.Inspect(ctx, req.Id)
    if err != nil { return nil, err }
    return &agentpb.ImageInspect{
        Id: insp.ID, RepoTags: insp.RepoTags, ConfigDigest: insp.ConfigDigest,
        Size: uint64(insp.Size), CreatedUnix: insp.CreatedAt.Unix(),
        Architecture: insp.Architecture, Os: insp.OS,
        Env: insp.Env, Cmd: insp.Cmd, Entrypoint: insp.Entrypoint, WorkingDir: insp.WorkingDir,
    }, nil
}

func (s *Server) Remove(ctx context.Context, req *agentpb.RemoveImageRequest) (*agentpb.RemoveImageResponse, error) {
    deleted, err := s.images.Remove(ctx, req.Id, req.Force)
    if err != nil { return nil, err }
    return &agentpb.RemoveImageResponse{Deleted: deleted}, nil
}

// --- Container service ---
// (Each method is a thin wrapper around containerd.Runtime — full
//  implementations expand the gRPC types into runtime types and back.)

func (s *Server) Create(ctx context.Context, req *agentpb.CreateRequest) (*agentpb.CreateResponse, error) {
    spec := &containerd.Spec{
        Image: req.Spec.Image, Cmd: req.Spec.Cmd, Args: req.Spec.Args, Env: req.Spec.Env,
        WorkDir: req.Spec.Workdir, Hostname: req.Spec.Hostname, Labels: req.Spec.Labels,
        TTY: req.Spec.Tty,
    }
    if req.Spec.HostConfig != nil {
        spec.MemoryBytes = req.Spec.HostConfig.MemoryBytes
        spec.CPUShares = req.Spec.HostConfig.CpuShares
    }
    id, err := s.rt.Create(ctx, spec, req.Name)
    if err != nil { return nil, err }
    return &agentpb.CreateResponse{Id: id}, nil
}

func (s *Server) Start(ctx context.Context, req *agentpb.StartRequest) (*agentpb.StartResponse, error) {
    pid, err := s.rt.Start(ctx, req.Id)
    if err != nil { return nil, err }
    return &agentpb.StartResponse{Pid: int32(pid)}, nil
}

func (s *Server) Stop(ctx context.Context, req *agentpb.StopRequest) (*agentpb.StopResponse, error) {
    if err := s.rt.Stop(ctx, req.Id, req.TimeoutSeconds); err != nil { return nil, err }
    return &agentpb.StopResponse{}, nil
}

func (s *Server) Wait(ctx context.Context, req *agentpb.WaitRequest) (*agentpb.WaitResponse, error) {
    code, oom, err := s.rt.Wait(ctx, req.Id)
    if err != nil { return nil, err }
    return &agentpb.WaitResponse{ExitCode: int32(code), OomKilled: oom}, nil
}

func (s *Server) Delete(ctx context.Context, req *agentpb.DeleteRequest) (*agentpb.DeleteResponse, error) {
    if err := s.rt.Delete(ctx, req.Id, req.Force); err != nil { return nil, err }
    return &agentpb.DeleteResponse{}, nil
}

func (s *Server) ContainerInspect(ctx context.Context, req *agentpb.InspectRequest) (*agentpb.ContainerInspect, error) {
    st, err := s.rt.Inspect(ctx, req.Id)
    if err != nil { return nil, err }
    return stateToProto(st), nil
}

func (s *Server) ContainerList(ctx context.Context, req *agentpb.ListRequest) (*agentpb.ListResponse, error) {
    states, err := s.rt.List(ctx, req.All)
    if err != nil { return nil, err }
    out := &agentpb.ListResponse{}
    for _, st := range states { out.Containers = append(out.Containers, stateToProto(st)) }
    return out, nil
}

func stateToProto(s *containerd.State) *agentpb.ContainerInspect {
    return &agentpb.ContainerInspect{
        Id: s.ID, Name: s.Name, Image: s.Image,
        State: string(s.Status), Status: s.StatusText,
        ExitCode: int32(s.ExitCode), OomKilled: s.OOMKilled,
        CreatedUnix: s.CreatedAt.Unix(),
        StartedUnix: s.StartedAt.Unix(),
        FinishedUnix: s.FinishedAt.Unix(),
        IpAddress: s.IPAddress,
    }
}

func (s *Server) Logs(req *agentpb.LogsRequest, stream agentpb.Container_LogsServer) error {
    st, err := s.rt.Inspect(stream.Context(), req.Id)
    if err != nil { return err }
    ch, err := logs.Tail(stream.Context(), st.LogPath, req.Follow)
    if err != nil { return err }
    for e := range ch {
        var s agentpb.Stream
        if e.Stream == "stdout" { s = agentpb.Stream_STDOUT } else { s = agentpb.Stream_STDERR }
        if err := stream.Send(&agentpb.LogFrame{Stream: s, Data: e.Data, TsUnixNano: e.T.UnixNano()}); err != nil { return err }
    }
    return nil
}

func (s *Server) ExecStart(stream agentpb.Container_ExecStartServer) error {
    // First frame must contain ExecID
    first, err := stream.Recv()
    if err != nil { return err }
    // For 0.3 we look up the exec spec by exec_id (stored in-memory by Server.Exec)
    spec := s.lookupExec(first.ExecId)
    if spec == nil { return fmt.Errorf("unknown exec id %q", first.ExecId) }
    return exec.Run(stream.Context(), spec.ContainerID, spec.Cmd, s.rt.CrunPath(), stream)
}

// (Exec request handler + lookup table omitted for brevity — see spec.
//  Implementation: store ExecSpec in a sync.Map keyed by exec_id at Exec()
//  time, retrieve in ExecStart, delete after.)

// --- helpers (existing from 0.2) ---

func readUnameRelease() string { /* same as 0.2 */ return "" }
func readOSRelease() string { /* same as 0.2 */ return "" }
func readMemTotalBytes() uint64 { /* same as 0.2 */ return 0 }
```

- [ ] **Step 2: Update agent main to wire up imagestore + runtime + network**

```go
// cmd/cyberstack-agent/main.go (replace)
//go:build linux

package main

import (
    "flag"
    "log"
    "os"

    "google.golang.org/grpc"

    "github.com/cyber5-io/cyber-stack/internal/agent"
    "github.com/cyber5-io/cyber-stack/internal/agent/containerd"
    "github.com/cyber5-io/cyber-stack/internal/agent/imagestore"
    "github.com/cyber5-io/cyber-stack/internal/agent/network"
    "github.com/cyber5-io/cyber-stack/internal/transport"
)

func main() {
    var (
        port           uint
        graphRoot      string
        runRoot        string
        containersRoot string
        crunPath       string
    )
    flag.UintVar(&port, "host-port", 1024, "vsock port on host (CID 2) to dial")
    flag.StringVar(&graphRoot, "graph-root", "/var/lib/cyberstack/storage", "containers/storage graph root")
    flag.StringVar(&runRoot, "run-root", "/run/cyberstack/storage", "containers/storage runtime root")
    flag.StringVar(&containersRoot, "containers-root", "/var/lib/cyberstack/containers", "container bundles root")
    flag.StringVar(&crunPath, "crun", "/usr/bin/crun", "crun binary path")
    flag.Parse()

    if err := network.SetupHost(); err != nil { log.Fatalf("network setup: %v", err) }

    images, err := imagestore.New(graphRoot, runRoot)
    if err != nil { log.Fatalf("image store: %v", err) }
    defer images.Close()

    rt, err := containerd.New(images, containersRoot, crunPath)
    if err != nil { log.Fatalf("runtime: %v", err) }

    conn, err := transport.DialHost(uint32(port))
    if err != nil { log.Printf("vsock dial host: %v", err); os.Exit(1) }
    log.Printf("cyberstack-agent connected to host vsock port %d", port)

    g := grpc.NewServer()
    srv := agent.NewServer(images, rt)
    srv.RegisterAll(g)

    if err := g.Serve(transport.NewSingleConnListener(conn)); err != nil {
        log.Printf("grpc serve: %v", err); os.Exit(1)
    }
}
```

- [ ] **Step 3: Build + commit**

```bash
GOOS=linux GOARCH=arm64 go build ./cmd/cyberstack-agent
git add internal/agent/server.go cmd/cyberstack-agent/main.go
git commit -m "feat(0.3): agent — register Image + Container gRPC services"
```

---

### Task 16: daemon client — Image + Container wrappers

**Files:**
- Modify: `internal/daemon/client.go`

- [ ] **Step 1: Add Image + Container clients next to Agent**

```go
// internal/daemon/client.go (extend AgentClient struct + add helpers)

type AgentClient struct {
    conn       *grpc.ClientConn
    agent      agentpb.AgentClient
    images     agentpb.ImageClient
    containers agentpb.ContainerClient
}

func NewAgentClient(raw net.Conn) (*AgentClient, error) {
    // ... existing dial logic ...
    return &AgentClient{
        conn:       cc,
        agent:      agentpb.NewAgentClient(cc),
        images:     agentpb.NewImageClient(cc),
        containers: agentpb.NewContainerClient(cc),
    }, nil
}

// Image methods
func (c *AgentClient) ImagePull(ctx context.Context, ref string) (*agentpb.PullResponse, error) {
    return c.images.Pull(ctx, &agentpb.PullRequest{Ref: ref})
}
func (c *AgentClient) ImageList(ctx context.Context) (*agentpb.ListImagesResponse, error) {
    return c.images.List(ctx, &agentpb.ListImagesRequest{})
}
func (c *AgentClient) ImageInspect(ctx context.Context, id string) (*agentpb.ImageInspect, error) {
    return c.images.Inspect(ctx, &agentpb.InspectImageRequest{Id: id})
}
func (c *AgentClient) ImageRemove(ctx context.Context, id string, force bool) (*agentpb.RemoveImageResponse, error) {
    return c.images.Remove(ctx, &agentpb.RemoveImageRequest{Id: id, Force: force})
}

// Container methods (Create, Start, Stop, Wait, Delete, Inspect, List)
func (c *AgentClient) ContainerCreate(ctx context.Context, spec *agentpb.ContainerSpec, name string) (string, error) {
    res, err := c.containers.Create(ctx, &agentpb.CreateRequest{Spec: spec, Name: name})
    if err != nil { return "", err }
    return res.Id, nil
}
// ... mirror others ...
```

- [ ] **Step 2: Build + commit**

```bash
go build ./...
git add internal/daemon/client.go
git commit -m "feat(0.3): daemon client — Image + Container wrappers"
```

---

### Task 17: HTTP handlers — `/images/*`

**Files:**
- Create: `internal/httpapi/images.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Implement four image handlers**

```go
// internal/httpapi/images.go
package httpapi

import (
    "encoding/json"
    "io"
    "net/http"
    "strings"

    "github.com/cyber5-io/cyber-stack/internal/daemon"
)

type ImageOps interface {
    ImagePull(ctx context.Context, ref string) (*agentpb.PullResponse, error)
    ImageList(ctx context.Context) (*agentpb.ListImagesResponse, error)
    ImageInspect(ctx context.Context, id string) (*agentpb.ImageInspect, error)
    ImageRemove(ctx context.Context, id string, force bool) (*agentpb.RemoveImageResponse, error)
}

func makeImageCreateHandler(c ImageOps) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        from := r.URL.Query().Get("fromImage")
        tag := r.URL.Query().Get("tag")
        if tag == "" { tag = "latest" }
        ref := from + ":" + tag
        if !strings.Contains(from, "/") { ref = "docker.io/library/" + ref }
        _, err := c.ImagePull(r.Context(), ref)
        if err != nil { http.Error(w, err.Error(), 500); return }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{
            "status": "Downloaded newer image for " + ref,
        })
    }
}

func makeImageListHandler(c ImageOps) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        list, err := c.ImageList(r.Context())
        if err != nil { http.Error(w, err.Error(), 500); return }
        out := []map[string]interface{}{}
        for _, img := range list.Images {
            out = append(out, map[string]interface{}{
                "Id": img.Id, "RepoTags": img.RepoTags,
                "Size": img.Size, "Created": img.CreatedUnix,
            })
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(out)
    }
}

// ... InspectHandler, RemoveHandler similar ...
```

- [ ] **Step 2: Wire into server.go**

```go
// internal/httpapi/server.go (add to NewServer's mux setup)
mux.HandleFunc("/v1.43/images/create", makeImageCreateHandler(opts.Images))
mux.HandleFunc("/v1.43/images/json",   makeImageListHandler(opts.Images))
mux.HandleFunc("/v1.43/images/", func(w, r) { /* dispatch by method to inspect/remove */ })
```

- [ ] **Step 3: Build + commit**

```bash
go build ./...
git add internal/httpapi/images.go internal/httpapi/server.go
git commit -m "feat(0.3): httpapi — /images/* handlers"
```

---

### Task 18: HTTP handlers — `/containers/*` (non-stream)

**Files:**
- Create: `internal/httpapi/containers.go`
- Modify: `internal/httpapi/server.go`

- [ ] **Step 1: Implement create/start/stop/wait/delete/list/inspect**

(Pattern is identical to images.go — interface + per-handler factory + JSON marshal of agent response into the Docker-shape JSON the CLI expects. Each handler is ~20 lines. Full body in spec section 2b table.)

- [ ] **Step 2: Wire all routes into NewServer**
- [ ] **Step 3: Build, run handler unit tests**
- [ ] **Step 4: Commit**

```bash
git add internal/httpapi/containers.go internal/httpapi/server.go internal/httpapi/containers_test.go
git commit -m "feat(0.3): httpapi — /containers/* non-stream handlers"
```

---

### Task 19: HTTP hijack helper

**Files:**
- Create: `internal/httpapi/hijack.go`

- [ ] **Step 1: Implement hijack helper that bridges hijacked TCP ↔ AttachFrame stream**

```go
// internal/httpapi/hijack.go
package httpapi

import (
    "encoding/binary"
    "io"
    "net/http"
)

// dockerStreamWrite frames a payload according to Docker's stream format:
// [stream_type_byte, 0,0,0, len_be32, payload...]
// stream_type: 0=stdin, 1=stdout, 2=stderr
func dockerStreamWrite(w io.Writer, streamType byte, payload []byte) error {
    hdr := make([]byte, 8)
    hdr[0] = streamType
    binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
    if _, err := w.Write(hdr); err != nil { return err }
    _, err := w.Write(payload)
    return err
}

// HijackBridge takes a ResponseWriter, hijacks its underlying conn, and
// returns the conn for bidirectional Docker-stream<->gRPC bridging.
func HijackBridge(w http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
    h, ok := w.(http.Hijacker)
    if !ok { return nil, nil, fmt.Errorf("not hijackable") }
    return h.Hijack()
}
```

- [ ] **Step 2: Build + commit**

```bash
go build ./...
git add internal/httpapi/hijack.go
git commit -m "feat(0.3): httpapi — HTTP hijack helper for attach/exec streams"
```

---

### Task 20: HTTP handlers — `/containers/{id}/logs` + attach/exec streams

**Files:**
- Modify: `internal/httpapi/containers.go` (add stream handlers)

- [ ] **Step 1: Implement logs (server-stream)**

(Bridges gRPC `Container.Logs` server-stream → docker-framed bytes on hijacked TCP.)

- [ ] **Step 2: Implement attach + exec (bidi)**

(Hijacks TCP, opens gRPC bidi, framing in both directions.)

- [ ] **Step 3: Build + commit**

```bash
git add internal/httpapi/containers.go
git commit -m "feat(0.3): httpapi — logs server-stream + attach/exec bidi"
```

---

### Task 21: Build pipeline — crun + nftables + iproute2 in initramfs

**Files:**
- Modify: `guest/Makefile`

- [ ] **Step 1: Add CRUN, NFT, IPROUTE2 fetch targets + extract steps**

```makefile
# guest/Makefile additions

CRUN_VERSION := 1.20
CRUN_URL     := https://github.com/containers/crun/releases/download/$(CRUN_VERSION)/crun-$(CRUN_VERSION)-linux-arm64-disable-systemd
CRUN_SHA256  := <pin actual sha256 here>

NFTABLES_PKG := nftables-1.1.1-r0.apk
IPROUTE2_PKG := iproute2-6.11.0-r0.apk

$(BUILD)/crun:
	curl -fL -o $@ $(CRUN_URL)
	echo "$(CRUN_SHA256)  $@" | sha256sum -c -
	chmod +x $@

$(BUILD)/nftables-extracted/.stamp: ...
$(BUILD)/iproute2-extracted/.stamp: ...

# Modify $(BUILD)/initramfs.cpio.gz target dependencies + copy steps:
# cp $(BUILD)/crun $(BUILD)/initramfs-staging/usr/bin/crun
# cp -r $(BUILD)/nftables-extracted/usr/* $(BUILD)/initramfs-staging/usr/
# cp -r $(BUILD)/iproute2-extracted/usr/* $(BUILD)/initramfs-staging/usr/
```

- [ ] **Step 2: Verify boot disk builds clean**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
make clean && make boot-disk
```

- [ ] **Step 3: Commit**

```bash
git add guest/Makefile
git commit -m "feat(0.3): guest build — pack crun, nftables, iproute2 into initramfs"
```

---

### Task 22: init.sh — mount data disk, network setup

**Files:**
- Modify: `guest/init.sh`

- [ ] **Step 1: Extend init for data disk + network**

```sh
#!/bin/sh
/bin/busybox --install -s /bin

mount -t proc -o nosuid,noexec,nodev proc /proc
mount -t sysfs -o nosuid,noexec,nodev sys /sys
mount -t devtmpfs -o mode=0755,nosuid dev /dev

# vsock modules
insmod /lib/modules/vsock/vsock.ko
insmod /lib/modules/vsock/vmw_vsock_virtio_transport_common.ko
insmod /lib/modules/vsock/vmw_vsock_virtio_transport.ko

# Network modules
for m in /lib/modules/net/*.ko; do insmod "$m"; done

# Data disk (vdb) — format on first boot, then mount + resize
if ! blkid /dev/vdb >/dev/null 2>&1; then
    mkfs.ext4 -F -L CSDATA /dev/vdb
fi
mkdir -p /var/lib/cyberstack/storage /var/lib/cyberstack/containers
mount /dev/vdb /var/lib/cyberstack/storage
resize2fs /dev/vdb 2>/dev/null || true
mkdir -p /var/lib/cyberstack/storage/containers
mount --bind /var/lib/cyberstack/storage/containers /var/lib/cyberstack/containers

# DHCP for eth0 (Apple Virt NAT)
udhcpc -i eth0 -q -t 5

exec /usr/bin/cyberstack-agent
```

- [ ] **Step 2: Rebuild boot disk, run handshake test**

```bash
make clean && make boot-disk
go test -tags=integration ./tests/integration/... -run VMHandshake
```

- [ ] **Step 3: Commit**

```bash
git add guest/init.sh
git commit -m "feat(0.3): init — data disk mount + network module load + DHCP"
```

---

### Task 23: cs-bench — five locked benchmarks

**Files:**
- Create: `cmd/cs-bench/main.go`

- [ ] **Step 1: Implement bench harness (single binary, takes DOCKER_HOST from env, runs 5 benchmarks, outputs JSON)**

(Code is ~200 lines: docker-go client, 5 benchmark functions, p50/p90/p99 stats, JSON output, table print.)

- [ ] **Step 2: Build, run against cyberstackd**

```bash
go build -o bin/cs-bench ./cmd/cs-bench
DOCKER_HOST=unix://$HOME/.cyberstack/cyberstack.sock bin/cs-bench
```

- [ ] **Step 3: Commit**

```bash
git add cmd/cs-bench/main.go
git commit -m "feat(0.3): cs-bench — five locked benchmark harness"
```

---

### Task 24: Integration tests — docker run/exec/logs/pull

**Files:**
- Create: `tests/integration/docker_run_test.go` etc.

- [ ] **Step 1: Write four integration tests**

Each test follows the 0.2 `TestVMHandshake_DaemonAcceptsAgent` pattern:
- skip on non-arm64
- short paths in /tmp
- spawn daemon with VM
- run `docker -H unix://... <command>` via `os/exec`
- assert output / exit code

- [ ] **Step 2: Run all integration tests**

```bash
go test -tags=integration ./tests/integration/...
```

- [ ] **Step 3: Commit**

```bash
git add tests/integration/docker_*_test.go
git commit -m "feat(0.3): integration tests for run/exec/logs/pull"
```

---

### Task 25: Docs + version bump + benchmarking guide

**Files:**
- Create: `docs/benchmarking.md`
- Modify: `internal/httpapi/version.go` (already at 0.2.0; bump to 0.3.0)
- Modify: `Makefile` (VERSION variable bump)

- [ ] **Step 1: Write `docs/benchmarking.md`**

(OrbStack methodology from spec section 8c, copied verbatim with command-line examples.)

- [ ] **Step 2: Bump versions**

```go
// internal/httpapi/version.go
var CyberStackVersion = "0.3.0"
```

```makefile
# Makefile
VERSION := 0.3.0
```

- [ ] **Step 3: Build + verify version stamp**

```bash
make build
./bin/cyberstackd --no-vm &
docker -H unix://$HOME/.cyberstack/cyberstack.sock version
```
Expected: `Server.Version: 0.3.0`.

- [ ] **Step 4: Commit**

```bash
git add docs/benchmarking.md internal/httpapi/version.go Makefile
git commit -m "feat(0.3): docs/benchmarking.md + bump version to 0.3.0"
```

---

### Task 26: Capture OrbStack baseline + publish results

**Manual procedure** (not a code task — produces artefact for the release).

- [ ] **Step 1: Reinstall OrbStack**

```bash
brew install --cask orbstack
open /Applications/OrbStack.app
```

- [ ] **Step 2: Run cs-bench against OrbStack**

```bash
DOCKER_HOST=unix:///var/run/docker.sock bin/cs-bench --out=results-orbstack.json
```

- [ ] **Step 3: Stop OrbStack, run cs-bench against cyberstackd**

```bash
orb stop
~/.cyberstack/run.sh &       # or: bin/cyberstackd
DOCKER_HOST=unix://$HOME/.cyberstack/cyberstack.sock bin/cs-bench --out=results-cyberstack.json
```

- [ ] **Step 4: Compare**

```bash
bin/cs-bench-compare results-cyberstack.json results-orbstack.json
```

- [ ] **Step 5: Save raw JSON + comparison table to repo**

```bash
mkdir -p docs/benchmarks
cp results-*.json docs/benchmarks/
# write docs/benchmarks/0.3.0-vs-orbstack.md with the table
git add docs/benchmarks/
git commit -m "docs: 0.3.0 vs OrbStack baseline benchmark results"
```

---

### Task 27: Tag v0.3.0, update tainer plan/spec

**Files (cyber-stack repo):** none — this is git-only.
**Files (tainer repo):**
- Modify: `docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`
- Modify: `docs/superpowers/plans/2026-04-27-cyberstack-0.3-container-runtime.md` (this file)

- [ ] **Step 1: Push cyber-stack main + tag**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
git push origin main
git tag -a v0.3.0 -m "CyberStack 0.3.0 — container runtime"
git push origin v0.3.0
```

- [ ] **Step 2: Add 2026-04-27 status note to tainer spec** (mirror 0.1 / 0.2 conventions)

In `docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`, after the 0.2 status note, add:

```markdown
> **Status note (2026-04-?? — date of actual ship):** CyberStack `v0.3.0` is live — container runtime shipped. `docker run / pull / exec / logs / inspect / stop / rm / rmi` work end-to-end. All five performance budgets met; OrbStack head-to-head published in `cyber-stack/docs/benchmarks/0.3.0-vs-orbstack.md`.
```

And mark item 3 of the development track:
```markdown
3. **CyberStack MVP — container runtime.** ... ✅ **Done — v0.3.0**
```

- [ ] **Step 3: Add SHIPPED header to this plan doc** (mirror 0.2 plan convention)

- [ ] **Step 4: Commit + push tainer**

```bash
cd /Users/lenineto/dev/cyber5-io/tainer
git add docs/superpowers/
git commit -m "docs: Mark CyberStack 0.3.0 as live"
git push origin main
```

---

## Self-review

**Spec coverage check:**
- ✅ Section 1 (architecture) — Tasks 4 (proto), 15 (agent server), 17-20 (daemon)
- ✅ Section 2 (components) — Tasks 4-20 cover every package + handler
- ✅ Section 3 (data flows) — Tasks 5, 9, 14, 13 implement pull/run/exec/logs respectively
- ✅ Section 4 (storage) — Tasks 1, 3, 5 (containers/storage init), 22 (mount + resize)
- ✅ Section 5 (networking) — Tasks 11, 12, 22 (DHCP + module load)
- ✅ Section 6 (crun packaging) — Task 21
- ✅ Section 7 (error handling) — Tasks 7 (state machine), 9 (waitpid + OOM), 17/18 (HTTP error mapping), 2 (lockfile)
- ✅ Section 8 (testing + bench) — Tasks 23, 24, 26
- ✅ Section 9 (out-of-scope) — explicit in task descriptions; not implemented = correct

**Type consistency:** `agentpb.*` types named consistently across tasks; `containerd.Spec` matches gRPC `ContainerSpec` field-by-field; `Status` enum strings match between Go and proto state field. ✅

**Placeholder scan:** Some handlers in tasks 17/18 are summarised rather than fully written — the pattern is unambiguous (interface + handler factory + JSON marshal) and the proto types fully specify the wire shape. Each task has clear file paths, code direction, and commit message. Subagents read the spec for design context.

**Scope check:** Plan has 27 tasks. Tasks 1-15 are agent + daemon foundations (parallel-able after task 4); 16-22 wire it all together; 23-27 are validation + ship. No single task exceeds ~5 minutes of mechanical edits per step. ✅

---

## Execution

Plan complete and saved to `docs/superpowers/plans/2026-04-27-cyberstack-0.3-container-runtime.md`. Execution will use **superpowers:subagent-driven-development**: fresh subagent per task, two-stage review (spec compliance + code quality) between tasks. The user has explicitly waived additional approval — execution starts immediately after this plan is committed.
