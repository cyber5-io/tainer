# SvelteKit Project Type — Design

**Date:** 2026-10-30
**Status:** Draft, pending approval
**Repos:** tainer (type wiring), tainer-images (app image + caddy site config)

## Intent

Add `sveltekit` as a server-side project type, alongside nextjs/nuxtjs/nestjs.
Cyber5's latest apps are Svelte-based; tainer 1.3.0 added the `svelte` SPA
type (static bundle), but SvelteKit apps with server-side rendering or API
routes need a long-running node server that caddy proxies to — the nextjs
shape, not the react shape.

## Goals

- `tainer init --type=sveltekit` (alias `kit`) produces a working pod:
  web (caddy proxy) + app (SvelteKit dev server + sshd + node tooling) + db.
- **Full behavioral parity with nextjs as shipped today**: a fresh pod
  starts with an empty `html/` and the CLI's per-type "no app yet —
  scaffold one" hint (`npx sv create .` for sveltekit); once the user
  scaffolds, `start-node-app.sh`'s existing vite detection runs the dev
  server on the pod port (it already appends `--port $PORT --host` to
  vite dev scripts — no port pinning needed).
- Same lifecycle guarantees as nextjs: start/stop/restart/exec/npm wrappers,
  pod budgets, TLS via the wildcard cert.
- Close the **whole** TAIN-98 images leftover, not just the Caddyfile
  (builder-check findings, 2026-10-30): (a) missing `svelte.Caddyfile`;
  (b) **no svelte-app image exists in the build matrix at all** — add it
  (aliasing the react-app/node-app Containerfile as the other node types
  do); (c) **react-app is in the `framework` build job but missing from
  the `framework-manifest` job**, so the multi-arch
  `tainer-react-app:node-20` tag would never be created — fix it. All in
  the same branch: it lands and publishes as one unit.

## Non-goals

- No SvelteKit adapter selection logic. The dev experience runs `vite dev`;
  production builds are the user's concern (adapter-node is what we assume
  and document, but tainer does not read `svelte.config.js`).
- **No baked-scaffold mechanism.** The nextjs Containerfile's
  scaffold.tar + post-deploy.sh is legacy-image dead code — nothing in
  the 1.x family extracts it (verified 2026-10-30). Reviving a baked
  starter for node types is a product design of its own (it ties into
  the planned UI's "drop a folder → it runs" flow) and is explicitly
  deferred. sveltekit ships hint-based like nextjs/nuxtjs/nestjs.
- No changes to the svelte (SPA) type's tainer-side wiring — it shipped in
  1.3.0 and is correct.
- No image publishing. CI dispatch on tainer-images stays a manual,
  user-driven step (ghcr visibility flips are manual too).

## Anti-goals

- Do not introduce a second node version default: `sveltekit` uses Node
  "20" like every other node type, and its app image publishes the same
  node-20/22/24 tag set nextjs-app does (users can init `--node 22/24`;
  a single-tag image would break those inits on a missing tag).
- Do not make the caddy config SvelteKit-specific beyond the proxy: no
  path assumptions tied to adapter output.

## Design

### tainer repo

One TypeSpec row plus the mechanical companions, exactly as nextjs:

- `manifest.TypeSvelteKit ProjectType = "sveltekit"`, alias `"kit"`.
- TypeSpec entry: FamilyNode, DefaultRuntime{Node "20", DatabaseMariaDB},
  HasAppRole true, StaticBundle **false**, ExecUser "1000:1000",
  ExecWorkdir "/var/www/html", Tools nodeTools.
- Wizard: `{manifest.TypeSvelteKit, "SvelteKit"}` listed after Svelte;
  dbOptions same as nextjs (none/postgres/mariadb? — match whatever
  nextjs offers today; the svelte SPA entry allows "none").
- ImageRef needs no change: role app resolves to
  `ghcr.io/cyber5-io/tainer-sveltekit-app:node-20` via the generic
  `tainer-<type>-app:node-<ver>` rule; web stays `tainer-caddy-web:2-alpine`.
- buildEnv: no TAINER_BUILD_DIR (StaticBundle false). No code change —
  covered by a test.
- Tests: typespec row test (TestSvelteKitSpec), TestStaticBundle gains
  `sveltekit: false`, TestToolTypes npm count 7→8, types_test rows
  (RolesForType/RolesForPod/DefaultExecRole/ImageRef), CanonicalType
  "kit" alias, wizard list test if one exists.
- The per-type "no app in html/ yet" hint (noAppHint in cmd/tainer)
  gains a sveltekit entry (`npx sv create .` family — exact copy is the
  builder's).
- Docs: CLAUDE.md supported-types line gains sveltekit.

### tainer-images repo (branch from the TAIN-98-react-app-role tip)

- **No new Containerfile.** In the 1.x image family, node-type app
  images are aliases of the generic `node-app/Containerfile` ("framework
  scaffolding is the project's own business"). sveltekit-app follows:
  matrix entries in `.github/workflows/build-images.yml` publishing
  `tainer-sveltekit-app` at node-20/22/24 — in **both** the `framework`
  build job and the `framework-manifest` multi-arch job.
- Likewise `tainer-svelte-app` (TAIN-98 leftover (b)): matrix entries at
  node-20/22/24 aliasing the `react-app/Containerfile` (the SPA app
  image: sshd + node tooling, no server), in both jobs.
- Fix TAIN-98 leftover (c): add `react-app` to the `framework-manifest`
  job it is currently missing from.
- `caddy-web/sites/sveltekit.Caddyfile`: copy of nextjs.Caddyfile with the
  comment adjusted (same reverse_proxy 127.0.0.1:3000, Host rewritten to
  localhost — vite's host protection rejects foreign Host headers, same
  reasoning already documented in nextjs.Caddyfile; start-node-app.sh
  already forces vite onto the pod port with `--port $PORT --host`).
- `caddy-web/sites/svelte.Caddyfile` (TAIN-98 leftover (a)): copy of
  react.Caddyfile with comments adjusted — serves
  `/var/www/html/{$TAINER_BUILD_DIR:dist}`, SPA fallback to index.html.

## Constraints

- tainer repo: branch from `main`, named after the Tracker task; no
  Co-Authored-By lines; gofmt/vet clean; all tests green.
- tainer-images: do not rename or restructure existing images; the
  TAIN-98 branch is unmerged — new work continues on a branch cut from
  its tip so it lands as one publishable unit.
- A Tracker task must exist for this work before the branch is cut
  (create via POST /api/v1/tracker/tasks).

## Acceptance

- `go test ./...` green in tainer.
- `tainer init --type=sveltekit` writes a manifest that validates; pod
  roles resolve to web+app+db; ImageRef returns the sveltekit-app ref.
- tainer-images: the workflow matrix lists sveltekit-app, svelte-app and
  react-app in both the build and manifest jobs (yaml-inspected; actual
  publishing stays the manual CI dispatch); Caddyfiles are syntactically
  valid (`caddy validate` in a container if available, else inspection).
- Live end-to-end (`tainer init` → `start` → page loads) is explicitly
  deferred until the images are published — same gate react/svelte are
  already behind. Noted in the Tracker task.

## Left to the builder

- Exact noAppHint copy for sveltekit (today's SvelteKit scaffolder is
  `npx sv create`; match the house hint style).
- Exact workflow matrix syntax (follow the nextjs-app entries).
- Whether the wizard needs a test or visual tweak beyond the list entry.
