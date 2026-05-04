# Common Tainer Workflows

## Starting fresh on a new project

```
mkdir my-app && cd my-app
tainer init --name=my-app --type=nextjs --start
```

This:
1. Generates `tainer.yaml`, `.env`, `data/`, `db/`
2. Pulls the Next.js image (first time only)
3. Extracts the app scaffold (package.json + node_modules) into `html/`
4. Starts the pod
5. Opens https://my-app.tainer.me in your browser (if `auto-open: true`)

## Cloning an existing tainer project

```
git clone git@github.com:org/my-app.git
cd my-app
tainer start
```

`tainer start` detects the project is not registered (cloned from another machine) and offers to auto-init: generates fresh `.env`, creates `data/` and `db/`, registers the project. Then it starts.

## Running yarn/npm commands

**Never run yarn or npm directly on the host.** Always go through tainer:

```
tainer yarn add @tanstack/react-query   # add a dependency
tainer yarn install                      # install all deps
tainer npm run build                     # run a script
```

The command runs inside the project's Node container, with the same Node version specified in `tainer.yaml`.

## Switching dev / prod mode

By default, `tainer start` runs the app in dev mode (`next dev` / `nuxt dev`). To switch to prod:

```
tainer node prod
```

This:
1. Cleans `.next` cache
2. Runs `yarn build` inside the container (with progress spinner)
3. Updates `package.json` start script to `next start`
4. Restarts the container

To go back:

```
tainer node dev
```

## Accessing the database

The database container is named `tainer-<project>-db-ct`. To open a psql shell (Postgres):

```
tainer exec tainer-my-app-db-ct psql -U tainer -d tainer
```

For MariaDB:

```
tainer exec tainer-my-app-db-ct mysql -u tainer -p tainer
```

The credentials are in `.env` at the project root.

## Viewing logs

```
tainer logs tainer-my-app-node-ct
tainer logs --tail 50 -f tainer-my-app-node-ct  # follow
tainer logs tainer-my-app-db-ct                  # database logs
```

## Opening a shell in the container

```
tainer exec tainer-my-app-node-ct sh
```

The container has zsh + oh-my-zsh by default.

## Multiple projects running simultaneously

Tainer handles multiple projects without conflict. Each project gets its own subnet, its own containers, and a unique `*.tainer.me` domain. The router (single instance) routes based on hostname.

```
cd ~/projects/site-a && tainer start
cd ~/projects/site-b && tainer start
cd ~/projects/site-c && tainer start
# All three running, accessible at site-a.tainer.me, site-b.tainer.me, site-c.tainer.me
```

`tainer list` shows all of them.

## Stopping everything

```
tainer list  # interactive — press 's' on each to stop
```

Or per project:

```
cd ~/projects/site-a && tainer stop
```
