# Tainer SSH Key Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ssh <pod>@ssh.tainer.me` work out-of-the-box for every user by authenticating with the per-install `tainer_rsa` key instead of the user's personal `~/.ssh` key.

**Architecture:** Downstream `authorized_keys` is stamped from `tainer_rsa.pub` (already generated per install) and reconciled on every start. The client is pointed at `tainer_rsa` two ways: a `tainer ssh` wrapper (`-F /dev/null`, guaranteed) and an injected `0-tainer.conf` (`/etc/ssh/ssh_config.d/` + a first-line `Include` in `~/.ssh/config`, convenience).

**Tech Stack:** Go 1.x, standard library only (`os`, `path/filepath`, `os/exec`, `testing`). Plain `switch`-based CLI in `cmd/tainer/main.go`. In-package `testing` with `t.TempDir()`.

## Global Constraints

- **Platform:** macOS implementation only this pass. Linux/Windows paths are documented in the spec, not implemented.
- **Key:** reuse the existing per-install `tainer_rsa` (Ed25519) at `config.PrivateKey()` / `config.PublicKey()`. Never bundle a shared key.
- **Drop-in filename:** `0-tainer.conf` (the `0-` prefix sorts first lexically, before `100-macos.conf`).
- **Drop-in locations:** `/etc/ssh/ssh_config.d/0-tainer.conf` (system) and `~/.cyberstack/0-tainer.conf` (per-user).
- **Include line:** `Include ~/.cyberstack/0-tainer.conf`, placed as the **first line** of `~/.ssh/config` when that file exists.
- **Client identity path in config files:** the tilde form `~/.config/tainer/keys/tainer_rsa` (expands per connecting user; works in the system drop-in).
- **Fail-soft:** any edit to `~/.ssh/config` or `/etc/ssh/ssh_config.d/` that fails must be logged and must NOT abort `tainer start`.

---

### Task 1: Stamp downstream `authorized_keys` from the tainer pubkey

**Files:**
- Modify: `pkg/tainer/router/sshpiper.go:26-44` (the `authorized_keys` block in `AddSSHPiperEntry`)
- Test: `pkg/tainer/router/sshpiper_test.go`

**Interfaces:**
- Consumes: `AddSSHPiperEntry(baseDir, projectName, projectIP, privateKeyPath string) error` (signature unchanged). The pubkey is read from `privateKeyPath + ".pub"`.
- Produces: each pod's `authorized_keys` now contains exactly the contents of `<privateKeyPath>.pub`.

- [ ] **Step 1: Write the failing test** (add to `sshpiper_test.go`)

```go
func TestAddSSHPiperEntryAuthorizedKeysFromTainerPub(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "tainer_rsa")
	if err := os.WriteFile(keyPath, []byte("fake-private"), 0600); err != nil {
		t.Fatal(err)
	}
	pub := "ssh-ed25519 AAAA_tainer_pub tainer@host\n"
	if err := os.WriteFile(keyPath+".pub", []byte(pub), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AddSSHPiperEntry(dir, "my-client", "10.77.1.2", keyPath); err != nil {
		t.Fatalf("AddSSHPiperEntry: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "my-client", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if string(got) != pub {
		t.Errorf("authorized_keys = %q, want %q", got, pub)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./pkg/tainer/router/ -run TestAddSSHPiperEntryAuthorizedKeysFromTainerPub -v`
Expected: FAIL (current code reads `~/.ssh/*.pub`, so `authorized_keys` won't equal `pub`).

- [ ] **Step 3: Replace the `~/.ssh` glob** in `AddSSHPiperEntry`. Delete lines 26-44 (the `homeDir`/`pubKeyFiles` block) and substitute:

```go
	// Create authorized_keys from the tainer public key (per-install key).
	// Reconciled on every start via router.WriteConfig, so a regenerated key
	// re-stamps every pod.
	pubData, err := os.ReadFile(privateKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("reading tainer public key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "authorized_keys"), pubData, 0644); err != nil {
		return fmt.Errorf("writing authorized_keys: %w", err)
	}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./pkg/tainer/router/ -v`
Expected: PASS (new test + existing `TestAddSSHPiperEntry`).

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/router/sshpiper.go pkg/tainer/router/sshpiper_test.go
git commit -m "fix(ssh): stamp pod authorized_keys from tainer_rsa, not ~/.ssh"
```

---

### Task 2: Client SSH config content + drop-in writer

**Files:**
- Create: `pkg/tainer/ssh/clientconfig.go`
- Test: `pkg/tainer/ssh/clientconfig_test.go`

**Interfaces:**
- Produces:
  - `const ClientIdentityTilde = "~/.config/tainer/keys/tainer_rsa"`
  - `const DropInName = "0-tainer.conf"`
  - `const UserConfigIncludeLine = "Include ~/.cyberstack/0-tainer.conf"`
  - `func ClientConfigContent(identityPath string) string`
  - `func WriteDropIn(path, identityPath string) error`

- [ ] **Step 1: Write the failing test** (`clientconfig_test.go`)

```go
package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientConfigContent(t *testing.T) {
	got := ClientConfigContent(ClientIdentityTilde)
	for _, want := range []string{
		"Host ssh.tainer.me",
		"IdentityFile ~/.config/tainer/keys/tainer_rsa",
		"IdentitiesOnly yes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("content missing %q; got:\n%s", want, got)
		}
	}
}

func TestWriteDropIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), DropInName)
	if err := WriteDropIn(p, ClientIdentityTilde); err != nil {
		t.Fatalf("WriteDropIn: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "Host ssh.tainer.me") {
		t.Errorf("drop-in content wrong:\n%s", b)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./pkg/tainer/ssh/ -run 'TestClientConfigContent|TestWriteDropIn' -v`
Expected: FAIL (undefined `ClientConfigContent` / `WriteDropIn`).

- [ ] **Step 3: Write `clientconfig.go`**

```go
package ssh

import (
	"fmt"
	"os"
)

const (
	ClientIdentityTilde   = "~/.config/tainer/keys/tainer_rsa"
	DropInName            = "0-tainer.conf"
	UserConfigIncludeLine = "Include ~/.cyberstack/0-tainer.conf"
)

// ClientConfigContent returns the ssh_config stanza that points ssh.tainer.me
// at the tainer key only. IdentitiesOnly drops unrelated agent keys so a loaded
// agent can't exhaust the server's MaxAuthTries before the tainer key is tried.
func ClientConfigContent(identityPath string) string {
	return fmt.Sprintf("Host ssh.tainer.me\n  IdentityFile %s\n  IdentitiesOnly yes\n", identityPath)
}

// WriteDropIn writes the client config stanza to path (0644, world-readable so
// any user's ssh can read the system drop-in).
func WriteDropIn(path, identityPath string) error {
	return os.WriteFile(path, []byte(ClientConfigContent(identityPath)), 0644)
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./pkg/tainer/ssh/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/ssh/clientconfig.go pkg/tainer/ssh/clientconfig_test.go
git commit -m "feat(ssh): client config drop-in content + writer"
```

---

### Task 3: `~/.ssh/config` Include manager (idempotent, first-line, fail-soft)

**Files:**
- Modify: `pkg/tainer/ssh/clientconfig.go` (add function)
- Test: `pkg/tainer/ssh/clientconfig_test.go` (add cases)

**Interfaces:**
- Produces: `func EnsureUserConfigInclude(sshConfigPath, includeLine string) error` — no-op returning nil if the file is missing; otherwise guarantees `includeLine` is the first non-empty line, exactly once. Returns an error only on read/write failure (caller logs and continues).

- [ ] **Step 1: Write the failing tests** (add to `clientconfig_test.go`)

```go
func TestEnsureUserConfigIncludeMissingFileIsNoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatalf("want nil for missing file, got %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("missing config must stay missing (system drop-in covers it)")
	}
}

func TestEnsureUserConfigIncludePrependsWhenAbsent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\n  UseKeychain yes\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines[0] != UserConfigIncludeLine {
		t.Errorf("first line = %q, want include", lines[0])
	}
	if !strings.Contains(string(b), "UseKeychain yes") {
		t.Error("original content must be preserved")
	}
}

func TestEnsureUserConfigIncludeIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte(UserConfigIncludeLine+"\nHost *\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), UserConfigIncludeLine) != 1 {
		t.Errorf("include must appear exactly once, got:\n%s", b)
	}
}

func TestEnsureUserConfigIncludeMovesToFirst(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host *\n  IdentityFile ~/.ssh/x\n"+UserConfigIncludeLine+"\n"), 0600)
	if err := EnsureUserConfigInclude(p, UserConfigIncludeLine); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines[0] != UserConfigIncludeLine {
		t.Errorf("include must be moved to first line, got first = %q", lines[0])
	}
	if strings.Count(string(b), UserConfigIncludeLine) != 1 {
		t.Errorf("include must appear exactly once, got:\n%s", b)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./pkg/tainer/ssh/ -run TestEnsureUserConfigInclude -v`
Expected: FAIL (undefined `EnsureUserConfigInclude`).

- [ ] **Step 3: Implement** (append to `clientconfig.go`; add `bufio`? no — use `strings`. Update imports to include `strings`.)

```go
// EnsureUserConfigInclude guarantees includeLine is the first non-empty line of
// the ssh config at sshConfigPath, exactly once. If the file does not exist it
// is a no-op (the system drop-in covers that case). Preserves the file's mode.
func EnsureUserConfigInclude(sshConfigPath, includeLine string) error {
	data, err := os.ReadFile(sshConfigPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", sshConfigPath, err)
	}

	var kept []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) != includeLine {
			kept = append(kept, ln)
		}
	}
	// Drop a leading empty line so the include lands on line 1 cleanly.
	for len(kept) > 0 && strings.TrimSpace(kept[0]) == "" {
		kept = kept[1:]
	}
	rebuilt := includeLine + "\n" + strings.Join(kept, "\n")

	if string(data) == rebuilt {
		return nil
	}
	mode := os.FileMode(0600)
	if fi, statErr := os.Stat(sshConfigPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(sshConfigPath, []byte(rebuilt), mode); err != nil {
		return fmt.Errorf("writing %s: %w", sshConfigPath, err)
	}
	return nil
}
```

Add `"strings"` to the import block in `clientconfig.go`.

- [ ] **Step 4: Run, verify pass**

Run: `go test ./pkg/tainer/ssh/ -v`
Expected: PASS (all four new cases + Task 2 cases).

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/ssh/clientconfig.go pkg/tainer/ssh/clientconfig_test.go
git commit -m "feat(ssh): idempotent ~/.ssh/config Include manager"
```

---

### Task 4: Wire client-config injection into `tainer start`

**Files:**
- Modify: `pkg/tainer/pod/start.go` (`ensureHostSetup`, ~line 68)

**Interfaces:**
- Consumes: `ssh.WriteDropIn`, `ssh.EnsureUserConfigInclude`, `ssh.ClientIdentityTilde`, `ssh.DropInName`, `ssh.UserConfigIncludeLine` (Task 2/3).
- Produces: `ensureClientSSHConfig()` — best-effort, fail-soft; called from `ensureHostSetup`.

- [ ] **Step 1: Add `ensureClientSSHConfig` to `start.go`** (after `ensureCerts`)

```go
// ensureClientSSHConfig points the user's ssh at the tainer key for
// ssh.tainer.me. Best-effort and fail-soft: it must never abort a start.
//   - per-user drop-in at ~/.cyberstack/0-tainer.conf + Include as line 1 of
//     ~/.ssh/config (when that file exists)
//   - system drop-in at /etc/ssh/ssh_config.d/0-tainer.conf (succeeds only with
//     write access — e.g. installer/daemon context; the per-user path covers
//     the rest)
func ensureClientSSHConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("ssh client config: home dir: %v", err)
		return
	}
	userDrop := filepath.Join(home, ".cyberstack", ssh.DropInName)
	if err := ssh.WriteDropIn(userDrop, ssh.ClientIdentityTilde); err != nil {
		log.Printf("ssh client config: write %s: %v", userDrop, err)
	}
	sshCfg := filepath.Join(home, ".ssh", "config")
	if err := ssh.EnsureUserConfigInclude(sshCfg, ssh.UserConfigIncludeLine); err != nil {
		log.Printf("ssh client config: include in %s: %v", sshCfg, err)
	}
	sysDrop := filepath.Join("/etc/ssh/ssh_config.d", ssh.DropInName)
	if err := ssh.WriteDropIn(sysDrop, ssh.ClientIdentityTilde); err != nil {
		log.Printf("ssh client config: system drop-in skipped (%s): %v", sysDrop, err)
	}
}
```

Ensure `start.go` imports `"log"` and `"path/filepath"` (add if missing).

- [ ] **Step 2: Call it from `ensureHostSetup`** — add before `return nil` (after the `ensureCerts()` block):

```go
	ensureClientSSHConfig()
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Manual verification** (macOS, existing pod running)

```bash
tainer start          # in any project dir
head -1 ~/.ssh/config # => Include ~/.cyberstack/0-tainer.conf
cat ~/.cyberstack/0-tainer.conf
ssh -o BatchMode=yes <pod>@ssh.tainer.me 'echo OK'   # => OK
```
Expected: include is line 1, drop-in present, ssh authenticates.

- [ ] **Step 5: Commit**

```bash
git add pkg/tainer/pod/start.go
git commit -m "feat(ssh): inject tainer client ssh config on start (fail-soft)"
```

---

### Task 5: `tainer ssh <pod>` wrapper + `SSHHint`

**Files:**
- Modify: `cmd/tainer/main.go` (add `case "ssh"` at ~line 92; add `cmdSSH` + `sshArgs`)
- Modify: `pkg/tainer/router/ensure.go:154` (`SSHHint`)
- Test: `cmd/tainer/main_test.go` (create) and `pkg/tainer/router/ensure_test.go` (create or extend)

**Interfaces:**
- Produces:
  - `func sshArgs(pod, identityPath string, port int, extra []string) []string`
  - `func cmdSSH(args []string)`
  - `SSHHint(name string)` now returns `tainer ssh <name>` (port-2222 variant preserved).

- [ ] **Step 1: Write failing tests**

`cmd/tainer/main_test.go`:
```go
package main

import (
	"reflect"
	"testing"
)

func TestSSHArgsDefaultPort(t *testing.T) {
	got := sshArgs("newnode", "/keys/tainer_rsa", 22, []string{"ls", "-la"})
	want := []string{
		"-F", "/dev/null",
		"-o", "IdentitiesOnly=yes",
		"-i", "/keys/tainer_rsa",
		"newnode@ssh.tainer.me",
		"ls", "-la",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSSHArgsPort2222(t *testing.T) {
	got := sshArgs("newnode", "/k", 2222, nil)
	if got[0] != "-p" || got[1] != "2222" {
		t.Errorf("expected -p 2222 first, got %v", got)
	}
}
```

`pkg/tainer/router/ensure_test.go`:
```go
package router

import "testing"

func TestSSHHintUsesWrapper(t *testing.T) {
	if got := SSHHint("newnode"); got != "tainer ssh newnode" {
		t.Errorf("SSHHint = %q, want %q", got, "tainer ssh newnode")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./cmd/tainer/ ./pkg/tainer/router/ -run 'SSHArgs|SSHHint' -v`
Expected: FAIL (undefined `sshArgs`; `SSHHint` returns old string).

- [ ] **Step 3a: Update `SSHHint`** in `ensure.go`:

```go
func SSHHint(name string) string {
	if SSHHostPort() == 2222 {
		return "tainer ssh -p 2222 " + name
	}
	return "tainer ssh " + name
}
```

- [ ] **Step 3b: Add `sshArgs` + `cmdSSH`** to `main.go`:

```go
func sshArgs(pod, identityPath string, port int, extra []string) []string {
	var a []string
	if port == 2222 {
		a = append(a, "-p", "2222")
	}
	a = append(a, "-F", "/dev/null", "-o", "IdentitiesOnly=yes", "-i", identityPath, pod+"@ssh.tainer.me")
	return append(a, extra...)
}

func cmdSSH(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: tainer ssh <pod> [command...]")
		os.Exit(2)
	}
	pod, extra := args[0], args[1:]
	c := exec.Command("ssh", sshArgs(pod, config.PrivateKey(), router.SSHHostPort(), extra)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "tainer ssh: %v\n", err)
		os.Exit(1)
	}
}
```

Add `case "ssh":\n\t\tcmdSSH(os.Args[2:])` to the switch in `main()` (near the `case "exec"` line). Ensure `main.go` imports `"os/exec"`, `"fmt"`, `"github.com/cyber5-io/tainer/pkg/tainer/config"`, and `"github.com/cyber5-io/tainer/pkg/tainer/router"` (add any missing).

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./cmd/tainer/ ./pkg/tainer/router/ -v && go build ./...`
Expected: PASS + build success.

- [ ] **Step 5: Commit**

```bash
git add cmd/tainer/main.go cmd/tainer/main_test.go pkg/tainer/router/ensure.go pkg/tainer/router/ensure_test.go
git commit -m "feat(ssh): tainer ssh wrapper (-F /dev/null) + SSHHint uses it"
```

---

### Task 6: Restart pods when the tainer key is (re)generated

**Files:**
- Modify: `pkg/tainer/ssh/keygen.go` (`EnsureKeyPair` returns `generated bool`)
- Modify: `pkg/tainer/pod/start.go:72` and any other `EnsureKeyPair` caller in the non-legacy tree
- Test: `pkg/tainer/ssh/keygen_test.go` (create)

**Interfaces:**
- Produces: `func EnsureKeyPair(privPath, pubPath string) (generated bool, err error)` — `generated` is true only when a new key was written.
- Consumes: `engine.Client.Stop(ctx, name)` / `engine.Client.Start(ctx, name)` for the restart (wiring step).

- [ ] **Step 1: Write failing test** (`keygen_test.go`)

```go
package ssh

import (
	"path/filepath"
	"testing"
)

func TestEnsureKeyPairReportsGenerated(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "tainer_rsa")
	pub := priv + ".pub"

	gen, err := EnsureKeyPair(priv, pub)
	if err != nil {
		t.Fatal(err)
	}
	if !gen {
		t.Error("first call must report generated=true")
	}
	gen, err = EnsureKeyPair(priv, pub)
	if err != nil {
		t.Fatal(err)
	}
	if gen {
		t.Error("second call must report generated=false (key already exists)")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./pkg/tainer/ssh/ -run TestEnsureKeyPairReportsGenerated -v`
Expected: FAIL (compile error: `EnsureKeyPair` returns one value).

- [ ] **Step 3: Change `EnsureKeyPair`** signature in `keygen.go`:

```go
func EnsureKeyPair(privPath, pubPath string) (bool, error) {
	if _, err := os.Stat(privPath); err == nil {
		return false, nil // already exists
	}
	// ... existing generation body, replacing each `return fmt.Errorf(...)`
	// with `return false, fmt.Errorf(...)` ...
	return true, nil
}
```

Every early `return err`/`return fmt.Errorf(...)` inside the body becomes `return false, ...`; the final `return nil` becomes `return true, nil`.

- [ ] **Step 4: Update callers.** In `pkg/tainer/pod/start.go` `ensureHostSetup` (line 72), capture the flag and thread it out. Change `ensureHostSetup` to `func ensureHostSetup() (keyGenerated bool, err error)`:

```go
	keyGenerated, err := ssh.EnsureKeyPair(config.PrivateKey(), config.PublicKey())
	if err != nil {
		return false, fmt.Errorf("pod start: ssh key pair: %w", err)
	}
```

Update its other returns to `return false, ...` (and the final to `return keyGenerated, nil`). Update the caller of `ensureHostSetup` to receive both values.

- [ ] **Step 5: Add restart-on-regen** at the pod-start orchestration site, after pods are brought up and `router.WriteConfig` has re-stamped keys. Where the running pod names (`[]string`) and `*engine.Client` are in scope:

```go
	if keyGenerated {
		for _, name := range runningPodNames {
			if err := eng.Stop(ctx, name); err != nil {
				log.Printf("key rotation: stop %s: %v", name, err)
				continue
			}
			if err := eng.Start(ctx, name); err != nil {
				log.Printf("key rotation: start %s: %v", name, err)
			}
		}
	}
```

(Restarting refreshes each pod's `/etc/ssh/tainer_authorized_keys` bind-mount, which reflects the new `tainer_rsa.pub`.)

- [ ] **Step 6: Run tests + build**

Run: `go test ./pkg/tainer/... && go build ./...`
Expected: PASS + build success.

- [ ] **Step 7: Commit**

```bash
git add pkg/tainer/ssh/keygen.go pkg/tainer/ssh/keygen_test.go pkg/tainer/pod/start.go
git commit -m "feat(ssh): restart pods when tainer key is regenerated"
```

---

### Task 7: Uninstall cleanup

**Files:**
- Modify: `packaging/scripts/tainer-uninstall.sh`

- [ ] **Step 1: Add cleanup** near the existing `rm -f` block (before the `--purge` branch, so it runs on every uninstall):

```sh
# Remove tainer SSH client config (both drop-ins + the ~/.ssh/config include).
rm -f /etc/ssh/ssh_config.d/0-tainer.conf
rm -f "$CONSOLE_HOME/.cyberstack/0-tainer.conf"
SSH_CFG="$CONSOLE_HOME/.ssh/config"
if [ -f "$SSH_CFG" ]; then
    # Strip the include line; keep a backup.
    grep -vF 'Include ~/.cyberstack/0-tainer.conf' "$SSH_CFG" > "$SSH_CFG.tainer-tmp" 2>/dev/null \
        && mv "$SSH_CFG.tainer-tmp" "$SSH_CFG"
fi
```

(Confirm `$CONSOLE_HOME` is the variable the script already uses for the invoking user's home — it is, per the existing `--purge` block.)

- [ ] **Step 2: Verify (dry run)** on a scratch copy:

```bash
printf 'Include ~/.cyberstack/0-tainer.conf\nHost *\n  UseKeychain yes\n' > /tmp/sshcfg
grep -vF 'Include ~/.cyberstack/0-tainer.conf' /tmp/sshcfg
```
Expected: output is just `Host *` / `UseKeychain yes` — include line stripped, rest intact.

- [ ] **Step 3: Commit**

```bash
git add packaging/scripts/tainer-uninstall.sh
git commit -m "feat(ssh): uninstall removes tainer ssh drop-ins + config include"
```

---

## Self-Review

**Spec coverage:**
- Downstream `authorized_keys` from `tainer_rsa` → Task 1.
- Reconcile-on-start → already provided by `router.WriteConfig` → `writeSSHPiperUpstreams` → `AddSSHPiperEntry` (Task 1 makes it stamp the tainer key on every start; no new task needed).
- Pod restart on key change → Task 6.
- Key durability across reinstall → existing uninstaller behavior (no code change; documented in spec).
- `tainer ssh` wrapper → Task 5. `SSHHint` → Task 5.
- Config drop-in (system + user) + first-line Include + start resilience → Tasks 2, 3, 4.
- Uninstall cleanup → Task 7.
- Fail-soft → Task 4 (log-and-continue) + Task 3 (missing file no-op).

**Placeholder scan:** none — every code step has complete code.

**Type consistency:** `EnsureKeyPair` returns `(bool, error)` in Task 6 and all callers updated there; `ClientIdentityTilde`/`DropInName`/`UserConfigIncludeLine`/`ClientConfigContent`/`WriteDropIn`/`EnsureUserConfigInclude` defined in Tasks 2–3 and consumed with matching signatures in Tasks 4–5.
