# Native Menu Apps — design

Status: DRAFT for Leni's review (2026-10-31). Not yet approved.
Companion specs: `2026-10-31-host-bridge-design.md` (the watch stream and
JSON contract this consumes), `2026-10-31-control-webapp-design.md` (where
"Manage" leads).
Visual reference: the "Tainer UI Mockups" canvas (menu dark/light
artboards), https://claude.ai/artifact/VpCdTiu2sifKNNat6KqGuJ.

## Intent

The menu bar face of tainer: a super-light, truly native tray app that
exposes tainer on the OS — at-a-glance health in the bar icon, quick pod
actions in the dropdown, and a "Manage" path into the control webapp for
everything else. It is free and never asks for a login (part of the
deliberate contrast with OrbStack's login-walled free tier). It is a
shortcut, not a second dashboard: anything beyond quick status/manipulation
belongs to the control webapp.

Scope of this spec: the **SwiftUI macOS app** (MVP), with the Linux tray
equivalent following after the laptop validates the Linux port, and Windows
later. The existing Go fyne/systray `tainer-menu` remains the shipping
implementation on every platform until its native replacement reaches
parity there.

## Goals

- Feature parity with today's `cmd/tainer-menu`, restyled per the approved
  canvas artboards (see inventory below) — "what's in there, but native and
  beautiful, as proper Mac apps should be" (Leni, 2026-10-31).
- Instant state: live updates from the watch stream, not 5-second polling.
- Tiny: negligible RAM/CPU, instant launch, no dock icon — a menu bar extra
  in the platform's own idiom (SwiftUI `MenuBarExtra`).
- Zero login, zero configuration. Install tainer, the icon is there.

## Non-goals

- Any feature the dropdown inventory below does not name: no pod creation,
  no settings, no log viewing, no hosting/push-live surface. "Manage" hands
  those to the control webapp.
- Windows implementation (TBD with the Windows port — the Go systray app
  is the placeholder there).
- Replacing the Go app before the SwiftUI app reaches parity on macOS.

## Anti-goals

- Login or account awareness of any kind in the menu app.
- Growing into a mini-dashboard. Every "could we also show…" that is not
  quick status/manipulation is the webapp's job; the menu app's size is a
  feature.
- Linking tainer internals. Today's Go app imports
  `pkg/tainer/{engine,pod,manifest,runtime}`; no native app ever does —
  the JSON contract is the only surface (host-bridge spec §d).
- UX copy that says podman/docker/engine/VM/container.

## Constraints

- Data in: `tainer watch --json` as a subprocess (hello snapshot + deltas);
  actions out: spawn the installed CLI (`tainer start|stop|restart <pod>
  --json`, `tainer doctor --fix`), exactly as today's app shells out —
  zero business logic in the app.
- 8GB floor: the app itself is a rounding error, and it adds no daemon —
  if the engine is down, it renders that state; it may wake the engine only
  via a CLI command the user chose.
- Brand assets: the proper per-theme logo lockups (light wordmark on dark,
  dark wordmark on light — `packaging/brand/tainer-logo-outlined-*.svg`),
  never the white-pill plated fallback.
- Brand links keep the `/go/` redirect behaviour from today's app
  (`tainer.dev/go/tainerapp`, `cyber5.io/go/tainerapp`): permanent short
  paths, server-side retargeting/credit, nothing tracked in-app.
- No AI attribution anywhere.

## Decided hows

### Bar icon — four states (carried from today's app)

Full-colour mark (healthy, pods running), dimmed (healthy, nothing
running), pulsing (stack unreachable, self-healing), red ✗ (unreachable
over a minute — healing failed). States derive from `engine` events on the
watch stream; the recovering/failed timing logic lives daemon-side (host
bridge), not in the app.

### Dropdown — the approved canvas design

Top to bottom (dark and light artboards on the canvas are the reference):

1. **Brand header:** tainer logo (per-theme variant) + version chip.
2. **Status line:** HEALTHY in a green pill (state-coloured: amber pill
   "RECOVERING", red "UNREACHABLE") + "N of M pods running" with the
   counts emphasised.
3. **PODS section:** one row per pod — pod box-icon coloured by state
   (teal running, grey stopped), name over domain, and **inline action
   icons** (no hover submenu): open-in-browser, restart, stop for running
   pods; start for stopped; always a teal **Manage** icon that deep-links
   to the control webapp's pod detail
   (`https://my.tainer.me/pods/<name>`).
4. **Open Control Dashboard** (teal, dashboard icon) — the webapp's front
   door; **Run Doctor — check & heal**; **Quit Tainer Menu** (quits the
   menu app only, never the stack).
5. **Footer:** Cyber5 logo (per-theme) + "tainer is a Cyber5 product",
   linking via `/go/`.

Rows reflect watch events immediately (a started pod flips to running with
no refresh gesture). An in-flight action shows its row in a transient
working state; failures surface as the row reverting plus a system
notification naming the pod — the menu app never grows error dialogs.

### Platform sequencing

- **macOS (MVP):** SwiftUI `MenuBarExtra`, signed and bundled in the .pkg
  beside the CLI; replaces the Go app in the macOS package once at parity.
- **Linux (post-laptop):** tray equivalent (StatusNotifierItem/AppIndicator)
  consuming the identical watch stream; design transfers one to one;
  toolkit is the builder's call after the laptop validates the port.
- **Windows (later):** with the Windows port; until then (and until each
  native app ships), the Go fyne/systray app — itself migrated to the watch
  stream per the host-bridge spec — remains the shipping implementation
  everywhere.

## Left to the builder

- SwiftUI implementation details: MenuBarExtra style, watch-subprocess
  lifecycle (restart/backoff when the CLI or stream dies), icon rendering
  for the four states in light/dark menu bars (template vs colour, matching
  today's `TAINER_MENU_ICON` style options or simplifying them).
- Exactly how Manage/dashboard links open (default browser via URL; no
  embedded webview).
- Pod-row overflow behaviour past a sane count (today's app pre-creates 16
  slots; the native app has no such systray limitation — scroll or "and N
  more…" is the builder's call).
- Packaging/signing mechanics within the existing .pkg pipeline.

## Questions for Leni

None open — the inventory is deliberately frozen to today's app plus the
approved canvas restyle; anything new belongs to the control webapp.
