# Tainer Accounts Service (TAIN-112) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the id.tainer.dev accounts + sessions service as a new tainer-pod project: email+password identity, rotating device-bound refresh tokens with an absolute 7-day session lifetime (weekly re-login), ES256 access JWTs with published JWKS, and a pluggable transactional-email interface (dev logger + Postmark adapter).

**Architecture:** A SvelteKit (adapter-node) app whose `/v1/*` server routes are the entire product — there is no UI beyond a health page. Postgres holds users/identities/sessions plus empty-but-present orgs/entitlements tables; Drizzle owns the schema and migrations; tests run on the host against in-process PGlite so no pod or local Postgres is ever required. The service develops locally as the `tainer-id` pod at `id.tainer.me` and is NOT deployed live in this plan.

**Tech Stack:** SvelteKit 2 + TypeScript (adapter-node), Drizzle ORM + postgres-js (PGlite `@electric-sql/pglite` in tests), `jose` (ES256/JWKS), `@node-rs/argon2` (argon2id), vitest. Node 22.

**Spec:** `docs/superpowers/specs/2026-10-31-tainer-accounts-design.md` (in the tainer repo; this plan lives there too, the code does not). Companion for consumer expectations: `2026-10-31-host-bridge-design.md`, `2026-10-31-control-webapp-design.md`.

## Global Constraints

- New repo at `/Users/lenineto/dev/cyber5-io/tainer-id`, branch `main`, created by Task 1. All code tasks commit there; this plan file stays in the tainer repo.
- Tainer project layout: app in `html/`, `tainer.yaml` at root — **type `nodejs` for dev** (the sveltekit image is unpublished; Task 8 records the later `type: sveltekit` flip), `database: postgres`, domain `id.tainer.me`, name `tainer-id`.
- Scope is accounts + sessions ONLY. OAuth is a stretch: no OAuth task exists in this plan; the `identities` table ships, providers are a follow-up.
- Tokens: refresh = 32-byte opaque, stored as SHA-256 hash, device-bound, **absolute 7-day session lifetime** (login sets `expiresAt = login + 7d`; rotation NEVER extends it — controller ruling from Leni's "re-login weekly", 2026-10-31), rotated on every use with family reuse-detection; access = ES256 JWT, **15-minute TTL**, claims `sub`, `session`, `entitlements: []`, `iss: "https://id.tainer.dev"`, `aud: "tainer"`. JWKS at `/.well-known/jwks.json`.
- Passwords: argon2id only. Password policy: minimum 10 characters, no composition rules.
- Email: every send goes through the `EmailProvider` interface. Task 3 builds the interface + dev/log provider; Task 7 adds the Postmark adapter **unit-tested against a mocked HTTP layer — never invent or require a real API key**; live sending is a bring-up-checklist item.
- Account deletion endpoint exists but is feature-flagged OFF (`SELF_SERVE_DELETE`, default unset → endpoint returns 404). PENDING Leni; do not expose anywhere.
- Enumeration safety: signup, verify, and reset endpoints return the same shape for existing and unknown emails.
- All unit/service tests run on the host with `npm test` (vitest + PGlite). No pod, no Docker, no network. Pod-based verification is Task 8's checklist only.
- Nothing is pushed to any remote; no live deploy; never print or invent credentials. No AI attribution in code or commits. Single-line shell commands only.
- User-facing copy (emails, errors) never says podman/docker/engine/VM.

## Review Focus

Failure modes the spec implies but no task's happy path exercises — each line names the owning task, which carries the test:

1. **Stolen pre-rotation refresh token replay** → reuse of a superseded token revokes the whole session family (Task 4, `test_refresh_reuse_revokes_family`).
2. **Concurrent refresh race locking out the real client** → a retry with the just-superseded token inside the 30s grace window returns the same new pair instead of tripping reuse detection (Task 4, `test_refresh_retry_grace`).
3. **JWKS rotation window** → an access token signed by a retired-but-unexpired key still verifies via the published JWKS (Task 5, `test_retired_key_still_verifies`).
4. **Email enumeration** → signup-with-existing-email and reset-for-unknown-email return responses indistinguishable from the fresh-email case (Task 3 `test_signup_existing_email_indistinguishable`, Task 6 `test_reset_request_unknown_email_200`).
5. **Post-deletion/post-reset token survival** → after account deletion or password reset, every refresh attempt fails even though in-flight access tokens may live out their ≤15-minute TTL (Task 6, `test_reset_revokes_all_sessions`, `test_deleted_account_refresh_fails`).

---

### Task 1: Repo scaffold, tainer pod layout, health endpoint

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/tainer-id/` (git init, branch `main`)
- Create: `tainer.yaml`, `.gitignore`, `README.md`, `data/.gitignore`, `db/` (empty)
- Create: `html/` SvelteKit skeleton — `package.json`, `svelte.config.js` (adapter-node), `vite.config.ts` (vitest config in it), `tsconfig.json`, `src/app.d.ts`, `src/routes/healthz/+server.ts`
- Test: `html/src/routes/healthz/server.test.ts`

**Interfaces:**
- Produces: `GET /healthz` → `200 {"status":"ok","service":"tainer-id"}`; the repo + test harness every later task builds on. Route handlers are plain functions (`RequestHandler`) invoked directly in tests — no listening server in unit tests, ever.

- [ ] **Step 1: Create the repo and tainer layout**

Run: `mkdir -p /Users/lenineto/dev/cyber5-io/tainer-id && cd /Users/lenineto/dev/cyber5-io/tainer-id && git init -b main`
Then write `tainer.yaml` exactly:

```yaml
version: 2
project:
    name: tainer-id
    type: nodejs
    domain: id.tainer.me
    auto-open: false
runtime:
    node: "22"
    database: postgres
    shell: zsh
pod:
    size: small
```

`.gitignore`: `html/node_modules/`, `html/build/`, `html/.svelte-kit/`, `html/.env`, `.tainer-authorized_keys`, `.tainer.local.yaml`, `data/`, `db/`. `data/.gitignore` = `*` + `!.gitignore`; `db/` stays empty and untracked (Postgres needs an empty dir).

- [ ] **Step 2: Scaffold SvelteKit in html/ (skeleton project, TypeScript, vitest)**

Run: `cd html && npx sv create . --template minimal --types ts --no-add-ons --install npm` (accept current dir), then `npm i -D @sveltejs/adapter-node vitest && npm i drizzle-orm postgres jose @node-rs/argon2 && npm i -D drizzle-kit @electric-sql/pglite`
Swap adapter-auto for adapter-node in `svelte.config.js`. Add `"test": "vitest run"` script.

- [ ] **Step 3: Write the failing health test**

```ts
// html/src/routes/healthz/server.test.ts
import { describe, it, expect } from 'vitest';
import { GET } from './+server';

describe('healthz', () => {
  it('returns ok with the service name', async () => {
    const res = await GET();
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ status: 'ok', service: 'tainer-id' });
  });
});
```

- [ ] **Step 4: Run it — expect FAIL** (`Cannot find module './+server'`): `cd html && npm test`
- [ ] **Step 5: Implement `GET` in `src/routes/healthz/+server.ts`** returning that exact JSON via `json()` from `@sveltejs/kit`.
- [ ] **Step 6: Run `npm test` → PASS; `npm run build` → exits 0.**
- [ ] **Step 7: Commit** — `git add -A && git commit -m "feat: tainer-id scaffold — SvelteKit pod layout, healthz"`

### Task 2: Schema, migrations, PGlite test harness

**Files:**
- Create: `html/src/lib/server/db/schema.ts`, `html/src/lib/server/db/index.ts`, `html/drizzle.config.ts`, `html/drizzle/` (generated SQL migration)
- Create: `html/src/lib/server/db/testdb.ts` (test harness)
- Test: `html/src/lib/server/db/schema.test.ts`

**Interfaces:**
- Produces: `schema.ts` exporting Drizzle tables `users`, `identities`, `sessions`, `emailTokens`, `signingKeys`, `orgs`, `orgMembers`, `entitlements`; `db/index.ts` exporting `getDb()` (postgres-js from `DATABASE_URL`); `testdb.ts` exporting `async function newTestDb(): Promise<Db>` — fresh PGlite instance with all migrations applied (every later task's tests consume this).
- Column contracts later tasks rely on: `users(id uuid pk default, email text unique not null, passwordHash text not null, emailVerified boolean default false, createdAt, deletedAt)`; `sessions(id uuid pk, userId fk cascade, deviceName text not null, refreshHash text unique not null, familyId uuid not null, prevRefreshHash text, rotatedAt timestamptz, expiresAt timestamptz not null, createdAt, revokedAt)`; `emailTokens(id, userId fk cascade, purpose text check in ('verify','reset'), tokenHash text unique, expiresAt, usedAt)`; `signingKeys(kid text pk, privatePem text not null, publicJwk jsonb not null, createdAt, retiredAt)`; `identities(id, userId fk cascade, provider text, providerId text, unique(provider, providerId))`; `orgs/orgMembers/entitlements` minimal (id, name / orgId+userId+role / userId, key, value, grantedAt).

- [ ] **Step 1: Write the failing schema test**

```ts
// html/src/lib/server/db/schema.test.ts
import { describe, it, expect } from 'vitest';
import { newTestDb } from './testdb';
import { users } from './schema';

describe('schema', () => {
  it('applies migrations and enforces unique emails', async () => {
    const db = await newTestDb();
    await db.insert(users).values({ email: 'a@b.co', passwordHash: 'x' });
    await expect(db.insert(users).values({ email: 'a@b.co', passwordHash: 'y' })).rejects.toThrow(/unique|duplicate/i);
  });
  it('has the forward-compat tables present but empty', async () => {
    const db = await newTestDb();
    const rows = await db.execute(`select table_name from information_schema.tables where table_name in ('orgs','org_members','entitlements')`);
    expect(rows.length).toBe(3);
  });
});
```

- [ ] **Step 2: Run → FAIL** (testdb/schema missing).
- [ ] **Step 3: Implement `schema.ts`** with the column contracts above; `drizzle.config.ts` (dialect postgresql, schema path, out `./drizzle`); generate the migration: `npx drizzle-kit generate`.
- [ ] **Step 4: Implement `testdb.ts`**: new `PGlite()`, `drizzle(pglite)`, apply `./drizzle` migrations via drizzle's migrator, return the db handle. Implement `db/index.ts` `getDb()` for runtime (postgres-js, `DATABASE_URL` required).
- [ ] **Step 5: Run `npm test` → PASS (all files).**
- [ ] **Step 6: Commit** — `git commit -m "feat: schema + migrations + PGlite test harness"`

### Task 3: Email provider seam, signup, email verification

**Files:**
- Create: `html/src/lib/server/email/provider.ts`, `html/src/lib/server/email/dev.ts`, `html/src/lib/server/email/index.ts`
- Create: `html/src/lib/server/auth/password.ts`, `html/src/lib/server/auth/tokens.ts` (opaque token + hash helpers)
- Create: `html/src/routes/v1/signup/+server.ts`, `html/src/routes/v1/verify-email/+server.ts`
- Create: `html/src/lib/server/testutil.ts` (fake provider + request-event builder for route tests)
- Test: `html/src/routes/v1/signup/server.test.ts`, `html/src/routes/v1/verify-email/server.test.ts`

**Interfaces:**
- Produces: `interface EmailProvider { send(msg: {to: string; subject: string; text: string}): Promise<void> }`; `getEmailProvider(): EmailProvider` (env `EMAIL_PROVIDER`: `dev` default — logs to console/captures in tests; `postmark` added in Task 7); `hashPassword(pw: string): Promise<string>` / `verifyPassword(hash, pw)` (argon2id); `newOpaqueToken(): {token: string; hash: string}` (32B base64url + sha256 hex); `makeEvent({body, headers, locals})` test builder every route test uses.
- Consumes: Task 2 `newTestDb()`, tables.
- Route contracts: `POST /v1/signup {email, password}` → always `200 {"ok":true,"message":"check your email"}` (fresh: creates user + verify token + sends verify mail; existing: sends "you already have an account" mail — same response); password < 10 chars → `400 {error:{code:"weak_password"}}`. `POST /v1/verify-email {token}` → `200` once, flips `emailVerified`; second use or expired (24h TTL) → `400 {error:{code:"invalid_token"}}`.

- [ ] **Step 1: Write failing signup tests** — `test_signup_creates_unverified_user_and_sends_verify`, `test_signup_weak_password_400`, `test_signup_existing_email_indistinguishable` (same status+body, second mail is account-exists, no new user row).
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement provider seam, password/token helpers, signup route** (db via `event.locals.db` so tests inject PGlite; a server hook wires the real db at runtime).
- [ ] **Step 4: Run → signup tests PASS.**
- [ ] **Step 5: Write failing verify tests** — `test_verify_flips_flag`, `test_verify_token_single_use`, `test_verify_token_expired_400`.
- [ ] **Step 6: Run → FAIL; implement verify route (hash lookup, TTL check, mark used).**
- [ ] **Step 7: Full `npm test` → PASS. Commit** — `git commit -m "feat: signup + email verification behind pluggable email provider"`

### Task 4: Login, refresh rotation, logout

**Files:**
- Create: `html/src/lib/server/auth/sessions.ts`
- Create: `html/src/routes/v1/login/+server.ts`, `html/src/routes/v1/token/+server.ts`, `html/src/routes/v1/logout/+server.ts`, `html/src/routes/v1/logout-all/+server.ts`
- Test: `html/src/lib/server/auth/sessions.test.ts`, `html/src/routes/v1/login/server.test.ts`, `html/src/routes/v1/token/server.test.ts`

**Interfaces:**
- Produces (`sessions.ts`): `createSession(db, userId, deviceName): Promise<{refreshToken, session}>` (sets `expiresAt = now + 7d`, the session's one and only expiry); `rotateRefresh(db, refreshToken): Promise<{refreshToken, session} | 'reused' | 'invalid' | 'expired'>`; `revokeSession(db, refreshToken)`; `revokeAllSessions(db, userId)`. Constants: `SESSION_LIFETIME_DAYS = 7` (absolute, set at login), `ROTATION_GRACE_SECONDS = 30`.
- Rotation semantics (the part Task 5's `/v1/token` response and the Review Focus tests pin): each rotation stores the new hash in `refreshHash`, moves the old one to `prevRefreshHash` with `rotatedAt = now`, and **leaves `expiresAt` untouched** — nothing extends a session past 7 days from login; past `expiresAt`, rotation returns `'expired'` and the user re-authenticates. Presenting `prevRefreshHash` within 30s of `rotatedAt` returns the CURRENT token again (grace); presenting it later, or any hash from the family that is neither current nor in-grace, revokes every session row sharing `familyId` and returns `'reused'`.
- Route contracts: `POST /v1/login {email, password, device_name}` → `200 {refresh_token, access_token, token_type:"Bearer", expires_in}` (access token wired in Task 5; until then the field holds `""` and the login test asserts only refresh + session row — Task 5 tightens it); unverified email → `403 {error:{code:"email_unverified"}}`; bad credentials → `401 {error:{code:"invalid_credentials"}}` (same for unknown email). `POST /v1/token {refresh_token}` → same shape, rotated. `POST /v1/logout {refresh_token}` → 200 always. `POST /v1/logout-all` requires `Authorization: Bearer <access>` (guard lands in Task 5; here it takes `{refresh_token}` AND the task leaves a `TODO(T5)` the Task 5 diff removes — Task 5's test covers the bearer path).

- [ ] **Step 1: Write failing sessions unit tests** — `test_create_session_stores_hash_not_token`, `test_rotate_does_not_extend_expiry` (rotate at day 6; `expiresAt` still `login + 7d`), `test_session_expires_7_days_after_login` (rotation at day 7 + 1min returns `'expired'` even after heavy prior use), `test_refresh_reuse_revokes_family`, `test_refresh_retry_grace` (fake clock via injected `now()` param).
- [ ] **Step 2: Run → FAIL; implement `sessions.ts`.**
- [ ] **Step 3: Run → PASS.**
- [ ] **Step 4: Write failing route tests** — login happy/unverified/bad-creds-uniform (`test_login_unknown_email_same_401`), token rotate happy + reuse → `401 {error:{code:"session_revoked"}}`, logout idempotent.
- [ ] **Step 5: Run → FAIL; implement the four routes.**
- [ ] **Step 6: Full `npm test` → PASS. Commit** — `git commit -m "feat: login + rotating device-bound sessions with reuse detection"`

### Task 5: ES256 access tokens, JWKS, bearer guard, /v1/me

**Files:**
- Create: `html/src/lib/server/auth/jwt.ts`, `html/src/lib/server/auth/guard.ts`
- Create: `html/src/routes/.well-known/jwks.json/+server.ts`, `html/src/routes/v1/me/+server.ts`
- Modify: `html/src/routes/v1/login/+server.ts`, `html/src/routes/v1/token/+server.ts` (real `access_token`), `html/src/routes/v1/logout-all/+server.ts` (bearer guard, drop the T4 TODO)
- Test: `html/src/lib/server/auth/jwt.test.ts`, `html/src/routes/v1/me/server.test.ts`

**Interfaces:**
- Produces: `ensureSigningKey(db): Promise<void>` (generates ES256 keypair into `signingKeys` if none active); `signAccessToken(db, {userId, sessionId}): Promise<string>` (claims `sub`, `session`, `entitlements: []`, `iss: "https://id.tainer.dev"`, `aud: "tainer"`, `exp: now+900s`, header `kid`); `verifyAccessToken(db, jwt)` (against active + retired-unexpired keys); `requireAuth(event): Promise<{userId, sessionId}>` guard; `retireKey(db, kid)`.
- Route contracts: `GET /.well-known/jwks.json` → `{keys:[...]}` public JWKs for all non-expired keys. `GET /v1/me` (bearer) → `200 {id, email, email_verified, created_at}`; no/garbage token → `401`.

- [ ] **Step 1: Write failing jwt tests** — `test_access_token_claims_and_ttl`, `test_verify_via_published_jwks` (verify with `jose` against the JWKS route's output, not the private key), `test_retired_key_still_verifies`, `test_unknown_kid_rejected`.
- [ ] **Step 2: Run → FAIL; implement `jwt.ts` + JWKS route.**
- [ ] **Step 3: Run → PASS.**
- [ ] **Step 4: Write failing /v1/me + guard tests** (`test_me_returns_profile`, `test_me_401_without_bearer`, `test_logout_all_bearer`), wire real access tokens into login/token responses and tighten Task 4's login test to assert a verifiable JWT.
- [ ] **Step 5: Run full `npm test` → PASS. Commit** — `git commit -m "feat: ES256 access tokens, JWKS endpoint, bearer guard"`

### Task 6: Session management, password reset, flagged account deletion

**Files:**
- Create: `html/src/routes/v1/sessions/+server.ts` (GET), `html/src/routes/v1/sessions/[id]/+server.ts` (DELETE)
- Create: `html/src/routes/v1/password-reset/request/+server.ts`, `html/src/routes/v1/password-reset/confirm/+server.ts`
- Create: `html/src/routes/v1/account/+server.ts` (DELETE, feature-flagged)
- Test: one `server.test.ts` beside each new route file

**Interfaces:**
- Consumes: `requireAuth`, `sessions.ts`, email provider, `emailTokens`.
- Route contracts: `GET /v1/sessions` (bearer) → `{sessions:[{id, device_name, created_at, last_used_at: rotatedAt, current: bool}]}`. `DELETE /v1/sessions/:id` (bearer) → 200, revokes that session. `POST /v1/password-reset/request {email}` → always `200 {"ok":true}` (unknown email: no token, no error — `test_reset_request_unknown_email_200`); token TTL 1h. `POST /v1/password-reset/confirm {token, new_password}` → 200, rehashes password, marks token used, **revokes all sessions** (`test_reset_revokes_all_sessions`: a pre-reset refresh token subsequently fails). `DELETE /v1/account` (bearer): env `SELF_SERVE_DELETE` unset/not `"true"` → `404`; `"true"` → soft-delete (`deletedAt`), revoke all sessions, `test_deleted_account_refresh_fails`; login after deletion → 401 invalid_credentials.

- [ ] **Step 1: Write the failing tests for all five routes** (names above, plus `test_sessions_list_marks_current`, `test_revoke_other_session`, `test_account_delete_flag_off_404`).
- [ ] **Step 2: Run → FAIL; implement the routes.**
- [ ] **Step 3: Full `npm test` → PASS. Commit** — `git commit -m "feat: session management, password reset, flagged account deletion"`

### Task 7: Rate limiting, lockout, Postmark adapter

**Files:**
- Create: `html/src/lib/server/ratelimit.ts`, `html/src/lib/server/email/postmark.ts`
- Modify: `html/src/routes/v1/login/+server.ts`, `signup/+server.ts`, `password-reset/request/+server.ts` (apply limits), `html/src/lib/server/email/index.ts` (register adapter)
- Test: `html/src/lib/server/ratelimit.test.ts`, `html/src/lib/server/email/postmark.test.ts`

**Interfaces:**
- Produces: `class RateLimiter { constructor(opts: {max: number; windowSeconds: number}); allow(key: string, now?: Date): boolean }` (in-memory, per-process — documented as per-instance); policy pinned: auth routes 30/min per IP; failed logins 5 per account per 15 min → further attempts get `429 {error:{code:"too_many_attempts"}}` until the window passes. `PostmarkProvider implements EmailProvider` using `fetch` to `https://api.postmarkapp.com/email` with `X-Postmark-Server-Token` from env `POSTMARK_TOKEN`; constructor throws if the env is missing (selected only when `EMAIL_PROVIDER=postmark`).
- Tests inject `fetch` (constructor param, default `globalThis.fetch`) — `test_postmark_posts_expected_payload`, `test_postmark_non_200_throws`. **No real token anywhere**; live send is Task 8 checklist.

- [ ] **Step 1: Failing ratelimit tests** (`test_allows_under_limit`, `test_blocks_over_limit_within_window`, `test_window_slides`), fake clock.
- [ ] **Step 2: Run → FAIL; implement; PASS.**
- [ ] **Step 3: Failing lockout route test** (`test_login_lockout_after_5_failures` → 429, correct creds also 429 until window ends) and Postmark tests.
- [ ] **Step 4: Run → FAIL; wire limiter into routes, implement adapter; full `npm test` → PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat: rate limiting + lockout, Postmark email adapter"`

### Task 8: Pod bring-up checklist + README

**Files:**
- Create: `/Users/lenineto/dev/cyber5-io/tainer-id/docs/bringup.md`
- Modify: `README.md` (run/test instructions, env reference: `DATABASE_URL`, `EMAIL_PROVIDER`, `POSTMARK_TOKEN`, `SELF_SERVE_DELETE`, `JWT_ISSUER` override for dev)

**Interfaces:**
- Consumes: everything; produces the humans-run-this list. No product code in this task.

- [ ] **Step 1: Write `docs/bringup.md`** — numbered checklist, each item one command + expected output, all UNEXECUTED-until-run: (1) `tainer start` from the repo root; (2) migrations in the pod: `tainer exec tainer-id app -- sh -c "cd /var/www/html && set -a && . ./.env && npx drizzle-kit migrate"`; (3) `curl -sk https://id.tainer.me/healthz` → the Task 1 JSON; (4) signup → dev-provider log shows the verify mail (via `tainer exec` on the app container's output); (5) verify + login + `/v1/me` curl chain; (6) JWKS fetch; (7) DEFERRED items: Postmark live send (needs `POSTMARK_TOKEN`), `type: sveltekit` flip once tainer-images publishes, live deploy to cyber5 infra (its own milestone with Leni), OAuth providers (stretch, not built), `SELF_SERVE_DELETE` decision (PENDING Leni).
- [ ] **Step 2: Update README; run full `npm test` one last time → PASS; `npm run build` → exit 0.**
- [ ] **Step 3: Commit** — `git commit -m "docs: bring-up checklist + README"`

---

## Self-review notes (performed while writing)

- **Spec coverage:** every v1 API surface item from the spec maps to a task (signup/verify T3, login/refresh/logout T4, JWT+JWKS T5, session list + delete + reset + account-delete T6); pluggable email T3+T7; data-model skeleton incl. empty orgs/entitlements T2; dogfood pod layout T1/T8. OAuth deliberately has no task (stretch = fast-follow; `identities` table keeps it additive). "Pushed live on cyber5 infra" is explicitly OUT (Global Constraints + T8 deferred list).
- **Type consistency fixes made:** `logout-all` originally consumed a refresh token in T4 but a bearer in T5 — resolved by having T5's diff remove the T4 interim path and own the test; `last_used_at` in T6's session list pinned to `rotatedAt` so no new column is needed.
- **Step scan:** no step leaves more than one reasonable implementation; bodies appear only where the algorithm isn't determined by signature+test (rotation grace semantics are spelled out in T4's Interfaces because three tests and one later consumer depend on the exact behaviour).
- **Proportion:** 8 tasks for a ~120-line spec with 5 named Review Focus tests; code blocks are test skeletons and one YAML manifest only.
- **Flagged:** (1) RESOLVED by controller ruling (2026-10-31): the spec's original "7-day sliding" wording contradicted Leni's "re-login weekly"; sessions now have an absolute 7-day lifetime — rotation free within the window, nothing extends past `login + 7d`; offline work within the window needs no contact. Spec working tree amended to match. (2) `@node-rs/argon2` ships prebuilt binaries for darwin and linux-arm64, so host tests and pod runtime both work without a toolchain; if install fails in the pod, the fallback is pure-JS `argon2-browser` — builder's call, noted here so the implementer doesn't thrash.
