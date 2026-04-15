# Tainer

Tainer is a container-based local development tool and CLI, forked from Podman. It abstracts container administration completely — users run `tainer init`, `tainer start`, and get a full dev environment with TLS, SSH, database, and hot reload.

## Quick Context

- **Owner**: Cyber5 IO (cyber5.io) — UK company
- **Ecosystem**: Tainer (local dev tool) + Kompozi (CMS) + Blenzi (hosting platform)
- **Jira project**: TAIN
- **Current version**: Check `version/rawversion/version.go` for TainerVersion
- **Website**: tainer.dev
- **GitHub**: github.com/cyber5-io/tainer

## Architecture

- **Language**: Go
- **Upstream**: containers/podman (synced via `origin-upstream` remote)
- **VM provider**: vfkit (Apple Virtualization.framework) — NOT krunkit
- **VM filesystem**: virtio-fs for host ↔ VM file sharing
- **Container images**: ghcr.io/cyber5-io/tainer-* (WordPress, PHP, Node.js, Next.js, Nuxt.js, NestJS, React, Kompozi)
- **Image repo**: github.com/cyber5-io/tainer-images

## Supported Project Types

wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, kompozi

Each type has: container image, scaffold (baked into image), entrypoint script, Caddy config.

## Key Directories

| Path | What |
|------|------|
| `cmd/podman/tainer/` | CLI commands (Go) — init, start, stop, list, db, status, etc. |
| `pkg/tainer/` | Core packages — cli, config, machine, manifest, project, registry, router, ssh, tui, update |
| `pkg/tainer/tui/` | Bubbletea v2 TUI components — wizard, list, picker, progress, status, home |
| `pkg/tainer/project/` | Project lifecycle — start, stop, build, scaffold |
| `pkg/tainer/manifest/` | tainer.yaml parsing and validation |
| `pkg/tainer/machine/` | VM management (vfkit) |
| `contrib/pkginstaller/` | macOS pkg installer build scripts |
| `contrib/release.sh` | Full release automation (build, sign, notarize, publish) |
| `version/rawversion/` | Version constants |

## CLI Commands

| Command | Description |
|---------|-------------|
| `tainer init` | Create new project (TUI wizard) |
| `tainer start [project]` | Start project (auto-init on fresh clone) |
| `tainer stop [project]` | Stop project |
| `tainer destroy [project]` | Tear down project |
| `tainer list` | Interactive project list (async data loading) |
| `tainer list --raw` | Plain text project list |
| `tainer status [project]` | Project status dashboard |
| `tainer exec [target] [cmd]` | Exec into container |
| `tainer db export [file]` | Export database dump |
| `tainer db import [file]` | Import database dump (TUI picker for multiple files) |
| `tainer update core` | Self-update binary from GitHub releases |
| `tainer reset` | Emergency VM force-restart |

## Build & Release

- **Build**: `make tainer-remote` (builds to `bin/darwin/tainer`)
- **Release**: `./contrib/release.sh` — builds arm64 + amd64 binaries and pkg installers, signs, notarizes, creates GitHub release with all 4 assets
- **Local test**: `sudo cp bin/darwin/tainer /opt/tainer/bin/tainer`
- **Version bump**: edit `version/rawversion/version.go` TainerVersion constant

## Branch Workflow

- Feature branches: `TAIN-000-description` off `main`
- Never commit directly to main
- Merge feature branch → main → push → release

## TUI Framework

All TUIs use bubbletea v2 + lipgloss v2 + bubbles v2:
- Alt screen via `v.AltScreen = true` in View()
- `tui.FullScreen()` for centred layout
- `tui.Colors()` for theme-aware colours (light/dark detection)
- `tui.NewProgram()` wrapper
- `bubbles/table` for row selection (list, picker)

## Coding Standards

- Follow existing Go conventions in the codebase
- Use `Containerfile` (not Dockerfile) for images
- Never say "podman" in user-facing output — we are tainer
- No Co-Authored-By or AI mentions in commits
- Bump TainerVersion on every feature/fix
- Every version bump needs a GitHub release with installers

## Related Repos

- **Kompozi** (CMS): `/Users/lenineto/dev/cyber5-io/kompozi`
- **tainer.dev** (website): `/Users/lenineto/dev/websites/tainer.dev/html/`
- **tainer-images**: `github.com/cyber5-io/tainer-images`
