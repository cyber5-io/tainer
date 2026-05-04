# Tainer Troubleshooting

## Tainer commands hang

If `tainer list`, `tainer start`, or any command hangs:

1. The VM might be unresponsive. Check VM processes:
   ```
   ps aux | grep -E "vfkit|krunkit|gvproxy" | grep -v grep
   ```
2. If you see high CPU or stuck processes, force-kill them:
   ```
   pkill -9 -f krunkit
   pkill -9 -f vfkit
   ```
3. Restart the machine:
   ```
   tainer machine start
   ```

## Container exits immediately after start

Check the container logs:

```
tainer logs tainer-<project>-node-ct
```

Common causes:

### Node app fails with "Could not find a production build"
The `package.json` start script is set to `next start` but no build exists. Either:
- Switch to dev mode: `tainer node dev`
- Or build first: `tainer node prod`

### Permission errors on `.next` or `node_modules`
Stale cache from a previous build with different uid/gid. Clean and restart:
```
rm -rf html/.next
tainer restart
```

### Database container exits
Check if `db/` directory has stray files (must be empty for first init):
```
ls -la db/
```
If there's a `.gitignore` inside `db/`, remove it. Postgres requires the directory to be completely empty on first start.

## "No tainer.yaml found in current directory"

You're not in a tainer project directory. Either:
- `cd` into a project that has `tainer.yaml`
- Or run `tainer init` to create a new project here

## "Project not registered on this machine"

The project has `tainer.yaml` but tainer doesn't know about it. This happens after cloning from another machine. `tainer start` will offer to auto-init: generate fresh `.env`, create `data/` and `db/`, register the project. Say yes if you trust the source.

## Domain not resolving (https://my-app.tainer.me unreachable)

1. Check the project is running: `tainer list`
2. Check the router: should be running. If not:
   ```
   tainer pod start tainer-router
   ```
3. Check DNS resolver:
   ```
   sudo cat /etc/resolver/tainer.me
   ```
   Should contain `nameserver 127.0.0.1`. If missing, tainer should reinstall on next start.

## Image pull fails (offline)

Tainer caches images locally. If you've started a project type before, subsequent starts work offline. If pull fails on first start:
- Check internet connection
- Check GHCR is reachable: `curl https://ghcr.io`
- Tainer will skip pull if image is cached locally — no warnings

## SSH into project container

Each project has SSH access via the router on port 22 (or fallback if blocked):

```
ssh <project-name>@ssh.tainer.me
```

The SSH key is auto-injected from `~/.ssh/`.

## Database access

Open psql/mysql shell directly:

```
# Postgres
tainer exec tainer-<project>-db-ct psql -U tainer -d tainer

# MariaDB
tainer exec tainer-<project>-db-ct mysql -u tainer -p tainer
```

Credentials are in the project's `.env` file (`DB_USER`, `DB_PASSWORD`, `DB_NAME`).

## Reset everything for a project

If a project is completely broken:

```
tainer destroy --force
rm -rf db data .env .tainer-*
tainer start  # auto-init will recreate everything
```

If you want to wipe project files too:

```
tainer destroy --nuke
```

## Tainer binary issues

### Update tainer
```
tainer update core
```

### Reinstall tainer
Download the latest installer from https://tainer.dev/download and run it.

### Tainer machine won't start
```
tainer machine stop tainer-machine-default
pkill -9 -f krunkit
pkill -9 -f vfkit
tainer machine start
```

If still failing, check the machine log:
```
cat /var/folders/*/T/tainer/tainer-machine-default.log
```
