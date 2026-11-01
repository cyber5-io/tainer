# Tainer menu bar app (macOS)

Native SwiftUI `MenuBarExtra` app (no dock icon), bundle id `io.cyber5.tainer.menu`, macOS 14+.

    swift build
    swift test
    scripts/make-app.sh           # assembles build/Tainer Menu.app
    scripts/make-app.sh --check   # same, then asserts bundle structure without launching

The bundle version is stamped from `VERSION` in the repo-root `Makefile` (fails loudly if it can't be read); the pkg pipeline passes `--app-version $(VERSION)` explicitly.

Brand rasters in `Sources/TainerMenu/Resources/` are generated from `packaging/brand/` with
`swift packaging/brand/svg2png.swift <in.svg> <out.png> <height-px>`.

## Packaging into the .pkg

The shipping menu app is still the Go `cmd/tainer-menu`. The Makefile target `menu-app` builds it into `dist/Tainer Menu.app`, and `pkg-full-root` (both in the repo-root `Makefile`) copies that bundle into the installer payload:

    cp -R "$(MENU_APP)" "$(PKG_FULL_ROOT)/Applications/"

That line, under the comment `# Menu-bar app in /Applications + a login agent to start it.`, is the insertion point. To ship the SwiftUI app, `menu-app` (or a sibling target it depends on) must:

1. Run `apps/menu-macos/scripts/make-app.sh` and take `apps/menu-macos/build/Tainer Menu.app` as `$(MENU_APP)`, instead of the `go build` and `install` steps.
2. Pass `--app-version $(VERSION)` so the bundle's `Info.plist` carries tainer's version (make-app.sh does the stamping).
3. Sign the finished bundle with the existing `codesign $(SIGN_FLAGS_BASE) --sign "$(SIGN_IDENTITY)"` line (`--force --options runtime --timestamp`).

Things that must change together with that swap:

- `packaging/menu/io.cyber5.tainer.menu.plist` launches `/Applications/Tainer Menu.app/Contents/MacOS/tainer-menu`. The SwiftUI executable is `TainerMenu`, so the plist `ProgramArguments` has to be updated (or `make-app.sh` taught to name the executable `tainer-menu`). Left as is, the login agent would fail to start the new app.
- `packaging/installer/pkg-full-components.plist` already pins the bundle to `Applications/Tainer Menu.app`, `postinstall-full` already bootstraps the `io.cyber5.tainer.menu` agent, and `tainer-uninstall.sh` already removes the bundle and the agent. None of these change.
- The bundle id (`io.cyber5.tainer.menu`) matches the Go app, so the agent label and the uninstaller keep working.
- The app finds the CLI via `TAINER_CLI`, then `/opt/tainer/bin/tainer`, then `PATH`. It needs a CLI that supports `tainer watch --json`; against an older CLI it stays in the recovering state with an "update tainer" hint and turns unreachable after 60 seconds.
- Sandboxing is not used (it spawns the CLI). Hardened runtime needs no extra entitlements for subprocess launch, but verify once with a signed build.

### Signing and notarisation (human steps)

Nothing in this directory signs or notarises. `make-app.sh` output is unsigned apart from the linker's ad-hoc signature, and will not pass Gatekeeper on another machine. A person with the Cyber5 credentials has to:

1. Export `TAINER_SIGNING_IDENTITY` (Developer ID Application) and `TAINER_SIGNING_INSTALLER_IDENTITY`, then run the pkg targets (`make pkg-full-signed`) so the bundle is signed with hardened runtime and a secure timestamp.
2. Submit the signed installer for notarisation (`xcrun notarytool submit ... --wait`) using the team's notary credentials, then staple the ticket (`xcrun stapler staple`).
3. Verify on a clean machine: `spctl --assess --type install` on the pkg, `codesign --verify --deep --strict` on the installed app, and a login-agent launch after install.

## Parity with the Go app (`cmd/tainer-menu`)

| Go app feature | SwiftUI app |
| --- | --- |
| Bar icon: full colour when healthy with pods running | Bar icon, active state |
| Bar icon: dimmed when healthy with nothing running | Bar icon, idle state |
| Bar icon: pulsing bars while recovering | Bar icon, recovering state, driven by `PulseDriver` |
| Bar icon: red cross after unreachable over a minute | Bar icon with red unreachable badge; escalation after 60 s in `WatchClient` |
| `TAINER_MENU_ICON` icon-style override | Dropped; one brand mark |
| Brand header row opening tainer.dev | `BrandHeader` |
| Status line with coloured dot (checking, healthy N pods, recovering with detail, healing failed) | `StatusHeader` pill (HEALTHY, RECOVERING, UNREACHABLE) with counts |
| One row per pod: status dot, domain and name; running pods first, then alphabetical | `PodRow`, same ordering, status shown by icon tint and opacity |
| Pod cap of 16 pre-created slots | No cap; all registered projects (stream plus `tainer list --json`), pages of 10 with running pods first |
| Submenu action: Open in browser | Inline icon button, opens the pod's domain |
| Submenu actions: Restart, Stop (running pods) and Start (stopped pods) | Inline icon buttons by pod state, with a spinner while the command runs |
| Action failure feedback (status line only, errors ignored) | User notification banner on failure (`ActionRunner`) |
| Run doctor (check & heal) | "Run Doctor" item, runs `doctor --fix --json` |
| Quit item | "Quit Tainer Menu" item |
| Cyber5 footer opening cyber5.io | Footer with Cyber5 mark and text link |
| Event stream via `tainer watch --json`, respawn with backoff, wake on action | `WatchClient`, same stream and contract; updates arrive from the stream rather than a forced wake |
| Fixed CLI path `/opt/tainer/bin/tainer` | `CLILocator`: `TAINER_CLI`, installed path, then `PATH` |
| Pod management beyond start, stop, restart | New: "Manage" button per pod and "Open Control Dashboard" item, both open the web app |
| Logs, config editing, deploy, pod creation | Webapp's job |
| Light and dark menu backgrounds via translucent logo pill | Native light and dark palettes (`Theme.swift`) |
