# Windows port recon dossier (TAIN-106)

Status: draft. Part 1 (engine research: WSL2 vs Hyper-V vs other) follows in a later commit and will sit above Part 2. Design intent: `2026-10-30-windows-port-recon-design.md`. Linux input: `2026-10-30-linux-native-port-design.md` (not re-derived here).

Scope notes: the target is a Windows amd64 laptop (ARM is out of scope). Citations are `file:line` against the TAIN-106 worktree (tainer) and the cyber-stack main checkout (read only), both as of 2026-10-31.

## Part 2 — Portability audit

### tainer

The CLI already cross-compiles for `GOOS=windows` (`make cross`, Makefile:58), but only because the unix-only calls sit behind seams that return "unsupported". Compiling is not working: every row below is a behaviour change still needed.

| Location | Assumption | Porting note |
|---|---|---|
| pkg/tainer/runtime/runtime.go:129 | `DOCKER_HOST` is `unix://<path>`, prefix stripped to a filesystem path | Engine socket may become a named pipe (`npipe://`) or loopback TCP; make `resolveSocket` return a URL, not a path |
| pkg/tainer/runtime/runtime.go:138 | Exports `unix://`+socket as `DOCKER_HOST` | Choose scheme per OS (`npipe:////./pipe/...` or `tcp://127.0.0.1:port`) |
| pkg/tainer/runtime/runtime.go:135 | `os.Stat(socket)` as the "daemon is up" probe | A named pipe is not stat-able like a file; replace with a dial and ping |
| pkg/tainer/runtime/runtime.go:193 | `exec.Command(bin, ...)` spawns `cyberstackd` as a child of the CLI | Windows equivalent is a detached process (`DETACHED_PROCESS`/`CREATE_NEW_PROCESS_GROUP`) or a Windows service via NSSM/SCM |
| pkg/tainer/runtime/runtime.go:324 | Installed-binary fallback `/opt/tainer/bin/cyberstackd` | Needs `%ProgramFiles%\Tainer\bin\cyberstackd.exe`, with `.exe` handled in `exec.LookPath` |
| pkg/tainer/runtime/runtime.go:33 | Daemon liveness tracked by a pid file next to the socket | Pid reuse and stale files are worse on Windows; prefer a named mutex or a service-state query |
| pkg/tainer/runtime/runtime.go:107 | `ErrUnsupportedOS` ("not supported on Windows yet") is the interim answer | Remove when the Windows engine exists; message is user-facing and must not name the engine |
| pkg/tainer/runtime/proc_windows.go:12 | `ProcessAlive` returns false unconditionally | Replace with `OpenProcess` + `GetExitCodeProcess` (or service query) |
| pkg/tainer/runtime/proc_windows.go:16 | `SignalTerm` returns `ErrUnsupportedOS` | No SIGTERM on Windows; use a control-pipe "shutdown" request or `GenerateConsoleCtrlEvent`, falling back to `TerminateProcess` |
| pkg/tainer/runtime/proc_windows.go:25 | `detachAttr` returns nil (no detach) | Set `syscall.SysProcAttr{CreationFlags: DETACHED_PROCESS \| CREATE_NEW_PROCESS_GROUP}` |
| pkg/tainer/runtime/proc_windows.go:30 | `canSpawnDaemon` hard-fails | Flip to success once a Windows engine ships |
| pkg/tainer/networkcmd/networkcmd.go:148 | `stopDaemon` sends SIGTERM to `cyberstackd` | Goes through the same `SignalTerm` seam; needs the Windows shutdown path above |
| pkg/tainer/engine/client.go:31 | Socket default is `~/.cyberstack/cyberstack.sock` | Move to `%LOCALAPPDATA%\cyberstack\` (or a pipe name) behind a seam |
| pkg/tainer/engine/client.go:53 | Sets `DOCKER_HOST=unix://` for the Docker SDK | Same scheme choice as pkg/tainer/runtime/runtime.go:138 |
| pkg/tainer/config/paths.go:9 | `XDG_CONFIG_HOME` honoured on every OS | Harmless; on Windows prefer `%APPDATA%` unless XDG is set |
| pkg/tainer/config/paths.go:13 | Base dir `~/.config/tainer` | Use `os.UserConfigDir()` on Windows (`%APPDATA%\tainer`); keys, certs, router and templates all hang off this |
| pkg/tainer/config/paths.go:41 | `os.MkdirAll(d, 0755)` for key and cert dirs | Mode bits are ignored on NTFS; private-key confidentiality needs an ACL, not a mode |
| pkg/tainer/pod/secrets.go:33 | Secrets under `~/.cyberstack/tainer/secrets` | `0o700` at pkg/tainer/pod/secrets.go:135 does nothing on NTFS; restrict the directory ACL to the current user |
| pkg/tainer/pod/registry.go:24 | Slot registry at `~/.cyberstack/tainer/slots.json` | Relocate with the other state dirs via one helper |
| pkg/tainer/network/mode.go:38 | `network-mode` file under `~/.cyberstack` | Same relocation; the `external-gvproxy` default must be re-examined for the Windows network path |
| pkg/tainer/pod/start.go:94 | Home-relative `~/.cyberstack/0-tainer.conf` for the ssh drop-in | Windows OpenSSH reads `%USERPROFILE%\.ssh\config` and `%ProgramData%\ssh\ssh_config` |
| pkg/tainer/pod/start.go:126 | Resolves `/usr/local/bin/tainer` symlink to `/opt/tainer/bin/tainer` to find `share/tainer/certs` | No symlink convention on Windows; derive the share dir from `os.Executable()` only |
| pkg/tainer/pod/start.go:229 | `os.Getuid()`/`os.Getgid()` passed into the pod context | Both return -1 on Windows; route through the identity seam (default 1000:1000) |
| pkg/tainer/pod/start.go:575 | Bind-mount sources built as `projectDir + "/db"` (also :587-599) | Use `filepath.Join`; the engine layer must translate `C:\...` to the mount form the engine expects (`/mnt/c/...` under WSL2) |
| pkg/tainer/pod/start.go:628 | Bind-mounts the host public key to `/etc/ssh/tainer_authorized_keys` | Container side is fine; the host source path needs the same translation as the project mounts |
| pkg/tainer/router/ensure.go:43 | Router publishes host :80 and :443 | Ports below 1024 are fine unprivileged on Windows, but IIS/http.sys, Skype-era services or Hyper-V reservations may hold them; add a bind pre-check |
| pkg/tainer/router/ensure.go:146 | `remoteLoginActive` returns false when `GOOS != "darwin"` | On Windows the OpenSSH Server service (`sshd`) can own :22; add a service-state check |
| pkg/tainer/router/ensure.go:149 | macOS Remote Login probe via `launchctl` | Windows analog: `Get-Service sshd` or an `sc query` / SCM call; falls back to :2222 using the existing logic |
| pkg/tainer/router/ensure.go:172 | `ensureSSH` publishes the router on `SSHHostPort()` (22, else 2222) | Port logic is portable once the Windows probe exists |
| pkg/tainer/router/ensure.go:218 | Mounts sshpiper host key to `/etc/ssh/ssh_host_ed25519_key` | Key mode 0600 cannot be enforced on NTFS; sshpiper inside the guest sees whatever the share exposes, so check permissions through the share |
| pkg/tainer/dns/resolver.go:14 | `switch runtime.GOOS` with darwin and linux only | Add a `windows` case; `default` returns empty and `InstallResolver` errors |
| pkg/tainer/dns/resolver.go:16 | macOS `/etc/resolver/tainer.me` with a custom port | Windows has no per-zone resolver file; use an NRPT rule (`Add-DnsClientNrptRule -Namespace .tainer.me`), which cannot specify a port, so the responder must bind :53 on loopback |
| pkg/tainer/dns/resolver.go:50 | Uses `sudo mkdir -p` | No sudo; needs an elevated helper (UAC prompt) or writes done by an installer service |
| pkg/tainer/dns/resolver.go:59 | Writes the config via `sudo tee` | Replace with a PowerShell/registry write under elevation |
| pkg/tainer/dns/resolver.go:68 | `systemctl restart systemd-resolved` on linux | Windows analog: `Clear-DnsClientCache` after the NRPT change |
| pkg/tainer/certsync/certsync.go:24 | Comment documents `/etc/resolver/tainer.me` hijack of the whole zone | Behaviour holds under NRPT too: the cert endpoint (get.tainer.dev) must stay outside tainer.me |
| pkg/tainer/tls/refresh.go:109 | `os.Chmod(keyPath, 0600)` on the private key | No-op on Windows; set a user-only ACL |
| pkg/tainer/tls/refresh.go:112 | `os.Chmod(certPath, 0644)` | No-op on Windows; harmless |
| pkg/tainer/ssh/clientconfig.go:10 | `~/.config/tainer/keys/tainer_rsa` written into ssh config | `~` expansion works in Windows OpenSSH, but the path must match `config.KeysDir()` after the base-dir change |
| pkg/tainer/ssh/clientconfig.go:33 | `os.Chmod(path, 0644)` on the drop-in | No-op on Windows |
| pkg/tainer/machine/machine.go:35 | Early `GOOS != "darwin"` return in the machine helper | Confirms darwin-only pf logic; make explicit that windows takes a separate path |
| pkg/tainer/machine/machine.go:48 | `sudo pfctl -e` | macOS firewall only; Windows needs no equivalent unless port forwarding is added |
| pkg/tainer/machine/machine.go:54 | `sudo pfctl -f /etc/pf.conf` | Same; dead code on Windows |
| pkg/tainer/doctor/doctor.go:187 | Hint text points at `/opt/tainer/bin` or `$PATH` | Make the hint OS-specific |
| pkg/tainer/doctor/doctor.go:575 | `processRunning` shells out to `pgrep -f` | `pgrep` does not exist on Windows; use a process listing API (`tasklist` or `golang.org/x/sys/windows`) |
| pkg/tainer/gitsetup/gitsetup.go:78 | Shells out to `git init` | Needs Git for Windows on PATH; doctor should verify |
| pkg/tainer/tui/list/list.go:568 | Re-invokes `exec.Command("tainer", action)` by bare name | Relies on PATH and `.exe` resolution; prefer `os.Executable()` |
| pkg/tainer/tui/list/list.go:592 | `openBrowser` switch has darwin and linux only | Add `windows` using `rundll32 url.dll,FileProtocolHandler` or `cmd /c start` |
| pkg/tainer/machine/machine.go:15 | Shells out to `tainer machine ls` | Legacy-style call by bare name; same PATH concern |
| cmd/tainer/main.go:2032 | `signal.Notify(ch, SIGINT, SIGTERM, SIGHUP)` for terminal restore | Compiles on Windows (the constants exist) but only SIGINT is delivered; console close events need `SetConsoleCtrlHandler` |
| cmd/tainer/main.go:2287 | `term.MakeRaw(int(os.Stdin.Fd()))` for interactive attach | `golang.org/x/term` supports Windows console mode; verify behaviour in Windows Terminal and legacy conhost |
| cmd/tainer/main.go:2071 | Execs the system `ssh` binary for `tainer ssh` | Depends on the OpenSSH client feature (present on Windows 10 1809 and later); doctor should check |
| cmd/tainer/main.go:1967 | Terminal size read once via `term.GetSize` before attach | Works on Windows; live updates covered under Exec/TTY note |
| cmd/tainer-menu/main.go:54 | Menu bar app hardcodes `/opt/tainer/bin/tainer` | The menu app is macOS-only (systray plus Info.plist); a Windows tray app is a separate decision |
| cmd/tainer-menu/main.go:188 | Opens URLs with `open` | Add the OS-specific opener shared with pkg/tainer/tui/list/list.go:592 |
| packaging/menu/io.cyber5.tainer.menu.plist:1 | launchd agent keeps the menu app alive | Windows analog is a Startup shortcut or Run registry key; packaging is a .pkg today, an MSI/MSIX equivalent is needed |
| packaging/scripts/tainer-uninstall.sh:1 | Shell-script uninstaller | Needs an MSI uninstall action or PowerShell equivalent |

### cyber-stack

Input: the Linux-side recon is summarised in `2026-10-30-linux-native-port-design.md` and is not repeated; this section applies the Windows lens only. Confirmed by search: there are **no `_windows.go` files and no `windows` build tags in cyber-stack first-party code** (`cmd/`, `internal/`, `cyberstackd/`, `cyberstack-agent/`). The only `_windows.go` files live under `third_party/containers-storage` (vendored upstream), which are not wired into anything Windows-specific. The only build tags in first-party code are `linux` (the guest agent, `internal/agent/...`, `internal/transport/vsock_linux.go`) and one `darwin` file. The host daemon and its helpers are untagged, so they currently fail to compile for Windows wherever they call `syscall.Kill`, `Setpgid`, `Setsid`, or `Credential`.

| Location | Assumption | Porting note |
|---|---|---|
| internal/httpapi/server.go:114 | Docker-compat API served with `net.Listen("unix", path)` | Go supports AF_UNIX on Windows 10 1803+, so this may work as is; named pipe via `go-winio` is the Docker-native alternative the SDK understands (`npipe://`) |
| internal/httpapi/server.go:118 | `os.Chmod(path, 0o660)` after listen | Mode ignored on Windows; access control moves to a pipe ACL or the user profile directory ACL |
| internal/httpapi/server.go:107 | `ListenUnix` removes a stale socket path first | A Windows unix socket file also persists after a crash, so the remove-first behaviour still applies |
| internal/daemon/daemon.go:119 | Host-side vsock endpoint is a unix socket (`net.Listen("unix", VsockSocketPath)`) bound before the VM boots | Under vfkit this is vfkit's vsock proxy; WSL2 and Hyper-V would use AF_HYPERV sockets (GUID service IDs via `go-winio/hvsock`) or loopback TCP, so this listener becomes a transport interface |
| internal/daemon/daemon.go:144 | `httpapi.ListenUnix(d.cfg.SocketPath)` | Switch to a listener factory chosen per OS |
| internal/transport/vsock_linux.go:15 | Guest dials the host with `vsock.Dial(vsock.Host, port, nil)` (AF_VSOCK, host CID 2) | Hyper-V sockets differ (VM GUID plus service GUID); a Linux guest under Hyper-V can use `AF_VSOCK` through the hv_sock transport, but CID/port mapping and host registry entries need verification in Part 1 |
| cmd/cyberstack-agent/main.go:42 | Agent defaults to vsock port 1024 on host | Port-to-service-GUID mapping needed under Hyper-V (`00000400-FACB-11E6-BD58-64006A7986D3` template) |
| cmd/cyberstack-agent/main.go:126 | Dial loop retries `transport.DialHost` with backoff | Loop is transport-agnostic and reusable as is |
| internal/vm/vfkit_launcher.go:74 | `--device virtio-vsock,port=1024,socketURL=...,listen` (vfkit flag) | vfkit is a macOS Virtualization.framework wrapper and has no Windows build; the whole launcher is the engine question for Part 1 |
| internal/vm/vfkit_launcher.go:78 | `virtio-fs,sharedDir=...,mountTag=...` for host shares | WSL2 exposes `/mnt/c` through 9P; Hyper-V needs SMB or Plan 9 shares; see the filesystem roadmap for performance |
| internal/vm/vfkit_launcher.go:150 | `Setpgid: true` on the spawned vfkit | Windows uses job objects (`CREATE_NEW_PROCESS_GROUP` plus a job) |
| internal/vm/vfkit_launcher.go:236 | `syscall.Kill(-pgid, SIGKILL)` kills the gvproxy process group | No process groups or negative pids on Windows; use a job object with kill-on-close |
| internal/vm/vfkit_launcher.go:334 | Globs `vfkit-*.sock` leftovers | Only meaningful for vfkit; drop on Windows |
| internal/vm/vm.go:63 | `DataDiskPath` is a raw virtio-blk image | Windows analog is a VHDX (Hyper-V) or the WSL2 distro's own ext4.vhdx; sparse-file semantics differ |
| cmd/cyberstackd/main.go:46 | `defaultPath` is `~/.cyberstack/<name>` | Use `%LOCALAPPDATA%\Cyberstack`; every socket, disk and log path derives from this one helper |
| cmd/cyberstackd/main.go:89 | `-vfkit-binary` defaults to `/opt/tainer/bin/vfkit` | Hardcoded install prefix; installer location on Windows is `%ProgramFiles%` |
| cmd/cyberstackd/main.go:98 | `-gvproxy-binary` defaults to `/opt/tainer/bin/gvproxy` | gvproxy itself builds on Windows (it ships with Podman on Windows), so it is a reusable network piece once the path is fixed |
| cmd/cyberstackd/main.go:29 | vmnet-run helper at `/opt/vmnet-helper/bin` | macOS only (vmnet.framework); drop on Windows |
| cmd/cyberstackd/main.go:90 | Boot disk named `boot-<GOARCH>.img` (EFI raw disk) | Bootable raw disk is the vfkit model; Windows engines boot a kernel plus rootfs or reuse a WSL distro |
| cmd/cyberstackd/main.go:151 | `signal.NotifyContext(..., os.Interrupt, syscall.SIGTERM)` drives shutdown | `syscall.SIGTERM` exists on Windows but is never delivered; as a service, shutdown arrives via the SCM stop control, so wire a service handler to cancel the same context |
| internal/daemon/daemon.go:153 | Requires `/opt/tainer/bin/cyberstack-pf` to exist | pf (macOS packet filter) helper; no Windows equivalent needed unless host-port forwarding goes through a helper |
| internal/daemon/daemon.go:187 | `pfclient.New("/opt/tainer/bin/cyberstack-pf")` | Same; port forwarding on Windows can be an in-process TCP proxy (`internal/vm/hybridforwarder`) |
| internal/daemon/lockfile.go:51 | Stale lockfile detection via `syscall.Kill(pid, 0)` | Does not compile on Windows; use `OpenProcess` or an exclusive-open `LockFileEx` file lock |
| internal/vm/pfclient/client.go:54 | Runs the pf helper with `sudo -n` | No sudo on Windows; irrelevant if the helper is dropped |
| internal/vm/pfclient/client.go:99 | `killer: syscall.Kill` default | Compile blocker on Windows for the package; put behind a build-tagged file |
| internal/vm/pfclient/client.go:252 | Orphan cleanup shells out to `pgrep -f` | `pgrep` is absent on Windows |
| cmd/cyberstack-pf/main.go:265 | Helper binds `127.0.0.1:<hostPort>` for forwards | The TCP listener part is portable |
| cmd/cyberstack-pf/main.go:301 | `Setsid` and `Credential{Uid,Gid}` to drop privileges | Unix-only fields; the whole helper is darwin/linux by design |
| internal/dns/responder.go:19 | UDP DNS responder via `net.ListenPacket("udp", ...)` | Portable; Windows NRPT cannot carry a port, so the listen address must be 127.0.0.1:53 (check conflicts with the Windows DNS client and ICS) |
| internal/vm/sizing/probe_darwin.go:17 | Host memory probed via `unix.SysctlUint64("hw.memsize")` (darwin-tagged) | Needs a `probe_windows.go` using `GlobalMemoryStatusEx`; the 8 GB floor from the roadmap depends on this |
| internal/vm/vfkit_launcher.go:16 | Comment documents the vfkit path as `/opt/tainer/bin/vfkit` and says it will move | The launcher file has no build tag, so it is darwin-only only by content; add `//go:build darwin` and a Windows launcher behind one interface |

### Seam inventory

The house pattern is a pair of files selected by filename suffix (`_unix.go` with `//go:build !windows`-equivalent, `_windows.go` with `//go:build windows`), exposing one small function that the shared code calls.

Exists today:
- **identity**: `pkg/tainer/identity/identity_unix.go` (reads uid/gid from file stat via `syscall.Stat_t`, line 17) and `pkg/tainer/identity/identity_windows.go` (returns 1000:1000). Complete.
- **process control**: `pkg/tainer/runtime/proc_unix.go` (`ProcessAlive`, `SignalTerm`, `SignalKill`, `detachAttr` with `Setsid`, `canSpawnDaemon`) and `pkg/tainer/runtime/proc_windows.go` (stubs returning `ErrUnsupportedOS`; `ProcessAlive` returns false). Added by Task 1 of this ticket.
- **terminal resize**: `cmd/tainer/resize_unix.go` (`SIGWINCH` watcher, line 14) and `cmd/tainer/resize_windows.go` (no-op). Added by Task 1.
- **compile gate**: `make cross` builds darwin/arm64, linux/amd64, linux/arm64 and windows/amd64 (Makefile:55-58).

Missing (needed for a working Windows port, all currently inline or unsupported):
- engine endpoint (socket path versus pipe versus TCP) and the `DOCKER_HOST` scheme (pkg/tainer/runtime/runtime.go:129, pkg/tainer/engine/client.go:31)
- state and config directory resolution (`~/.config/tainer`, `~/.cyberstack`) behind one helper
- filesystem permission hardening (ACLs for keys, secrets and certs; paths at pkg/tainer/tls/refresh.go:109, pkg/tainer/pod/secrets.go:135)
- DNS resolver install (pkg/tainer/dns/resolver.go:14 has no windows case)
- privileged-operation runner replacing `sudo` (pkg/tainer/dns/resolver.go:50, pkg/tainer/machine/machine.go:48)
- port-conflict probe for :22 / :80 / :443 (pkg/tainer/router/ensure.go:146)
- host-path to engine-mount-path translation (pkg/tainer/pod/start.go:575)
- browser opener and process lookup (`pgrep`) helpers
- in cyber-stack: process-group handling, lockfile liveness, transport listener/dialer, VM launcher, host memory probe, signal-to-service-stop bridge. None have any Windows file today.

### Exec/TTY note

Interactive `tainer exec`/attach reads the terminal size once before attach (cmd/tainer/main.go:1967) and, on unix, subscribes to `SIGWINCH` to resend it on every resize (cmd/tainer/resize_unix.go:14). Windows has no `SIGWINCH`, so `watchResize` is a deliberate no-op (cmd/tainer/resize_windows.go:7): the container pty keeps the initial size for the whole session and a resized terminal wraps incorrectly until the user re-attaches. Raw mode still works through `golang.org/x/term` (cmd/tainer/main.go:2287).

This is acceptable degradation for the first Windows milestone. The proper fix is a small poll of `term.GetSize` (about 250 ms) inside `resize_windows.go`, or console-buffer-size events from `ReadConsoleInput`, calling the same callback. It needs no change to shared code because the seam already exists.
