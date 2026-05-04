# Tainer Project Structure

A tainer project always follows this layout:

```
my-project/
├── tainer.yaml          # Project config (tracked in git)
├── .env                 # Generated credentials (gitignored)
├── .gitignore           # Includes .env, .tainer-*, db/, data/
├── .tainer.local.yaml   # Local network state (gitignored, machine-specific)
├── .tainer-authorized_keys  # SSH key for tainer user (gitignored, machine-specific)
├── html/                # APP CODE — mounted into container at /var/www/html
│   ├── package.json
│   ├── src/
│   └── ...
├── data/                # Persistent runtime data — mounted at /var/www/data
│   └── .gitignore       # Tracked, contents ignored
└── db/                  # Database files (Postgres/MariaDB) — mounted at /var/lib/...
    └── (no .gitignore — Postgres requires empty dir to initialise)
```

## tainer.yaml

The single source of truth for project configuration. Example:

```yaml
version: 1
project:
    name: my-project
    type: kompozi
    domain: my-project.tainer.me
    auto-open: false
runtime:
    node: "22"
    database: postgres
    shell: zsh
```

**Project types:**
- `wordpress` — WordPress site, PHP + MariaDB by default
- `php` — Generic PHP app, configurable database
- `nodejs` — Generic Node.js app
- `nextjs` — Next.js (React)
- `nuxtjs` — Nuxt.js (Vue)
- `kompozi` — Kompozi CMS (Next.js + PayloadCMS)

**Runtime fields:**
- `php` — PHP version (7.4, 8.1, 8.2, 8.3, 8.4, 8.5)
- `node` — Node version (20, 22, 24)
- `database` — `mariadb`, `postgres`, or `none`
- `shell` — `zsh` or `bash`

## html/ — App source

This is where the user works. It's mounted into the container at `/var/www/html`. Anything the user edits here is live in the container.

**Editor experience:**
- User opens `html/` in their IDE
- All edits hot-reload inside the container
- Imports, package.json changes, etc. all happen in `html/`

**What goes in html/:**
- All app source code (`src/`, `app/`, `pages/`, etc.)
- `package.json` and lockfiles
- Config files (`next.config.mjs`, `nuxt.config.ts`, etc.)
- Static assets (`public/`)

**What does NOT go in html/:**
- Tainer config (`tainer.yaml` is at project root)
- Database files (in `db/`)
- Runtime data (in `data/`)
- Container or VM config

## data/ — Persistent runtime data

Files that need to survive container restarts but aren't part of the codebase:
- WordPress uploads (`data/wp-content/uploads/`)
- User-uploaded media
- Cache files
- Generated content

Mounted at `/var/www/data` inside the container. Symlinked to relevant locations (e.g. WordPress symlinks `wp-content/uploads` → `/var/www/data/wp-content/uploads`).

## db/ — Database files

Raw database storage. Mounted directly to `/var/lib/postgresql/data` (Postgres) or `/var/lib/mysql` (MariaDB). **Never edit these files directly.** Use `tainer exec tainer-<project>-db-ct psql ...` to interact with the database.

This directory must be empty when Postgres first initialises. Tainer creates it empty during init.

## Why this structure?

The split between `tainer.yaml` (project root) and app code (`html/`) means:
- Tainer config travels with the repo (every clone gets the same setup)
- App code is cleanly separated from container/infra concerns
- The git repo can be cloned and `tainer start` just works (auto-init flow)
- Multiple projects can share the same pattern without conflict
