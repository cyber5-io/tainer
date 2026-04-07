# Migrating Existing Projects to Tainer

When the user has an existing project (Docker, docker-compose, raw Node, etc.) and wants to use tainer, follow this playbook. **Always ask for confirmation before running any commands that move files, delete data, or modify the repo structure.**

## Step 1: Detect the current setup

Look for these signs:
- `docker-compose.yml`, `Dockerfile`, `Caddyfile` → Docker-based
- `package.json` at project root with no container config → raw Node
- `composer.json` → PHP project
- `wp-config.php` → WordPress
- Existing `tainer.yaml` → already a tainer project (skip migration)

## Step 2: Determine project type

Map the existing project to a tainer type:

| Stack | Tainer type |
|---|---|
| WordPress | `wordpress` |
| Plain PHP / Laravel / Symfony | `php` |
| Next.js | `nextjs` |
| Nuxt.js | `nuxtjs` |
| PayloadCMS + Next.js | `kompozi` |
| Other Node.js | `nodejs` |

## Step 3: Propose the migration plan

Show the user exactly what will happen, in this order:

1. **Create `tainer.yaml`** at project root (you write the file)
2. **Move app code into `html/`** — all source files, configs, package files (except git, claude, env files)
3. **Remove old container files** — `docker-compose.yml`, `Dockerfile`, `Caddyfile` (only if user confirms)
4. **Update `.gitignore`** — add `db/`, `data/`, `.tainer-*`, and update existing entries with `html/` prefix where needed
5. **Add `.gitignore` to `data/`** — so the directory is tracked but contents ignored. **NOT to `db/`** — Postgres requires an empty directory to initialise.
6. **Run `tainer start`** — first start auto-inits and pulls images

## Step 4: Get confirmation

Show the user:
- What files will move
- What files will be deleted
- What new files will be created
- The proposed `tainer.yaml` content

Wait for explicit "yes" before proceeding. Suggest a `git commit` of the current state first as a safety net.

## Step 5: Execute (after approval)

Use git commands so file moves preserve history:

```bash
mkdir -p html
git mv src html/
git mv package.json yarn.lock html/
git mv next.config.mjs html/
# ... move all app files
```

For Docker file removal:

```bash
git rm docker-compose.yml Dockerfile Caddyfile
```

Create the `tainer.yaml`:

```yaml
version: 1
project:
    name: <user-provided>
    type: <detected>
    domain: <name>.tainer.me
    auto-open: false
runtime:
    node: "22"
    database: postgres
    shell: zsh
```

Update `.gitignore`:

```
# Dependencies
html/node_modules/

# Next.js (or framework-specific)
html/.next/
html/*.tsbuildinfo

# Environment
html/.env
.env

# Tainer (local dev)
.tainer-authorized_keys
.tainer.local.yaml
db/
data/
```

Create directories:

```bash
mkdir -p data db
printf '*\n!.gitignore\n' > data/.gitignore
# DO NOT create db/.gitignore — Postgres needs empty dir
```

## Step 6: Verify

Run from the project root:

```
tainer start
```

If it fails, check the troubleshooting reference. Common issues:
- Database initialisation fails → `db/` not empty (check for stray `.gitignore`)
- App container exits → Node mode mismatch (try `tainer node dev`)
- Build errors → corrupted `.next` cache, run `tainer node dev` to clean

## Migration variants

### From docker-compose

The user's `docker-compose.yml` reveals:
- Service ports → tainer handles via router (no port conflicts)
- Database credentials → tainer generates fresh ones in `.env`
- Volume mounts → translate to `data/` and `db/`
- Custom networks → tainer uses its own per-project subnet

If the user has data they want to preserve:
- Database: dump from old container, import after `tainer start`
- Files: copy into `data/` before first `tainer start`

### From raw local development

If the user runs the app locally without containers:
- Stop any locally running services (yarn dev, postgres, etc.)
- Move all source into `html/`
- Add `tainer.yaml`
- Run `tainer start`
- Use `tainer yarn` instead of `yarn` from now on

### From Vagrant / VirtualBox

Similar to docker-compose:
- Move app code into `html/`
- Discard `Vagrantfile`
- Recreate database in tainer's db container

## Important rules

1. **Never delete the user's data without explicit permission**
2. **Always offer to commit before changing anything**
3. **Use `git mv` instead of `mv`** to preserve history
4. **Explain every command before running it**
5. **If unsure, ask** — better to ask twice than to break the user's project
