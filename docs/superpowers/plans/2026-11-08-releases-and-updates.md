# Releases and Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tainer, the menu app, the control dashboard and pods all show available updates; tainer and the menu app update themselves from verified, signed packages on click or command, warning before restarting running pods and bringing them back afterwards.

**Architecture:** A new `pkg/tainer/release` package owns latest.json (fetch, daily cache, version compare), verified download, and the resume file. `cmd/tainer` extends `tainer update` (listing + y/N, `tainer`/`menu`/`resume` targets, additive JSON). The SwiftUI menu app (`apps/menu-macos`, now the shipped one; the Go `cmd/tainer-menu` is retired) only shells out to the CLI (`update --check --json`, `update tainer --yes`), and draws a badge, update rows and pod highlights. Packaging gains a menu-only pkg, a release-files target, and a resume step in postinstall. tainer-control parses the new JSON keys.

**Tech Stack:** Go (stdlib + existing modules), SwiftUI/AppKit (`apps/menu-macos`, Swift Package, XCTest), macOS `pkgutil`/`spctl`/`open`/`osascript`/`launchctl`, Make, SvelteKit (tainer-control `html/`).

**Spec:** `docs/superpowers/specs/2026-11-08-releases-and-updates-design.md`

## Global Constraints

- Nothing updates automatically. Background work is limited to *checking*; installing always needs a click or a command.
- User-facing words never say podman, docker, engine, VM or container.
- No new Go module dependencies (stdlib + existing go.mod only).
- Postinstall stays graceful TERM-only; never kill -9 the runtime.
- Every check failure (offline, bad JSON, HTTP error) is silent and non-blocking: it means "no news", never an error exit, never a delay longer than the fetch timeout.
- latest.json URL: `https://get.tainer.dev/latest.json`, HTTPS only; override env `TAINER_RELEASE_URL` (base URL, for tests/dev), mirroring certsync's `TAINER_CERT_URL`.
- Check at most once per 24h per machine, cached on disk; `tainer update --check` forces a fresh fetch.
- Signature requirement: `pkgutil --check-signature` must show a "Developer ID Installer" certificate with team ID `C8J8D9XJP6` (Cyber5 Internet Ltd), and `spctl -a -t install` must accept it (notarised). sha256 must equal latest.json's.
- JSON stays additive: the existing `update --check --json` keys (`checkedAt, pods, upToDate, available, unknown, held`) keep their shape; new keys are added beside them. Update `docs/api/cli-json-v1.md` and the golden file when keys change.
- Package identifiers: full pkg `io.cyber5.tainer` (existing), menu-only pkg `io.cyber5.tainer.menu`.
- macOS only for self-update. Elsewhere, `tainer update` prints that a newer tainer exists and points to `https://tainer.dev`.
- Copy (exact): running-pods warning — `Updating tainer restarts your running pods (<a, b, c>).` followed by the y/N or Update now/Later choice; dashboard while updating — `Tainer is updating — this page comes back when it's done.`
- Commits: conventional messages, no Co-Authored-By trailers, no AI mentions.

## Review Focus

1. **Installer cancelled or failed after postinstall already stopped the runtime** — the recorded pods must come back and the resume file must not linger to fire later. Test in Task 4 (`TestResumeAfterCancelledInstallStartsRecordedPods`, `TestStaleResumeFileIsDiscarded`).
2. **Two resumers racing** (postinstall's detached resume and the menu relaunch both fire) — pods are started once, not twice. Test in Task 4 (`TestResumeRunsOnceUnderConcurrency`).
3. **Offline / slow get.tainer.dev** — `tainer update --check` and the menu still list pod updates, and nothing waits longer than the fetch timeout. Test in Task 1 (`TestFetchTimeoutIsNoNews`) and Task 5 (`TestCheckListsPodsWhenReleaseFetchFails`).
4. **Stack down** — `update --check --json` must still report tainer/menu updates (today it fails `stack-unreachable` before anything). Test in Task 5 (`TestCheckReportsTainerUpdateWithStackDown`).
5. **A project named `tainer`, `menu` or `resume`** — the reserved target must still leave the project reachable, and must say how. Test in Task 5 (`TestReservedTargetsDontShadowProjects`).

---

### Task 1: release package — latest.json, cache, version compare

**Files:**
- Create: `pkg/tainer/release/latest.go`, `pkg/tainer/release/version.go`, `pkg/tainer/release/latest_test.go`, `pkg/tainer/release/version_test.go`
- Modify: `pkg/tainer/config/paths.go` (add `ReleaseCacheFile() string` → `<BaseDir>/release-check.json`, `ResumeFile() string` → `<BaseDir>/update-resume.json`)

**Interfaces:**
- Produces:
  - `type Artifact struct { Version, URL, SHA256, Notes string; MinTainer string \`json:"minTainer,omitempty"\` }`
  - `type Latest struct { Tainer Artifact \`json:"tainer"\`; Menu Artifact \`json:"menu"\`; Control struct{ Tag string \`json:"tag"\` } \`json:"control"\` }`
  - `func BaseURL() string` (env `TAINER_RELEASE_URL`, default `https://get.tainer.dev/`)
  - `func Check(ctx context.Context, force bool) (Latest, bool)` — returns cached value if younger than 24h and !force; else fetches `<BaseURL>latest.json` with a 10s budget independent of ctx; on any failure returns the last cached value (if any) and `ok=false` only when there is nothing at all. Never returns an error.
  - `func Newer(candidate, installed string) bool` — dotted numeric compare (`1.4.10 > 1.4.9`); `installed == "dev"` → false (dev builds never nag); unparsable → false.
- Follow certsync's fetch pattern (`pkg/tainer/tls/refresh.go` fetch: status 200, `io.LimitReader` 1 MiB).

- [ ] **Step 1: Write failing tests** — `TestNewerComparesNumerically` (`1.4.10`>`1.4.9`, `1.5.0`>`1.4.99`, equal → false, `dev` → false, `x.y` → false); `TestCheckUsesCacheWithin24h` (httptest server counts hits; two calls → 1 hit; `force` → 2 hits); `TestFetchTimeoutIsNoNews` (server sleeps 15s; `Check` returns within 11s with the previous cache, or ok=false with no cache); `TestCheckRejectsMissingFields` (JSON without `tainer.version` → treated as failure, cache untouched); `TestCheckIgnoresUnknownKeys`.
- [ ] **Step 2: Run** `go test ./pkg/tainer/release/` — Expected: FAIL (package doesn't exist).
- [ ] **Step 3: Implement** the types and functions above; cache file stores `{checkedAt, latest}`.
- [ ] **Step 4: Run** `go test ./pkg/tainer/release/ ./pkg/tainer/config/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(release): read latest.json with a daily cache`

### Task 2: verified download and installed versions

**Files:**
- Create: `pkg/tainer/release/verify.go`, `pkg/tainer/release/installed_darwin.go`, `pkg/tainer/release/installed_other.go`, tests `verify_test.go`, `installed_test.go`
- Modify: `Makefile` (add `MENU_VERSION ?= $(VERSION)`; the menu bundle is stamped with it — Task 2b passes it to `make-app.sh --app-version`)

**Interfaces:**
- Produces:
  - `type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)` (test seam; default runs exec)
  - `func Download(ctx context.Context, a Artifact, dir string) (path string, err error)` — streams to a file in `dir` (0700 temp dir made by caller), computes sha256 while writing, deletes the file and errors `the download didn't match its checksum` on mismatch.
  - `func VerifyPkg(ctx context.Context, run Runner, path string) error` — requires `pkgutil --check-signature` output to contain `Developer ID Installer` and `C8J8D9XJP6`, and `spctl -a -t install <path>` exit 0. Error text names what failed in plain words (`isn't signed by Cyber5`, `isn't notarised by Apple`).
  - `func InstalledTainer(run Runner) string` — `pkgutil --pkg-info io.cyber5.tainer` → `version:` line; "" if absent.
  - `func InstalledMenu() string` — reads `CFBundleShortVersionString` from `/Applications/Tainer Menu.app/Contents/Info.plist` (parse the XML plist with encoding/xml; no `defaults`); "" if absent. Non-darwin stubs return "".

- [ ] **Step 1: Write failing tests** — `TestDownloadChecksSHA256` (good hash keeps file; bad hash deletes it and errors); `TestVerifyPkgRequiresCyber5Team` (fake Runner outputs: correct team → nil; other team → error mentioning Cyber5; spctl non-zero → error mentioning notarised); `TestInstalledTainerParsesReceipt`; `TestInstalledMenuReadsPlist` (fixture plist in a temp dir via an internal path var).
- [ ] **Step 2: Run** `go test ./pkg/tainer/release/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/tainer/release/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(release): verify downloaded packages and read installed versions`

### Task 2b: ship the SwiftUI menu app, retire the Go one

Leni (2026-11-09): the SwiftUI app in `apps/menu-macos` ships; the Go `cmd/tainer-menu` is retired. `apps/menu-macos/README.md` ("Packaging into the .pkg") lists the exact swap; follow it.

**Files:**
- Modify: `Makefile` (`menu-app` runs `apps/menu-macos/scripts/make-app.sh --app-version $(MENU_VERSION)`, takes `apps/menu-macos/build/Tainer Menu.app` as `$(MENU_APP)` (copy into `dist/` so later targets are unchanged), signs it with the existing codesign line; drop the Go build lines and the darwin-only explicit-target comment at line ~51), `packaging/menu/io.cyber5.tainer.menu.plist` (`ProgramArguments` → `/Applications/Tainer Menu.app/Contents/MacOS/TainerMenu`), `apps/menu-macos/README.md` (packaging section now describes the shipped state)
- Delete: `cmd/tainer-menu/` and `packaging/menu/Info.plist` + `packaging/menu/tainer-menu.icns` if nothing else uses them (grep first; the Swift app has its own Info.plist via make-app.sh)

- [ ] **Step 1: Write the failing check** — `make menu-app MENU_VERSION=9.9.9` then `test -x "dist/Tainer Menu.app/Contents/MacOS/TainerMenu" && /usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "dist/Tainer Menu.app/Contents/Info.plist"` and `plutil -extract ProgramArguments.0 raw packaging/menu/io.cyber5.tainer.menu.plist`.
- [ ] **Step 2: Run** — Expected: FAIL (still the Go app, executable `tainer-menu`).
- [ ] **Step 3: Implement** the swap and removal.
- [ ] **Step 4: Run** the check (expect `9.9.9` and `…/MacOS/TainerMenu`), `apps/menu-macos/scripts/make-app.sh --check`, `(cd apps/menu-macos && swift test)`, `go build ./... && GOOS=linux go build ./... && go test ./...` — Expected: all PASS (the Linux build now succeeds since the systray menu is gone).
- [ ] **Step 5: Commit** `build: ship the SwiftUI menu app and retire the Go one`

### Task 3: running-pod snapshot and confirm helper

**Files:**
- Create: `pkg/tainer/release/resume.go`, `pkg/tainer/release/resume_test.go`, `cmd/tainer/confirm.go`, `cmd/tainer/confirm_test.go`

**Interfaces:**
- Produces:
  - `type ResumeEntry struct { Name, Path string }`
  - `type ResumeFile struct { ForVersion string \`json:"forVersion"\`; FromVersion string \`json:"fromVersion"\`; WrittenAt time.Time \`json:"writtenAt"\`; Pods []ResumeEntry \`json:"pods"\` }`
  - `func WriteResume(f ResumeFile) error` / `func ReadResume() (ResumeFile, bool, error)` / `func ClearResume() error` (at `config.ResumeFile()`, 0600, atomic rename)
  - `func WithResumeLock(fn func() error) error` — exclusive file lock `<ResumeFile>.lock`, following `cmd/tainer/control_lock.go`'s pattern (move or reuse the platform lock helper; `pkg/tainer/filelock` exists — use it if it fits).
  - `func confirm(in io.Reader, out io.Writer, question string) bool` in cmd/tainer — prints `question + " [y/N] "`, true only for `y`/`yes` (case-insensitive, trimmed); EOF → false. Replace the inline copy in `controldev.go` with it.

- [ ] **Step 1: Write failing tests** — `TestResumeRoundTrip`; `TestClearResumeIsIdempotent`; `TestResumeLockIsExclusive` (second locker blocks until first returns); `TestConfirmAcceptsOnlyYes` (`y`,`Yes\n` → true; ``,`n`,`yep`, EOF → false).
- [ ] **Step 2: Run** `go test ./pkg/tainer/release/ ./cmd/tainer/ -run 'Resume|Confirm'` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** same + `go test ./cmd/tainer/ -run ControlDev` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(update): resume file and a shared y/N prompt`

### Task 4: resume logic

**Files:**
- Create: `cmd/tainer/updateresume.go`, `cmd/tainer/updateresume_test.go`

**Interfaces:**
- Consumes: Task 3 resume API; `pod.List`, `registry.All`, `pod.Start` with `StartOptions{ManifestPath, ProjectDir}`; `release.InstalledTainer`.
- Produces:
  - `type resumeOps struct { list func(ctx) ([]release.ResumeEntry, error); start func(ctx, e release.ResumeEntry) error; installed func() string; out io.Writer }` with `liveResumeOps()`.
  - `func snapshotRunning(ctx, ops resumeOps) ([]release.ResumeEntry, error)` — running or mixed pods joined with the registry; excludes pinned projects (the control pod comes back via `EnsureControlPod` on runtime boot).
  - `func runResume(ctx, ops resumeOps, installerDone bool) error` — under `WithResumeLock`: read the file; none → nil. Then exactly one of:
    - `installed() == f.ForVersion` (install succeeded) → start each recorded pod, clear.
    - `installerDone && installed() != f.ForVersion` (cancelled or failed; postinstall may already have stopped the runtime) → start each recorded pod, clear.
    - `!installerDone && installed() != f.ForVersion` and the file is older than 1 hour (stale, from a crash or an abandoned install) → start each recorded pod, clear.
    - otherwise (an install may still be open) → leave the file and return nil.

    A pod that fails is reported as `<name> didn't come back: <reason>` and the others still start. Starting an already-running pod counts as success. `installerDone` is true only when called by `installTainer` after `openAndWait` returns; postinstall, the menu app and the next-command safety net call it with false.
  - Wire `tainer update resume` (hidden from usage) → `runResume(liveResumeOps())`; exits 0 even if some pods failed (they're reported).

- [ ] **Step 1: Write failing tests** — `TestResumeStartsOnlyRecordedPods`; `TestResumeAfterCancelledInstallStartsRecordedPods` (installed == FromVersion → pods started, file cleared); `TestStaleResumeFileIsDiscarded` (file for a version that never installed, older than 1 hour, installerDone=false → pods started once, file cleared, second run does nothing); `TestResumeLeavesFileWhileInstallMayBeOpen` (younger than 1 hour, installerDone=false, version unchanged → nothing started, file kept); `TestResumeRunsOnceUnderConcurrency` (two goroutines call runResume; start called once per pod); `TestResumeReportsFailuresAndContinues`; `TestSnapshotSkipsStoppedAndPinned`.
- [ ] **Step 2: Run** `go test ./cmd/tainer/ -run 'Resume|Snapshot'` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** same — Expected: PASS.
- [ ] **Step 5: Commit** `feat(update): bring back the pods that were running before an update`

### Task 5: `tainer update` — listing, y/N, targets, JSON

**Files:**
- Modify: `cmd/tainer/main.go` (`cmdUpdate`), `cmd/tainer/updatecheck.go`, `docs/api/cli-json-v1.md` (`### update`), `cmd/tainer/testdata/json/update-check.golden`
- Create: `cmd/tainer/updateself.go` (listing + `tainer`/`menu` targets), `cmd/tainer/updateself_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces (JSON, added beside existing keys in the `--check` document):
  - `"tainer": {"installed","available","updateAvailable","notes"}`
  - `"menu": {"installed","available","updateAvailable","notes","minTainer","needsTainerFirst"}`
  - `"releaseCheck": "ok"|"unavailable"` (fetch failed and no cache)
- Command behaviour:
  - `tainer update --check [--json]`: computes tainer/menu first via `release.Check(ctx, true)`; then the pod report. If the stack is unreachable, emit the document with tainer/menu filled, `pods: []`, and `"podsCheck": "unavailable"` instead of failing — exit 0.
  - `tainer update` (no args, no flags) — Leni (2026-11-09):
    - **inside a project folder** (manifest found walking up from cwd, as today): shows only that pod's update and asks `confirm("Update <name>?")`; nothing available → `<name> is up to date`. Tainer/menu updates are not offered here, but if one exists print one extra line: `A newer tainer is available — run "tainer update" outside a project folder to see everything.`
    - **anywhere else**: prints the full list (tainer, menu, each pod with an update) and asks `confirm("Update all of these?")`. Order of work: tainer first (Task 6 flow), then menu if still needed, then pods. Nothing to update → `everything is up to date`.
    - `-y`/`--yes` skips the question in both cases. `tainer update <project>` keeps the single-project path.
  - `tainer update tainer` / `tainer update menu` / `tainer update resume`: reserved words always mean these targets. A project with one of those names is updated with `tainer update ./<name>` (a path argument) or `tainer update .` inside its folder. When such a project exists, the reserved target also prints one line: `(to update the project named "<name>", run tainer update ./<name>)`.
  - Non-darwin: tainer/menu shown as `available — download it from https://tainer.dev`; `update tainer` prints that and exits 0.

- [ ] **Step 1: Write failing tests** (use seams for release.Check, the pod check and the installers) — `TestCheckAddsTainerAndMenuKeys` (golden updated); `TestCheckReportsTainerUpdateWithStackDown`; `TestCheckListsPodsWhenReleaseFetchFails` (`releaseCheck: unavailable`, pods present, exit 0); `TestUpdateNoArgsListsAndAsks` (outside a project: stdin `n` → nothing updated; `--yes` → tainer, menu, pods in that order); `TestUpdateNoArgsInProjectOffersOnlyThatPod` (inside a project folder: only that pod is offered, plus the one-line tainer hint when tainer has an update); `TestUpdateNothingToDo`; `TestReservedTargetsDontShadowProjects` (project named `menu` exists: `update menu` updates the menu app and prints the hint; `update ./menu` updates the project); `TestMenuNeedsTainerFirst` (menu.minTainer > installed tainer → listed as needing tainer first, tainer updated before it).
- [ ] **Step 2: Run** `go test ./cmd/tainer/ -run 'Check|UpdateNoArgs|Reserved|MenuNeeds|JSONSnapshot'` — Expected: FAIL.
- [ ] **Step 3: Implement**; document the new keys and the no-args behaviour change in `docs/api/cli-json-v1.md`.
- [ ] **Step 4: Run** `go test ./cmd/tainer/ ./pkg/...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(update): list tainer, menu and pod updates together and ask first`

### Task 6: installing tainer and the menu app

**Files:**
- Create: `cmd/tainer/updateinstall.go`, `cmd/tainer/updateinstall_test.go`

**Interfaces:**
- Consumes: Tasks 1–4; `confirm`.
- Produces:
  - `type installOps struct { check func(ctx) (release.Latest, bool); download func(ctx, release.Artifact, dir string) (string, error); verify func(ctx, path string) error; openAndWait func(ctx, path string) error /* open -W */; installed func() string; snapshot func(ctx) ([]release.ResumeEntry, error); resume func(ctx) error; in io.Reader; out io.Writer; yes bool }`
  - `func installTainer(ctx, ops installOps) error` — order: check (not newer → `tainer is up to date`); snapshot running pods; if any and !yes → print the exact warning copy and `confirm("Update now?")`, no → `update postponed` exit 0; download to a 0700 temp dir; verify (failure → delete, error, nothing written); write resume file `{ForVersion: latest, FromVersion: installed(), Pods}`; `openAndWait`; then call `resume` (which handles success/cancel per Task 4). With `--json`, emit `{"mode":"tainer","from","to","updated":bool,"resumed":[names],"failed":[{name,reason}]}`.
  - `func installMenu(ctx, ops installOps) error` — same without snapshot/warning/resume; refuses with `the menu app needs tainer <v> first` when minTainer > installed.

- [ ] **Step 1: Write failing tests** — `TestInstallTainerWarnsAndPostpones` (stdin `n`: no download, no resume file); `TestInstallTainerNoRunningPodsNoWarning`; `TestInstallTainerVerifyFailureWritesNothing`; `TestInstallTainerWritesResumeBeforeOpening` (order recorded by fakes); `TestInstallMenuNeedsNewerTainer`; `TestInstallTainerUpToDate`.
- [ ] **Step 2: Run** `go test ./cmd/tainer/ -run Install` — Expected: FAIL.
- [ ] **Step 3: Implement**; wire into Task 5's targets.
- [ ] **Step 4: Run** `go test ./cmd/tainer/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(update): tainer and the menu app update themselves from verified packages`

### Task 7: packaging — resume in postinstall, menu-only pkg, release files

**Files:**
- Modify: `packaging/scripts/postinstall-full` (new final step), `Makefile`
- Create: `packaging/scripts/postinstall-menu`, `packaging/installer/distribution-menu.xml` (copy of `distribution.xml` adapted), `scripts/release-files.sh`

**Interfaces:**
- postinstall-full step 5 (after the menu relaunch, same CONSOLE_USER/UID guard): if `<console user's config dir>/tainer/update-resume.json` exists, run `launchctl asuser "$CONSOLE_UID" sudo -u "$CONSOLE_USER" /opt/tainer/bin/tainer update resume >/dev/null 2>&1 &` detached (`nohup`), `|| true`. Respect `XDG_CONFIG_HOME` is not knowable from root — check `~$CONSOLE_USER/.config/tainer/update-resume.json` only; the menu/next-command safety net covers custom config dirs.
- postinstall-menu: only the menu relaunch block from postinstall-full (factor it into a shared snippet if the Makefile copies both).
- Make targets: `pkg-menu` (pkgbuild, identifier `io.cyber5.tainer.menu`, `--version $(MENU_VERSION)`, root = Tainer Menu.app + LaunchAgent, component plist reused), `pkg-menu-signed`, `pkg-menu-notarised`, `pkg-dist-menu*` mirroring the existing signed/notarised chains; `release-files` → runs `scripts/release-files.sh dist/release`.
- `scripts/release-files.sh <outdir>`: takes `dist/tainer-dist-$(VERSION).pkg` and `dist/tainer-menu-dist-$(MENU_VERSION).pkg`, refuses unless both pass `pkgutil --check-signature` + `spctl -a -t install`, copies them to `<outdir>/releases/<version>/`, writes `<outdir>/latest.json` with versions, URLs `https://get.tainer.dev/releases/<v>/<file>`, sha256s, notes from `RELEASE_NOTES` / `MENU_NOTES` env (required, one paragraph), menu `minTainer` from `MENU_MIN_TAINER` (default `$(VERSION)`), control tag from `pkg/tainer` `controlImageVersion` (grep). Prints the folder path and "upload this folder to get.tainer.dev".

- [ ] **Step 1: Write a failing check** — `scripts/release-files_test.sh` (or a Go test in `pkg/tainer/release` that runs the script against fixture pkgs with `SKIP_SIGNATURE_CHECK=1` test-only env) asserting the layout and that latest.json parses with `release.Latest` and its sha256s match.
- [ ] **Step 2: Run it** — Expected: FAIL.
- [ ] **Step 3: Implement** targets and scripts. `bash -n` both postinstall scripts.
- [ ] **Step 4: Run** the check, `bash -n packaging/scripts/postinstall-full packaging/scripts/postinstall-menu`, `make pkg-menu MENU_VERSION=9.9.9 && pkgutil --payload-files dist/tainer-menu-9.9.9.pkg | head` — Expected: PASS; payload lists `./Applications/Tainer Menu.app/...` and the LaunchAgent.
- [ ] **Step 5: Commit** `build: menu-only package, release files, and resume after install`

### Task 8: menu app (SwiftUI) — checks, badge, update rows, pod highlights

**Files (all under `apps/menu-macos/`):**
- Create: `Sources/TainerMenu/UpdateChecker.swift`, `Sources/TainerMenu/Contract/UpdateCheckDocument.swift`, `Tests/TainerMenuTests/UpdateCheckerTests.swift` (match the existing test target's folder name)
- Modify: `Sources/TainerMenu/AppModel.swift`, `Sources/TainerMenu/ActionRunner.swift` (new actions), `Sources/TainerMenu/Views/BarIcon.swift`, `Sources/TainerMenu/Views/MenuContent.swift`, `Sources/TainerMenu/Views/PodRow.swift`, `Sources/TainerMenu/Views/ViewModels.swift`

**Interfaces:**
- Consumes: Task 5 JSON (`tainer update --check --json`, decoded tolerantly — missing `tainer`/`menu` keys from an older CLI mean no update rows; unknown keys ignored, like the existing `Contract/` decoders); Task 6 commands (`tainer update tainer --yes --json`, `update menu --yes --json`, `update <pod> --json`); Task 4 (`tainer update resume`). The CLI is found via the existing `CLILocator`.
- Produces:
  - `UpdateChecker` (an actor or `@MainActor` class, following `ListRefresher`'s pattern): polls at launch (+10s), every 4h, and right after any update action; never two checks at once; exposes `tainer`, `menu` (installed/available/notes/needsTainerFirst) and `pods: [String: PodUpdate]` (parts text).
  - `PodAction` gains `.update`; `AppModel` gains `updateTainer()`, `updateMenu()`, `updateAllPods()`.
  - Bar icon: the existing red ✗ badge for `.error` stays as is. A new **update badge** — a small neutral dot (white with a thin dark ring on coloured icons; plain dot on template icons) in the **top-right** corner — is drawn when updates exist and the style is `.full` or `.dimmed`. Never on `.pulsing` or `.error` (health wins). Implement as `markImage(style:pulseHigh:updateBadge:)`.
  - Menu top rows (only when relevant): `Tainer <v> is available — Update…` / `Menu <v> is available — Update…` with notes as help text. Click → if any pod is running, an `NSAlert` with the exact warning copy and buttons **Update now** / **Later**; Later does nothing; Update now runs the command and the status header shows `Updating tainer…`, then `Restarting N pods…` until the resume finishes.
  - Pod rows: a pod with an update gets an accent colour on its name and an `arrow.down.circle` SF Symbol, help text `Update available: <parts>`; its action set gains `.update` (with an `NSAlert` `<name> restarts while it updates.` Update/Cancel when running). `Update all pods` appears when ≥2 pods have updates.
  - On launch: run `tainer update resume` once in the background (safety net), ignoring its output.
  - Keep "New Pod" and "Open Control Dashboard" as they are.
- [ ] **Step 1: Write failing tests** (XCTest, with the existing `Fakes.swift` subprocess fakes) — `testDecodesNewUpdateKeys`, `testOlderCLIHasNoUpdateRows`, `testBadgeOnlyWhenHealthyOrIdle` (pure function from style + hasUpdates), `testPollerNeverOverlaps`, `testUpdateActionRunsTheRightCommand` (tainer → `update tainer --yes --json`; pod → `update <name> --json`).
- [ ] **Step 2: Run** `(cd apps/menu-macos && swift test)` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `(cd apps/menu-macos && swift test && scripts/make-app.sh --check)` — Expected: PASS. List in the report what the live test must look at (badge on teal and on dimmed icons, light and dark menu bar).
- [ ] **Step 5: Commit** `feat(menu): show updates with a badge, update rows and pod highlights`

### Task 9: dashboard (tainer-control)

**Files (repo `/Users/lenineto/dev/cyber5-io/tainer-control`, `html/`):**
- Modify: `src/lib/update-check.ts`, `src/lib/components/UpdatesPanel.svelte`, their tests (`*.test.ts` beside them)

**Interfaces:**
- Consumes: Task 5 JSON keys; gateway `cliCall('update', ['tainer','--yes'])`, `['menu','--yes']`.
- Produces: `UpdateCheck` gains `tainer?: {installed, available, updateAvailable, notes}`, `menu?: {…, minTainer, needsTainerFirst}`, `releaseCheck?: 'ok'|'unavailable'`. Old documents parse unchanged.
- UpdatesPanel: tainer and menu items listed above pods when available; tainer's button first shows the exact running-pods warning with Update now / Later (pods from the store's running list); while updating shows `Tainer is updating — this page comes back when it's done.` and reuses the existing `awaitRestart()` reconnect; if the gateway call fails with `cli` kind mentioning the installer, show `Update from the menu bar or run "tainer update" in a terminal.`
- Never run `npm` on the host: run tests through the devmy pod (`tainer exec` into control-dev, or its existing test script) — check `tainer-control/README.md` for the dogfood test command.

- [ ] **Step 1: Write failing tests** — `toUpdateCheck` parses new keys / old doc unchanged; panel shows tainer row and the warning (component test, existing test style).
- [ ] **Step 2: Run** the test command in the dev pod — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests + `npm run check` (svelte-check) in the dev pod — Expected: PASS.
- [ ] **Step 5: Commit** (tainer-control `main`, do not tag) `feat(updates): tainer and menu updates on the Updates page`
