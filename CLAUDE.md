# Tainer (rebuild — `dev/v1`)

This branch is the fresh-build tainer 1.0.0, replacing the podman fork. The old tree lives at [`legacy/`](./legacy) for reference and is **read-only** — never modify code under `legacy/`. After 1.0.0 ships and merges to `main`, `legacy/` will be removed from `main` (preserved on a `legacy-archive` branch).

## Quick Context

- **Owner**: Cyber5 IO (cyber5.io) — UK company
- **License**: BSL 1.1 (the `legacy/` tree is Apache 2.0, inherited from podman)
- **Module path**: `github.com/cyber5-io/tainer`
- **Engine**: CyberStack (separate repo at `/Users/lenineto/dev/cyber5-io/cyber-stack`, private). Tainer talks to `cyberstackd`'s Docker-compatible Unix socket; users never see "VM" or "engine" in the UX.
- **Hypervisor**: Apple Virtualization.framework on macOS (via vfkit), wrapped by cyberstackd. Linux runs containers natively (no VM). Windows TBD.
- **Task tracker**: TAIN project in the Cyber5 Tracker (ops.cyber5.io; local dev instance at the `cyber5-ops` tainer project, `/Users/lenineto/dev/websites/cyber5.io/ops`). Jira is retired — tasks were migrated 2026-07-24 keeping their numbers (`tain-1`…`tain-97` carry the original Jira payload in `jira_raw`). Create/update tasks via `POST`/`PATCH /api/v1/tracker/tasks` with the `OPS_API_TOKEN` bearer from `ops/html/.env`; read via psql in the ops pod's db container (database `tainer`, tables `tracker_projects`/`tracker_tasks`).
- **Current version target**: 1.2.0 (shipping from `dev/v1`, with CyberStack 0.7.0). 1.0.0 and 1.1.0 shipped internally. **Smart pods landed in 1.2.0**: a pod size is one absolute budget (nano 512M/1cpu … small 1G/1 … xxl 16G/8) enforced on a shared pod-level cgroup (`tainer-<project>`); containers run as limitless leaves that burst within it. Spec: `docs/superpowers/specs/2026-07-23-smart-pods-design.md`.

## Architecture

```
Host:
  tainer CLI           (short-lived, per command — this repo)
  cyberstackd          (long-lived daemon, spawned by tainer on first use — cyber-stack repo)
    |
    +-- VM (managed by cyberstackd)
          cyberstack-agent  (long-lived, in-guest)
          crun              (short-lived, per container)
```

- `tainer` CLI talks to `cyberstackd` over a Unix socket exposing the Docker Engine API.
- `cyberstackd` lives at `/opt/tainer/bin/cyberstackd` (signed + entitled, bundled in the tainer .pkg).
- The boot disk and agent ship alongside (paths TBD during Step 11 of the rebuild plan).

## Reference plan

The rebuild is governed by [`docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`](./docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md). It was briefly retargeted to 0.9.0 mid-rebuild (Step 8 design) but shipped as **1.0.0** — the rebuild milestone itself. CyberStack steps 1-4 shipped as `v0.1.0`–`v0.4.0` (see status notes in the spec); steps 5-12 landed as tainer 1.0.0. The smart-pods capstone — originally deferred pending engine support — **shipped in tainer 1.2.0 / CyberStack 0.7.0** (see the 2026-07-23 smart-pods spec + plan).

Each milestone gets a plan in `docs/superpowers/plans/` written via the `superpowers:writing-plans` skill, executed via `superpowers:subagent-driven-development` or `superpowers:executing-plans`.

## Supported Project Types

wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, svelte, kompozi. react and svelte are the SPA types (`StaticBundle` in the typespec): caddy serves the built bundle (`TAINER_BUILD_DIR`, default `dist`, override via `runtime.build-dir`), and the app role is an sshd + node-tooling workbench outside the request path (TAIN-98).

## Branch Workflow

- All rebuild work lands on `dev/v1`.
- `main` continues to receive bug fixes for the legacy tree (merge from a `legacy-fixes` branch if needed) until 1.0.0 ships.
- Final `dev/v1` → `main` merge completes the rebuild; tag `v1.0.0` from main.
- After tag, remove `legacy/` from main and push the `legacy-archive` branch from the pre-merge `dev/v1` SHA.

## Coding Standards

- Follow Go community conventions (gofmt, golint, go vet clean).
- Use `Containerfile` (not Dockerfile) for project type images — but cyberstackd accepts either via the Docker API.
- User-facing CLI output never says "podman", "docker", or "engine". Tainer is the only product surface.
- No Co-Authored-By or AI mentions in commits.

## Related Repos

- **CyberStack** (engine): `/Users/lenineto/dev/cyber5-io/cyber-stack`
- **Kompozi** (CMS): `/Users/lenineto/dev/cyber5-io/kompozi`
- **tainer.dev** (website): `/Users/lenineto/dev/websites/tainer.dev/html/`
- **tainer-images** (project type images): `github.com/cyber5-io/tainer-images`

## Open handoffs

- `docs/handoff/2026-09-08-dev-pod-resilience.md` — three dev-pod bugs found while building cyber5-io-comms, with root causes and proposed fixes: the node-app dev server is OOM-killed under the pod budget and never restarted (tainer-images); `pod.size` changes are ignored for existing pods because `start` reuses containers and the agent re-asserts the budget stored at creation (tainer); `destroy` cannot remove a stopped container whose overlay mount stays busy (cyber-stack). Ticketed as `tain-99`, `tain-100`, `tain-101` (2026-10-30); the branch work itself is `tain-98`.
