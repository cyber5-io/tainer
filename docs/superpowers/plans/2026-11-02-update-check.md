# Update Check + Updates Page Implementation Plan (TAIN-122)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `tainer update --check` reports update availability without pulling; updates leave stopped pods stopped; the dashboard's Updates page shows only pods with updates and acts on one, all, or a selection.

**Architecture:** A CLI-side registry client (`pkg/tainer/imagecheck`) compares each role image's local config digest (engine image inspect) with the remote manifest's config digest (anonymous ghcr v2 flow, index resolved by architecture). The command walks visible registered projects, dedupes refs, and emits a v1 JSON report the webapp renders. `pod.Update` records prior state and skips the final start for stopped pods.

**Tech Stack:** Go (tainer CLI, stdlib net/http + httptest fakes), SvelteKit 3 + Tailwind v4 design system (tainer-control), vitest.

**Spec:** docs/superpowers/specs/2026-11-02-update-check-design.md

## Global Constraints

- Tainer repo tasks (1-4) run in the tainer worktree; webapp tasks (5-6) in the tainer-control worktree. Paths are stated per task.
- Go: gofmt + go vet clean; cgo-linking builds/tests need `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer SDKROOT=$(xcrun --show-sdk-path)`.
- The check never pulls images and never mutates pod or engine state.
- Pinned project (`control`) excluded from every user-facing list (use `visibleProjects`/`tainer.IsPinnedProject`).
- Image refs come ONLY from `pod.ImageRef` + `pod.RolesForPod`; `TAINER_IMAGE_REPO` respected end to end.
- JSON envelope: `"v": 1` everywhere; new shapes documented in docs/api/cli-json-v1.md with the existing drift-guard test pattern.
- Webapp: tests select by data-*/aria only; text sizes as `text-[length:var(--text-*)]`; shared classes (card, btn, btn-ghost, banner) over bespoke CSS; UX copy never says podman/docker/engine/VM/container/image — user-facing vocabulary is "parts" and "updates".
- Suites green at every task boundary (`go test ./...` 23 pkgs; `npx vitest run` 359 tests at start). No pushes; commit style without attribution lines.

## Review Focus

- A registry that answers the token request but 404s one repo must mark only that repo's pods unknown, not fail the run — Task 3 test.
- A multi-arch index whose entries lack the local architecture (or local arch is empty) falls back to linux/arm64 and still compares — Task 1 test.
- `update --check` while the daemon is down fails with the standard unreachable envelope, not a panic — Task 3 test.
- A stopped pod updated twice in a row stays stopped and keeps its volumes — Task 4 test.
- Updates page with zero updates available: the grid is replaced by a friendly all-clear line, not an empty grid — Task 6 test.

---

### Task 1: Registry client — remote config digest (tainer repo)

**Files:**
- Create: `pkg/tainer/imagecheck/registry.go`, `pkg/tainer/imagecheck/registry_test.go`

**Interfaces:**
- Produces: `type Ref struct { Host, Repo, Tag string }`; `func ParseRef(s string) (Ref, error)` (handles `ghcr.io/cyber5-io/tainer-caddy-web:2-alpine`; default tag `latest`; digest refs rejected with a clear error); `func RemoteConfigDigest(ctx context.Context, httpc *http.Client, ref Ref, arch string) (string, error)` returning `sha256:<hex>`; exported `var ErrNotFound` for 404s.

- [ ] **Step 1 (failing tests):** httptest server faking ghcr: token endpoint (`/token` returning `{"token":"t"}` after a 401 challenge with `WWW-Authenticate: Bearer realm=...,service=...,scope=...`), `/v2/<repo>/manifests/<tag>` serving (a) an OCI index with amd64+arm64 entries, (b) each arch manifest with a `config.digest`. Tests: ParseRef cases (host/repo/tag split, default tag, digest ref error); RemoteConfigDigest resolves the arm64 entry's config digest; empty arch falls back to linux/arm64; single-manifest (non-index) response works; 404 → ErrNotFound; token flow followed when challenged.
- [ ] **Step 2:** Run: `go test ./pkg/tainer/imagecheck/` → FAIL (package missing).
- [ ] **Step 3:** Implement. Accept headers: `application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json`. Follow one 401→token→retry cycle; 10s timeout per request via the caller's client.
- [ ] **Step 4:** `go test ./pkg/tainer/imagecheck/` → PASS; gofmt/vet clean.
- [ ] **Step 5:** Commit `feat(imagecheck): remote config digest lookup`.

### Task 2: Engine image inspect (tainer repo)

**Files:**
- Modify: `pkg/tainer/engine/client.go`
- Test: alongside the package's existing client tests (follow the file's current test harness; if the package has no HTTP-level harness, add a minimal httptest-over-unix-socket one in `pkg/tainer/engine/imageinspect_test.go`)

**Interfaces:**
- Produces: `func (c *Client) ImageInspect(ctx context.Context, ref string) (ImageInfo, error)` with `type ImageInfo struct { ID, Architecture string }`, from `GET /images/{ref}/json` (`Id`, `Architecture` fields); a 404 returns a wrapped error callers can detect via `errors.Is(err, ErrImageNotFound)` (new sentinel).

- [ ] **Step 1:** Failing test: fake endpoint serving `{"Id":"abc123","Architecture":"arm64"}` → ImageInfo{ID:"abc123", Architecture:"arm64"}; 404 → ErrImageNotFound.
- [ ] **Step 2:** Watch it fail. **Step 3:** Implement following the file's existing request idioms (same transport as Pull/Inspect). **Step 4:** package tests PASS. **Step 5:** Commit `feat(engine): image inspect`.

### Task 3: `tainer update --check` (tainer repo)

**Files:**
- Create: `pkg/tainer/imagecheck/check.go`, `pkg/tainer/imagecheck/check_test.go`, `pkg/tainer/imagecheck/parts.go`, `pkg/tainer/imagecheck/parts_test.go`
- Modify: `cmd/tainer/main.go` (update command parsing + output), `docs/api/cli-json-v1.md` (+ its drift-guard test file)

**Interfaces:**
- Consumes: Task 1 `ParseRef`/`RemoteConfigDigest`; Task 2 `ImageInspect`/`ErrImageNotFound`; `registry.All`/`visibleProjects` idiom (cmd passes the visible project map in); `pod.ImageRef`, `pod.RolesForPod`, `manifest.Load`.
- Produces: `imagecheck.Run(ctx, deps, projects) (Report, error)` where `deps` is a small interface struct (ImageInspect func, RemoteConfigDigest func, EnginePods func) so tests fake everything; `Report` marshals EXACTLY the spec's JSON (v, checkedAt, pods[], upToDate, available, unknown). `PartsLine(m *manifest.Manifest) string` → e.g. `WordPress · PHP 8.3 · MariaDB 11 · Caddy`, `SvelteKit · Node 22 · Caddy` (one test per project type, exact strings).
- CLI: `tainer update --check` (+ `--json`): engine pods fetched once; projects without an engine pod skipped; refs deduped; bounded concurrency 6; per-pod `error` + unknown counting on registry failures; styled output lists pods with updates then `N up to date · M updates available · K unknown`; exit 0.

- [ ] **Step 1 (failing tests):** check_test.go with faked deps: two pods sharing caddy-web (dedupe: RemoteConfigDigest called once per unique ref — count calls); one pod updateAvailable via one role; one registry-only project excluded; one remote failure → unknown with error text; local-image-missing → updateAvailable. parts_test.go exact strings.
- [ ] **Step 2:** FAIL. **Step 3:** Implement check.go + parts.go, then wire cmd (flag parse: `--check` conflicts with `--all`/`--base` → usage error) and the JSON doc emitter. **Step 4:** `go test ./pkg/tainer/imagecheck/ ./cmd/tainer/` PASS; update docs/api/cli-json-v1.md + drift test, suite green. **Step 5:** Commit `feat(cli): tainer update --check reports available updates`.

### Task 4: Stopped pods stay stopped through update (tainer repo)

**Files:**
- Modify: `pkg/tainer/pod/update.go`
- Test: `pkg/tainer/pod/update_test.go` (follow the package's existing engine-fake pattern; if Update has no unit harness, test the extracted decision helper)

**Interfaces:**
- Produces: `Update` returns a result struct `UpdateResult{WasRunning bool}` (or extends the existing return) so cmd can render "updated" vs "updated (still stopped)"; behaviour: prior state captured via `Get` BEFORE stopping; stopped pods: pull + remove containers, skip `Start`; running pods unchanged.

- [ ] **Step 1:** Failing test pinning the decision: stopped pod → Start NOT invoked, containers removed, pull happened (via the package's fake engine or an extracted `shouldStart(prior pod.State) bool` helper with the orchestration refactored to honour it).
- [ ] **Step 2:** FAIL. **Step 3:** Implement; update cmd's update/update-all render lines ("updated (still stopped)") and the `updated` JSON field stays `true` with new `"started": bool` field added to both single and --all rows (document in cli-json-v1.md, same commit).
- [ ] **Step 4:** `go test ./pkg/tainer/pod/ ./cmd/tainer/` PASS; full suite green. **Step 5:** Commit `fix(update): stopped pods stay stopped`.

### Task 5: Updates check client (tainer-control repo)

**Files:**
- Create: `html/src/lib/update-check.ts`, `html/src/lib/update-check.test.ts`

**Interfaces:**
- Produces: `interface UpdateCheck { checkedAt: string; pods: CheckedPod[]; upToDate: number; available: number; unknown: number }`, `interface CheckedPod { name: string; type: string; domain: string; state: string; updateAvailable: boolean; parts: string; roles: { role: string; image: string; updateAvailable: boolean }[]; error?: string }`; `toUpdateCheck(doc: unknown): UpdateCheck | null` (defensive parse, same idiom as `toUpdateReport` in health-reports.ts); `runUpdateCheck(cliCall)` helper calling `cliCall('update', ['--check'])`.

- [ ] **Step 1:** Failing tests: full doc parses; missing/malformed fields → null or safe defaults (mirror health-reports tests' style).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** `npx vitest run src/lib/update-check.test.ts` PASS. **Step 5:** Commit `feat: update-check report parsing`.

### Task 6: Updates page redesign (tainer-control repo)

**Files:**
- Modify: `html/src/lib/components/UpdatesPanel.svelte`
- Test: `html/src/lib/components/ControlPages.reactivity.test.ts` (updates describe block)

**Interfaces:**
- Consumes: Task 5 types/parsers; the spec's Updates-page section is the behaviour contract (default filter, Show all toggle, three action paths, counts line, busy-disables-everything).
- Produces: `data-updates-grid` (kept), `data-pod-update="<name>"` (kept), new `data-up-to-date-count`, `data-show-all-toggle`, `data-select="<name>"` checkboxes, `data-selected-bar` with its `data-update-selected` button.

- [ ] **Step 1 (failing tests):** on load the panel calls `update --check` and renders only available pods as cards (up-to-date pod absent); counts line shows "2 pods are up to date"; Show all reveals quiet cards; checking two boxes shows "Update selected (2)" and clicking it calls `update <name>` for each in order; "Update all" updates exactly the available set; zero-available renders the all-clear line and no grid; unknown>0 renders the connection note.
- [ ] **Step 2:** FAIL. **Step 3:** Implement per spec (min column 320px grid; shared classes; card flips to up-to-date style after success; progress/results inside cards as today). **Step 4:** Full `npx vitest run` green; prettier clean. **Step 5:** Commit `feat(updates): check-driven page with single, all and selected updates`.
