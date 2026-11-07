# Self-contained control pod — design

Date: 2026-11-07. Status: approved by Leni 2026-11-07.

## Intent

Installing tainer should give a complete, working setup: the CLI, the menu bar app, and my.tainer.me running the tainer control dashboard. Today the control pod is an ordinary nodejs project that happens to be named `control`; its only link to the dashboard code is a hand-made entry in `~/.config/tainer/projects.json` pointing at Leni's checkout of `cyber5-io/tainer-control`. On any other machine there is no control pod and nothing at my.tainer.me.

After this work the control pod is part of tainer itself: a sealed, deployed app whose image carries the built dashboard, provisioned by tainer, always running while the runtime runs, and updated like any other pod.

Terminology (agreed): **control pod** = the pinned pod `control`; **tainer control** = the dashboard app (repo `cyber5-io/tainer-control`).

## Context: why the trust model is simple

tainer itself needs no authentication and only acts on existing pods on that machine, under that user. The tainer account (id.tainer.dev) exists to identify users and collect usage data; tainer reads no remote database. So anything with access to the control gateway can do at most what the machine's user can already do in their own terminal — the design protects against a *project* gaining that access without the user's knowledge, not against the user.

## Goals

1. A fresh `.pkg` install ends with my.tainer.me serving the released tainer control, with no user step.
2. The control pod runs whenever the runtime runs, cannot be stopped/destroyed by the user, and goes down only when cyberstack does.
3. `tainer update` checks every existing pod, the control pod included; updating the control pod pulls the newer image and so updates the dashboard.
4. Dashboard data (settings, previews) survives updates.
5. Cyber5 developers (Leni and others, always using tainer) can develop tainer control from a checkout at a separate domain (e.g. `devmy.tainer.me`) side by side with the released my.tainer.me, with the same gateway and Chromium access.
6. Releasing is explicit: pushing a version tag in `tainer-control` builds and publishes the new control image (same convention as every tainer image).

## Non-goals

- tainer self-update, the version endpoint on get.tainer.dev, the y/N `tainer update` prompt and the menu/dashboard "updates available" messages (the wider "releases and updates" design, saved direction `project-releases-updates-design`). This design only needs `tainer update` to already treat the control pod like other pods.
- Hiding the dashboard's code. The published image is public (users pull anonymously), so its built JavaScript is readable; that is accepted ("we don't need to hide it, we just don't need to expose it" — the source repo stays private).
- Multi-user sharing of one control pod: each macOS user has their own runtime, pods and control pod.

## Anti-goals

- A control pod that depends on any folder the user can see or edit.
- Any repo or downloaded project being able to grant itself gateway access just by what is in its files.
- An update that wipes `data/` (settings, previews).
- User-facing copy mentioning podman, docker, engine, VM or container.

## Decisions

### 1. Where the control pod lives

- Pod folder: `<tainer config dir>/control/`, i.e. `~/.config/tainer/control/` (`config.BaseDir()` + `control`; honours `XDG_CONFIG_HOME` like the rest of tainer's config). *Leni was open to `~/.tainer` or `~/Library/Application Support`; this is the controller's call because tainer already keeps `projects.json` there.*
- Contents: `tainer.yaml` and `data/` only. **No `html/`, no `db/`** — the code is in the image and the dashboard uses no database (`database: none` today; `data/` holds `settings.json`, `thumbnails/`, `memwatch.log`).
- Created by tainer on first run for that user (the `.pkg` installs as root and must not create per-user folders). Idempotent: if it exists, leave `data/` alone; rewrite `tainer.yaml` only if tainer's built-in definition changed.
- The registry entry for `control` points at this folder. `tainer list` keeps hiding it as it does today.

### 2. The image carries the dashboard

- New image `ghcr.io/cyber5-io/tainer-control:<version>` (plus a moving channel tag, see §4) = the existing `tainer-control-app` runtime (node + Chromium) + the built tainer control app, started from inside the image (no `/var/www/html` mount). It keeps the `TAINER_CHROMIUM_PATH` and everything the current control pod gets (gateway token mount, router address).
- `data/` is mounted where the app already expects its data mount, so `settings.json` and `thumbnails/` keep working unchanged.
- The current `tainer-control-app` image in tainer-images stays as the base/runtime layer; builder decides whether the new image builds FROM it in tainer-control's CI or tainer-images produces both.
- The image declares the oldest tainer it works with (image label, e.g. `io.tainer.min-version`). tainer refuses to switch the control pod to an image needing a newer tainer and says so ("update tainer first").

### 3. Provisioning and always-on

- The CLI ensures the control pod exists and is running every time it brings the runtime up (the path that spawns cyberstackd), and the menu bar app's start path goes through the same code. Users never run `tainer start control`.
- It stays pinned: `stop`, `destroy`, `init` of `control` remain refused for users (existing `IsPinnedProject` guards).
- Every platform tainer runs on gets it — macOS and Linux alike (Leni, 2026-11-07: "the control pod is part of tainer, as the menu app is"). The trigger therefore can't be "tainer spawned the runtime" (on Linux systemd starts cyberstackd): tainer ensures the control pod once per runtime boot, after any command connects to the runtime; a first image download runs in a detached background process so the user's command is never held up.
- First run on a machine that already has a hand-registered `control` project (Leni's): re-point the registry entry to the new folder, copy `data/settings.json` and `data/thumbnails/` from the old project folder if present, and leave the old checkout untouched. Leni then renames her checkout's project (§5).

### 4. Releases, pinning and updates

- `tainer-control` repo: work happens on `main`; pushing a tag `vX.Y.Z` runs CI that builds and publishes `tainer-control:X.Y.Z` and moves the channel tag (e.g. `tainer-control:stable`). The ghcr package must be public (anonymous pulls; remember ghcr creates packages private by default).
- The `.pkg` pins the control image version it ships with (the version known to work with that tainer). A fresh install provisions the pod on that version.
- `tainer update` (and `tainer update control`) treats the control pod like any pod: if the channel tag points at a newer image whose min-version the installed tainer satisfies, it pulls it and recreates the pod, keeping `data/`. `tainer update --check` lists it like other pods.
- The busy-overlay update failure (tain-101) applies to the control pod too and is fixed separately, before this ships.

### 5. Developing tainer control (dev track)

- After the new `.pkg` is installed, every developer's my.tainer.me runs the released dashboard like everyone else's.
- A developer's checkout of `tainer-control` is an ordinary tainer project at another domain, e.g. `devmy.tainer.me`, with project name ≠ `control` (e.g. `control-dev`). Comparing my.tainer.me and devmy.tainer.me side by side is the approval step before tagging.
- The checkout's `tainer.yaml` carries a marker (e.g. `control-dev: true`). A project with the marker gets the control pod's runtime: the Chromium-enabled image, the gateway token mount and the router address — **only if** the folder has been approved on that machine:
  - `tainer control dev allow` (run in the folder): shows the folder path and a plain warning that the project will be able to run any tainer command on this machine, including deleting pods and reading their databases; asks y/N; records the absolute path in the user's local tainer config.
  - `tainer control dev list` and `tainer control dev revoke [path]`.
  - The approval is tied to the folder path; moving the checkout needs a new approval.
  - Without both the marker and the approval, the project is a plain nodejs project (no token, no Chromium).
  - These commands stay out of the main `tainer --help` (documented in the tainer-control repo), but are not secret.
  - The built-in dashboard shows a notice while any dev approval is active.
- A regular user who runs `allow` only affects their own machine and their own pods, and only for a folder carrying the marker — no more than they could already do in their terminal.

## Settled after the builder check (controller's calls — Leni to confirm while reading)

1. **Shape:** the control pod keeps today's two-container nodejs shape (caddy web container proxying to the app container). The new image overrides the app container's entrypoint to run the built adapter-node server from inside the image; no `/var/www/html` bind mount, no dev server, no install step at start.
2. **Pin vs channel — never downgrade:** the `.pkg` pin is a floor, not a target. tainer records the control image version actually running (in its own state, next to the registry entry). On provision or re-provision it uses the newer of {pinned version, recorded version}. `tainer update` moves to the channel tag's image when it is newer than the running one and its min-version is satisfied. Upgrading tainer never rolls the control pod back.
3. **Where the pin lives:** a Go constant in the tainer CLI (e.g. `controlImageVersion`), bumped by hand in the release commit. Release order: tag tainer-control first (publishes the image), then bump the constant and cut the `.pkg`.
4. **Min-version enforcement:** the label is read from the remote image config before pulling (the existing remote-config lookup). Missing label → allowed. Dev builds of tainer (no release version) → allowed. The pinned version must satisfy its own tainer by construction (release order above). `tainer update --check` shows a blocked control image as "needs a newer tainer".
5. **Dev approval trust boundary:** accepted as "same as the user's own terminal" (Leni, 2026-11-07: tainer only acts on that user's own pods; id only authenticates). Code a project runs (e.g. npm postinstall) already runs as the user and could invoke tainer directly, so no integrity protection (keychain, root-owned files) is built. Two cheap guards: `tainer control dev allow` requires an interactive terminal (refuses piped stdin), and the approval is checked every time the dev project's containers are created or started; `revoke` recreates a running dev pod without the token.
6. **Dev project runtime:** a marked and approved project stays a source-mounted nodejs project (as the control project is today) on the base `tainer-control-app` runtime image, which tainer-images keeps publishing; `TAINER_PROJECT_MOUNT`/`TAINER_PROJECT_PATH` keep working. `ImageRef` and the gateway injection switch from "is pinned" to "is pinned, or is marked and approved" — the pinned control pod gets the sealed `tainer-control` image instead.
7. **Image build:** in tainer-control's own CI on tag, `FROM` the published base for node 22. Base-image security refreshes reach users through a tainer-control patch tag; the workflow also allows a manual dispatch to rebuild the current tag. Making the new ghcr package public is a one-time manual step for Leni after the first publish.
8. **First run and migration:** the "ensure control pod" step runs in the CLI path that brings the runtime up; the menu bar app starts the runtime through the CLI, so it gets the same step. If the image can't be pulled (offline first run), warn once and carry on — the command the user ran still works, and the next runtime start retries. The existing `control` container and cgroup are destroyed and recreated (the old one bind-mounts `html/`). Leni's checkout keeps its folder; until its `tainer.yaml` is renamed (e.g. `control-dev`, `devmy.tainer.me`), `tainer start` there is refused as pinned, which is the reminder to rename it.
9. **Budget:** pod size `small`, as today. If Chromium plus the node server hit the budget (cf. tain-99), raise it to `medium` in the built-in definition.

## Constraints

- Repos: `tainer` (provisioning, pinning, update, dev approval), `tainer-control` (build output runnable from the image, CI on tag, dev-track docs), `tainer-images` (runtime base image, if it stays there), `cyber-stack` only if the gateway needs a change (not expected).
- Follow each repo's CLAUDE.md; no AI mentions in commits; user-facing copy rules apply.
- tainer-control is private on GitHub; CI building it must run in that repo (or have read access).
- Single-line shell commands in any docs aimed at Leni.

## Left to the builder

- Exact image name/tags, label name, and where the image is built (tainer-control CI vs tainer-images), within §2 and §4.
- How the app is started inside the image (adapter-node build + entrypoint), and the mount point for `data/` (match what the app reads today).
- The exact `control-dev` marker key and where approvals are stored in tainer's config.
- How `tainer update` discovers the newer control image (channel tag digest compare, as it does for other pods' tags).
- Copy for the `allow` warning and the dashboard notice.
