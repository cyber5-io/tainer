# Cert Auto-Renew Client (tainer side) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tainer refreshes the `*.tainer.me` cert from `https://certs.tainer.dev` once a day, in-place, without ever blocking a user command.

**Architecture:** A new `pkg/tainer/certsync` package orchestrates a daily gate (stamp file) → local expiry check → fingerprint compare → fetch+validate → in-place install → caddy reload. Pure fetch/validate logic lives in `pkg/tainer/tls`; the hook is one call in `runtime.Engine` after a successful engine connection.

**Tech Stack:** Go stdlib only (net/http, crypto/x509, crypto/ecdsa|rsa, encoding/json, httptest for tests).

**Spec:** `docs/superpowers/specs/2026-10-30-cert-auto-renew-design.md`

## Global Constraints

- Install by **in-place write only** — never rename over `config.CertFile()`/`KeyFile()` (per-file bind mounts sever on rename; verified 2026-10-30).
- Write order: key fully, then cert fully, then reload. Validate everything before the first byte touches the live files.
- Any failure: warn and return — never an error exit, never block the command. One attempt per day (stamp is written on every attempt).
- Base URL const `https://certs.tainer.dev/tainer.me/`, env override `TAINER_CERT_URL` (same pattern as `CYBERSTACKD`).
- HTTP timeout 5s; strict TLS verification (no InsecureSkipVerify anywhere).
- Key perms `0600`, cert perms `0644`.
- User-facing output never says "podman", "docker", or "engine".
- Version ships as 1.2.4 (Makefile `VERSION` is the single source of truth).

## Review Focus

- Empty or truncated PEM bodies from the endpoint (the live endpoint served 0-byte files on 2026-10-30) → reject before install. *Test in Task 1.*
- Key/cert mismatch from the server's non-atomic publish window (new cert, old key) → reject pair, retry tomorrow. *Test in Task 1.*
- Local cert file missing entirely (fresh dir, deleted by user) → fetch proceeds instead of panicking on CheckExpiry. *Test in Task 2.*
- Router container not running during refresh → files installed, reload skipped silently, no error. *Test in Task 3.*
- Malformed `fingerprint.json` (not JSON, missing fields, bad notAfter) → counts as a failed attempt, no install. *Test in Task 1.*

---

### Task 1: Fetch + validate in `pkg/tainer/tls`

**Files:**
- Create: `pkg/tainer/tls/refresh.go`
- Test: `pkg/tainer/tls/refresh_test.go`

**Interfaces:**
- Consumes: nothing new (stdlib; existing `renewalThresholdDays` stays).
- Produces:
  - `type Fingerprint struct { SHA256 string; NotAfter time.Time; Serial string; Generated time.Time }` (JSON keys `sha256`, `notAfter`, `serial`, `generated`; RFC3339 times)
  - `func FetchFingerprint(ctx context.Context, baseURL string) (Fingerprint, error)`
  - `func FileSHA256(path string) (string, error)` — lowercase hex; `("", nil)` is never returned: missing file is an error
  - `func FetchAndValidate(ctx context.Context, baseURL string, fp Fingerprint) (certPEM, keyPEM []byte, err error)` — GETs `fullchain.pem` + `privkey.pem` and validates
  - `func InstallInPlace(certPEM, keyPEM []byte, certPath, keyPath string) error` — key first `0600`, then cert `0644`, via `os.WriteFile` (in-place)

Validation inside `FetchAndValidate` (each failure returns a distinct error): cert PEM parses to ≥1 certificate; leaf DNS names include both `*.tainer.me` and `tainer.me`; `leaf.NotAfter` is after `time.Now()` and equals `fp.NotAfter` (compare `.Unix()`); `sha256(certPEM) == fp.SHA256`; key PEM parses (PKCS1/PKCS8/EC) and its public key matches the leaf's (compare via `x509.MarshalPKIXPublicKey` bytes); both bodies non-empty.

- [ ] **Step 1: Write failing tests** in `refresh_test.go`. Test helper `makeCertPair(t, dnsNames []string, notAfter time.Time) (certPEM, keyPEM []byte)` generates a self-signed ECDSA cert. Serve fixtures with `httptest.NewServer`. Tests:

```go
func TestFetchFingerprint(t *testing.T)            // happy path parses all four fields
func TestFetchFingerprintMalformed(t *testing.T)   // non-JSON body, missing sha256, bad notAfter → error
func TestFetchAndValidateHappyPath(t *testing.T)   // matching pair + fingerprint → both PEMs returned
func TestFetchAndValidateEmptyBody(t *testing.T)   // 0-byte fullchain.pem → error (the live-endpoint case)
func TestFetchAndValidateWrongDomains(t *testing.T)// cert for *.example.com → error
func TestFetchAndValidateExpired(t *testing.T)     // notAfter in the past → error
func TestFetchAndValidateKeyMismatch(t *testing.T) // key from a different pair → error
func TestFetchAndValidateHashMismatch(t *testing.T)// fingerprint sha != served bytes → error
func TestInstallInPlace(t *testing.T)              // writes in place: inode unchanged (os.Stat Sys() ino before/after), perms 0600/0644
```

- [ ] **Step 2: Run to verify they fail** — `go test ./pkg/tainer/tls/ -run 'TestFetch|TestInstall' -v` → FAIL (undefined symbols).
- [ ] **Step 3: Implement `refresh.go`** per the Produces block. HTTP client: 5s timeout, created in the package (`var httpClient = &http.Client{Timeout: 5 * time.Second}` — tests use httptest URLs through the same client).
- [ ] **Step 4: Run to verify pass** — same command → PASS; then `go vet ./pkg/tainer/tls/`.
- [ ] **Step 5: Commit** — `git add pkg/tainer/tls/ && git commit -m "feat(tls): fetch and validate renewed certs from certs.tainer.dev"`

### Task 2: Daily gate + orchestration in `pkg/tainer/certsync`

**Files:**
- Create: `pkg/tainer/certsync/certsync.go`
- Test: `pkg/tainer/certsync/certsync_test.go`

**Interfaces:**
- Consumes: Task 1's `tls` functions; `config.CertsDir()/CertFile()/KeyFile()`; `router.ReloadCaddy` (Task 3 — stub the reload via the `reload func(...)` parameter until then).
- Produces:
  - `const DefaultBaseURL = "https://certs.tainer.dev/tainer.me/"`
  - `func BaseURL() string` — `TAINER_CERT_URL` env or default
  - `type Outcome struct { CheckedAt time.Time; Refreshed bool; Err string }` — JSON, written to `filepath.Join(config.CertsDir(), ".last-check")`
  - `func ReadLastOutcome() (Outcome, bool)` — for doctor
  - `func DailyRefresh(ctx context.Context, reload func(context.Context) error)` — the whole flow; returns nothing (all failures are warnings)
  - `func shouldAttempt(now time.Time) bool` (unexported, tested) — false if `.last-check` exists and `CheckedAt` < 24h ago; else true only when cert is missing OR `tls.CheckExpiry` says needsRenewal

`DailyRefresh` flow (whole body wrapped in `defer func(){ recover() }()` — a bug here must never take down a user command): `shouldAttempt` gate (silent return) → `FetchFingerprint` → if `fp.SHA256 == FileSHA256(CertFile())` write Outcome, return → `FetchAndValidate` → `InstallInPlace` → `reload(ctx)` (errors from reload are logged as warning, not failure — files are installed) → write Outcome, print `tainer: refreshed tainer.me certificate (valid to <fp.NotAfter YYYY-MM-DD>)`. Every early failure writes Outcome with `Err` set and prints one `warning:` line to stderr.

- [ ] **Step 1: Write failing tests**:

```go
func TestShouldAttemptFreshStamp(t *testing.T)    // stamp 1h old → false (no stat of cert needed)
func TestShouldAttemptOutsideWindow(t *testing.T) // stamp stale, cert valid 200 more days → false, stamp NOT advanced
func TestShouldAttemptInsideWindow(t *testing.T)  // cert expiring in 10 days → true
func TestShouldAttemptMissingCert(t *testing.T)   // no cert file at all → true
func TestDailyRefreshNoChange(t *testing.T)       // server fingerprint == local sha → no install, outcome written, Refreshed=false
func TestDailyRefreshInstallsAndReloads(t *testing.T) // full happy path against httptest; reload func called once; cert bytes replaced in place; Refreshed=true
func TestDailyRefreshServerDown(t *testing.T)     // unreachable URL → no panic, outcome has Err, cert untouched
```

Tests point `config` at a temp dir — check how existing tests fake `BaseDir()` (`pkg/tainer/config`) and follow that pattern; if none exists, give the package a `certsDir` variable defaulting to `config.CertsDir` and override in tests.

- [ ] **Step 2: Run to verify fail** — `go test ./pkg/tainer/certsync/ -v` → FAIL.
- [ ] **Step 3: Implement `certsync.go`** per Produces.
- [ ] **Step 4: Run to verify pass** — `go test ./pkg/tainer/certsync/ -v` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(certsync): daily cert refresh with expiry-gated fetch"`

### Task 3: Wire into `runtime.Engine` + export caddy reload

**Files:**
- Modify: `pkg/tainer/router/update.go` (add exported wrapper)
- Modify: `pkg/tainer/runtime/runtime.go:78-96` (`Engine`)
- Test: `pkg/tainer/router/update_test.go` (or existing router test file)

**Interfaces:**
- Consumes: `certsync.DailyRefresh` (Task 2).
- Produces: `func ReloadCaddy(ctx context.Context, eng *engine.Client) error` in `router` — body: if the router web container is not running (inspect `WebContainerName`; treat inspect error as not-running), return nil; else `return reloadCaddy(ctx, eng)`.

- [ ] **Step 1: Write failing test** — `TestReloadCaddySkipsWhenRouterDown`: against a fake engine (follow `pkg/tainer/engine/client_test.go`'s existing fake/socket pattern) where inspect fails → returns nil, no exec attempted.
- [ ] **Step 2: Run to verify fail.**
- [ ] **Step 3: Implement `ReloadCaddy`; hook `Engine`**: in `runtime.Engine`, at both success returns (fast path `return cli, nil` and `waitForDaemon` result), call `certsync.DailyRefresh(ctx, func(c context.Context) error { return router.ReloadCaddy(c, cli) })` before returning the client. Guard the whole call with `defer func(){ recover() }()` inside `DailyRefresh` itself so a bug here can never take down a user command.
- [ ] **Step 4: Run** — `go test ./pkg/tainer/router/ ./pkg/tainer/runtime/ -v` → PASS; `go build ./...` clean.
- [ ] **Step 5: Commit** — `git commit -m "feat(runtime): refresh tainer.me cert on first engine command of the day"`

### Task 4: Doctor surfacing

**Files:**
- Modify: `pkg/tainer/doctor/doctor.go:354-380` (`checkTLS`)
- Test: doctor's existing test file (follow its Result-assertion pattern)

**Interfaces:**
- Consumes: `certsync.ReadLastOutcome()`, `certsync.BaseURL()`.

- [ ] **Step 1: Write failing tests**: expired cert + last outcome has `Err` → Fail detail contains the base URL and the last error; expired cert + no outcome yet → Hint says the daily refresh will fetch it on the next command (replace both stale hints: "reinstall tainer…" and "update tainer to get the renewed certificate").
- [ ] **Step 2: Run to verify fail.**
- [ ] **Step 3: Implement** — extend `checkTLS` to append last-check time/result to Detail when an outcome exists; also delete the stale comment above it ("re-downloading needs the release URL wiring" is no longer true).
- [ ] **Step 4: Run doctor package tests** → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(doctor): show cert refresh status in tls check"`

### Task 5: Release 1.2.4 (gated)

**Files:**
- Modify: `Makefile` (VERSION → 1.2.4)

- [ ] **Step 1: Verify the release gate (manual, blocking)** — all three must pass, else STOP and hand back:

```bash
curl -sS -m 10 https://certs.tainer.dev/tainer.me/fingerprint.json   # valid JSON, no TLS error
curl -sS -m 10 https://certs.tainer.dev/tainer.me/fullchain.pem | openssl x509 -noout -subject -enddate  # CN=*.tainer.me, notAfter in the future
echo | openssl s_client -connect certs.tainer.dev:443 2>/dev/null | openssl x509 -noout -issuer -enddate # issuer is Let's Encrypt/Enhance-issued, NOT SSL2BUY; not expired
```

As of 2026-10-30 this gate FAILS (site serves the expired SSL2BUY wildcard; published files are 0 bytes) — Leni's side.

- [ ] **Step 2: Live end-to-end check** — backdate the stamp (`rm ~/.config/tainer/certs/.last-check`), run `tainer status`, confirm the one-line refresh message, `tainer doctor` tls goes ok, and `curl -sS https://ops.tainer.me` (or any running pod) presents the new cert without `-k`.
- [ ] **Step 3: Bump** `VERSION` to `1.2.4` in the Makefile.
- [ ] **Step 4: Full suite** — `go test ./... && go vet ./...` → clean.
- [ ] **Step 5: Commit** — `git commit -m "build(pkg): bump version to 1.2.4 (auto-renewing tainer.me certificate)"`
- [ ] **Step 6: Update Tracker** — mark the cert task done via `PATCH /api/v1/tracker/tasks` (see CLAUDE.md "Task tracker").
