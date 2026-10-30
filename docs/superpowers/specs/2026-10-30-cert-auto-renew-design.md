# Automatic `*.tainer.me` certificate renewal and distribution — design

**Date**: 2026-10-30 · **Decided with**: Leni · **Ships as**: tainer **1.2.4** (point release from `dev/v1`, independent of the TAIN-98 branch — Claude's call)

## Intent

The CA/Browser Forum has capped certificate lifespans (max ~200 days now, 100 in 2026, 47 by 2029), so bundling a long-lived `*.tainer.me` cert in the tainer .pkg is dead as a strategy. Field confirmation: on 2026-10-30 the cert serving a local pod on this machine was already expired (`curl: (60) SSL certificate problem: certificate has expired` against `ops.tainer.me`). Users must get a fresh cert automatically, with zero action and no visible machinery.

The architecture was decided 2026-07-15 (Leni) and refined 2026-10-30:

> Cert generation/renewal is a separate hosted service on Cyber5 infra — **never coded into tainer, never running on user machines**. Tainer is a dumb consumer that only checks-and-fetches. All ACME/DNS secrets stay server-side.

## Goals

- Every tainer install always has a valid `*.tainer.me` + `tainer.me` cert in `config.CertsDir()` (`~/.config/tainer/certs/`), picked up before the 30-day renewal window closes.
- Zero user action, no user-side daemon, no user-side cron: the check rides the first tainer engine command of each day.
- The one secret (bunny.net API key, scoped to the tainer.me zone) exists only on the Cyber5 server.
- Cyber5 can later publish additional certs without touching tainer (per-cert directory on the endpoint).

## Non-goals

- No ACME client in tainer. No per-user or per-machine certs. No cert pinning in tainer (the fetch endpoint is authenticated by ordinary WebPKI TLS).
- Migrating the renewer off Enhance when hosting infra moves to tainer/blenzi is out of scope (the design makes it trivial: one binary, one script, one cron line, one state dir).

## Anti-goals (failure = shipping any of these)

- A fetch failure or an unreachable endpoint that **blocks or delays a user's command** beyond a short timeout. Cert refresh is best-effort background hygiene.
- The publish endpoint's own TLS depending on the wildcard cert it serves. If renewal breaks, clients must still be able to fetch the fix. (This coupling is exactly what bit us today.)
- The bunny API key, or any ACME state, appearing in the tainer repo, binary, or a user machine.
- The publish endpoint living under **any zone tainer's resolver captures**. tainer installs `/etc/resolver/tainer.me`, which sends the whole tainer.me zone to the local responder (127.0.0.1) on every user machine — an endpoint under tainer.me resolves to the user's own router and never reaches the real server (discovered live 2026-10-30; the original `certs.tainer.me` choice was moved to `get.tainer.dev` for exactly this reason).

## Why the key being public is fine

`fullchain.pem` **and `privkey.pem` are publicly downloadable by design**. `*.tainer.me` resolves to 127.0.0.1; the cert only ever authenticates a user's own localhost traffic to their own pods. An attacker with the key gains nothing they couldn't get by controlling the victim's DNS anyway — this was weighed and accepted on 2026-07-15. Consequently there is no auth on the endpoint and no obscurity in the hostname (the URL ships in tainer's public source regardless).

## Architecture

```
Cyber5 server (Enhance-hosted website, get.tainer.dev, own independent TLS cert)
  lego (static binary, no Go toolchain) + daily cron as the website user
    --dns bunny (BUNNY_API_KEY env) → renews *.tainer.me at ~30 days left
    on renewal: copy PEMs into web root, regenerate fingerprint.json
        |
        | plain HTTPS GET
        v
tainer CLI (user machine, first engine command of the day)
  local CheckExpiry → inside renewal window? → GET fingerprint.json
  → differs from installed? → fetch PEMs → validate → atomic replace → reloadCaddy
```

### Server half (Leni deploys; this is the reference setup)

A regular website `get.tainer.dev` on an existing Enhance server (**nginx**; no Caddy anywhere on this half). A specific A record overrides the wildcard's 127.0.0.1. The site's own TLS is **managed by Enhance itself** (its built-in LE HTTP-01 flow) — independent of, and never, the wildcard it publishes, so a wildcard-renewal failure can't lock clients out of the fix. The renewer needs nothing from nginx beyond static file serving from the docroot.

**No root, ever (hard constraint — the Enhance account has none).** Everything runs as the site user: the lego binary and its ACME state live in the user's home (`--path $HOME/.lego`), and publishing is a plain copy of the three files into the site's docroot. Nothing is installed into system cert stores, nginx config, or anywhere outside the user's home + docroot. There is no Go app on the server — the renewer is the static lego binary plus the cron script; the only Go code in this feature is the fetcher inside tainer on user machines.

Published layout (web root):

```
/tainer.me/fingerprint.json
/tainer.me/fullchain.pem
/tainer.me/privkey.pem
```

`fingerprint.json` schema (all fields required):

```json
{
  "sha256": "<lowercase hex sha256 of fullchain.pem bytes>",
  "notAfter": "<RFC3339 UTC, e.g. 2027-01-28T12:00:00Z>",
  "serial": "<cert serial, decimal or hex string>",
  "generated": "<RFC3339 UTC timestamp of this publish>"
}
```

Renewal script, run daily by Enhance per-site cron as the website user (`~/certs-renew.sh`; `$HOME/lego` is the release binary from GitHub, linux/amd64):

```sh
#!/bin/sh
set -eu
export BUNNY_API_KEY="<zone-scoped key, pasted at deploy time — never leaves this file>"
LEGO_PATH="$HOME/.lego"
WEBROOT="$HOME/public_html/tainer.me"   # adjust to Enhance's docroot layout
EMAIL="leni@cyber5.net"

# lego v5 (flags live on the command; `renew` was folded into `run`):
# issues on first use, renews after, and only touches the network
# inside the renewal window
"$HOME/lego" run --accept-tos --email "$EMAIL" --dns bunny \
  --domains '*.tainer.me' --domains 'tainer.me' \
  --path "$LEGO_PATH" --renew-days 30

CRT="$LEGO_PATH/certificates/_.tainer.me.crt"
KEY="$LEGO_PATH/certificates/_.tainer.me.key"

# publish only when content changed
NEW_SHA=$(sha256sum "$CRT" | cut -d' ' -f1)
OLD_SHA=$(sed -n 's/.*"sha256": *"\([0-9a-f]*\)".*/\1/p' "$WEBROOT/fingerprint.json" 2>/dev/null || true)
# NOTE: an && list here would trip set -e when the hashes differ
if [ "$NEW_SHA" = "$OLD_SHA" ]; then exit 0; fi

NOT_AFTER=$(openssl x509 -in "$CRT" -noout -enddate | cut -d= -f2 | xargs -I{} date -u -d "{}" +%Y-%m-%dT%H:%M:%SZ)
SERIAL=$(openssl x509 -in "$CRT" -noout -serial | cut -d= -f2)
mkdir -p "$WEBROOT"
cp "$CRT" "$WEBROOT/fullchain.pem.tmp" && mv "$WEBROOT/fullchain.pem.tmp" "$WEBROOT/fullchain.pem"
cp "$KEY" "$WEBROOT/privkey.pem.tmp"   && mv "$WEBROOT/privkey.pem.tmp"   "$WEBROOT/privkey.pem"
printf '{\n  "sha256": "%s",\n  "notAfter": "%s",\n  "serial": "%s",\n  "generated": "%s"\n}\n' \
  "$NEW_SHA" "$NOT_AFTER" "$SERIAL" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  > "$WEBROOT/fingerprint.json.tmp" && mv "$WEBROOT/fingerprint.json.tmp" "$WEBROOT/fingerprint.json"
```

Deploy-time checks (30 seconds each): Enhance per-site cron actually runs as the site user with outbound network; nginx serves `.pem`/`.json` from the docroot verbatim (drop a dummy file and curl it — watch for a PHP-site template routing unknown paths through an app handler, and for any rule blocking `.pem`); `date -u -d` works on the box (BusyBox date differs — builder may substitute `openssl x509 -enddate` parsing in Go-free POSIX if needed).

### Client half (tainer — the code this repo ships)

**Trigger**: the first engine-touching command of the day. Gate with a last-checked timestamp file (`CertsDir()/.last-check`, mtime-based); skip entirely when it is < 24h old. No daemon, no cron, no background process.

**Flow** (all best-effort; any failure → warn once, continue the user's command):

1. `tls.CertExists` / `tls.CheckExpiry` on the installed cert. If the cert exists and is outside the 30-day renewal window (`renewalThresholdDays`, `pkg/tainer/tls/cert.go:13`), touch `.last-check` and stop — **no network at all** on the happy path, ~364 days a year.
2. Otherwise GET `https://get.tainer.dev/tainer.me/fingerprint.json` (short timeout, ~5s). If its `sha256` equals the sha256 of the installed `fullchain.pem`, the server has nothing newer (renewal pending server-side): touch `.last-check`, stop.
3. Fetch `fullchain.pem` + `privkey.pem` to temp files in `CertsDir()`. Validate before install: PEM parses; leaf covers `*.tainer.me` and `tainer.me`; `NotAfter` is in the future and matches `fingerprint.json`; the private key pairs with the leaf's public key; downloaded fullchain's sha256 matches `fingerprint.json`.
4. **Install by in-place write, never rename** (key `0600`, cert `0644`). Verified empirically on the live stack 2026-10-30: the router bind-mounts the cert/key as individual files (`router/ensure.go:263-264`), and renaming a new file over the source **permanently severs the mount inside the container** — the file reads "No such file or directory" in the container even after restoring the original bytes, and only a container stop/start heals it. In-place writes (`os.WriteFile` to the existing file) propagate into the container instantly and byte-exact, matching the Caddyfile-update precedent (`router/update.go`). The half-written-file window is harmless because caddy only reads cert files at reload: write the key fully, then the cert fully, **then** call `router.reloadCaddy` — and only if the router container is running. Validate the downloaded PEMs (step 3) strictly before touching the live files, since in-place writing forgoes atomicity. Touch `.last-check`.
5. One user-visible line on success (`tainer: refreshed tainer.me certificate (valid to <date>)`); one warning per day on failure, never an error exit.

**Doctor**: extend the existing cert check (`pkg/tainer/doctor/doctor.go:358-364`) to also show the last check time/result and, when the cert is inside the renewal window and fetch is failing, say so explicitly with the URL.

**Existing code to build on**: `tls.CertExists`, `tls.CheckExpiry`, `tls.DownloadCert` (`pkg/tainer/tls/cert.go` — DownloadCert's comment says "GitHub Releases", a relic; repoint and add the validation step, which it currently lacks), `config.CertsDir()` (`pkg/tainer/config/paths.go:19`), `router.reloadCaddy` (`pkg/tainer/router/update.go:52`).

**Base URL**: hardcoded const `https://get.tainer.dev/tainer.me/`, overridable via `TAINER_CERT_URL` env for testing (same pattern as the `CYBERSTACKD` dev override).

## Decisions and their owners

| Decision | Owner |
|---|---|
| Hosted renewer, tainer as dumb consumer, secrets server-side only | Leni (2026-07-15) |
| DNS-01 via lego + bunny (wildcards require DNS-01; certbot has no bunny plugin) | Leni (2026-07-15) |
| Public key+cert on an unauthenticated endpoint | Leni (2026-07-15) |
| Endpoint is `get.tainer.dev`, a regular website on an Enhance server, with its own independent cert | Leni (2026-10-30) |
| Daily check on first engine command; expiry check gates all network | Leni (2026-07-15, reaffirmed 2026-10-30) |
| lego **CLI** (static binary) rather than lego-as-library custom app | Claude (Enhance constraint: no Go toolchain, no root needed) |
| Per-cert directory layout (`/tainer.me/…`) for future certs | Claude |
| Failure behavior: never block, warn once/day, doctor tells the full story | Claude (offered 2026-10-30, not objected) |
| Ships as 1.2.4 point release | Claude (field certs already expired; veto open) |

## Left to the builder

- Exact hook point for "first engine command of the day" (wherever the CLI ensures the engine/daemon before a command runs; keep it out of pure-local commands like `tainer config`).
- Validation helper shape and tests; whether `DownloadCert` is extended or superseded.
- Whether `.last-check` also records the last outcome for doctor (a one-line JSON is fine).
- Pkg interplay: the .pkg continues to bundle whatever cert is current at build time (`TAINER_CERT_DIR` in the Makefile); first daily check upgrades it if stale. No installer changes expected.
- TAIN Tracker task for this work (create via the ops tracker API at plan time — the Tracker, not Jira; see "Task tracker" in CLAUDE.md for the endpoint, auth, and read path).
- Release sequencing: `get.tainer.dev` must be live and serving a fresh cert before 1.2.4 is tagged — the .pkg build itself bundles the current cert via `TAINER_CERT_DIR`, and field certs are already expired. The plan gets an explicit verification gate on the endpoint.
