# Update Check + Updates Page Redesign (TAIN-122)

## Intent

`tainer update` cannot say whether an update exists: `updated: true` is
unconditional, and the dashboard's Updates page therefore lists every pod
(30 today) with no signal. Leni's direction: the page should show only
pods with updates available, on bigger cards that describe each pod's
parts, with three ways to act — update one pod, update everything, update
a selected lot. That requires tainer itself to learn to check for updates
without pulling, and to stop booting stopped pods as a side effect of
updating them.

## Goals

- `tainer update --check [--json]`: report, per registered pod, whether
  any of its images has a newer version in the registry — without
  pulling anything or touching any pod.
- Updating a stopped pod leaves it stopped (pull + recreate containers,
  no start). "Update all" therefore never boots idle pods.
- Updates page: default view lists only pods with updates available
  (plus "N pods are up to date" and a toggle to show all); bigger cards
  carrying the pod's parts description; act on one pod, on all pods with
  updates, or on a checked selection.

## Non-goals

- No engine (cyber-stack) changes; the check runs in the CLI against
  the public image registry.
- No automatic/background update checks or notifications (later).
- No menu-app changes.

## Anti-goals

- Pull-to-find-out: the check must be metadata-only. 30 pods must check
  in a few seconds on a normal connection.
- A second source of truth for what "a pod's images" are: the check uses
  the same ImageRef/RolesForPod the updater uses.

## Decisions (how it gets made)

### The check (tainer CLI, no engine changes)

- Images live at `ghcr.io/cyber5-io` (`TAINER_IMAGE_REPO` override
  respected). For each VISIBLE registered project (pinned `control`
  excluded) that has an engine pod (containers exist): resolve each
  role's `ImageRef(m, role)` via `RolesForPod(manifest)`.
- **Local side**: new `engine.Client.ImageInspect(ctx, ref)` → `GET
  /images/{ref}/json` → `Id` (containers-storage image ID, which is the
  config digest hex) and `Architecture`. Missing image locally counts as
  update available (a pull would fetch something new).
- **Remote side**: anonymous registry v2 flow per unique ref —
  `GET https://<host>/token?service=…&scope=repository:<repo>:pull` (or
  the `WWW-Authenticate` challenge's realm), then
  `GET /v2/<repo>/manifests/<tag>` with Accept headers for OCI
  index/manifest and Docker list/v2. A multi-arch index is resolved to
  the entry matching the local image's architecture (default linux/arm64)
  and that manifest fetched; the manifest's `config.digest` is compared
  to the local Id (normalised: strip `sha256:`). Different or missing →
  update available for that role.
- Refs are deduped across pods (every pod shares tainer-caddy-web) and
  fetched with bounded concurrency (6). Registry failures mark the
  affected pods `"unknown"` rather than failing the run; the error text
  rides along.
- Registry-only pods (no containers yet) are EXCLUDED from the report —
  their first start pulls fresh images anyway.
- JSON (added to docs/api/cli-json-v1.md with the existing drift-guard
  test pattern):

```json
{ "v": 1, "checkedAt": "<RFC3339>",
  "pods": [ { "name": "blog", "type": "wordpress", "domain": "blog.tainer.me",
    "state": "running|stopped", "updateAvailable": true,
    "parts": "WordPress · PHP 8.3 · MariaDB 11 · Caddy",
    "roles": [ { "role": "app", "image": "ghcr.io/cyber5-io/tainer-wordpress-app:php-8.3",
                 "updateAvailable": true } ],
    "error": "registry unreachable (optional)" } ],
  "upToDate": 27, "available": 2, "unknown": 1 }
```

- `parts` is a human line derived from the manifest: type label +
  runtime versions + DB engine + "Caddy" for the web role — built by one
  function with unit tests per project type. UX copy rules apply (never
  "container"/"image" in the human line's vocabulary beyond product
  terms; "parts" is the user-facing word).
- Styled/plain output: one line per pod with updates, then the summary
  counts; `--check` exits 0 even when updates exist (it is a report, not
  a gate).

### Stopped pods stay stopped

- `pod.Update` learns the pod's prior state. Stopped before → pull,
  remove containers, DO NOT start; report notes "updated (still
  stopped)". Running before → exactly today's behaviour. `update --all`
  inherits this per pod.

### Updates page (tainer-control)

- On load (and on "Check again"), POST `/api/cli/update {args:
  ["--check"]}` through the existing relay; render from the JSON above.
- Default view: only `updateAvailable` pods as cards in the existing
  auto-fill grid, but min column 320px (bigger cards). Each card: name,
  domain, type pill, the `parts` line, the roles getting updates, a
  checkbox, and an **Update** button. Below the grid: "N pods are up to
  date" (+ "M unknown — check your connection" when present) and a
  **Show all** toggle that reveals up-to-date pods as quieter cards
  (no checkbox, "Up to date" where the button was).
- Actions: card button updates that pod; header **Update all** runs
  `update <name>` sequentially over every pod with updates (NOT
  `--all`, so unknown/up-to-date pods are untouched); a sticky bar
  appears when any checkbox is ticked — "Update selected (n)" — running
  the same sequential flow over the selection. Per-pod progress and
  results render inside the cards exactly as today; after a successful
  update the pod's card flips to the up-to-date style and counts move.
- While any update runs, checkboxes and the toggles disable (one flow
  at a time, as today's `anyBusy`).

## Left to the builder

- Exact Go package layout for the registry client (suggested:
  `pkg/tainer/imagecheck`) and its test fakes (httptest registry).
- Card internals and the sticky bar's exact styling, within the design
  system (shared classes + theme utilities; text sizes via
  `text-[length:var(--text-*)]`).

## Acceptance

- `tainer update --check --json` on this machine reports 30-pod status
  in under ~5s warm, with correct counts; unit tests cover token flow,
  index→manifest resolution, digest compare, dedupe, and parts lines.
- A stopped pod updated via CLI or dashboard ends stopped.
- Webapp suite green; Updates page default view shows only
  pods with updates; all three action paths work against the live stack.
- Both contract docs updated (cli-json-v1.md) with drift-guard tests.
