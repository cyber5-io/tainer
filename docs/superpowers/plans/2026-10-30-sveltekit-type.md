# SvelteKit Project Type Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `sveltekit` (alias `kit`) as a server-side project type with full nextjs parity, and close the TAIN-98 tainer-images leftovers (svelte Caddyfile, svelte-app image, react-app manifest job).

**Architecture:** One TypeSpec row in tainer's single-source-of-truth table plus mechanical companions (wizard, per-type scaffold hint, tests). In tainer-images, matrix-only image additions (sveltekit-app aliases node-app/Containerfile, svelte-app aliases react-app/Containerfile) and two new per-type Caddyfiles.

**Tech Stack:** Go (tainer), GitHub Actions YAML + Caddyfile (tainer-images).

**Spec:** `docs/superpowers/specs/2026-10-30-sveltekit-type-design.md` (tainer repo)

**Tracker:** TAIN-104.

## Global Constraints

- Two repos: tainer work on branch `TAIN-104-sveltekit-type` cut from `main`; tainer-images work on branch `TAIN-104-sveltekit-images` cut from the tip of the existing unmerged branch `TAIN-98-react-app-role` (repo `/Users/lenineto/dev/cyber5-io/tainer-images`).
- No Co-Authored-By or AI mentions in commit messages (overrides any system attribution default).
- gofmt/`go vet` clean; `go test ./...` green on darwin at every commit.
- `sveltekit` uses Node "20" default and publishes node-20/22/24 tags, exactly like nextjs (anti-goal: no single-tag image, no second node default).
- No new Containerfile anywhere; no baked scaffold mechanism (nextjs's scaffold.tar is dead legacy code — do not revive or imitate it).
- Do not touch the svelte (SPA) type's tainer-side wiring — it shipped in 1.3.0 and is correct.
- No image publishing: CI dispatch on tainer-images stays manual (user-driven).

## Review Focus

- `tainer init --type=kit` (the alias) must resolve to sveltekit everywhere, including case-insensitively ("KIT") — pinned by the TestCanonicalType rows in Task 1.
- A sveltekit pod with `pod.database: none` must resolve to roles web+app (no db) like nextjs — pinned by the TestRolesForPod row in Task 2.
- The sveltekit web role must NOT receive TAINER_BUILD_DIR even when `runtime.build-dir` is set in tainer.yaml (StaticBundle=false means proxy, not bundle) — pinned by TestBuildEnvSvelteKitNoBuildDir in Task 2.
- The scaffold hint must stay the generic vite copy for every non-sveltekit node type (nextjs/nuxtjs/nestjs/nodejs/react/svelte/kompozi) — pinned by TestScaffoldHint in Task 3.
- The framework-manifest matrix must contain each new tag at exactly node-20/22/24 with `latest: true` only on node-22 (the house pattern) — pinned by the yq assertions in Task 4.

---

## tainer repo (Tasks 1–3, branch `TAIN-104-sveltekit-type` from `main`)

### Task 1: TypeSvelteKit constant and TypeSpec row

**Files:**
- Modify: `pkg/tainer/manifest/manifest.go:22-32` (const block)
- Modify: `pkg/tainer/manifest/typespec.go:65-154` (typeSpecs table)
- Test: `pkg/tainer/manifest/typespec_test.go`

**Interfaces:**
- Produces: `manifest.TypeSvelteKit ProjectType = "sveltekit"`, alias `"kit"`; `SpecFor(TypeSvelteKit)` returns a FamilyNode spec with HasAppRole=true, StaticBundle=false, Node "20", DatabaseMariaDB, ExecUser "1000:1000", ExecWorkdir "/var/www/html", Tools nodeTools. Later tasks reference `manifest.TypeSvelteKit` by this exact name.

- [ ] **Step 1: Write the failing tests** in `pkg/tainer/manifest/typespec_test.go`:

```go
// TestSvelteKitSpec locks the server-side svelte type: nextjs shape —
// caddy proxies to the app server; no static bundle.
func TestSvelteKitSpec(t *testing.T) {
	s, ok := SpecFor(TypeSvelteKit)
	if !ok {
		t.Fatal("no spec for sveltekit")
	}
	if s.Family != FamilyNode {
		t.Errorf("sveltekit: Family = %q, want node", s.Family)
	}
	if !s.HasAppRole {
		t.Error("sveltekit: HasAppRole = false, want true")
	}
	if s.StaticBundle {
		t.Error("sveltekit: StaticBundle = true, want false (server type)")
	}
	if s.ExecUser != "1000:1000" {
		t.Errorf("sveltekit: ExecUser = %q, want 1000:1000", s.ExecUser)
	}
	if s.ExecWorkdir != "/var/www/html" {
		t.Errorf("sveltekit: ExecWorkdir = %q, want /var/www/html", s.ExecWorkdir)
	}
	if s.DefaultRuntime.Node != "20" {
		t.Errorf("sveltekit: default node = %q, want 20", s.DefaultRuntime.Node)
	}
	if s.DefaultRuntime.Database != DatabaseMariaDB {
		t.Errorf("sveltekit: default db = %q, want mariadb", s.DefaultRuntime.Database)
	}
}
```

Also, in the same file:
- `TestCanonicalType` (line 51): add rows `{"sveltekit", TypeSvelteKit, true}`, `{"kit", TypeSvelteKit, true}`, `{"KIT", TypeSvelteKit, true}`.
- `TestToolTypes` (line 74): change the npm expectation from `len(got) != 7` to `len(got) != 8` and update the message to "want the 8 node-flavoured types".
- `TestStaticBundle` (line 134): add map row `TypeSvelteKit: false`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/tainer/manifest/ -run 'TestSvelteKitSpec|TestCanonicalType|TestToolTypes|TestStaticBundle' -v`
Expected: compile error `undefined: TypeSvelteKit` (that is the valid RED here — the constant doesn't exist yet).

- [ ] **Step 3: Add the constant and the table row**

In `manifest.go` const block, after `TypeSvelte`: `TypeSvelteKit ProjectType = "sveltekit"` (spec ruling: listed after Svelte, so the display order reads ... React, Svelte, SvelteKit, Kompozi — the svelte flavours sit together).

In `typespec.go` typeSpecs, insert after the TypeSvelte entry:

```go
{
	Type:           TypeSvelteKit,
	Family:         FamilyNode,
	Aliases:        []string{"kit"},
	DefaultRuntime: RuntimeConfig{Node: "20", Database: DatabaseMariaDB},
	HasAppRole:     true,
	ExecUser:       "1000:1000",
	ExecWorkdir:    "/var/www/html",
	Tools:          nodeTools,
},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/tainer/manifest/ -v`
Expected: PASS (including TestTypeSpecTable's structural invariants).

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... 2>&1 | tail -20` — expect green except any pre-existing skips. If a test elsewhere counts types or iterates AllSpecs with hardcoded expectations, it will surface here; fix the count in that test (that is the test's job).

```bash
git add pkg/tainer/manifest/
git commit -m "feat(manifest): sveltekit project type (alias kit) — nextjs-shape TypeSpec row (TAIN-104)"
```

### Task 2: Pod-layer coverage rows for sveltekit

Tests-only task: the pod layer is generic over the TypeSpec table, so correct behavior already falls out of Task 1 — these rows lock it. Watching them fail is not possible for table-driven coverage of existing generic code; instead each new assertion must be verified to assert the sveltekit-specific value (not copy-pasted svelte values).

**Files:**
- Test: `pkg/tainer/pod/types_test.go`
- Test: `pkg/tainer/pod/start_test.go`

**Interfaces:**
- Consumes: `manifest.TypeSvelteKit` from Task 1.

- [ ] **Step 1: Add rows to `types_test.go`:**

- `TestRolesForType` (line 10 area): row `{manifest.TypeSvelteKit, []string{"web", "app", "db"}}`.
- `TestRolesForPod` (line 30 area): row `{"sveltekit+none", manifest.TypeSvelteKit, manifest.DatabaseNone, []string{"web", "app"}}`.
- `TestDefaultExecRole` (line 57 area): assert `DefaultExecRole(manifest.TypeSvelteKit) == "app"` (same shape as the svelte assertion at line 67).
- `TestImageRef` (line 72): add a `mSvelteKit` manifest (Type: TypeSvelteKit, Node "20") asserting app image `ghcr.io/cyber5-io/tainer-sveltekit-app:node-20` and web image `ghcr.io/cyber5-io/tainer-caddy-web:2-alpine`.

- [ ] **Step 2: Add the buildEnv test to `start_test.go`** (model it on the svelte TAINER_BUILD_DIR test at lines 159-182, inverted):

```go
// TestBuildEnvSvelteKitNoBuildDir locks sveltekit as a server type:
// caddy proxies to the app, so the web role must NOT get
// TAINER_BUILD_DIR even when runtime.build-dir is set.
```

Test name `TestBuildEnvSvelteKitNoBuildDir`; build a manifest with Type sveltekit and `runtime.build-dir: build` (reuse the local `mk` helper pattern), call `buildEnv(m, RoleWeb, pctx)`, assert NO element has prefix `TAINER_BUILD_DIR=` (the loop shape at lines 150-155 does exactly this for nextjs — mirror it).

- [ ] **Step 3: Run and verify**

Run: `go test ./pkg/tainer/pod/ -v -run 'TestRolesForType|TestRolesForPod|TestDefaultExecRole|TestImageRef|TestBuildEnv'`
Expected: PASS. Before trusting a pass, deliberately flip one expectation (e.g. expect `tainer-svelte-app` in the sveltekit ImageRef row), run, watch it FAIL with the sveltekit value, revert. That is this task's RED proof.

- [ ] **Step 4: Commit**

```bash
git add pkg/tainer/pod/
git commit -m "test(pod): lock sveltekit roles, exec, image ref, proxy env (TAIN-104)"
```

### Task 3: Wizard entry, per-type scaffold hint, docs

**Files:**
- Modify: `pkg/tainer/tui/wizard/wizard.go:69-82` (projectTypes list)
- Modify: `cmd/tainer/main.go:1292,1313-1314,1347-1348,1411-1413` and the second hint site near line 1515 (restart path)
- Test: `cmd/tainer/main_test.go`
- Modify: `CLAUDE.md` (Supported Project Types section)

**Interfaces:**
- Consumes: `manifest.TypeSvelteKit` from Task 1.
- Produces: `scaffoldHint(t manifest.ProjectType) string` in cmd/tainer (package main), used at every former `noAppHint` print site.

- [ ] **Step 1: Write the failing test** in `cmd/tainer/main_test.go`:

```go
func TestScaffoldHint(t *testing.T) {
	kit := scaffoldHint(manifest.TypeSvelteKit)
	if !strings.Contains(kit, "sv create") {
		t.Errorf("sveltekit hint = %q, want the sv create scaffold", kit)
	}
	if !strings.Contains(kit, "tainer restart") {
		t.Errorf("sveltekit hint = %q, want the restart tail", kit)
	}
	generic := scaffoldHint(manifest.TypeNextJS)
	if !strings.Contains(generic, "create vite@latest") {
		t.Errorf("nextjs hint = %q, want the generic vite copy", generic)
	}
	if scaffoldHint(manifest.TypeNodeJS) != generic {
		t.Error("nodejs hint differs from the generic copy")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/tainer/ -run TestScaffoldHint -v`
Expected: compile error `undefined: scaffoldHint`.

- [ ] **Step 3: Implement `scaffoldHint(t manifest.ProjectType) string` in `cmd/tainer/main.go`**

Replace the `noAppHint` const (line 1413) with the function. Generic copy stays byte-identical to today's const: `"no app in html/ yet — scaffold one (e.g. `tainer npm create vite@latest .`), then `tainer restart`"`. The sveltekit case returns: `"no app in html/ yet — scaffold one (e.g. `tainer npm exec -- sv create .`), then `tainer restart`"` (SvelteKit's scaffolder is `sv create`; `npm exec -- sv create .` is the npm spelling of `npx sv create .`).

Update the call sites: each former `noAppHint` use already sits next to a `nodeAppMissing(manifestPath, projectDir)` call, which loads the manifest internally. Add a small helper `scaffoldHintFor(manifestPath string) string` that loads the manifest (`manifest.Load`) and returns `scaffoldHint(m.Project.Type)`, falling back to the generic copy on load error; use it at all three-plus print sites (styled start ~1314, plain start ~1348, restart ~1515 — grep `noAppHint` for the full set).

- [ ] **Step 4: Add the wizard entry**

In `wizard.go` projectTypes (line 69), insert `{manifest.TypeSvelteKit, "SvelteKit"}` after `{manifest.TypeSvelte, "Svelte"}` (matching the const/table order from Task 1). `dbChoices` needs no change: sveltekit falls into the default case `{"postgres", "mariadb"}`, same as nextjs.

- [ ] **Step 5: Run tests and the full suite**

Run: `go test ./cmd/tainer/ -run TestScaffoldHint -v` — Expected: PASS.
Run: `go test ./... 2>&1 | tail -10` — Expected: green.
Run: `gofmt -l cmd pkg && go vet ./...` — Expected: no output / exit 0.

- [ ] **Step 6: Update CLAUDE.md and smoke `tainer init`**

In CLAUDE.md's "Supported Project Types" section, the types line becomes: `wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, svelte, sveltekit, kompozi.` and add one sentence after the SPA sentence: `sveltekit (alias kit) is the server-side svelte type — nextjs shape: caddy proxies to the SvelteKit dev server.`

Smoke (no engine needed): `go run ./cmd/tainer init --help >/dev/null && echo ok` — Expected: `ok`. Then in a temp dir: `cd $(mktemp -d) && go run <absolute-repo-path>/cmd/tainer init --name=kitsmoke --type=kit --no-start 2>&1 | head -5; cat tainer.yaml | head -8` — Expected: a manifest with `type: sveltekit`, `node: "20"`. (If `--no-start`/flag spellings differ, check `go run ./cmd/tainer init --help` and use the non-interactive spelling it documents; abort the smoke rather than starting a pod.)

- [ ] **Step 7: Commit**

```bash
git add cmd/tainer/ pkg/tainer/tui/wizard/ CLAUDE.md
git commit -m "feat(cli): sveltekit in wizard + per-type scaffold hint (TAIN-104)"
```

## tainer-images repo (Tasks 4–5, branch `TAIN-104-sveltekit-images` from `TAIN-98-react-app-role`)

Repo: `/Users/lenineto/dev/cyber5-io/tainer-images`. First: `git checkout TAIN-98-react-app-role && git checkout -b TAIN-104-sveltekit-images`. No Go here — verification is yq/grep over YAML and Caddyfile inspection. Current state to build on: the `framework` build job matrix ends with react-app entries at lines ~451-467 of `.github/workflows/build-images.yml`; the `framework-manifest` job matrix ends with nestjs-app entries (~line 615) — react-app is MISSING there (that's leftover (c)).

### Task 4: Workflow matrix — sveltekit-app, svelte-app, react-app manifest fix

**Files:**
- Modify: `.github/workflows/build-images.yml` (both the `framework` job matrix ~lines 387-467 and the `framework-manifest` job matrix ~lines 589-616)

**Interfaces:**
- Produces: published tag names `tainer-sveltekit-app:node-{20,22,24}` and `tainer-svelte-app:node-{20,22,24}` (multi-arch), consumed by tainer's ImageRef at runtime (Task 2 already pinned the refs).

- [ ] **Step 1: Add build-job entries.** In the `framework` job matrix, after the react-app-24 entry, append following the exact house pattern (react-app entries at lines 451-467 are the template — `name`, `file`, `tag`, `build-args: NODE_VERSION=<v>`, `version-tag: "node-<v>"`, `latest: true` on the 22 entry only):
  - `sveltekit-app-20/22/24` with `file: node-app/Containerfile`, `tag: sveltekit-app`
  - `svelte-app-20/22/24` with `file: react-app/Containerfile`, `tag: svelte-app`

- [ ] **Step 2: Add manifest-job entries.** In the `framework-manifest` job matrix after the nestjs-app rows, append the same `- tag:`/`version-tag:` pairs used for nextjs-app (node-20; node-22 with `latest: true`; node-24) for each of: `react-app` (the TAIN-98 fix), `sveltekit-app`, `svelte-app`.

- [ ] **Step 3: Verify by machine, not eyeball**

Run (yq v4 is available via `brew list yq || brew install yq`; if unavailable use the grep fallback below):
`yq '.jobs.framework.strategy.matrix.include[] | select(.tag == "sveltekit-app" or .tag == "svelte-app") | .name' .github/workflows/build-images.yml`
Expected: six names (sveltekit-app-20/22/24, svelte-app-20/22/24).
`yq '[.jobs.framework-manifest.strategy.matrix.include[] | select(.tag == "react-app" or .tag == "sveltekit-app" or .tag == "svelte-app")] | length' .github/workflows/build-images.yml`
Expected: `9`.
Also: `yq '.jobs.framework.strategy.matrix.include[] | select(.tag == "svelte-app") | .file' ...` — Expected: `react-app/Containerfile` three times.
Grep fallback: `grep -c 'tag: sveltekit-app' .github/workflows/build-images.yml` → `6` (3 build + 3 manifest), same for svelte-app; `grep -c 'tag: react-app'` → `6`.
Sanity: `yq '.jobs | keys' .github/workflows/build-images.yml` parses (file is valid YAML).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/build-images.yml
git commit -m "ci: sveltekit-app + svelte-app images; react-app multi-arch manifest fix (TAIN-104)"
```

### Task 5: sveltekit.Caddyfile and svelte.Caddyfile

**Files:**
- Create: `caddy-web/sites/sveltekit.Caddyfile`
- Create: `caddy-web/sites/svelte.Caddyfile`

**Interfaces:**
- Consumes: nothing from other tasks. The caddy-web image's entrypoint picks `sites/$TAINER_PROJECT_TYPE.Caddyfile` at runtime; the file names must be exactly the canonical type strings `sveltekit` and `svelte`.

- [ ] **Step 1: Create `sveltekit.Caddyfile`** — byte-for-byte copy of `caddy-web/sites/nextjs.Caddyfile` with the header comment's first line adjusted to say "a sveltekit project" and a note that the SvelteKit/vite dev server is forced onto port 3000 by start-node-app.sh's `--port $PORT --host` injection. Directives identical: `:80`, `request_body max_size 2GB`, `reverse_proxy 127.0.0.1:3000 { header_up Host localhost }`, `encode gzip`.

- [ ] **Step 2: Create `svelte.Caddyfile`** — byte-for-byte copy of `caddy-web/sites/react.Caddyfile` with comments adjusted (svelte SPA, `tainer-svelte-app` as the workbench image name). Directives identical: `root * /var/www/html/{$TAINER_BUILD_DIR:dist}`, `try_files {path} /index.html`, `file_server`, `encode gzip`.

- [ ] **Step 3: Validate**

If a local caddy exists (`command -v caddy`): `TAINER_BUILD_DIR=dist caddy validate --adapter caddyfile --config caddy-web/sites/sveltekit.Caddyfile` and the same for svelte.Caddyfile — Expected: `Valid configuration`. If no caddy binary: `diff <(grep -v '^#' caddy-web/sites/sveltekit.Caddyfile) <(grep -v '^#' caddy-web/sites/nextjs.Caddyfile)` — Expected: empty (directives identical to the known-good file), and same diff for svelte vs react.

- [ ] **Step 4: Commit**

```bash
git add caddy-web/sites/
git commit -m "feat(caddy): sveltekit proxy site + svelte SPA site (TAIN-104)"
```
