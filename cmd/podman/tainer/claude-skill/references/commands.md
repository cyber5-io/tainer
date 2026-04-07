# Tainer Command Reference

## Project lifecycle

### `tainer init`
Create a new tainer project in the current directory.

**Interactive (wizard):**
```
tainer init
```
Launches a TUI wizard that prompts for project name, type, version, database, subdomain, and git setup.

**Non-interactive:**
```
tainer init --name=blog --type=wordpress --start
tainer init --name=api --type=nextjs --db=postgres
tainer init --name=app --type=php --php=8.3 --db=none
```

**Required flags:** `--name`, `--type`
**Optional flags:** `--php`, `--node`, `--db`, `--subdomain`, `--start`, `--git-init`, `-q/--quiet`

**Project types:** `wordpress`, `php`, `nodejs`, `nextjs`, `nuxtjs`, `kompozi` (aliases: `wp`, `node`, `next`, `nuxt`)

### `tainer start`
Start the project pod (containers + router). Run from the project directory or pass the project name.
```
tainer start              # current directory
tainer start my-project   # by name
```

If the project is not yet registered (e.g. freshly cloned), tainer will offer to auto-initialise it: generate `.env`, create `data/` and `db/`, register the project.

### `tainer stop`
Stop the project pod.
```
tainer stop
tainer stop my-project
```

### `tainer restart`
Stop + start in one command.
```
tainer restart
```

### `tainer destroy`
Remove containers, network, and registry entry. Project files on disk are preserved unless `--nuke` is added.
```
tainer destroy           # asks for confirmation
tainer destroy --force   # skip confirmation
tainer destroy --nuke    # also delete project files
```

### `tainer list`
Interactive TUI showing all registered projects, their status, and the router state. Press `s` to start/stop, `enter` to open in browser.

## Inside the container

### `tainer yarn`
Run yarn commands inside the Node container. Never run yarn locally on the host.
```
tainer yarn install
tainer yarn add lodash
tainer yarn build
```

**Intercepted commands** (you should not run these directly — use `tainer node` instead):
- `tainer yarn start` — blocked, use `tainer node dev` or `tainer node prod`
- `tainer yarn dev` — blocked, use `tainer node dev`

### `tainer npm`
Same as `tainer yarn` but for npm.

### `tainer node dev` / `tainer node prod`
Switch Node.js between development and production mode. Updates `package.json` start script and restarts the container. Production mode runs `yarn build` first.
```
tainer node dev   # next dev / nuxt dev
tainer node prod  # builds, then runs next start / nuxt start
```

### `tainer exec`
Run a command inside a container. Container names follow the pattern `tainer-<project>-<type>-ct`.
```
tainer exec tainer-myproject-node-ct sh
tainer exec tainer-myproject-db-ct psql -U tainer
```

## Information

### `tainer config`
Show project info: name, type, domain, path, backup status. Run from a project directory.

### `tainer config backup`
Backup `tainer.yaml` and `.env` for the current project.

### `tainer config restore`
Restore `tainer.yaml` and `.env` from backup.

### `tainer version`
Show tainer version, Go version, build time, OS/arch.

### `tainer logs <container>`
View container logs.
```
tainer logs tainer-myproject-node-ct
tainer logs --tail 50 tainer-myproject-db-ct
```

## Mounts

### `tainer mount`
Manage custom mounts. Useful for sharing folders between host and container.
```
tainer mount                     # interactive TUI
tainer mount add public          # add ./public mount
tainer mount del public          # remove mount
```

## Updates

### `tainer update`
Pull latest container images for the current project.

### `tainer update <project-name>`
Pull latest images for a named project.

### `tainer update core`
Self-update the tainer binary from GitHub Releases. Architecture-aware (downloads the correct binary for darwin/linux + amd64/arm64).
