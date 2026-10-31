# Tainer Accounts — design

Status: DRAFT for Leni's review (2026-10-31). Not yet approved.
Companion spec: `2026-10-31-host-bridge-design.md` (the local side that consumes these sessions).

## Intent

The remote auth service behind the tainer UI programme. The control webapp
requires login from day 0; the CLI requires login only for actions that touch
the live world (binding a dev pod to a hosted site). Blenzi plans, teams
licenses, and cross-machine features will all hang off this identity later —
but this spec builds **accounts + sessions only**.

Why now: the control webapp (dashboard) cannot ship without it, and the
login-required line is the business line — every UI user enters the blenzi
funnel with an identity attached, while the free CLI + menu app stay
friction-free with no login at all.

## Goals

- Free account signup + login, usable from the local control pod and the CLI.
- Long-lived sessions: users re-authenticate weekly, never per-use; offline
  work within the session window is unaffected.
- Revocable sessions (lost laptop story) with bounded revocation lag.
- A data model that teams/licenses/blenzi plans can extend without migration
  pain — modeled, not built.
- The service itself is developed as a tainer pod and pushed live on cyber5
  infra: dogfooding end to end.

## Non-goals (this phase)

- Billing, plans, payments (arrives with blenzi integration).
- Teams, orgs, ACLs, machine registry (arrives with the teams feature).
- SSO/SAML, 2FA (post-MVP; data model must not preclude 2FA).
- Any per-user cloud storage of pod/project data.

## Anti-goals

- Login required to use the CLI or menu app locally. Never. That is the
  OrbStack mistake we are explicitly selling against.
- Local features degrading when the service is unreachable mid-session: a
  valid session must keep working offline until it expires.
- Tracking beyond what the account needs. UK company, GDPR: data minimalism,
  deletable accounts.

## Constraints

- **Domain: `id.tainer.dev` (confirmed)** — never under `tainer.me` for the
  live service, since the local resolver hijacks `*.tainer.me` to the
  machine (the `get.tainer.dev` precedent). The local dev instance runs as
  `id.tainer.me` deliberately: the same hijack makes it resolve locally,
  which is exactly what developing the service as a tainer pod wants.
- User-facing copy never says podman/docker/engine/VM.
- Works within the 8GB-floor story: nothing here runs on user machines
  except token storage (host side, see companion spec).
- No AI attribution anywhere in the codebase or commits.

## Decided hows

**Identity (v1): email + password, with email verification and reset.**
Email + password is the committed floor. OAuth (GitHub/Google one-click,
which vibe coders expect) is an **MVP stretch goal**: in scope if it does
not jeopardise the timeline, otherwise the first fast-follow (Leni,
2026-10-31). The `users` table carries an `identities` relation from day
one so providers are additive either way.

**Token shape: OAuth2-style refresh + access pair.**
- Refresh token: opaque, server-side record, **absolute 7-day session
  lifetime** (weekly re-login — Leni, 2026-10-31: "users are only forced to
  re-login once a week"): rotation works freely within the window but never
  extends it; 7 days after login the user re-authenticates. Bound to a named
  device ("Leni's Studio"), revocable individually.
- Access token: JWT, ES256, 15-minute TTL, carrying `sub`, `session`, and a
  forward-compatible `entitlements` claim (empty today). JWKS published at
  a well-known URL so the local host bridge can verify offline.
- Revocation lag is therefore ≤ access TTL; "log out everywhere" revokes
  all refresh tokens.

**Where sessions live on the user's machine: the HOST, not the pod.**
The local session (refresh token) is stored host-side by the host bridge
(macOS Keychain; Linux secret service or 0600 file) — one session per
machine, shared by the CLI and the control pod. The control pod never holds
long-lived secrets; it obtains short-lived access tokens from the bridge.
Rationale in the companion spec; consequence here: the service sees one
device per machine, not per surface.

**API surface (v1):** signup, verify-email, login (password → refresh),
token refresh, logout (this device / everywhere), session list, account
delete. JSON over HTTPS, versioned under `/v1/`.

**Data model skeleton (built now, used later):** `users`, `identities`,
`sessions`; empty-but-present `orgs`, `org_members`, `entitlements` so
teams/licenses/blenzi plans extend rather than migrate.

**Dogfooding path:** developed locally as a tainer pod (builder's choice of
node-family type; SvelteKit keeps the stack uniform with the control
webapp), Postgres in the pod's `db/`, then pushed live on cyber5 infra the
same way ops.cyber5.io runs today. This deploy is also our first real
rehearsal of the "develop local, push live" story we intend to sell.

**Email delivery:** transactional only (verify, reset), behind a
**pluggable provider interface — a hard requirement, not a nicety**: the
service depends on the interface, concrete providers are adapters.
**Postmark is the first adapter, Telnyx the second** (Leni intends to
switch, 2026-10-31), so the seam gets exercised by real use, not just
design intent.

## Left to the builder

- Framework/stack of the service within the node family; schema details;
  password hashing (argon2id expected); rate limiting and lockout policy.
- Exact JWT claims beyond the ones named; JWKS rotation cadence.
- Admin tooling (can start as psql, like the ops tracker).

## Questions for Leni

1. **Self-serve account deletion — PENDING Leni.** Drafted in as a v1 API
   endpoint (GDPR-friendly). Leni asked for clarification before deciding;
   the drafted behaviour stands until he rules. For clarity, the choice is:
   (a) as drafted — a "delete my account" action in the dashboard that
   erases the user record and revokes all sessions, vs (b) deletion only by
   contacting support, with the endpoint built but not exposed in the UI.
