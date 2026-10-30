# Dev-pod resilience: three bugs found while building cyber5-io-comms (2026-09-05 to 2026-09-08)

Written from the comms project session (Claude Code, session
https://claude.ai/code/session_01UmKFvbUzsAtr9bSWeQycmj) as a handoff for a fresh
tainer/cyber-stack session. Each item has the observed symptom, the root cause with
file:line pointers, and a proposed fix. Items 1 and 2 are tainer/tainer-images;
item 3 is cyber-stack. None are guesses: every root cause was confirmed against the
running system (kernel OOM lines in `~/.cyberstack/console.log`, engine error text,
and the code paths below).

## 1. The dev server dies under the pod's memory budget and nothing restarts it

**Symptom.** `tainer status` says the pod is `running`, `https://<project>.tainer.me`
returns 502 from Caddy. Happened ~10 times per day on `cyber5-comms` whenever vitest or
`svelte-check` ran inside the app container.

**Root cause.** Smart pods put every container under one memcg budget
(`/tainer-<project>`, 1 GiB for `size: small`). When a test runner or type-checker
needs more, the kernel OOM-kills the largest task in the cgroup, which is the Vite dev
server (`MainThread`, ~150 MB RSS). Console log excerpts:

```
oom-kill:constraint=CONSTRAINT_MEMCG,...,oom_memcg=/tainer-cyber5-comms,task_memcg=/tainer-cyber5-comms/tainer-cyber5-comms-app,task=MainThread
Memory cgroup out of memory: Killed process 1658 (MainThread) total-vm:1007116kB, anon-rss:146260kB
memory: usage 1048576kB, limit 1048576kB
```

`tainer-images/node-app/start-node-app.sh` runs the dev script once and then, by
design, keeps the container alive with `tail -f /dev/null` so `tainer exec` still works:

```sh
su-exec tainer $PM run dev $DEV_ARGS || echo "tainer: app exited ($?) — container stays up for tainer exec/shell"
...
tail -f /dev/null
```

So after the kill the container is "running" with nothing on :3000. `tainer doctor`
reports everything healthy because it checks layers, not the app listener.

**Proposed fix (tainer-images, `node-app/start-node-app.sh`).** Supervise the app
instead of running it once. Keep the "container stays up" property.

```sh
# replace the single `su-exec tainer $PM run dev ...` with a supervise loop
backoff=2
while :; do
    if grep -q '"dev"' package.json; then
        su-exec tainer $PM run dev $DEV_ARGS
    else
        su-exec tainer $PM start
    fi
    rc=$?
    echo "tainer: app exited ($rc) — restarting in ${backoff}s (tainer exec/shell keep working)"
    sleep "$backoff"
    [ "$backoff" -lt 30 ] && backoff=$((backoff * 2))
done
```

Notes: reset `backoff=2` when the app ran for more than, say, 60 s before exiting
(so a healthy long run does not inherit a 30 s delay). A user who intentionally stops
the app (`tainer exec ... pkill`) will see it come back; if that matters, honour a
sentinel file such as `/var/www/html/.tainer-hold` that the loop checks before
restarting, and have `tainer` set/remove it. Also worth a `tainer doctor` check: for
`nodejs`-type pods, verify something listens on 127.0.0.1:3000 inside the app
container and print the restart hint if not.

**Secondary cause worth fixing in the same pass.** Test runners and type-checkers
compete with the dev server for one budget. Options: document `vitest run
--no-file-parallelism` for tainer projects (comms did this), or give `tainer npm`
an opt-in `--oom-shield` that lowers the *invoked* command's oom_score_adj so the
kernel prefers killing the transient process over the dev server (the DB already gets
`-500` via `podCgroupSettings` in `pkg/tainer/pod/start.go:335-341`; the app leaf
could get the same protection relative to exec'd children).

## 2. Changing `pod.size` in tainer.yaml does nothing for an existing pod

**Symptom.** Set `size: medium`, then `large`; `tainer restart`, then `tainer stop` +
`tainer start`; the kernel kept reporting `limit 1048576kB` (1 GiB). A 1.5 GB
allocation probe inside the app container was killed every time.

**Root cause.** `pkg/tainer/pod/start.go` computes the budget from the manifest
(`PodBudget`, `split.go:27-45`) and passes it as `Resources.Memory` on the container
spec (`start.go:335-341`). But `startContainer` is idempotent: if the container
exists it is started in place (`start.go:404-420`) and the spec is never re-applied.
On the engine side, `internal/agent/containerd/runtime.go:475` re-asserts the *stored*
budget (`state.PodMemoryBytes`, captured at container creation, `runtime.go:233`) via
`ensurePodCgroup` on every start. So the parent cgroup's `memory.max` stays at the
value from when the container was first created.

`ManifestHash` (`pod.go:131-140`) already includes `Pod.Size`, and every container is
labelled with it (`LabelManifestHash`, `start.go:365`). The key-rotation path
(`start.go:246-256`) already shows the intended pattern: remove the containers so the
loop recreates them.

**Proposed fix (tainer, `pkg/tainer/pod/start.go`).** In `startContainer` (or the loop
above it), after `eng.Inspect`, compare `insp.Config.Labels[LabelManifestHash]` with
the current `mhash`; on mismatch, `eng.Remove(ctx, name, true)` and fall through to
`eng.Run(ctx, spec)`. Log one line: `pod <name>: manifest changed (size/runtime), recreating <role>`.
Because the hash also covers runtime versions and ports, this fixes those drifts too.
Bind-mounted `html/`, `data/`, `db/` are unaffected by container recreation (same
guarantee `destroy` without `--clean` gives, `destroy.go:25-30`).

A `tainer status` line showing the effective budget (`memory.max` read via the agent)
next to the manifest's `size:` would have made this visible immediately.

## 3. `tainer destroy` cannot remove a stopped container: overlay mount busy (cyber-stack)

**Symptom.** After `tainer stop`, `tainer destroy cyber5-comms` (plain mode) fails:

```
tainer: remove tainer-cyber5-comms-app: engine: remove tainer-cyber5-comms-app:
Error response from daemon: rpc error: code = Unknown desc = replacing mount point
"/var/lib/cyberstack/storage/overlay/<id>/merged": device or resource busy
```

Repeatable across stop → destroy → start cycles; the container stayed at its old
budget as a result (item 2 could not be worked around). Only a VM restart clears it.

**Where to look (cyber-stack).** The agent's container removal unmounts the overlay
`merged` dir; something still holds it. Candidates observed on this machine: the
per-container `tainer exec` sessions (the `tainer ssh`/sshd backend runs inside the
container and may keep a cwd in the merged tree), or the pf/port-forward helper
children that survive stop (`internal/vm/pfclient/client.go:166-191` documents
respawning children after sleep/wake). Reproduce with: start a pod, `tainer exec
<pod> app -- sleep 600 &` (or an open `tainer ssh`), `tainer stop`, `tainer destroy`.
Fix options: kill exec/ssh sessions attached to the container before unmount, or
lazy-unmount (`MNT_DETACH`) the merged dir and retry the rmdir.

## Interim workaround used on comms

- Bring the dev server back: `tainer stop <pod> && tainer start` (a `restart` keeps
  the stale budget but does restart the app).
- Type-check without killing the dev server: `tainer stop <pod> && tainer start`
  first (fresh memory), then `tainer npm exec -- svelte-check ...`, then stop/start
  again because `npm exec` leaves the app container without the dev server.
- `npm test` runs with `--no-file-parallelism` in comms' package.json.

## Suggested tickets (TAIN)

1. Supervise the app process in node-app image; `doctor` checks the :3000 listener.
2. `tainer start` recreates containers whose manifest hash label differs.
3. cyber-stack: container removal fails with busy overlay mount after stop.
4. `tainer status` shows the effective pod memory budget.
