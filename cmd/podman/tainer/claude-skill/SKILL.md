---
name: tainer
description: Use when working in a tainer project (tainer.yaml present in cwd or parent dirs), when the user mentions tainer, or when migrating projects from Docker/docker-compose to tainer. Tainer is a local-first container dev tool that handles full project lifecycle (init, start, stop, build, deploy) for WordPress, Node.js, Next.js, Nuxt.js, Nest.js, Kompozi, and PHP projects. Never use raw docker, docker-compose, or podman commands in a tainer project — use tainer commands instead.
---

# Tainer

Tainer is a container-based local development tool. It bundles a VM (on macOS/Windows) and a full container engine, with project-aware commands that handle the entire lifecycle: init, start, stop, restart, build, exec.

## When to use this skill

- A `tainer.yaml` file exists in the current directory or any parent directory
- The user mentions tainer, podman, container, or docker in a context that involves their project
- The user asks how to run, deploy, or manage their app locally
- The user wants to migrate from Docker/docker-compose to tainer

## Core principles

1. **Always use tainer commands** — never `docker`, `docker-compose`, `podman`, or run yarn/npm/composer locally on the host
2. **App code lives in `html/`** — at the project root, alongside `tainer.yaml`
3. **Persistent data in `data/`** — uploads, user content, runtime files
4. **Database in `db/`** — Postgres or MariaDB data files (empty on first start)
5. **Never edit `db/` contents directly** — use `tainer exec` to access the database container
6. **Check `tainer.yaml`** to know the project type (wordpress, nodejs, nextjs, nuxtjs, kompozi, php)

## Quick reference

| Task | Command |
|---|---|
| Create new project | `tainer init` (interactive) or `tainer init --name=X --type=Y` |
| Start project | `tainer start` (from project dir) |
| Stop project | `tainer stop` |
| Restart project | `tainer restart` |
| Destroy project | `tainer destroy` |
| List all projects | `tainer list` |
| Run yarn in container | `tainer yarn <args>` |
| Run npm in container | `tainer npm <args>` |
| Switch Node mode | `tainer node dev` or `tainer node prod` |
| View project info | `tainer config` |
| Open shell in container | `tainer exec <container-name> sh` |

For full command reference, see [references/commands.md](references/commands.md).

## Detecting a tainer project

Run `ls tainer.yaml` from cwd. If not found, check parent directories. If found, the project root contains:
- `tainer.yaml` (config)
- `html/` (app source — mounted into container)
- `data/` (persistent data — gitignored contents)
- `db/` (database files — gitignored contents)

The user works on files in `html/` from the host. Tainer handles everything else.

## Asking for permission

Before doing anything that touches the user's project structure (creating files, moving directories, running migrations, removing old config), **always explain what you plan to do and ask for confirmation**. This is especially important for migrations.

For details on:
- **Project structure and conventions**: see [references/project-structure.md](references/project-structure.md)
- **Common workflows** (dev mode, prod build, package management): see [references/workflows.md](references/workflows.md)
- **Migrating existing projects** (Docker, raw Node, etc.): see [references/migration.md](references/migration.md)
- **Troubleshooting** (logs, debugging, recovery): see [references/troubleshooting.md](references/troubleshooting.md)
