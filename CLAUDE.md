# Tainer

Tainer is a container runtime and local dev CLI, forked from [Podman](https://github.com/containers/podman).

## Architecture

- **Language:** Go
- **Upstream:** `containers/podman` (synced via `origin-upstream` remote)
- **Branch model:** `upstream` (mirror) → `main` (features) → `brand` (rebased renames)
- **Jira project:** TAIN

## Branch Workflow

- Feature branches: `feature/TAIN-000-description` off `main`
- Bug branches: `bug/TAIN-000-description` off `main`
- Never branch off `brand` — it's auto-rebased
- Brand branch is force-pushed after rebase — always use `--force-with-lease`

## Brand Changes (on `brand` branch only)

- Binary name: `podman` → `tainer`
- Version output: "Tainer Engine"
- Docs/help text: rebranded (in progress)
- Internal Go packages: unchanged
- Config paths: unchanged (`~/.config/containers/`)
- See `.claude/conflict-zones.md` for full list

## Upstream Sync

- Remote: `origin-upstream` points to `containers/podman`
- Cadence: per stable Podman release (~monthly)
- Security patches: immediate
- Procedure: fetch origin-upstream → merge into upstream branch → merge into main → rebase brand

## Custom Code (not from upstream)

- `templates/` — pod templates for hosting (WordPress, PHP, Node.js)
- `.github/workflows/` — CI/CD pipelines
- `.claude/` — agent configuration

## Coding Standards

- Follow existing Podman conventions for Go code
- Use `Containerfile` (not `Dockerfile`) for pod templates
- Package manager: N/A (Go modules)
- No Co-Authored-By or AI mentions in commits
