# html/ → app/ rename, live domain, promote — design

Date: 2026-11-09. Pre-launch item #5, the last code change before signing the 1.5.0 installer. Recon map with file:line for every touched site: the survey saved alongside the implementation plan (rename-recon).

## Intent

A tainer project's source folder is named `html/`, a leftover from the WordPress-first days that reads wrong for every other type. Before launch it becomes `app/` — a straight rename with no dual-name support, since tainer has no outside users yet. The same release teaches `tainer.yaml` the project's production domain, so Push Live has somewhere to deploy to, and gives our Enhance-deployed repos a `promote` script for the moving `live` tag.

## Goals

- `app/` is the source folder everywhere a user sees one: on disk, in docs, in CLI copy, inside the pod (`/var/www/app`, `~/app`), in `tainer exec`'s working directory.
- Existing projects migrate on their next `tainer start` without losing anything.
- `tainer.yaml` optionally carries the production domain; pods learn `TAINER_ENV` so an app can tell dev from live.
- Our repos (tainer-control, tainer-id, comms, c5test, kompozi, oneputtpro, ops, website) are renamed and keep working, including Enhance deploys.
- Releases to Enhance are promoted by moving the `live` tag with one script.

## Non-goals

- No dual-name support (`html/` keeps working) beyond the one-time migration.
- Push Live itself — only the schema and env it will need.
- The pod-versions / framework-in-image work (separate design; after this).
- Windows.

## Anti-goals

- A migration that silently creates an empty `app/` beside a full `html/` — the user would believe their code vanished. (Today's mount code would do exactly this; the migration guard must land before or with the mount change.)
- Breaking a running Enhance deploy by renaming the tracked path out from under it.

## Decisions

### The rename is both sides, shipped in lockstep

The app/dev container mounts the whole project at `/var/www`, so the host rename is visible in-container no matter what; keeping `/var/www/html` would mean two names forever. So in one release:

- tainer: `HostAppDir()` → `"app"`, `ContainerAppPath()` → `/var/www/app`, every type's `ExecWorkdir` → `/var/www/app`, reserved mount names become `app`/`data`/`db` — and `html` stays refused with the message that it was renamed.
- tainer-images: every `/var/www/html` (Caddyfiles, WORKDIRs, start scripts, post-deploy scripts, the `~/html` shell symlink and greeting) → `/var/www/app` / `~/app`. Images are republished before the pkg ships.
- `MountsVersion` bumps `"2"` → `"3"`: first start after upgrading recreates each pod's containers (same mechanism, same one-time restart as 1.4.0's mount fix). Release notes say to run `tainer update --all` after installing so moving-tag images are re-pulled; `imagecheck` flags the stale ones regardless.
- The control pod (verified live): the sealed pod bind-mounts only `data/`; its `/var/www/html` is an empty folder from the image. But `tainer exec`'s working folder moves to `/var/www/app`, which must exist in the image, so the release pins a rebuilt control image — **v1.0.2**, which also carries the two held dashboard commits (dev banner, sticky sidebar). `staleControlContainer` keeps treating `html`-mounted containers as stale (recreate) and recognises `app` as current. The dev copy (`control-dev`) bind-mounts the repo source, so it follows the repo rename plus the rebuilt `tainer-control-app:node-22` base.

### Answers from the builder check (Claude's calls)

- **Rollout window:** moving-tag images republished before the pkg means an old tainer could pull a new image in between. Accepted — we are the only users, `imagecheck` compares digests without pulling, and the order is: push kompozi main (its image clones the repo from GitHub at build time) → publish images → install the pkg → `tainer update --all`. No `tainer update` runs in the window.
- **Pushing `main` is safe everywhere:** tainer-id's Enhance deploy follows the `live` branch and c5test's the `live` tag; comms and ops deploy manually. The tainer-id Enhance switch (live branch → live tag, start command, docroot, build path) is one coordinated manual step; the old `live` branch is deleted then, and `scripts/promote` pushes `refs/tags/live` explicitly so the branch/tag name can never make the push ambiguous.
- **Unsurveyed couplings:** each repo's rename task starts with a grep of `package.json` scripts, `.github` workflows and `scripts/` for `html` before the `git mv`.
- **WordPress sites** (oneputtpro, mywp, opp): WordPress stores absolute paths in its database and sometimes `wp-config.php` constants. tainer does not rewrite databases; after each site migrates, we run `wp search-replace '/var/www/html' '/var/www/app'` and check `wp-config.php` — a manual checklist item.
- **Half-migrated state:** an *empty* `app/` beside `html/` (e.g. left by a dev build) is removed and the migration proceeds; both non-empty → refuse with a clear message.

### Migration of existing projects

One guard, early in `pod.Start` before any mount building: `app/` absent and `html/` a real directory → `os.Rename` host `html/` → `app/` and print one line (`renamed html/ to app/`). Both present → refuse with a clear message. A symlinked `html/` → refuse (mount-safety rules unchanged). The same check runs before `initcmd` scaffolding, `secrets`' `.env` writes, and the `nodeAppMissing` hint, so no path can create `app/` beside `html/`. Projects' `.gitignore`s are our own repos' edits, not tainer's job. Relative symlinks inside the project (oneputtpro's `wp-content/*` → `../data/...`) keep the same depth, so they survive the rename; the plan verifies this on a fixture.

### live domain and TAINER_ENV

```yaml
project:
    name: myshop
    domain: myshop.tainer.me      # dev, as today
live:
    domain: myshop.com            # optional, new
```

- Optional `live:` block (`omitempty`), no manifest version bump (stays version 2); `live.domain` validated with the existing `validateDomain`; merged from `.tainer.local.yaml` overlays like `project:` is.
- Pods get `TAINER_ENV=dev` (always, locally) and `TAINER_LIVE_DOMAIN=<live.domain>` when set, next to today's `TAINER_DOMAIN`. Both join the manifest hash so changing them recreates containers. Push Live later sets `TAINER_ENV=live` on the server; nothing local ever does.
- **`TAINER_*` is tainer's namespace (Leni, 2026-11-09):** the local binary always sets `TAINER_ENV=dev`, and `tainer start` refuses a project that tries to set it — a `TAINER_ENV`, `TAINER_DOMAIN` or `TAINER_LIVE_DOMAIN` key in the project's `.env` or `app/.env` stops the start with a plain message ("TAINER_ variables are set by tainer — remove <key> from <file>"). Hosting servers run a different tainer binary that owns live/staging; users never select the environment by hand.
- Nothing else reads `live.domain` yet — the router, caddy and lists stay on `project.domain`.

### promote script

`scripts/promote` lands in tainer-id (the repo that deploys from Enhance today) and c5test, copy-paste-able to any repo: `scripts/promote vX.Y.Z` verifies the tag exists and is a plain `vX.Y.Z` release tag, then `git tag -f -a live vX.Y.Z^{} -m "live: vX.Y.Z" && git push -f origin live`. Refuses anything else. tainer-id's README release steps switch from the `live` branch to the `live` tag; the Enhance "branch or tag" selection is Leni's manual switch, done together with the docroot change below.

### Our repos

Each gets one commit: `git mv html app` plus every reference the recon lists (.gitignore, Containerfile, .dockerignore, READMEs, deploy docs, CLAUDE.md). tainer-control, tainer-id, comms, c5test, kompozi, oneputtpro.com, cyber5.io/ops, cyber5.io/website. Enhance manual steps (Leni, at deploy time, per repo that deploys from git): tainer-id start command `node --env-file=../../tainer-id.env app/build`, docroot `deploys/current/app/build/client`, build command path; c5test build command `cd app && npm ci`, start `node app/server.js`; the comms cron line's `html` path. The ops token pointer in tainer's CLAUDE.md updates to `ops/app/.env`.

## Constraints

- User-facing words never say podman, docker, engine, VM or container.
- No new Go dependencies. Conventional commits, no AI mentions/trailers.
- Local pods (Leni's) must survive: migration + recreate, data and db untouched.
- tainer-images changes build but aren't published until Leni dispatches the workflow; tainer must not ship before the images exist.

## Left to the builder

Exact wording of the migration notice and refusals; test fixture shapes; whether the migration guard lives in `pod.Start` or a shared helper the other three entry points call; the promote script's exact checks; order of commits inside each repo.
