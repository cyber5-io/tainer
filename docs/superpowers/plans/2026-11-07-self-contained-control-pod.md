# Self-contained Control Pod Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A fresh tainer install provisions and always runs the control pod from a sealed image that carries the built tainer control dashboard; `tainer update` updates it; developers run their checkout at devmy.tainer.me with approved gateway access.

**Architecture:** tainer-control gets a root `Containerfile` (FROM the existing `tainer-control-app` runtime) and a tag-triggered CI workflow publishing `ghcr.io/cyber5-io/tainer-control:<version>` and `:stable`. tainer gains a built-in control pod definition under `~/.config/tainer/control/` (only `tainer.yaml` + `data/`), provisions and starts it when it spawns the runtime, runs it on a recorded image version that never goes below the CLI's pin, updates it via the `stable` channel, and grants a marked + locally approved checkout the same runtime and gateway access.

**Tech Stack:** Go (tainer CLI), SvelteKit 3 + adapter-node (tainer-control), GitHub Actions + ghcr.

**Spec:** `/Users/lenineto/dev/cyber5-io/tainer/docs/superpowers/specs/2026-11-07-self-contained-control-pod-design.md` (read it, including "Settled after the builder check").

## Global Constraints

- Repos/branches: tainer `/Users/lenineto/dev/cyber5-io/tainer` branch `control-pod` from `main`; tainer-control `/Users/lenineto/dev/cyber5-io/tainer-control` branch `control-image` from `main`. Commit only your own files by explicit path (never `git add -A` / `commit -a`). No Co-Authored-By, no AI mentions. Never push, tag or publish.
- User-facing copy never says podman, docker, engine, VM or container. Brand lowercase "tainer".
- tainer: gofmt, go vet clean; verify with `go test ./...` and `make sign` (never copy to /opt). tainer-control: tabs + single quotes; `npm test` + `npm run check` on the host; NEVER `npm run build` on the host (the image build runs it inside Docker; CI runs it).
- Control pod folder: `filepath.Join(config.BaseDir(), "control")`; contents `tainer.yaml` + `data/` only — no `html/`, no `db/`.
- Built-in control definition: name `control`, type `nodejs`, domain `my.tainer.me`, `runtime.node: "22"`, `database: none`, `pod.size: small`.
- Images: sealed `ghcr.io/cyber5-io/tainer-control:<X.Y.Z>` and channel `ghcr.io/cyber5-io/tainer-control:stable`; runtime base (unchanged, still published by tainer-images) `ghcr.io/cyber5-io/tainer-control-app:node-22`. Image label `io.tainer.min-version` = oldest tainer version that may run it.
- Pin: Go constant `controlImageVersion` in package `pod` (initial value `"1.0.0"`; the first tainer-control tag will be `v1.0.0`).
- Dev marker: `project.control-dev: true` in a project's `tainer.yaml`. Approvals stored as absolute paths in `filepath.Join(config.BaseDir(), "control-dev.json")` `{"v":1,"approved":["/abs/path", ...]}`.
- Never downgrade: the control pod runs `max(controlImageVersion, recorded version)`.

## Review Focus

1. An existing install with a hand-registered `control` project (Leni's machine): first run must re-point the registry to the built-in folder, copy `data/settings.json` and `data/thumbnails/`, leave the old checkout untouched, and recreate the pod without the `html/` bind mount. (Task 3 test `migrates a hand-registered control project`.)
2. Offline first run / image pull fails: the user's command must still succeed with one warning, and the next runtime start retries. (Task 3 test `provision failure warns and does not fail the command`.)
3. A newer `stable` image whose `io.tainer.min-version` exceeds the installed tainer must not be pulled; `--check` says it needs a newer tainer. (Task 4 test `newer image needing newer tainer is held back`.)
4. Upgrading tainer must never roll the control pod back to the CLI's older pin. (Task 4 test `pin is a floor, not a target`.)
5. A folder with the `control-dev` marker but no local approval gets no gateway token and the plain nodejs image; `allow` from piped stdin is refused. (Task 5 tests `marker without approval grants nothing`, `allow refuses non-interactive stdin`.)

---

### Task 1: tainer-control — sealed image and tag-triggered publish

**Files:**
- Create: `tainer-control/Containerfile`, `tainer-control/.dockerignore`, `tainer-control/.github/workflows/publish-image.yml`, `tainer-control/scripts/start-control.sh`
- Modify: `tainer-control/README.md` (Release section)
- Test: `tainer-control/html/src/lib/server/image-contract.test.ts`

**Interfaces:**
- Produces: image `ghcr.io/cyber5-io/tainer-control:<X.Y.Z>` + `:stable`, label `io.tainer.min-version`, label `org.opencontainers.image.version=<X.Y.Z>`. Inside: built app at `/opt/tainer-control/build`, started by `/usr/local/bin/start-control.sh` (CMD), listening on `127.0.0.1:3000` (the caddy-web proxy contract), with `TAINER_PROJECT_MOUNT=/var/www` so `dataMount()` resolves to `/var/www/data` (mounted by tainer). `TAINER_CHROMIUM_PATH` inherited from the base.

- [ ] **Step 1: Failing test** `image-contract.test.ts` reads the repo-root `Containerfile` and `scripts/start-control.sh` and asserts: `FROM ghcr.io/cyber5-io/tainer-control-app:node-22`; a `LABEL io.tainer.min-version=` line; `ENV TAINER_PROJECT_MOUNT=/var/www`; CMD is `/usr/local/bin/start-control.sh`; the start script runs `node /opt/tainer-control/build` with `PORT=3000` and `HOST=127.0.0.1` and does NOT `cd /var/www/html` or run `npm install`/`npm run dev`.
- [ ] **Step 2:** `cd html && npx vitest run src/lib/server/image-contract.test.ts` → FAIL (files missing).
- [ ] **Step 3: Implement.** Multi-stage Containerfile: stage 1 `node:22-alpine` runs `npm ci --include=dev && npm run build && npm prune --omit=dev` in `html/`; stage 2 FROM the base, copies `build/`, `package.json`, `node_modules/` to `/opt/tainer-control/`, sets ENV/LABELs (min-version build-arg `MIN_TAINER`, default `1.4.0`; version build-arg `VERSION`), CMD the start script. Keep the base ENTRYPOINT (tainer-entrypoint.sh) so sshd/exec keep working; the start script must keep the container alive like the base script (`tail -f /dev/null` after the server exits). `.dockerignore` excludes `node_modules`, `html/build`, `.superpowers`, `data`, `db`.
- [ ] **Step 4: Workflow** `publish-image.yml`: on `push: tags: ['v*']` and `workflow_dispatch` (input `tag`); multi-arch `linux/arm64,linux/amd64` with buildx; logs in to ghcr with `GITHUB_TOKEN` (`packages: write`); pushes `:${VERSION}` (tag without `v`) and `:stable`; passes `VERSION`. Must run in this repo (private source).
- [ ] **Step 5: README** "Release" section: work on `main`, test at devmy.tainer.me, then `git tag -a vX.Y.Z -m "tainer control vX.Y.Z" && git push origin vX.Y.Z`; after the first publish, make the ghcr package `tainer-control` public once (GitHub → Packages → settings).
- [ ] **Step 6:** `npx vitest run src/lib/server/image-contract.test.ts` → PASS; `npm test`, `npm run check` → PASS.
- [ ] **Step 7: Commit** `feat(image): sealed control image with the built dashboard, published on tag`.

### Task 2: tainer — control pod definition, image selection and sealed mounts

**Files:**
- Create: `tainer/pkg/tainer/pod/control.go`, `tainer/pkg/tainer/pod/control_test.go`
- Modify: `tainer/pkg/tainer/config/paths.go` (add `ControlDir()`), `tainer/pkg/tainer/pod/types.go` (`ImageRef`), `tainer/pkg/tainer/pod/start.go` (mounts for the pinned pod), `tainer/pkg/tainer/registry/registry.go` (add `ImageVersion string \`json:"imageVersion,omitempty"\`` to `Project`)

**Interfaces:**
- Produces:
  - `func config.ControlDir() string` → `filepath.Join(BaseDir(), "control")`.
  - `const controlImageVersion = "1.0.0"` and `func ControlManifestYAML() []byte` (the built-in definition from Global Constraints).
  - `func ControlImageRef(version string) string` → `ghcr.io/cyber5-io/tainer-control:<version>` (repo from the same image repo setting ImageRef uses).
  - `func controlVersion(recorded string) string` → the greater of `controlImageVersion` and `recorded` (semver; use `golang.org/x/mod/semver` with a `v` prefix, add the module if absent).
  - `ImageRef` for the pinned control pod's app role returns `ControlImageRef(controlVersion(registry ImageVersion))`.
  - Mounts for the pinned control pod: app and web containers get only `ControlDir()/data → /var/www/data` (no `html/`, no whole-project mount); the web container's docroot mount is omitted (caddy only proxies). Gateway injection unchanged for the pinned pod.

- [ ] **Step 1: Failing tests** in `control_test.go`: `TestControlManifestIsBuiltIn` (parses `ControlManifestYAML()`: name control, domain my.tainer.me, node 22, database none, size small); `TestControlVersionIsAFloor` (`controlVersion("")=="1.0.0"`, `controlVersion("0.9.0")=="1.0.0"`, `controlVersion("1.2.0")=="1.2.0"`); `TestPinnedControlImageRef` (pinned app role → `…/tainer-control:1.0.0`; a non-pinned nodejs project unchanged); `TestPinnedControlMountsOnlyData` (no mount source ends in `/html`, exactly one data mount per container, no mount of the whole project dir).
- [ ] **Step 2:** `go test ./pkg/tainer/pod/ -run 'Control'` → FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4:** focused run PASS; `go test ./...`, `go vet ./...` PASS.
- [ ] **Step 5: Commit** `feat(pod): built-in control pod definition on the sealed control image`.

### Task 3: tainer — provision and start the control pod whenever the runtime comes up

**Files:**
- Create: `tainer/pkg/tainer/pod/provision.go`, `tainer/pkg/tainer/pod/provision_test.go`
- Modify: `tainer/pkg/tainer/runtime/runtime.go` (hook after a successful spawn), `tainer/cmd/tainer/main.go` (register the hook)

**Interfaces:**
- Consumes: Task 2 (`ControlDir`, `ControlManifestYAML`, `controlVersion`, registry `ImageVersion`).
- Produces:
  - `func EnsureControlPod(ctx context.Context, eng *engine.Client) error` — idempotent: create `ControlDir()` and `data/` (0755), write `tainer.yaml` if missing or different from `ControlManifestYAML()`, never touch existing `data/`; if the registry's `control` entry points elsewhere, copy `<old>/data/settings.json` and `<old>/data/thumbnails/` (only if absent in the new `data/`), then re-point the entry to `ControlDir()` keeping `ImageVersion`; remove the old control containers if they were created with an `html/` mount; start the pod (existing `Start`) if not running.
  - `runtime.OnSpawn func(ctx context.Context, cli *engine.Client)` — called once after `startDaemon` + `waitForDaemon` succeed in `Engine` (not on the fast path).
  - cmd registers `runtime.OnSpawn` to call `EnsureControlPod`; on error it prints ONE line to stderr — `tainer: couldn't start the control dashboard yet (<reason>); it will retry next time tainer starts` — and never fails the user's command.

- [ ] **Step 1: Failing tests** (fake engine/registry seams as the pod package's existing tests do): `fresh provision creates tainer.yaml and data/ only`; `existing data is never overwritten`; `migrates a hand-registered control project` (old dir with data/settings.json + thumbnails/a.png → copied, registry re-pointed, old dir untouched); `provision failure warns and does not fail the command` (start error → hook prints the line, caller's command result unchanged); `OnSpawn runs only after a spawn` (runtime test: fast path doesn't call it).
- [ ] **Step 2:** run them → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** `go test ./...`, `go vet ./...`, `make sign` PASS.
- [ ] **Step 5: Commit** `feat(pod): tainer provisions and starts the control pod when the runtime comes up`.

### Task 4: tainer — control pod updates through the stable channel

**Files:**
- Modify: `tainer/pkg/tainer/imagecheck/check.go` (+ test), `tainer/cmd/tainer/updatecheck.go`, `tainer/cmd/tainer/main.go` (`runUpdateSingle`/`runUpdateAll` for `control`)
- Test: `tainer/pkg/tainer/imagecheck/control_test.go`

**Interfaces:**
- Consumes: Task 2 (`ControlImageRef`, `controlVersion`, registry `ImageVersion`), Task 1 labels.
- Produces: for the pinned control pod, the check reads the remote config of `tainer-control:stable` (existing remote-config lookup), takes `org.opencontainers.image.version` and `io.tainer.min-version`; states: `up-to-date` (stable ≤ running), `update` (stable > running and min-version ≤ this tainer, or min-version missing, or this tainer is a dev build), `needs-newer-tainer` (min-version > this tainer). Updating sets registry `ImageVersion` to the stable version, pulls `tainer-control:<that version>`, recreates the pod keeping `data/`. `tainer update` / `--all` include control like any pod; `--check` shows `needs-newer-tainer` as "needs a newer tainer". The tainer version comes from `main.version` (ldflags); `"dev"`/empty counts as dev.

- [ ] **Step 1: Failing tests:** `newer stable is offered`; `newer image needing newer tainer is held back`; `pin is a floor, not a target` (recorded 1.3.0, pin 1.0.0, stable 1.2.0 → up-to-date, no downgrade); `missing min-version label is allowed`; `update keeps data and records the version` (cmd-level, fake engine).
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** `go test ./...`, `go vet ./...` PASS.
- [ ] **Step 5: Commit** `feat(update): the control pod updates from the stable channel, never downgrades`.

### Task 5: tainer — dev track: `control-dev` marker and `tainer control dev allow|list|revoke`

**Files:**
- Create: `tainer/pkg/tainer/controldev/controldev.go` (+ `_test.go`), `tainer/cmd/tainer/controldev.go` (+ `_test.go`)
- Modify: `tainer/pkg/tainer/manifest/manifest.go` (`ProjectConfig.ControlDev bool \`yaml:"control-dev,omitempty"\``), `tainer/pkg/tainer/pod/types.go` + `pod/gateway.go` (predicate), `tainer/cmd/tainer/main.go` (dispatch `control`; keep it out of the main help)

**Interfaces:**
- Produces:
  - `controldev.Approved(path string) bool`, `controldev.Allow(path string) error`, `controldev.Revoke(path string) error`, `controldev.List() ([]string, error)` over the approvals file in Global Constraints (absolute, cleaned paths; atomic write).
  - `func pod.hasControlAccess(m *manifest.Manifest, projectDir string) bool` = pinned control pod, OR (`m.Project.ControlDev` AND `controldev.Approved(projectDir)`). For a marked+approved project: app image `tainer-control-app:node-<ver>` (the runtime base, source-mounted as today) and the gateway injection; otherwise ordinary nodejs. Checked at every container create/start.
  - `tainer control dev allow [dir]` (default cwd): refuses unless stdin is a terminal ("run this in a terminal"); refuses unless the folder's `tainer.yaml` has `control-dev: true` and its name is not `control`; prints the folder and the warning `This project will be able to run any tainer command on this machine, including deleting pods and reading their databases.`; asks `Allow? [y/N]`; records. `list` prints approved paths; `revoke [dir]` removes and recreates that project's pod if running. `allow`/`revoke` restart the control pod's app container so its notice updates.
  - The pinned control pod's app container gets env `TAINER_CONTROL_DEV_ACCESS=<n>` (number of approved folders) at creation.

- [ ] **Step 1: Failing tests:** `marker without approval grants nothing` (no gateway mounts, image `tainer-nodejs-app`); `marker plus approval grants control access`; `approval is path-specific` (other path → nothing); `allow refuses non-interactive stdin`; `allow refuses a folder without the marker`; `allow refuses the name control`; `revoke removes the path`; `control pod gets the dev access count`.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** `go test ./...`, `go vet ./...`, `make sign` PASS.
- [ ] **Step 5: Commit** `feat(control): dev track — approved checkouts get the control pod's access`.

### Task 6: tainer-control — dev checkout config, dev-access notice, developer docs

**Files:**
- Modify: `tainer-control/tainer.yaml` (name `control-dev`, domain `devmy.tainer.me`, add `control-dev: true` under `project`), `tainer-control/html/src/routes/(app)/+layout.svelte` (or the app shell component) for the notice, `tainer-control/README.md` (Developing section)
- Create: `tainer-control/html/src/lib/server/dev-access.ts` (+ test)

**Interfaces:**
- Consumes: env `TAINER_CONTROL_DEV_ACCESS` (Task 5).
- Produces: `devAccessCount(): number` (0 when unset/invalid); the app shell shows a small warning banner when > 0: `Developer access is on for <n> project folder(s) on this machine. Manage it with tainer control dev list.` (dashboard look: orange-soft notice style already in the app).

- [ ] **Step 1: Failing tests:** `devAccessCount` parses `"2"` → 2, unset/`"x"`/`"-1"` → 0; layout load exposes it; banner renders only when > 0.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement; README "Developing tainer control": clone, `tainer start` (serves devmy.tainer.me), `tainer control dev allow` once, compare with my.tainer.me, release via tag.
- [ ] **Step 4:** `npm test`, `npm run check` PASS.
- [ ] **Step 5: Commit** `feat(dev): devmy.tainer.me dev checkout, developer-access notice, developer docs`.
