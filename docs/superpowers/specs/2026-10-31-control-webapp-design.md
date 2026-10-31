# Control Webapp — design

Status: DRAFT for Leni's review (2026-10-31). Not yet approved.
Companion specs: `2026-10-31-host-bridge-design.md` (the API it is a client
of), `2026-10-31-tainer-accounts-design.md` (the identity it logs into).
Visual reference: the "Tainer UI Mockups" canvas,
https://claude.ai/artifact/VpCdTiu2sifKNNat6KqGuJ (dashboard dark/light,
pod detail — approved direction, 2026-10-31).

## Intent

The universal tainer UI: a full-featured dashboard every install ships with,
served locally as `my.tainer.me` (decided — Leni, 2026-10-31;
`control.tainer.me` was the rejected alternative). It is a website, so it is the UI on macOS, Linux
and Windows from day one; the native menu apps are thin shortcuts that
deep-link into it. It is where "Push live" will live: the blenzi hosting
funnel is a section of this app, so every dashboard user is one click from
the paid product with an identity already attached (login is required from
day 0).

The product teaches its one noun here: every app is a **pod** — your own
private server. Everything beneath the pod (containers, roles, caddy,
cgroups) stays invisible.

## Goals

- Manage all tainer functions for pods on this machine: list, create,
  start/stop/restart, inspect (stats, parts, activity/logs), db
  export/import, doctor, updates, settings.
- A genuinely beautiful dashboard (the blazepress/Material-Dashboard merge,
  below) with proper light and dark themes.
- Always available while the engine runs: the pod is pinned (cannot be
  stopped) and auto-starts with the engine.
- Login against tainer accounts from day 0, with weekly refresh and
  uninterrupted offline work between logins.
- The blenzi HOSTING section present from v1 as a locked teaser.

## Non-goals (this phase)

- Teams, LAN routing, cross-machine or blenzi-hosted pod views (later
  specs; the layout leaves room, this phase builds this-machine-only).
- Editing app code, file management, or anything an editor/IDE does.
- Public/remote access to the dashboard. It is reachable on this machine
  only (the host bridge gateway is likewise machine-local).
- Mobile layout polish beyond "does not break on a narrow window".

## Anti-goals

- A web terminal. Ever. (Leni, 2026-10-31: "tainer itself is a terminal
  app." The CLI is the terminal surface.)
- Business logic in the webapp. It renders state and relays intents; every
  decision happens in the CLI/daemon behind the host bridge. If a feature
  needs logic the bridge does not expose, the bridge grows — the webapp
  never works around it.
- Usable-without-login mode. No login, no dashboard — the free tier is the
  CLI + menu app, deliberately.
- White main area with a dark sidebar (or any two-world theming). Both
  themes derive from one hue family; the sidebar is an undertone of the
  content.
- App pods carrying browsers or capture tooling for thumbnails.

## Constraints

- **Runs as a pinned SvelteKit pod** — dogfoods the TAIN-104 type; ships and
  updates through the tainer-images pipeline like any other image; served by
  the normal caddy-web with the wildcard `*.tainer.me` cert.
- Pinned semantics: auto-starts whenever the engine starts; `tainer stop`
  skips it; it is not listed as stoppable/destroyable anywhere. Engine down
  = dashboard down (no extra always-on RAM on idle machines; 8GB floor).
- Strict client of the host bridge: `tainer <cmd> --json` via the gateway,
  `tainer watch` for live state, versioned envelope `v:1`. No `pkg/tainer`
  imports, no engine-socket access, no direct filesystem reach.
- Login: tainer accounts (`id.tainer.dev`; local dev `id.tainer.me`),
  host-side session per the companion specs; the pod holds only short-lived
  access tokens.
- UX copy never says podman/docker/engine/VM/container. The pod's parts are
  named by role ("WordPress app", "Database", "Web server").
- No AI attribution anywhere.

## Decided hows

### Design base (binding): the blazepress / Material-Dashboard merge

The purchased Material Dashboard PRO React template (running in the
migrated blazepress `cp-bp` pod) is the **design reference only** — its
patterns are rebuilt in SvelteKit; no CRA/MUI code is reused. The merged
design, as mocked on the canvas:

- **Shell:** left sidebar — Pods (home), Doctor, Updates, Settings, then a
  HOSTING section ("Live sites — soon", locked until blenzi lands) and the
  account chip (avatar, signed-in state, sign-out). Content header: page
  title + teaching line, HEALTHY status pill, "N of M running" count,
  search, New pod button.
- **Pod cards** (the signature element): a floating thumbnail header
  overlapping the card top, colored by project type (teal sveltekit/node
  family, blue wordpress/php, orange react/SPA), carrying the pod's site
  screenshot; on hover the header lifts and an action icon row is revealed
  beneath it (open, refresh thumbnail, start/stop, logs, manage) — the
  blazepress hover, rebuilt. Card body: name, type pill, domain link, size
  pill, memory-vs-budget bar, status footer line with uptime and Manage.
  Stopped pods: grey header with the pod glyph.
- **New-pod card:** dashed teaching card — "Drop a folder here or run
  `tainer init` — any app becomes a pod." (Folder drop may land post-MVP;
  the copy and the card ship either way, opening the create flow.)
- **Pod detail (Manage view):** stat tiles (status/uptime, size + "one
  budget for the whole pod", memory bar, domain + cert state), an "Inside
  the pod" parts list (role name, friendly tech label, per-part memory,
  health — never the word container), recent activity with "open full
  logs", tabs Overview/Logs/Database/Settings, and the locked "Take <pod>
  live" strip.
- **Theming:** light and dark ship together, system-following with a manual
  override. One slate hue family; the sidebar is always an undertone of the
  content surface. Brand: teal `#00D4AA` (primary/running), orange
  `#FF6B35`, blue `#4E9EF4`, Space Grotesk for headings, Inter/system for
  UI text. Type-colored elements (card headers, pills) are what pop in both
  themes.

### Live data

`tainer watch` (hello snapshot, then deltas) drives the pod grid, status
pill and counts; `progress` events render long operations (init, start
pulls, db import) as in-card progress. No polling loops. Mutations go
through gateway endpoints that map 1:1 to CLI commands; optimistic UI is
allowed only for transitions the watch stream will confirm within seconds.

### Thumbnails: the capture service

- Lives **inside the control pod** (its own image bakes Chromium — it is
  our image; app pods never carry browsers, which the blazepress experiment
  proved unworkable anyway).
- Captures **local pods** by loading their `*.tainer.me` URL over the
  engine network, on demand (card button / after first successful start)
  with throttling (per-pod cooldown, one capture at a time, bounded memory;
  the control pod's budget accounts for a short-lived Chromium).
- Stores PNGs in the control pod's `data/` (survives restarts/updates);
  cards fall back to the type-colored placeholder until a capture exists.
- **Blenzi-hosted sites are never captured locally**: blenzi captures
  server-side and the dashboard fetches image URLs from its API. Same card,
  two thumbnail sources, invisible to the user.

### The HOSTING section (blenzi teaser → section)

Present from v1, locked: the sidebar entry and in-card "Push live" controls
render disabled with "coming soon" copy — seeding the funnel before the
integration exists. When blenzi lands, the section's design base is the
blazepress **websites screen** (thumbnail grid in three sizes, list view,
search, cursor "load more"), and its data model grows from the blazepress
Prisma schema (`/Users/lenineto/ddev/blazepress/orchestration/html/prisma/
schema.prisma` — users/roles, accounts, hosting plans, pod sizes, servers,
websites, domains, DNS, backups, deployments) — referenced as the seed, not
imported wholesale. That work is its own spec.

### Login flow

First open → login/signup screen (the only unauthenticated view; brand +
one form, no dashboard chrome). Auth is brokered by the host bridge: the
pod never stores the refresh token; "signed in as" renders from the access
token the bridge supplies. Session expiry mid-use degrades gracefully to
the login screen with state preserved where cheap.

## Left to the builder

- SvelteKit project layout, component structure, state management around
  the watch stream (reconnect/backoff, snapshot reconciliation).
- Capture service internals (Chromium flags, viewport, cooldown values,
  image sizing/retention) within the stated throttling requirements.
- Exact settings surface for v1 (theme override, thumbnail cadence, little
  else — settings is deliberately thin at MVP).
- Create-pod flow details (wizard mirroring `tainer init`'s questions via
  the gateway).
- Empty/edge states (no pods yet, engine recovering, gateway unreachable).

## Questions for Leni

None open — the dashboard's domain is decided: `my.tainer.me` (Leni,
2026-10-31).
