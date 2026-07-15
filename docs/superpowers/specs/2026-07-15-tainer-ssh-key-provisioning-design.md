# Tainer SSH key provisioning — design

- **Date:** 2026-07-15
- **Status:** Approved (design); implementation macOS-first
- **Scope:** How tainer authenticates a user's `ssh` into `ssh.tainer.me` (the sshpiperd router) so it works out-of-the-box for every user, not just those who happen to have a default-named personal SSH key.

## Problem

`pkg/tainer/router/sshpiper.go::AddSSHPiperEntry` builds each pod's downstream `authorized_keys` by reading the **local user's** default public keys:

```go
pubKeyFiles := []string{"id_rsa.pub", "id_ecdsa.pub", "id_ed25519.pub"}
```

This has three failure modes:

1. **No key** — a user with no `~/.ssh` keypair yields an empty `authorized_keys` (the file isn't even written), so SSH into their pods cannot work and there is no password fallback. Many end users have never generated an SSH key.
2. **Non-default key name** — a user whose key is e.g. `work_ed25519.pub` (not one of the three default names, and not symlinked to one) gets no access, silently.
3. **Personal-key interference** — because auth leans on whatever is in the user's `~/.ssh`/agent, a user with their own `Host *` `IdentityFile` can have that key offered first. sshpiperd answers a non-matching key with **"partial success"**, and if the correct key is only a bare on-disk default (never reached), OpenSSH loops on the wrong key until `MaxAuthTries` is exhausted and drops to a password prompt. (Observed live on 2026-07-15 after an unrelated personal key rotation.)

## Goals

- SSH into tainer pods works with **zero SSH setup** by the user, on a machine with no `~/.ssh` at all.
- No shared secret shipped in the installer.
- Survives reinstall, tainer updates, and user edits to `~/.ssh/config`.
- A user's personal SSH keys/config never interfere with tainer SSH.

## Non-goals

- Guaranteeing success under an arbitrarily hostile hand-edited power-user config. A power user who breaks it can diagnose from tainer logs and use the wrapper. No config-file scheme is 100%; the wrapper is the guaranteed escape hatch.
- Linux/Windows implementation in this pass (design documents them; only macOS ships now).

## Design

### 1. Use the per-install tainer key (not the personal key, not a bundled key)

Tainer already generates a unique Ed25519 keypair per machine via `pkg/tainer/ssh/keygen.go::EnsureKeyPair` at `~/.config/tainer/keys/tainer_rsa[.pub]` and already uses it for the **upstream** hop (sshpiperd → pod). We extend it to the **downstream** hop (user → sshpiperd).

- **Do not** bundle one shared keypair in the installer: every install would carry the same private key, unrevocable without a rebuild, catastrophic if the SSH port is ever exposed past localhost.
- Per-install generation gives out-of-the-box behavior with no shared secret. This is the OrbStack/Lima/colima model.

**Change:** `AddSSHPiperEntry` writes `tainer_rsa.pub` into each pod's downstream `authorized_keys`, replacing the `~/.ssh/*.pub` glob.

### 2. Treat `authorized_keys` as reconciled state, not write-once

The 2026-07-15 breakage was rooted in provisioning being frozen at pod-create. Fix the model:

- On daemon/router start, run `pkg/tainer/router/update.go::writeSSHPiperUpstreams` over **all** pods, re-stamping each pod's sshpiper working dir (`id_rsa`, `authorized_keys`, `sshpiper_upstream`) from the **current** `tainer_rsa`. This already exists; make it authoritative on every start.
- When the key changes out from under existing pods, **restart the affected pods** so the upstream mount (`/etc/ssh/tainer_authorized_keys`, a bind-mount of `tainer_rsa.pub`) refreshes — rewriting files alone is insufficient for the upstream side.

### 3. Key durability across reinstall

Already correct; preserve it:

- `packaging/scripts/tainer-uninstall.sh` keeps `~/.config/tainer` and `~/.cyberstack` by default; only `--purge` removes them.
- `EnsureKeyPair` regenerates only if the key is missing → normal reinstall reuses the same key → old pods still authenticate.
- Under `--purge`, key and pods are removed **together** (both live under those two dirs), so a new key never faces old pods. Invariant: the key can never outlive the pods.

### 4. Client side — two layers

**4a. `tainer ssh <pod>` wrapper (primary, guaranteed).**

```
ssh -F /dev/null -o IdentitiesOnly=yes -i ~/.config/tainer/keys/tainer_rsa <pod>@ssh.tainer.me
```

`-F /dev/null` discards **all** personal ssh config (and any `Host *` key with it); `-i` + `IdentitiesOnly=yes` present **only** the tainer key. Immune to whatever is in the user's `~/.ssh/config` or agent. `router.SSHHint` should print `tainer ssh <pod>` as the documented command.

**4b. Config injection (convenience, so bare `ssh <pod>@ssh.tainer.me` also works).**

A single file, dropped in two places:

```
# 0-tainer.conf
Host ssh.tainer.me
  IdentityFile ~/.config/tainer/keys/tainer_rsa
  IdentitiesOnly yes
```

- **System:** `/etc/ssh/ssh_config.d/0-tainer.conf` (installer runs as root). macOS `/etc/ssh/ssh_config` includes `/etc/ssh/ssh_config.d/*`; the `0-` prefix sorts first lexically (`'0' < '1'`, so it precedes `100-macos.conf`). Covers users with **no** `~/.ssh/config`. `~` in `IdentityFile` expands per connecting user, so one file serves every user on the machine and is harmlessly skipped for users with no tainer key.
- **Per-user:** the same file at `~/.cyberstack/0-tainer.conf` (cyberstack is a hard dependency, so `~/.cyberstack` always exists — no new dir needed), pulled in by adding `Include ~/.cyberstack/0-tainer.conf` as the **first line** of `~/.ssh/config`. Being first, it is read before any user `Host *` block. If `~/.ssh/config` does not exist, do nothing — the system drop-in covers it.

`IdentitiesOnly yes` is present in the file itself, so both drop locations carry it (redefinition is harmless — first value wins, and both values are identical).

**4c. Resilience check on every tainer start.**

If `~/.ssh/config` exists, ensure `Include ~/.cyberstack/0-tainer.conf` is present as the first line; prepend it if missing or not first. Idempotent (single occurrence, tolerant of whitespace/CRLF). **Fail-soft:** if the file isn't writable, log and continue the start — never abort a start over this.

### 5. Uninstall

Remove all three artifacts:
- `/etc/ssh/ssh_config.d/0-tainer.conf`
- `~/.cyberstack/0-tainer.conf`
- the `Include ~/.cyberstack/0-tainer.conf` line from `~/.ssh/config`

(A dangling `Include` of a missing file is silently ignored by ssh, so a stale line is not fatal — but strip it anyway.)

## Why it works (SSH client rules + empirical validation)

- **First value wins, not most-specific.** ssh reads stanzas top-to-bottom (command line → `~/.ssh/config` → `/etc/ssh/ssh_config`) and locks in the first value per option. Specificity is irrelevant. Hence the include must be read before any `Host *`.
- **`IdentityFile` accumulates** (multi-value), so all matching keys are offered in read order. A user's `Host *` key still accumulates — but that's fine (below).
- **ssh advances past a "partial success" key** to the next *explicit* identity. Because `tainer_rsa` is an explicit `IdentityFile`, ssh always reaches it. The 2026-07-15 loop happened only because the correct key was a bare on-disk default that was never reached.
- **`IdentitiesOnly yes` drops unrelated agent keys**, bounding the number of auth attempts before `tainer_rsa` so `MaxAuthTries` is not exhausted by a heavily-loaded agent.

**Validated 2026-07-15** against the live router (`leni-id_rsa` standing in for `tainer_rsa`, `leni-ed25519` as an interfering `Host *` key): with the tainer stanza present and `IdentitiesOnly yes`, the wrong key was offered first (partial success), ssh advanced to the correct key, and authentication succeeded — in both stanza orderings. The `tainer ssh` wrapper (`-F /dev/null`) offered only `tainer_rsa` and succeeded immediately.

## Platform support

- **macOS — implement now.** Paths as above. Confirmed `/etc/ssh/ssh_config` includes `ssh_config.d/*`.
- **Linux — spec only.** System drop-in `/etc/ssh/ssh_config.d/0-tainer.conf` (verify the distro's `ssh_config` includes the dir; most do). Per-user `~/.ssh/config` include identical. Key at `~/.config/tainer/keys/tainer_rsa` (or `$XDG_CONFIG_HOME`).
- **Windows — spec only.** System drop-in under `%PROGRAMDATA%\ssh\ssh_config.d\`; per-user `%USERPROFILE%\.ssh\config`. Wrapper identical with Windows paths.

## Edge cases & limitations

- **Heavily-customized power-user config** may still fail (e.g. an aggressive `Host *` that forces `IdentitiesOnly no` plus a large agent that exhausts `MaxAuthTries` before `tainer_rsa`). Accepted: such a user can read the failure from tainer logs and use `tainer ssh`. The wrapper is unaffected by any personal config.
- **`~/.ssh/config` not writable** → skip the include edit, log, continue start; the system drop-in still covers the common case.
- **Multi-user macOS** → the system drop-in's `~`-relative `IdentityFile` resolves per connecting user; each user who has run tainer has their own `tainer_rsa`.

## Implementation checklist (macOS)

- [ ] `sshpiper.go::AddSSHPiperEntry` — write `tainer_rsa.pub` to `authorized_keys` instead of globbing `~/.ssh/*.pub`.
- [ ] Ensure `writeSSHPiperUpstreams` runs on daemon/router start (reconcile all pods from current key).
- [ ] On key change, restart affected pods so the upstream bind-mount refreshes.
- [ ] `tainer ssh <pod>` subcommand → `ssh -F /dev/null -o IdentitiesOnly=yes -i <PrivateKey> <pod>@ssh.tainer.me`.
- [ ] `router.SSHHint` prints `tainer ssh <pod>`.
- [ ] Write `0-tainer.conf` to `/etc/ssh/ssh_config.d/` (root, install time) and `~/.cyberstack/`.
- [ ] Ensure `Include ~/.cyberstack/0-tainer.conf` is the first line of `~/.ssh/config` when it exists; re-check on every start; idempotent; fail-soft.
- [ ] `tainer-uninstall.sh` — remove both files and strip the include line.
- [ ] Tests: no-key user; custom-named-key user; user with conflicting `Host *` key; reinstall (key reused); `--purge` (key + pods gone together); uninstall cleanup.
