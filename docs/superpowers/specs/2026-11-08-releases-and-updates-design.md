# Releases and updates — design

Date: 2026-11-08. Pre-launch item #1. Builds on the direction agreed 2026-11-06 (memory: releases & updates design) and on the self-contained control pod (spec 2026-11-07).

## Why

Today `tainer update` only updates pods. Tainer itself (CLI + runtime + menu app) needs a manual .pkg download, and nothing tells a user an update exists. Tainer is about to go public, and many of its users are vibe coders who never open a terminal: for them the menu bar app *is* the update surface. They must learn an update exists, choose when it happens, and never lose their work or their running sites to it.

## Goals

- One place that says what's newest: a static `latest.json` on get.tainer.dev.
- `tainer update` lists everything that has an update — tainer, the menu app, the control dashboard, pods — and asks y/N before doing anything.
- Tainer updates itself: download, verify, install. Same code whether started from the terminal, the menu, or the dashboard.
- The menu app has its own update channel, so it can ship between tainer releases.
- The menu bar shows that updates exist, without alarm and without nagging.
- Updating tainer warns first if pods are running, lets the user postpone, and afterwards brings back exactly the pods that were running.

## Non-goals

- **Nothing updates automatically.** No background installs, no overnight pod updates, no auto-download. Leni: "especially for dev tools, an update at the wrong time can be catastrophic." Checking for updates in the background is fine; acting on them always needs a click or a command.
- No Sparkle or other update framework, no separate signing keys.
- No release channels (beta/stable) for tainer itself yet.
- Linux/Windows self-update (macOS only for now; on other platforms `tainer update` lists the tainer update and prints where to get it).
- The dev/live domain schema (`live.domain`, `TAINER_ENV`) — moved to the html→app rename design.

## Anti-goals

- An update that stops a user's running sites without telling them first.
- A coloured menu icon that makes "update available" look like "something is broken".
- Installing anything that isn't verifiably Cyber5's.

## Decisions

### latest.json

Served at `https://get.tainer.dev/latest.json` (same host certsync already uses; never under tainer.me). Static file, published as part of the release. Shape (field names are the builder's call; content is fixed):

- tainer: version, pkg URL, pkg sha256, short release notes (one paragraph, plain words), minimum macOS if relevant.
- menu: version, menu-only pkg URL, sha256, notes, **minimum tainer version** it needs.
- control: current dashboard image tag (informational — the control pod's own update path, image labels and `io.tainer.min-version`, stays as built in 1.4.0).

Checked at most once a day per machine, cached on disk, never blocking any other command, failing silently (a failed check just means "no news"). `tainer update --check` forces a fresh check.

### Packages and hosting

- The full pkg (CLI + runtime + menu app) as today, and a new **menu-only pkg** (`/Applications/Tainer Menu.app` + its LaunchAgent, relaunch in postinstall like the full pkg does today).
- Both signed (Developer ID Installer) and notarised; the menu app inside signed (Developer ID Application).
- **Hosting (Leni, 2026-11-08):** pkgs and latest.json live on get.tainer.dev. The tainer repos are private, so GitHub release assets aren't publicly downloadable. A release script (`make release-files` or similar), run after notarisation, writes a folder laid out exactly as get.tainer.dev serves it: `latest.json` at the root, and pkgs under `releases/<version>/`. latest.json carries the sha256s.
  - **For now:** Leni uploads that folder by hand.
  - **Later:** the dist files reach get.tainer.dev through its repo, possibly a second repo on the server symlinked in. That part isn't built now; the folder layout just has to make it a straight copy. The ~100 MB pkgs must not go into an ordinary git history.

### Verification before installing

Download to a private temp dir; check sha256 against latest.json; check `pkgutil --check-signature` reports Developer ID Installer for Cyber5 Internet Ltd, team C8J8D9XJP6, and that it's notarised (`spctl -a -t install`). Any mismatch: delete the download, refuse, plain message. latest.json is fetched over HTTPS only.

### `tainer update`

- With no arguments: lists every available update (tainer, menu, control, each pod), then asks y/N. `--yes`/`-y` skips the question. `--check` lists only (existing behaviour, extended to tainer and menu). `--json` for the menu and dashboard.
- Targets: `tainer update tainer`, `tainer update menu`, plus the existing per-pod / control forms.
- If the menu needs a newer tainer than installed, it's listed as "needs tainer X first" and the combined update does tainer first (the full pkg already contains a menu app; if that's new enough the separate menu update disappears).

### Updating tainer: warn, postpone, resume

1. Before installing, tainer lists the running pods. If any: "Updating tainer restarts your running pods (shop, blog, c5test). Update now?" — in the terminal a y/N; in the menu a dialog with **Update now** / **Later**. No running pods: no warning, just proceed.
2. **Later** changes nothing: the update stays listed, the badge stays, no reminders or popups.
3. On go: tainer records the set of running pods (project folders) in a resume file in tainer's config dir, then opens the verified pkg; macOS asks for the admin password. Installer's postinstall stops the old runtime as it does today.
4. After the install, the pods in the resume file are started again, and only those; stopped pods stay stopped. Resuming happens exactly once (lock + delete the file when done), whoever gets there first: the CLI that launched the installer (it waits for Installer to finish, then checks the installed version), or the menu app on its relaunch. If the user cancels the password prompt or the install fails, the version hasn't changed: nothing was stopped by tainer, the resume file is discarded, and the runtime is restarted if postinstall had stopped it.
5. While resuming, the menu shows "Restarting 3 pods…" and the CLI prints progress; a pod that fails to come back is reported by name, it doesn't stop the others.

Menu-only updates don't touch pods: no warning, no resume.

### Pod updates

Unchanged mechanism (`tainer update <pod>`). A running pod gets a one-line warning before it's restarted (menu: confirm dialog; CLI: part of the y/N listing). No resume file needed — the pod update itself brings the pod back up.

### Menu bar app

- Checks by running `tainer update --check --json` (the CLI is the API; the menu never downloads or installs anything itself): at launch, then every few hours, and after any update completes.
- **Icon:** health keeps owning the icon's colour/opacity exactly as today. "Updates available" is a **small dot badge** on the icon's corner in a neutral/accent colour — never amber or red. If tainer is unhealthy, the health state wins and the badge stays.
- **Top of the menu, only when relevant:** "Tainer 1.4.1 is available — Update…" (and/or "Menu 1.4.1 …"), release notes as tooltip. Clicking starts the warn → update → resume flow above.
- **Pods:** a pod with an update is shown in a different colour with a download icon; the tooltip names what's new. Clicking it updates that pod (with the running-pod warning). "Update all pods" appears when more than one pod has an update.
- Things that need more than a click (new pod, settings) keep opening the dashboard, as New Pod already does.

### Dashboard

The Updates page lists the same items from the same `--json` output and offers the same actions through the control gateway. If the gateway can't open the installer in the user's session, the dashboard says to use the menu bar or `tainer update` instead of failing silently. Same warn-before-restart rule.

## Calls made after the builder check (Claude's calls — check these)

- **Who resumes pods.** The CLI that opened the installer and the dashboard session can both die during the install (postinstall stops the runtime). So the full pkg's postinstall, after its existing steps, spawns a detached `tainer update resume` as the console user (`launchctl asuser`), exactly like it already relaunches the menu app. As a safety net, the menu app on launch and the CLI that opened the installer (once Installer closes) also run the resume. A lock plus deleting the file makes sure it happens once. The resume file also records the tainer version it was written for, so a stale file from a cancelled install is discarded rather than acted on.
- **Success and cancel.** Success means the installed receipt's version (`pkgutil --pkg-info io.cyber5.tainer`) equals the version in latest.json. The CLI that opened Installer waits for it to quit, then checks. If the version is unchanged (cancelled or failed), it discards the resume file. If postinstall had already stopped the runtime, it starts the recorded pods again. No timeout: the user can leave Installer open as long as they like, and the lock is held only while a resume is actually running.
- **Menu-only pkg.** A new Makefile target builds it with identifier `io.cyber5.tainer.menu`. The menu app's version is independent of tainer's: it lives in the app's Info.plist (`CFBundleShortVersionString`) and tainer reads it from there. The full pkg carries whatever menu version is current. A menu-only install asks for the admin password like any pkg; this is accepted, since it's the same verified path as tainer updates.
- **JSON.** `tainer update --check --json` stays additive: the existing `pods` shape is unchanged, and new top-level keys are added for tainer, menu and control. Old menu apps running against a new CLI keep working.
- **Dashboard.** The dashboard lists tainer and menu updates and starts them through the control gateway with the same command. It shows "Tainer is updating — this page comes back when it's done" and reconnects afterwards. If the gateway can't open Installer in the user's session, it falls back to "update from the menu bar or with `tainer update`".
- **The bundled tainer.me key** is already public: it ships in every pkg and certsync fetches it from get.tainer.dev. Hosting pkgs publicly changes nothing there.
- **Other platforms.** latest.json is macOS-only for now. Elsewhere, `tainer update` says a newer tainer exists and points to tainer.dev.

## Constraints

- User-facing words never say podman, docker, engine, VM or container.
- No new runtime dependencies for the CLI beyond Go stdlib + existing modules.
- Postinstall keeps the graceful TERM-only rule (never kill -9 the runtime).
- Must work offline: every check failure is silent and non-blocking.

## Left to the builder

latest.json field names, cache location and check interval details, the menu's exact refresh cadence, the resume file format and lock, how the CLI waits for Installer (e.g. `open -W` then re-read the installed version), the dot badge's exact colour and size, and the release script that writes latest.json.
