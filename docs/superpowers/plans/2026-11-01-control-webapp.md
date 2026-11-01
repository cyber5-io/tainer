# Control Webapp (TAIN-113) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The my.tainer.me dashboard — a pinned SvelteKit pod that is a strict client of the host bridge, with login against tainer accounts brokered host-side.

**Architecture:** Three repos. tainer gains the accounts client + `login`/`logout`/`whoami` commands, pinned-pod guards for the reserved `control` project, and gateway env/token injection when starting it. cyber-stack gains three gateway allowlist entries with stdin passthrough for `login`, a `control.addr` file beside `control.token`, control-pod exclusion from the event stream, and control-pod auto-start on first healthy. A new repo `tainer-control` holds the SvelteKit app (SvelteKit 2 / Svelte 5 runes, adapter-node, vitest) whose server proxies the gateway — the browser never sees the control token.

**Tech Stack:** Go (tainer, cyber-stack), SvelteKit 2 + Svelte 5 + TypeScript + vitest (tainer-control).

**Spec:** docs/superpowers/specs/2026-10-31-control-webapp-design.md (binding), with docs/superpowers/specs/2026-10-31-host-bridge-design.md and 2026-10-31-tainer-accounts-design.md as companions. The wire contract is docs/api/cli-json-v1.md — build against that page, not the code.

## Global Constraints

- Branch bases: tainer work on `TAIN-113-control-webapp` cut from `0d624fae63` (TAIN-111 tip); cyber-stack work on `TAIN-113-gateway-accounts` cut from `bcef37e` (host-bridge tip); `tainer-control` is a fresh repo (git init, branch `main`).
- Envelope rules are contractual: documents start `"v":1`; errors are exactly `{"v":1,"error":{"code":<kebab-slug>,"message":...}}` + exit 1; timestamps RFC 3339 UTC with exactly three fractional digits.
- UX copy (CLI output, web UI, docs) never says podman, docker, engine, VM, or container. Pod parts are named by role.
- No Co-Authored-By or AI mentions in commits.
- The webapp holds zero business logic and zero `pkg/tainer` imports: every decision happens behind the gateway. The browser never receives the control token or any account token; only the SvelteKit server talks to the gateway.
- The refresh token never leaves the host: it lives in `account.json` (0600) in the daemon state directory; the webapp learns only `loggedIn`/email via `whoami`.
- Accounts service base URL: `https://id.tainer.dev`, overridable via `TAINER_ID_URL` (dev: the tainer-id pod). API shapes are fixed by the running tainer-id service: `POST /v1/login` `{email,password,device_name}` → `{access_token,token_type,expires_in,refresh_token}`; `POST /v1/token` `{refresh_token}` → same shape (rotated); `GET /v1/me` (Bearer access) → `{id,email,email_verified,created_at}`; errors `{error:{code}}` with codes `invalid_credentials`, `email_unverified`, `too_many_attempts`, `session_revoked`, `session_expired`, `invalid_token`.
- Gateway consumption per cli-json-v1.md §8: events via `GET /v1/watch` (never a `watch` child through `/v1/cli`); token file read with a bounded wait (give up after 90s, retry later); re-read the token after any 401; treat HTTP 200 + top-level `error` by the discriminator rule; at most one streaming `/v1/cli` child (logs) at a time from the webapp.
- Design tokens (binding, from the approved canvas): teal `#00D4AA` (primary/running), orange `#FF6B35`, blue `#4E9EF4`, one slate hue family for both themes, sidebar always an undertone of the content surface, Space Grotesk headings / Inter-or-system UI text. Light + dark ship together, system-following with manual override.
- Suites: tainer `go test ./...` + `gofmt`/`go vet`; cyber-stack `go test ./...` (agent builds need `-tags containers_image_openpgp` for cross, not for unit tests); tainer-control `npm test` (vitest) + `npm run check`.

## Review Focus

1. Daemon restart mid-session: the control token rotates — a gateway 401 after success must trigger one token re-read + retry, not a logout or an error card (pinned by T7's `retries once after re-reading token on 401`).
2. "Stack down" vs "bad credentials" on the login screen: gateway unreachable must render the stack-down state, never "wrong password" (pinned by T6's `login distinguishes gateway-unreachable from invalid credentials`).
3. Watch stream death: the UI must show a reconnecting banner and mark pod data stale, never keep rendering a frozen HEALTHY (pinned by T8's `marks state stale when the stream drops`).
4. Concurrent whoami refresh races (CLI + webapp both rotating): rotation grace means both succeed, but the LAST write of account.json must hold a refresh token that still works — save-before-return and re-read-on-invalid are pinned by T1's rotation tests.
5. A pod whose state cannot be queried (`queryError`) or an unknown progress `op`: cards render gracefully, nothing throws (pinned by T8/T9 fixture tests).

---

### Task 1: tainer — accounts client package

**Files:**
- Create: `pkg/tainer/accounts/store.go`, `pkg/tainer/accounts/client.go`, `pkg/tainer/accounts/session.go`
- Test: `pkg/tainer/accounts/store_test.go`, `client_test.go`, `session_test.go`

**Interfaces:**
- Produces:
  - `type Session struct { Email, UserID, RefreshToken, AccessToken string; AccessTokenExpiresAt time.Time }`
  - `func Load(dir string) (*Session, error)` — (nil, nil) when the file is absent; `func Save(dir string, s *Session) error` — writes `account.json` atomically, mode 0600; `func Clear(dir string) error` — idempotent.
  - `type Client struct { BaseURL string; HTTP *http.Client }`; `func NewClient() *Client` — BaseURL from `TAINER_ID_URL` else `https://id.tainer.dev`.
  - `type TokenPair struct { AccessToken string; ExpiresIn int; RefreshToken string }`; `type User struct { ID, Email string; EmailVerified bool }`
  - `func (c *Client) Login(email, password, deviceName string) (*TokenPair, error)`; `func (c *Client) Refresh(refreshToken string) (*TokenPair, error)`; `func (c *Client) Me(accessToken string) (*User, error)`
  - `type APIError struct { Status int; Code string }` implementing `error` — every non-2xx becomes one.
  - `func Ensure(dir string, c *Client) (*Session, error)` — loads the session; when the stored access token has >60s left, returns as-is; otherwise calls `Refresh`, **saves the rotated pair before returning**, and on `APIError` code `invalid_token` re-reads the file once (another process may have rotated) before failing. Returns (nil, nil) when logged out.

- [ ] **Step 1: Write the failing tests** — `TestStoreRoundTripAndMode` (Save then Load equal; file mode 0600; Load on missing dir → nil,nil), `TestClientMapsAPIErrors` (httptest server returning 401 `{"error":{"code":"invalid_credentials"}}` → APIError{401,"invalid_credentials"}), `TestEnsureRefreshesAndSavesBeforeReturn` (expired access token → Refresh called once, file on disk holds the NEW refresh token even if the caller discards the result), `TestEnsureRereadsOnInvalidToken` (server rejects stale refresh with `invalid_token`; a second process already saved a newer session file; Ensure retries from the re-read file and succeeds).
- [ ] **Step 2: Run them — FAIL (package missing)** — `go test ./pkg/tainer/accounts/`
- [ ] **Step 3: Implement store.go, client.go, session.go per the Interfaces block.** JSON wire names are snake_case per the Global Constraints shapes.
- [ ] **Step 4: Run — PASS**, then `go test ./...` green.
- [ ] **Step 5: Commit** `feat(accounts): id.tainer.dev client, session store, refresh rotation`

### Task 2: tainer — login, logout, whoami commands + contract doc

**Files:**
- Create: `cmd/tainer/login.go`, `cmd/tainer/login_test.go`
- Modify: `cmd/tainer/api.go` (registry), `docs/api/cli-json-v1.md`, the api golden in `cmd/tainer/api_test.go`'s expectations
- Test: `cmd/tainer/login_test.go`

**Interfaces:**
- Consumes: Task 1's `accounts` package. The daemon state directory: resolve with the same helper the CLI already uses to locate daemon state (where cyberstackd writes `control.token`) — find it where the daemon spawn/config code lives and reuse it; never hardcode a path.
- Produces (wire, all three registered in `tainer api` as json:true, stream:false):
  - `tainer login --json`: reads ONE JSON object from stdin `{"email":"...","password":"...","deviceName":"..."}` (deviceName optional → default `<hostname> · tainer`). Success: `{"v":1,"loggedIn":true,"userId":"...","email":"..."}` (never echoes tokens). Errors: `login-failed` (invalid_credentials), `email-unverified`, `too-many-attempts`, `id-unreachable` (network/5xx), `invalid-arguments` (malformed stdin). Human mode: interactive prompts (password no-echo via term).
  - `tainer whoami --json`: document, exit 0 in both states: `{"v":1,"loggedIn":false}` or `{"v":1,"loggedIn":true,"userId":"...","email":"...","accessToken":"...","accessTokenExpiresAt":"2026-11-01T09:00:00.000Z"}` (refreshes via `accounts.Ensure`; a failed refresh with `session_expired`/`session_revoked`/`invalid_token` clears the store and returns loggedIn:false; `id-unreachable` while a stored unexpired access token exists returns it with `"offline":true`).
  - `tainer logout --json`: best-effort revoke at the id service (`POST /v1/logout` with the refresh token), always clears the local store: `{"v":1,"loggedOut":true}`.

- [ ] **Step 1: Write failing tests** — `TestLoginJSONReadsStdinAndStoresSession` (httptest id service; stdin JSON; asserts success document + account.json exists with refresh token), `TestLoginJSONBadCredentials` (→ `login-failed` envelope, exit 1), `TestWhoamiLoggedOutDocument` (no store → `{"v":1,"loggedIn":false}`, exit 0), `TestWhoamiRefreshesExpiredToken`, `TestWhoamiOfflineFallsBackToStoredToken`, `TestLogoutClearsStore`, and extend the api-listing test for the three new commands.
- [ ] **Step 2: Run — FAIL** — `go test ./cmd/tainer/ -run 'Login|Whoami|Logout|API'`
- [ ] **Step 3: Implement** per Interfaces; wire new slugs into the slug table source if one exists in code.
- [ ] **Step 4: Update `docs/api/cli-json-v1.md`:** new section "10. Accounts" documenting the three commands' shapes verbatim, the new slugs added to the §2 table (`login-failed`, `email-unverified`, `too-many-attempts`, `id-unreachable`, `pod-pinned` — the last one lands in Task 4 but the table entry lands here), §1 discovery example regenerated, §8 allowlist sentence updated to include `login`, `logout`, `whoami` and the `"stdin"` body field (behaviour lands in cyber-stack Task 3; the doc is the contract both sides build to). Run the doc drift-guard test and the goldens.
- [ ] **Step 5: Run `go test ./...` — PASS. Commit** `feat(cli): tainer login/logout/whoami against tainer accounts (TAIN-113)`

### Task 3: cyber-stack — gateway accounts passthrough, control.addr, control-pod stream exclusion, auto-start

**Files:**
- Modify: `internal/controlapi/server.go` (+allowlist, stdin), `internal/controlapi/token.go` or sibling (`control.addr`), `internal/daemon/events.go` (sampler exclusion), `internal/daemon/control.go` (auto-start hook)
- Test: `internal/controlapi/server_test.go`, `internal/daemon/events_test.go`

**Interfaces:**
- Consumes: the existing gateway (`internal/controlapi`), sampler (`internal/daemon/events.go`).
- Produces:
  - Allowlist grows by `login`, `logout`, `whoami`.
  - `POST /v1/cli/{cmd}` body may carry `"stdin": "<string>"` (≤64 KiB): piped to the child's stdin then closed. Permitted ONLY for `login`; any other command with `stdin` present → 400 `bad-request`. The stdin value is never logged.
  - `control.addr` file written beside `control.token` at listener start (same perms/lifecycle): one line, the base URL reachable **from inside a pod** for the current mode (e.g. `http://192.168.105.1:7754`), using the same per-mode address logic the listener already computes. Removed on stop.
  - Sampler: a pod named `control` never appears in `hello.pods` or `pod` events (the dashboard does not show itself).
  - Auto-start: on each boot, the first time the engine reaches `healthy` and the gateway is active, spawn `tainer start control --json` once (fire-and-forget, argv exec like the gateway's own CLI spawns, output logged at debug; a `project-not-found` failure is normal pre-provisioning and logged quietly).

- [ ] **Step 1: Write failing tests** — `TestGatewayStdinOnlyForLogin` (login child receives the body's stdin verbatim; `status` with stdin → 400), `TestGatewayAllowsAccountsCommands`, `TestControlAddrFileLifecycle` (exists after start with a valid URL line, gone after stop), `TestSamplerExcludesControlPod` (snapshot containing pod `control` → no pod event, hello omits it), `TestAutoStartSpawnsOnceOnFirstHealthy` (fake exec recorder; healthy→recovering→healthy spawns exactly once).
- [ ] **Step 2: Run — FAIL** — `go test ./internal/controlapi/ ./internal/daemon/`
- [ ] **Step 3: Implement.** Reuse the existing argv-exec helper for the auto-start spawn.
- [ ] **Step 4: Run `go test ./...` — PASS. Commit** `feat(controlapi,daemon): accounts passthrough, control.addr, control pod pinning hooks (TAIN-113)`

### Task 4: tainer — reserved `control` project: pinned guards, list/status exclusion

**Files:**
- Modify: where `stop`, `destroy`, `init`, `list`, and all-projects `status` resolve/iterate projects (locate in `pkg/tainer` / `cmd/tainer`)
- Test: alongside the touched commands' existing test files

**Interfaces:**
- Produces:
  - `tainer stop control` / `tainer destroy control` → envelope `pod-pinned`, message "the control dashboard runs whenever tainer runs" (human mode: same sentence, exit 1). Hidden flag `--pinned-override` (not in help) bypasses both, for development and packaging scripts.
  - `tainer stop --all` and `destroy --nuke` skip `control` silently (nuke with `--pinned-override` includes it).
  - `tainer list` (both modes) and the all-projects `status` document exclude `control`. `tainer status control`, `logs`, `exec`, `config` still work by name (debugging).
  - `tainer init` refuses the project name `control` without `--pinned-override` (slug `invalid-arguments`, message says the name is reserved).

- [ ] **Step 1: Write failing tests** — `TestStopControlIsPinned`, `TestDestroyControlIsPinned`, `TestPinnedOverrideBypasses`, `TestListExcludesControl`, `TestStatusAllExcludesControl`, `TestInitRefusesReservedName` — table-driven where the surrounding tests are.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Implement** with one helper `IsPinnedProject(name string) bool` (today: `name == "control"`) in `pkg/tainer`, used at every site — no scattered string literals.
- [ ] **Step 4: `go test ./...` — PASS. Commit** `feat(pod): reserved pinned control project (TAIN-113)`

### Task 5: tainer — gateway env + token injection when starting `control`

**Files:**
- Modify: pod container-creation code in `pkg/tainer/pod` (where env and mounts are assembled for the app container)
- Test: alongside the existing pod creation tests

**Interfaces:**
- Consumes: Task 3's `control.addr` + `control.token` files; the daemon state-dir helper from Task 2.
- Produces: when the project being started is `control`, the app container additionally gets: bind mount of the state dir's `control.token` → `/run/tainer/control.token` (read-only) and `control.addr` → `/run/tainer/control.addr` (read-only), plus env `TAINER_GATEWAY_ADDR_FILE=/run/tainer/control.addr` and `TAINER_CONTROL_TOKEN_FILE=/run/tainer/control.token`. If the files do not exist yet at start time, mount the state directory paths anyway (the files appear when the gateway is up; the webapp already waits with a timeout) — but if the state dir itself is unknown, start proceeds with no injection (webapp renders its gateway-unreachable state).

- [ ] **Step 1: Write failing test** — `TestControlPodGetsGatewayMounts` (creation spec for project `control` carries both mounts + both env vars; an ordinary project carries none).
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: `go test ./...` — PASS. Commit** `feat(pod): inject gateway address+token into the control pod (TAIN-113)`

### Task 6: tainer-control — repo scaffold, design tokens, shell, login screen

**Files:**
- Create: repo `/Users/lenineto/dev/cyber5-io/tainer-control` — `tainer.yaml` (name `control`, type `sveltekit`, domain `my.tainer.me`, size `small`), `html/` SvelteKit app (SvelteKit 2, Svelte 5, TypeScript, adapter-node, vitest; current stable majors), `README.md`, `.gitignore` (data/, db/ contents per tainer convention)
- Key app files: `html/src/app.css` (design tokens), `html/src/routes/+layout.svelte` (shell), `html/src/routes/login/+page.svelte`, `html/src/lib/server/gateway.ts`, `html/src/lib/server/auth.ts`, `html/src/hooks.server.ts`
- Test: `html/src/lib/server/gateway.test.ts`, `html/src/lib/server/auth.test.ts`

**Interfaces:**
- Produces (consumed by every later task):
  - `gateway.ts`: `export async function cli(cmd: string, args: string[], opts?: { stdin?: string }): Promise<unknown>` — POSTs `/v1/cli/{cmd}`, parses JSON, **throws `GatewayError`** (`kind: 'unreachable' | 'busy' | 'cli'`, plus `code`/`message` from the envelope for `kind:'cli'` per the discriminator rule — a document with no top-level `error` resolves, even on report commands). `export function cliStream(cmd: string, args: string[]): Promise<ReadableStream<Uint8Array>>` for NDJSON. `export async function watchStream(): Promise<ReadableStream<Uint8Array>>` for `GET /v1/watch`. Address from `TAINER_GATEWAY_ADDR_FILE` (fallback env `TAINER_GATEWAY_URL` for dev), token from `TAINER_CONTROL_TOKEN_FILE` (fallback `TAINER_CONTROL_TOKEN`); token cached, re-read once and the request retried on a 401; bounded startup wait (poll files up to 90s, then `unreachable`).
  - `auth.ts`: `export async function whoami(): Promise<{ loggedIn: boolean; email?: string; userId?: string }>` — `cli('whoami', [])`, cached 30s; `login(email, password)` → `cli('login', [], { stdin: JSON.stringify({email, password, deviceName: 'my.tainer.me dashboard'}) })`, mapping slugs `login-failed`→"wrong email or password", `email-unverified`, `too-many-attempts`, and `GatewayError kind:'unreachable'`→ the stack-down state; `logout()`.
  - `hooks.server.ts`: every route except `/login` and static assets requires `whoami().loggedIn`; otherwise redirect `/login`. `locals.user` carries the whoami result.
  - Design tokens in `app.css` as CSS custom properties: the brand trio, a slate scale used by BOTH themes (dark = low lightness steps, light = high), `data-theme` override persisted in a cookie, `prefers-color-scheme` default. Space Grotesk via self-hosted woff2 (no third-party fetch at runtime), system font stack for UI text.
  - Shell per the approved canvas: sidebar (Pods, Doctor, Updates, Settings; HOSTING section with locked "Live sites — soon"; account chip at bottom), content header slot (title, teaching line, status pill, counts, search, New pod).

- [ ] **Step 1: Scaffold** (`npx sv create` non-interactive, vitest + check wired) and commit the clean skeleton.
- [ ] **Step 2: Write failing tests** — `gateway.test.ts`: `retries once after re-reading token on 401` (mock fetch: first 401, token file changed, second 200), `maps envelope errors by the discriminator rule` (200 + `{"v":1,"error":{...}}` → GatewayError kind 'cli'; 200 + doctor document with `healthy:false` → resolves), `bounded wait gives unreachable` (fake timers). `auth.test.ts`: `login distinguishes gateway-unreachable from invalid credentials`, `whoami caches for 30s`.
- [ ] **Step 3: Run — FAIL** (`npm test`), implement `gateway.ts`/`auth.ts`/`hooks.server.ts`, run — PASS.
- [ ] **Step 4: Build the shell + login screen** (brand + one form, no dashboard chrome; error states from auth.ts mapping incl. stack-down). `npm run check` clean.
- [ ] **Step 5: Commit** `feat(app): scaffold, design tokens, shell, login (TAIN-113)`

### Task 7: tainer-control — watch SSE relay + live stack store

**Files:**
- Create: `html/src/routes/api/watch/+server.ts`, `html/src/lib/stores/stack.svelte.ts`
- Test: `html/src/lib/stores/stack.test.ts`, `html/src/routes/api/watch.test.ts`

**Interfaces:**
- Consumes: Task 6's `watchStream()`.
- Produces:
  - `GET /api/watch` (auth-gated like everything): Server-Sent Events; each NDJSON line from the gateway becomes one `data:` event verbatim; gateway stream end → SSE event `event: ended` then close; client reconnects (EventSource auto-reconnect) and each new connection re-relays a fresh hello.
  - `stack.svelte.ts` (Svelte 5 runes class, browser-side): `class StackStore { engine: { state: 'healthy'|'recovering'|'failed'|'unknown'; since?: string; detail?: string }; pods: Map<string, Pod>; progress: Map<string, Progress>; connection: 'live'|'reconnecting'; stale: boolean }` — hello replaces the pod map; `pod` events upsert; while engine is not healthy pod states render subordinate (stale); unknown event types and unknown fields ignored; one invalid JSON line skipped; `progress` keyed by `op\u0000pod`, cleared on `done:true` after a short linger. A 60s `cli('list')` reconciliation poll covers the pod-removed gap (TAIN-117): pods absent from list leave the map.

- [ ] **Step 1: Write failing store tests** (feed scripted event lines): `hello replaces state`, `pod event upserts`, `marks state stale when the stream drops` (connection 'reconnecting' + stale true), `engine not healthy subordinates pods`, `unknown event type ignored`, `invalid final line skipped`, `list reconciliation removes vanished pods`, `progress clears on done`.
- [ ] **Step 2: Run — FAIL, implement, PASS.**
- [ ] **Step 3: Relay route test** (mock watchStream): lines come out as SSE `data:` frames, end → `ended`.
- [ ] **Step 4: `npm test` + `npm run check` — PASS. Commit** `feat(app): live watch relay and stack store (TAIN-113)`

### Task 8: tainer-control — pod grid dashboard

**Files:**
- Create: `html/src/routes/+page.svelte`, `html/src/lib/components/PodCard.svelte`, `NewPodCard.svelte`, `StatusPill.svelte`, `html/src/lib/pod-actions.ts`, `html/src/routes/api/cli/[cmd]/+server.ts`
- Test: `html/src/lib/components/PodCard.test.ts`, `html/src/lib/pod-actions.test.ts`

**Interfaces:**
- Consumes: StackStore; `cli()`.
- Produces:
  - `POST /api/cli/[cmd]` — thin auth-gated relay to `cli()` for an allowlist the UI uses (`start`, `stop`, `restart`, `init`, `destroy`, `db`, `update`, `doctor`, `list`, `status`, `config`, `network`): body `{args: string[]}`; responds with the CLI document or `{error}` + 200 (the discriminator travels to the browser untouched). `login/logout/whoami/exec` are NOT relayable (auth has its own routes; exec is CLI-only policy).
  - `pod-actions.ts`: `start(name)`, `stop(name)`, `restart(name)`, each returning the document or throwing with the envelope message; optimistic row state allowed only for start/stop/restart (watch confirms).
  - Pod cards per the canvas: floating type-coloured thumbnail header (teal sveltekit/node family, blue wordpress/php, orange react/SPA; grey + pod glyph when stopped), hover lift revealing the action row (open, refresh thumbnail, start/stop, logs, manage), body (name, type pill, domain link, size pill, memory-vs-budget bar when stats exist, status footer + Manage). Grid header: HEALTHY pill from engine state, "N of M running", search filter, New pod. NewPodCard: dashed teaching card, copy exactly: "Drop a folder here or run `tainer init` — any app becomes a pod."
  - Empty/edge states: no pods (teaching card prominent), engine recovering (banner + subdued grid), gateway unreachable (full-screen stack-down state shared with login).

- [ ] **Step 1: Write failing component tests** — `PodCard shows actions on hover/focus`, `stopped pod renders grey header`, `queryError pod renders without throwing`, `pod-actions surfaces envelope message on failure`, relay route `refuses non-allowlisted cmd`.
- [ ] **Step 2: Run — FAIL, implement, PASS.**
- [ ] **Step 3: Visual pass against the canvas artboards** (Main.dc.html dark, MainLight.dc.html) in both themes.
- [ ] **Step 4: `npm test` + `npm run check` — PASS. Commit** `feat(app): pod grid dashboard (TAIN-113)`

### Task 9: tainer-control — pod detail (Manage view)

**Files:**
- Create: `html/src/routes/pods/[name]/+page.svelte` + `+page.server.ts`, `html/src/routes/api/logs/[name]/+server.ts`, components `StatTile.svelte`, `PartsList.svelte`, `LogView.svelte`
- Test: `html/src/routes/pods/detail.test.ts`

**Interfaces:**
- Consumes: `cli('status',[name])`, `cli('config',[name])`, `cliStream('logs',[name])`, `db` relay, StackStore progress.
- Produces: the canvas PodDetail layout — stat tiles (status/uptime, size + "one budget for the whole pod", memory bar, domain + cert state), "Inside the pod" parts list from `status.containers` (role → friendly label mapping, e.g. app→"WordPress app" by project type, db→"Database", web→"Web server" — never the word container), tabs Overview / Logs / Database / Settings; Logs tab streams `/api/logs/[name]` (SSE like watch; **only one logs stream open at a time app-wide** — opening a second closes the first, honouring the CLI-child cap); Database tab: export (document → browser download of the file via a follow-up relay reading the exported path is NOT possible — instead `db export` writes into the pod project dir; MVP shows the resulting path + bytes) and import (file upload to the server, written to a temp path, `db import` pointed at it); Settings tab: read-only `config` render + the locked "Take <pod> live" strip; progress events for this pod render as an in-card progress strip.

- [ ] **Step 1: Write failing tests** — `detail renders parts with friendly labels`, `only one logs stream at a time`, `unknown progress op still renders message`.
- [ ] **Step 2: Run — FAIL, implement, PASS.**
- [ ] **Step 3: Visual pass against PodDetail.dc.html.**
- [ ] **Step 4: `npm test` + `npm run check` — PASS. Commit** `feat(app): pod detail manage view (TAIN-113)`

### Task 10: tainer-control — create-pod wizard, doctor, updates, settings

**Files:**
- Create: `html/src/routes/new/+page.svelte`, `html/src/routes/doctor/+page.svelte`, `html/src/routes/updates/+page.svelte`, `html/src/routes/settings/+page.svelte`
- Test: `html/src/routes/pages.test.ts`

**Interfaces:**
- Consumes: relays from Task 8.
- Produces: New-pod wizard mirroring `tainer init` (name, type from the supported list, size; runs `init` with args, then offers Start; progress events animate); Doctor page (run `doctor`, render checks with status/detail/hint, a "Check & heal" button adding `--fix`, report exit semantics respected — a failing doctor still renders its document); Updates page (`update --all` report table, per-pod update buttons); Settings page: theme override (system/light/dark, cookie), thumbnail cadence placeholder, sign-out. Doctor and Updates live in the sidebar per the shell.

- [ ] **Step 1: Write failing tests** — `wizard refuses reserved name control`, `doctor renders failing report document (not an error state)`, `update --all failures render per-pod`.
- [ ] **Step 2: Run — FAIL, implement, PASS.**
- [ ] **Step 3: `npm test` + `npm run check` — PASS. Commit** `feat(app): create wizard, doctor, updates, settings (TAIN-113)`

### Task 11: tainer-control — thumbnail capture service

**Files:**
- Create: `html/src/lib/server/capture.ts`, `html/src/routes/api/thumbnail/[name]/+server.ts`
- Test: `html/src/lib/server/capture.test.ts`

**Interfaces:**
- Produces: `capture.ts`: `capture(name: string, url: string): Promise<'captured' | 'cooldown' | 'unavailable'>` — puppeteer-core against a Chromium found via `TAINER_CHROMIUM_PATH` env or common pod paths; **when no Chromium exists, returns 'unavailable' without throwing** (cards keep the type-coloured placeholder — the control image bakes Chromium later, ticketed); one capture at a time (queue of 1, else cooldown), per-pod cooldown 120s, viewport 1280×800 scaled to 640×400 PNG, stored at `data/thumbnails/<name>.png` (survives pod restarts), 10s navigation timeout. `GET /api/thumbnail/[name]` serves the PNG or 404 (card falls back); `POST` triggers a capture (the card's refresh button; also fired once after a pod's first successful start via pod-actions).
- Blenzi-hosted sites are never captured here (spec) — no code path takes a non-`*.tainer.me` URL.

- [ ] **Step 1: Write failing tests** — `no chromium → unavailable, no throw`, `cooldown enforced`, `single-flight queue`, `refuses non-tainer.me URLs`.
- [ ] **Step 2: Run — FAIL, implement, PASS.**
- [ ] **Step 3: `npm test` + `npm run check` — PASS. Commit** `feat(app): thumbnail capture service with graceful fallback (TAIN-113)`

### Task 12: cross-repo live smoke + contract doc truth pass

**Files:**
- Create: smoke scripts in the session scratchpad (not committed); Modify: `docs/api/cli-json-v1.md` only if the smoke proves a documented detail wrong (ledger a ruling if so)

**Steps:**
- [ ] **Step 1:** Build dev `cyberstackd` (TAIN-113 cyber-stack branch) and dev `tainer` (TAIN-113 tainer branch). Start the isolated dev daemon exactly as the TAIN-111 smoke did (scratch HOME, `-no-vm`, `-dns-port 0`, short socket path under $TMPDIR) — **never touch the production daemon**.
- [ ] **Step 2:** Start the tainer-id dev server locally (its html/, `npm run dev`, dev mailer, PGlite dev db per its README/bringup), `TAINER_ID_URL` pointed at it. Sign up a smoke user, verify via the dev-mailer link, `tainer login --json` via stdin — assert the success document and `account.json` 0600.
- [ ] **Step 3:** Against the dev gateway: `POST /v1/cli/whoami` returns loggedIn:true; `POST /v1/cli/login` with stdin and wrong password returns the `login-failed` envelope over HTTP 200; `control.addr` file exists and parses.
- [ ] **Step 4:** Run the webapp (`npm run dev` in tainer-control/html with `TAINER_GATEWAY_URL` + `TAINER_CONTROL_TOKEN` pointed at the dev gateway): login page → sign in → dashboard renders the (empty) pod grid live from the watch relay; screenshot for the report.
- [ ] **Step 5:** Record the full transcript in the ledger. Nothing merges, nothing pushes.
