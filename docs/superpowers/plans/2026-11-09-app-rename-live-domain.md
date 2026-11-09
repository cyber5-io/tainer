# app/ Rename + Live Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the project source folder `html/` → `app/` on both sides of the mount (host and `/var/www/app`), migrate existing projects automatically, add the optional `live:` domain block + `TAINER_ENV`, and rename every one of our repos, with a promote script for Enhance deploys.

**Architecture:** tainer owns both path names (`HostAppDir`/`ContainerAppPath`), so the rename is constants + copy + tests, plus a migration guard that runs before anything can create `app/`. Images change in lockstep (`MountsVersion` 3 recreates pods once). The live block is additive schema (no manifest version bump). Repo renames are mechanical `git mv` + reference fixes driven by the recon map.

**Tech Stack:** Go (tainer), shell + Caddyfiles + Containerfiles (tainer-images), git across 8 project repos.

**Spec:** `docs/superpowers/specs/2026-11-09-app-rename-live-domain-design.md`
**Recon map (exact file:line for every touched site):** `/Users/lenineto/.claude/jobs/d508acf3/tmp/rename-recon.md` — referenced below as *recon*.

## Global Constraints

- Straight rename: no dual-name support beyond the one-time migration; `html` as a mount name stays refused with a message that it was renamed.
- User-facing words never say podman, docker, engine, VM or container.
- No new Go dependencies. Conventional commits; **no Co-Authored-By trailers, no AI mentions** (CLAUDE.md rule, overrides any session default).
- Never run tainer against live projects (no start/stop/destroy/update on real pods); never restart cyberstackd; never run npm on the host.
- A migration must never silently create an empty `app/` beside a full `html/`.
- `TAINER_ENV` is tainer-owned: locally always `dev`; a user-set `TAINER_ENV` in `.env` or `app/.env` refuses the start: `TAINER_ENV is set by tainer — remove it from <file>`. ONLY that key — `TAINER_DOMAIN` is legitimately written into the generated `.env` by tainer (dotenv.go:102); the guard must not touch it. `TAINER_ENV`/`TAINER_LIVE_DOMAIN` reach apps via container env only.
- Branches: tainer and tainer-images work on branch `app-rename` off main; the 8 project repos commit directly on `main` (nothing pushed anywhere until Leni says).
- tainer repo tests: `go test ./...` + `gofmt -l` + `go vet ./...` green before every commit.

## Review Focus

1. A project whose `html/` is a **symlink** — migration must refuse (mount-safety), never rename through it. → test in Task 2 (`TestMigrateRefusesSymlinkedHtml`).
2. **Both `app/` and `html/` non-empty** — refuse with a clear message; an **empty** `app/` beside `html/` is removed and migration proceeds. → tests in Task 2.
3. `.env` sets `TAINER_ENV=live` — start refused naming the file; the `TAINER_DOMAIN` line tainer itself writes must not trip the guard. → test in Task 3 (`TestStartRefusesUserTainerEnv`).
4. `live.domain` carrying junk (`"not a domain!"`, uppercase, a URL with scheme) — refused by the same rules as `project.domain`. → test in Task 3 (`TestLiveDomainValidated`).
5. `tainer exec` into a control pod still on the old sealed image (no `/var/www/app` inside) would fail to land — the pin bump to v1.0.2 plus `MountsVersion` 3 recreate must make the new image the one that runs. → covered by Task 1's pin-bump test plus the live checklist (exec into the control pod after upgrade).

---

### Task 1: tainer — the rename itself (both sides, lockstep bits)

**Files (tainer repo, branch `app-rename`):**
- Modify: `pkg/tainer/manifest/manifest.go:231-239`, `pkg/tainer/manifest/safety.go:16,65`, `pkg/tainer/manifest/typespec.go` (10 `ExecWorkdir` lines — recon lists them), `pkg/tainer/pod/types.go:92-100`, `pkg/tainer/pod/start.go:760,767` (+ comments :592,:755), `pkg/tainer/pod/secrets.go:194-196` (+ comments), `pkg/tainer/pod/provision.go:375` (+ comments), `pkg/tainer/pod/labels.go:43`, `pkg/tainer/pod/control.go:12`, `pkg/tainer/env/dotenv.go` comments, `pkg/tainer/pod/destroy.go` comments, `cmd/tainer/main.go:1484` (+ comments), `CLAUDE.md:12,59`
- Test: the 12 test files recon lists under "Tests with html fixtures"

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `HostAppDir() == "app"`, `ContainerAppPath() == "/var/www/app"`, every type's `ExecWorkdir == "/var/www/app"`, `DefaultExecWorkdir` follows; `reservedMounts = {"app","data","db"}` **plus** `html` refused with its own message; `MountsVersion = "3"`; `controlImageVersion = "1.0.2"`; `staleControlContainer` treats destination `/var/www/html` or source base `html` as stale and `/var/www/app`/`app` as current.

- [ ] **Step 1: Write/adjust the failing tests** — flip the fixture assertions in `manifest_test.go` (`HostAppDir()=="app"`, `ContainerAppPath()=="/var/www/app"`), `typespec_test.go` (all `ExecWorkdir`), `start_test.go:49,58` (`Source==p+"/app"`, `Target=="/var/www/app"`), `initcmd_test.go:83` (`{"app","data","db"}`), plus a new `TestMountNamedHtmlIsRefusedAsRenamed` in `safety_test.go` asserting the error for `mounts: [html]` contains `renamed to app`, and a `TestControlPinIs102` beside the existing pin tests.
- [ ] **Step 2: Run** `go test ./pkg/tainer/manifest/ ./pkg/tainer/pod/ ./pkg/tainer/initcmd/ ./cmd/tainer/` — Expected: FAIL on the flipped assertions.
- [ ] **Step 3: Implement** — change the constants/returns, the reserved-mount check (`html` → message `the html folder was renamed to app — use app`), `MountsVersion` to `"3"`, `controlImageVersion` to `"1.0.2"`, `staleControlContainer` per Produces, the `main.go:1484` copy to `no app in app/ yet — …` wording (keep the hint content), and every comment/doc line recon lists (CLAUDE.md included). Leave migration behaviour alone (Task 2).
- [ ] **Step 4: Run** `go test ./... && gofmt -l cmd pkg && go vet ./...` — Expected: PASS, empty gofmt output. (The secrets/dotenv/mountsafe/provision test fixtures all flip here too — adjust until the suite is green; any test that *can't* pass without the migration guard gets a `// enabled in Task 2` skip with t.Skip and is listed in the report.)
- [ ] **Step 5: Commit** `feat(rename): the project source folder is app/, mounted at /var/www/app`

### Task 2: tainer — migration guard

**Files:**
- Create: `pkg/tainer/pod/migrate.go`, `pkg/tainer/pod/migrate_test.go`
- Modify: `pkg/tainer/pod/start.go` (call early in `Start`, before any mount building), `pkg/tainer/initcmd/initcmd.go` (before scaffolding an adopted dir), `pkg/tainer/pod/secrets.go` (before writing `app/.env`), `cmd/tainer/main.go:1473` (`nodeAppMissing` checks via the helper)

**Interfaces:**
- Consumes: Task 1's `HostAppDir()`.
- Produces: `func MigrateAppDir(projectDir string, out io.Writer) error` — behaviour: `app/` exists as a real dir → nil (no output). `app/` absent, `html/` a real dir → `os.Rename`, print `renamed html/ to app/`. `app/` an *empty* dir beside a real `html/` → remove it, rename, same notice. Both non-empty → error `both app/ and html/ exist — move your code into app/ and remove html/`. `html/` (or `app/`) a symlink → error `html is a link; tainer only migrates a real folder`. Neither exists → nil (fresh project; scaffolding happens elsewhere).

- [ ] **Step 1: Write failing tests** — `TestMigrateRenamesHtml` (files intact after rename, notice printed), `TestMigrateNoopWhenAppExists`, `TestMigrateRemovesEmptyAppThenRenames`, `TestMigrateRefusesBothNonEmpty`, `TestMigrateRefusesSymlinkedHtml`, `TestMigrateFreshProjectIsNoop`, and `TestStartMigratesBeforeMounts` (through the Start path with a fake engine seam if one exists in start_test.go — follow its pattern; otherwise test the call-site ordering by checking `buildMounts` never sees `html/`).
- [ ] **Step 2: Run** `go test ./pkg/tainer/pod/ -run Migrate` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `MigrateAppDir` + the four call sites; un-skip anything Task 1 parked.
- [ ] **Step 4: Run** `go test ./... && go vet ./...` — Expected: PASS, no skips left.
- [ ] **Step 5: Commit** `feat(rename): projects with html/ migrate to app/ on their next start`

### Task 3: tainer — live domain, TAINER_ENV, env guard

**Files:**
- Modify: `pkg/tainer/manifest/manifest.go` (schema + validate: recon "project.domain read sites"), `pkg/tainer/manifest/overlay.go:56-57`, `pkg/tainer/pod/start.go:604-608` (buildEnv), `pkg/tainer/pod/pod.go:136` (manifest hash), `pkg/tainer/pod/secrets.go` / `pkg/tainer/env/dotenv.go` (where `.env` is read — add the guard), docs: `docs/api/cli-json-v1.md` only if config output shows the manifest (check `tainer config` JSON)
- Test: `manifest_test.go`, `overlay` tests, `start_test.go`, new guard tests beside the `.env` readers

**Interfaces:**
- Consumes: Task 1 constants.
- Produces: `Manifest.Live *LiveConfig` (`yaml:"live,omitempty"`), `LiveConfig{Domain string \`yaml:"domain"\`}`; validation reuses `validateDomain`; overlay merge for the block; containers get `TAINER_ENV=dev` always and `TAINER_LIVE_DOMAIN=<live.domain>` only when set; both feed the manifest hash; a guard in the existing `.env` read path producing exactly `TAINER_ENV is set by tainer — remove it from <file>` when `TAINER_ENV` appears in the root `.env` or `app/.env`, checked at start. Only `TAINER_ENV` — never `TAINER_DOMAIN` (tainer writes that one itself).

- [ ] **Step 1: Write failing tests** — `TestLiveDomainParsesAndValidates` (valid accepted; `not a domain!`, `https://x.com`, empty-string-with-block refused), `TestLiveBlockMergesFromOverlay`, `TestBuildEnvHasTainerEnvDev` (+ `TAINER_LIVE_DOMAIN` present only when set), `TestManifestHashChangesWithLiveDomain`, `TestStartRefusesUserTainerEnv` (`.env` containing `TAINER_ENV=live` → the exact message; same via `app/.env`; and a generated `.env` with its usual `TAINER_DOMAIN` line must NOT be refused), `TestSaveOmitsEmptyLiveBlock`.
- [ ] **Step 2: Run** the named tests — Expected: FAIL.
- [ ] **Step 3: Implement.** The guard sits where the root `.env` is already read at start (secrets path) so no extra file read is added; `app/.env` is read only for the guard's keys (scan, don't load into env).
- [ ] **Step 4: Run** `go test ./... && go vet ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(manifest): optional live domain, TAINER_ENV, and the TAINER_ variable guard`

### Task 4: tainer-images — container paths

**Files (repo `/Users/lenineto/dev/cyber5-io/tainer-images`, branch `app-rename`):** every file in recon §tainer-images — caddy-web/sites/*.Caddyfile, php/Caddyfile, wordpress/Caddyfile, the 4 WORKDIR Containerfiles, 4 start scripts, 9 post-deploy.sh, scaffold copy lines, `shared/tainer-entrypoint.sh:40-41` (`~/app` symlink), `shared/greeting.zsh:93`, `docs/0.9-images.md:88,92`.

**Interfaces:** Consumes Task 1's `/var/www/app` (must match exactly). Produces images whose only app path is `/var/www/app`.

- [ ] **Step 1: Write the failing check** — a small `scripts/check-app-path.sh` in tainer-images: `! grep -rn --exclude-dir=.git --exclude=check-app-path.sh '/var/www/html\|~/html\|"html/' .` style assertions (match the repo's conventions; include `TAINER_BUILD_DIR` react root line explicitly). Run it: FAIL with today's hits.
- [ ] **Step 2: Implement** the replacements across every recon-listed site, including kompozi's `cp -a html/. /scaffold/` → `app/.` (it clones the kompozi repo, so this lands together with Task 5's kompozi rename — note the dependency in the commit message), and the user-visible scaffold copy (`app/src/App.tsx` etc.).
- [ ] **Step 3: Run** the check script (PASS, zero hits) and `bash -n` every touched .sh.
- [ ] **Step 4: Smoke-build one changed image** via the local runtime's Docker-compatible API (build `caddy-web` with context that directory; the runtime at `~/.cyberstack/cyberstack.sock` accepts builds) — Expected: build succeeds. If the build endpoint refuses, note it for the controller instead of forcing it.
- [ ] **Step 5: Commit** `feat(rename): images serve and work in /var/www/app`

### Task 5: tainer-id — rename + promote + release steps

**Files (repo `/Users/lenineto/dev/cyber5-io/tainer-id`, on `main`):**
- `git mv html app`; fix `.gitignore:1-4`, `README.md` (all recon lines; the Enhance start command becomes `node --env-file=../../tainer-id.env app/build`, docroot `deploys/current/app/build/client`, release steps switch from the `live` branch to `scripts/promote vX.Y.Z`), plan doc references stay historical (don't edit old plans).
- Create: `scripts/promote` (also copied to c5test in Task 6).

**Interfaces:** Produces `scripts/promote`: `promote vX.Y.Z` → refuses args not matching `^v[0-9]+\.[0-9]+\.[0-9]+$`, refuses when the tag doesn't exist, then `git tag -f -a live "$1^{}" -m "live: $1"` and `git push -f origin refs/tags/live` (explicit ref — never ambiguous with a `live` branch). `--dry-run` prints the two commands instead.

- [ ] **Step 1: Grep first** — `grep -rn html . --exclude-dir={node_modules,.git,.svelte-kit,build}` plus `.github` and `app/package.json` scripts; list every hit in the report, fix the real ones.
- [ ] **Step 2: Write the failing check for promote** — in a scratch clone (`git clone . /tmp/promote-test && cd there`): `scripts/promote v9.9.9` (tag absent) must refuse; `scripts/promote main` must refuse; after `git tag -a v9.9.9 -m x`, `scripts/promote --dry-run v9.9.9` prints the tag+push commands. Run before the script exists: FAIL (no such file).
- [ ] **Step 3: Implement** the rename, reference fixes, README release steps, and `scripts/promote` (bash, `set -euo pipefail`).
- [ ] **Step 4: Run** the promote checks (PASS) and `ls app/build 2>/dev/null; ls html 2>/dev/null` (html gone). Do NOT push anything; do not touch the `live` branch.
- [ ] **Step 5: Commit** `feat(rename): html/ is app/; releases promote the live tag` (one commit is fine).

### Task 6: batch rename of the remaining repos + Leni's checklist

One subagent, one pass (same-shape work): repos `tainer-control`, `comms`, `c5test` (+ copy `scripts/promote` from tainer-id verbatim), `kompozi`, `/Users/lenineto/dev/websites/oneputtpro.com`, `/Users/lenineto/dev/websites/cyber5.io/ops`, `/Users/lenineto/dev/websites/cyber5.io/website`. All on `main`, one commit per repo, message `chore(rename): html/ is app/`.

Per repo: (a) `grep -rn html` (excluding node_modules/.git/build dirs, *including* `.github`, `scripts/`, `package.json`, Containerfile, docs) and fix the recon-listed + newly found real references (`<!doctype html>`, `.html` extensions and historical plan docs are not references); (b) `git mv html app`; (c) verify any relative symlinks still resolve (`find app -maxdepth 3 -type l ! -exec test -e {} \; -print` must output nothing — oneputtpro's `wp-content` links); (d) tainer-control's Containerfile/.dockerignore/README per recon.

- [ ] **Step 1:** tainer-control (its dev pod `control-dev` is RUNNING — do the `git mv` anyway; the pod keeps the old mount until its next start, and never start/stop it yourself).
- [ ] **Step 2:** comms (incl. docs/DEPLOY.md cron line), c5test (+ promote script), kompozi (.gitignore block, CLAUDE.md:32).
- [ ] **Step 3:** oneputtpro.com (symlink check is mandatory here), ops (docs/DEPLOY.md, docs/dev-topology.md), website (.gitignore comment).
- [ ] **Step 4:** Write `docs/handoff/2026-11-09-app-rename-enhance-checklist.md` in the **tainer** repo (branch `app-rename`): Leni's manual steps — tainer-id Enhance (switch ref `live` branch → `live` tag, start command, docroot, build path, delete the `live` branch after), c5test Enhance build/start paths, comms cron path, ops deploy paths, `wp search-replace '/var/www/html' '/var/www/app'` + wp-config check for oneputtpro/mywp/opp, image publish → pkg install → `tainer update --all` order. Commit it there: `docs: enhance and wordpress checklist for the app rename`.
- [ ] **Step 5:** Report the per-repo grep results and commits.

### Task 7: whole-stack verification on the branch

Controller-driven (no subagent): `go test ./...`, `gofmt -l`, `go vet` in tainer; the images check script; a fixture end-to-end of the migration (create a throwaway fake project dir with `html/`+`tainer.yaml` under `$CLAUDE_JOB_DIR/tmp`, run the migration helper via a focused Go test — NOT tainer against the live runtime); then the final whole-branch review per the SDD skill across tainer + tainer-images + the 8 repo diffs.
