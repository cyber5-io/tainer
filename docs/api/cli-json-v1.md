# Tainer CLI JSON contract, v1

This is the single reference for anything that consumes `tainer ... --json`: the control webapp, the menu app, and scripts. Build against this page, not against the code.

Applies from tainer 1.4 (CyberStack with the host bridge). Everything here is version `"v":1`.

## Which test guards which example

Examples are copied from the tests' own golden strings, so the page and the code cannot drift apart silently. `TestAPIDocMatchesGolden` additionally fails if the `tainer api` and error-envelope examples below stop matching the code.

| Example in this page | Guarded by |
| --- | --- |
| `tainer api --json` output | `TestAPIListsAllJSONCommands` (`cmd/tainer/api_test.go`) |
| Error envelope | `TestJSONErrorEnvelopeAndExit` (`cmd/tainer/jsonenv_test.go`) |
| `destroy --nuke` refusal | `TestNukeGuardInJSONMode` (`cmd/tainer/jsonenv_test.go`) |
| `update --check` document | `TestJSONSnapshotUpdateCheck` against `update-check.golden` (`cmd/tainer/jsonsnap_test.go`), and `TestUpdateCheckRejectsCombinedModes` (`cmd/tainer/updatecheck_test.go`) |
| `update-failed` envelope | `TestFailedUpdateAndNetworkEmitEnvelope` (`cmd/tainer/jsonenv_test.go`) |
| Single-shot documents (status, doctor, start, stop, destroy, init, db, update, network) | `TestJSONSnapshot*` against `cmd/tainer/testdata/json/*.golden` (`cmd/tainer/jsonsnap_test.go`). The goldens omit `"v"`; the test requires the live output to equal the golden plus `"v":1` |
| `list` document | `TestJSONSnapshotList` against `list-v1.golden`, and `TestJSONListEnvelope` |
| `watch` relay, `watch-ended` | `TestWatchStreamEOF`, `TestWatchRequiresJSON` (`cmd/tainer/watch_test.go`) |
| Event lines (`hello`, `engine`, `pod`, `progress`) | `TestEnvelopeShape` and `TestTimestampsAreMilliseconds` in the CyberStack repo (`internal/events`) |
| `logs` lines | `TestLogsJSONLineShape` (`cmd/tainer/json_gaps_test.go`) |
| `doctor` document, skip hints | `TestDoctorJSONShape`, `TestDoctorJSONSkipCarriesReason` |
| `config` document | `TestConfigJSONShape` |
| `exec` lines | `TestExecJSONStreamShape`, `TestExecJSONRefusesTTY` |
| `login`, `whoami`, `logout` documents | `TestLoginJSONReadsStdinAndStoresSession`, `TestWhoamiLoggedOutDocument`, `TestLogoutClearsStore` (`cmd/tainer/login_test.go`) |
| Gateway behaviour | `internal/controlapi/server_test.go` in the CyberStack repo |

## 1. The envelope

Every single-shot document is one JSON object whose first key is `"v":1`, written to stdout (pretty-printed). Every NDJSON line is one compact object that also starts with `"v":1`, newline-terminated.

Timestamps in every payload are RFC 3339 in UTC with exactly three fractional digits, for example `2026-10-31T12:00:00.123Z`. Millisecond precision is contractual: nanosecond strings break strict parsers such as Swift's `ISO8601DateFormatter`.

Add `--json` to a command to get this output. The gateway (section 8) always adds it for you.

### Discovery

`tainer api --json` lists every command that accepts `--json`. `stream` is true for commands that emit NDJSON instead of one document.

```json
{"v":1,"commands":[{"name":"status","json":true,"stream":false},{"name":"list","json":true,"stream":false},{"name":"init","json":true,"stream":false},{"name":"start","json":true,"stream":false},{"name":"stop","json":true,"stream":false},{"name":"restart","json":true,"stream":false},{"name":"destroy","json":true,"stream":false},{"name":"db","json":true,"stream":false},{"name":"update","json":true,"stream":false},{"name":"network","json":true,"stream":false},{"name":"doctor","json":true,"stream":false},{"name":"logs","json":true,"stream":true},{"name":"exec","json":true,"stream":true},{"name":"config","json":true,"stream":false},{"name":"api","json":true,"stream":false},{"name":"watch","json":true,"stream":true},{"name":"login","json":true,"stream":false},{"name":"logout","json":true,"stream":false},{"name":"whoami","json":true,"stream":false}]}
```

Without `--json`, `tainer api` and `tainer watch` print a plain-text usage error on stderr and exit 1. They have no human mode.

## 2. Errors

A command that could not run prints exactly this on stdout and exits 1:

```json
{"v":1,"error":{"code":"watch-ended","message":"the event stream ended"}}
```

`code` is a stable kebab-case slug for programs to branch on. `message` is human text; show it but do not parse it.

### The discriminator rule

An output is an error if and only if it has a top-level `"error"` object. Nothing else tells you.

Report commands, which are `status`, `doctor` and `update --all`, are different on one point. When they ran but found bad content (an unhealthy doctor run, an `update --all` where some pods failed), they print their normal document and exit 1. That is not a transport failure and not an error envelope. Read the document; do not treat the exit status alone as "the command failed to run". The error envelope is reserved for runs that could not execute at all.

`status` exits 1 only when `daemon.reachable` is false, and it still prints the full document. Read `daemon.reachable`, not the exit status; stopped pods and per-project `queryError` do not affect the exit code.

`update <project>` is the single-target form: a failure there is an error envelope (`update-failed`). `update --all` is the report form: per-pod results, including failures, are inside the document (`pods[].updated` and `pods[].error`).

Example of an error with a document-shaped command:

```json
{"v":1,"error":{"code":"update-failed","message":"pull failed"}}
```

`destroy --nuke` never prompts under `--json`. Without `--yes` it refuses:

```json
{"v":1,"error":{"code":"confirmation-required","message":"destroy --nuke requires --yes in --json mode"}}
```

### Slugs

| Slug | Meaning |
| --- | --- |
| `stack-unreachable` | The tainer stack could not be reached or started. |
| `stack-unavailable` | The stack is not running and tainer cannot start it (package not installed, or the user lacks the required group on Linux). |
| `unsupported-os` | This operating system is not supported. |
| `invalid-arguments` | Unknown flag or bad usage. |
| `no-project-here` | No project given and none found from the working directory. |
| `project-not-found` | The named project is not registered, or its manifest file cannot be read. |
| `manifest-invalid` | The manifest exists but does not parse or validate. |
| `unknown-project-type` | The project type is not supported. |
| `init-failed` | Project creation failed. |
| `start-failed` | Start failed. |
| `stop-failed` | Stop failed. |
| `destroy-failed` | Destroy failed. |
| `update-failed` | Update failed (single-target update). |
| `network-switch-failed` | Switching the network mode failed. |
| `db-export-failed` | Database export failed. |
| `db-import-failed` | Database import failed. |
| `missing-input-file` | An input file argument is required but absent. |
| `input-unreadable` | The input file could not be read. |
| `confirmation-required` | A destructive action needs `--yes` under `--json`. |
| `exec-tty-unsupported` | `exec --json` was asked for a TTY or interactive stdin. |
| `exec-failed` | The command could not be started in the container. |
| `role-not-found` | The project has no container with that role. |
| `logs-unavailable` | Logs could not be read, or a line exceeded 4 MiB. |
| `login-failed` | `login`: the email or password is wrong. |
| `email-unverified` | `login`: the account's email address is not verified yet. |
| `too-many-attempts` | `login`: too many failed attempts; try again later. |
| `id-unreachable` | The tainer accounts service could not be reached, or answered with a server error. |
| `pod-pinned` | The pod is pinned and cannot be stopped or destroyed. |
| `watch-ended` | The event stream ended (see section 4). |
| `internal-error` | A bug or an unrepresentable result. |
| `command-failed` | Catch-all for failures that carry no more specific slug. Treat unknown slugs the same way. |

Consumers must tolerate slugs they do not know. New slugs are additive.

### Breaking changes versus tainer before 1.4

- `list --json` was a bare array. It is now `{"v":1,"projects":[...]}`.
- A failed `update` or `network set` used to print a result document with failure fields. It now prints the error envelope and exits 1.
- Every pre-existing document gains `"v":1` as its first key; no other key changed.

## 3. Event families

`tainer watch --json` (section 4) and the gateway's `GET /v1/watch` stream one envelope per line: `{"v":1,"ts":...,"type":...,"data":...}`. There are four families. A consumer must ignore unknown `type` values and unknown fields.

`hello` is always the first line. It carries the full current state so a consumer never needs a separate query:

```json
{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"hello","data":{"engine":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"},"pods":[{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2}]}}
```

`pods` is `[]` when there are none, never `null`.

`engine` is sent when the engine state changes. `state` is `healthy`, `recovering` or `failed`. The daemon owns the timing: `recovering` becomes `failed` after 60 seconds unhealthy.

```json
{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"engine","data":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"}}
```

`pod` is sent when one pod's state changes. `state` is `running`, `stopped` or `partial`. While the engine is not healthy the pod table is frozen at its last known values, so a UI should show pod states subordinate to the engine state.

```json
{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"pod","data":{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2}}
```

`progress` reports a long operation (section 7). `percent` is `-1` when unknown.

```json
{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"progress","data":{"op":"start","pod":"blog","step":"pull","percent":-1,"message":"pulling","done":false}}
```

Progress `step` and `message` values are free text, not an enum: display them, never switch on them.

`domain` is captured when the pod's containers were created, so a manifest domain change shows up only after the pod is recreated.

## 4. `tainer watch --json`

A verbatim relay of the stack's event stream, one line per event.

- `hello` is the first line.
- When the stream ends (the daemon stopped or restarted), watch prints the `watch-ended` error envelope and exits 1. This is the only error it emits after streaming has begun.
- There is no automatic reconnect. Respawn the process, with backoff, to resubscribe. Each new process starts with a fresh `hello`.
- On SIGINT or SIGTERM watch exits 0 and prints no envelope: a user-initiated stop is not an error.
- If the daemon dies in the middle of writing a line, one truncated, invalid JSON line can be relayed just before `watch-ended`. Consumers must tolerate one possibly-invalid final line and skip it.
- If the stack cannot be reached when watch starts, the first and only line is an error envelope with a `stack-*` slug (`stack-unreachable`, `stack-unavailable`, or `unsupported-os` depending on platform and state), exit 1.

## 5. Per-command shapes

All documents below start with `"v":1` on the wire; the examples omit it only where they are copied from goldens that predate the envelope.

### `list`

```json
{
  "v": 1,
  "projects": [
    {"name": "blog", "type": "wordpress", "domain": "blog.tainer.me", "path": "/p/blog", "status": "running"},
    {"name": "shop", "type": "nodejs", "domain": "shop.tainer.me", "path": "/p/shop", "status": "stopped"}
  ]
}
```

### `status`

`status` (all projects) and `status <project>` are report commands. `status` exits 1 only when `daemon.reachable` is false, and it still prints the full document. Read `daemon.reachable`, not the exit status; stopped pods and per-project `queryError` do not affect the exit code. Project form:

```json
{
  "v": 1,
  "daemon": {"socket": "/run/cyberstack.sock", "running": true, "pid": 42, "reachable": true},
  "project": "blog",
  "podId": 7,
  "state": "running",
  "domain": "blog.tainer.me",
  "ports": [{"Role": "web", "Container": 80, "Protocol": "", "HostPort": 0}],
  "containers": [{"role": "app", "name": "blog-app", "state": "running", "ip": "10.0.0.2"}]
}
```

All-projects form: `daemon` plus `pods` (each with `name`, `podId`, `state`, and when running `domain` and `ports`). It lists every registered project, as `list` does: a project with no engine presence has `state` `stopped` and no `podId` (the key is absent). A project whose state could not be queried carries `queryError`. Note the `ports` entries use capitalised keys (`Role`, `Container`, `Protocol`, `HostPort`); this is frozen for v1.

The reserved pinned project `control` (the control dashboard) never appears in `list` or in the all-projects `status` document. `status control`, `logs`, `exec` and `config` still accept it by name, while `stop control` and `destroy control` fail with the `pod-pinned` error.

### `start`

```json
{
  "v": 1,
  "Pod": "blog",
  "PodID": 7,
  "Domain": "blog.tainer.me",
  "HTTPServices": [{"Role": "web", "URL": "https://blog.tainer.me"}],
  "TCPServices": [{"Role": "db", "Host": "127.0.0.1:70"}],
  "AlreadyRunning": false
}
```

`restart` returns the same document as `start`.

### `stop`, `destroy`, `init`

```json
{"v": 1, "pod": "blog", "stopped": true, "elapsedMs": 2310}
```

```json
{"v": 1, "pod": "blog", "mode": "clean", "destroyed": true, "keepImages": true, "elapsedMs": 1500}
```

A destroy the user declined at a prompt: `{"pod":"blog","destroyed":false,"cancelled":true}`.

```json
{"v": 1, "projectName": "blog", "manifestPath": "/p/blog/tainer.yaml", "steps": [{"action": "created", "kind": "file", "path": "tainer.yaml"}]}
```

`init` accepts `--size <size>` (also `--size=<size>`), one of `nano`, `small`, `medium`, `large`, `xlarge`, `xxl`, written to `pod.size` in the scaffolded `tainer.yaml`; the default is `small`. An unknown size fails with `invalid-arguments`, and the message lists the valid sizes.

### `db export` and `db import`

```json
{"v": 1, "pod": "blog", "file": "/tmp/blog.sql", "bytes": 2048, "elapsedMs": 900}
```

### `update`

Single project (error envelope on failure):

```json
{"v": 1, "mode": "project", "pod": "blog", "updated": true, "started": true, "elapsedMs": 1000}
```

`started` is true when the pod was brought back up after the update. A pod that was stopped before the update is recreated with the new images but left stopped, and reports `"updated": true, "started": false`. A pod that was running (or partly running) before the update reports `started: true`. The same `started` field appears on each `update --all` row, where it is false for a failed pod.

Base image (`update --base <type>`; error envelope on failure):

```json
{"v": 1, "mode": "base", "baseType": "wordpress", "updated": true, "elapsedMs": 1000}
```

`update --check` is a metadata-only report: it compares each registered pod's local parts with the registry and changes nothing. It exits 0 even when updates exist. Pods that have never been started (no containers yet) are left out. If a pod's check fails (registry unreachable, local record unreadable), it counts as `unknown`, carries `error`, and its `updateAvailable` is false. With the stack not running it is an error envelope (`stack-unreachable`). Combining `--check` with `--all`, `--base` or a project name is `invalid-arguments`.

```json
{
  "v": 1,
  "checkedAt": "2026-11-02T12:00:00.000Z",
  "pods": [
    {
      "name": "blog",
      "type": "wordpress",
      "domain": "blog.tainer.me",
      "state": "running",
      "updateAvailable": true,
      "parts": "WordPress · PHP 8.3 · MariaDB 11 · Caddy",
      "roles": [
        {"role": "web", "image": "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine", "updateAvailable": false},
        {"role": "app", "image": "ghcr.io/cyber5-io/tainer-wordpress-app:php-8.3", "updateAvailable": true}
      ]
    },
    {
      "name": "shop",
      "type": "react",
      "domain": "shop.tainer.me",
      "state": "stopped",
      "updateAvailable": false,
      "parts": "React · Node 22 · Caddy",
      "roles": [],
      "error": "registry unreachable"
    }
  ],
  "upToDate": 0,
  "available": 1,
  "unknown": 1
}
```

`state` is `running` or `stopped`. `parts` is a display line, not a stable identifier. `error` is present only when something failed.

`update --all` is a report: it exits 1 if any pod failed, but still prints this document.

```json
{
  "v": 1,
  "mode": "all",
  "pods": [
    {"name": "blog", "updated": true, "started": true, "elapsedMs": 100},
    {"name": "docs", "updated": true, "started": false, "elapsedMs": 50},
    {"name": "shop", "updated": false, "started": false, "error": "manifest missing: x", "elapsedMs": 0}
  ],
  "totalElapsedMs": 3000
}
```

### `network`

`network show`:

```json
{"v": 1, "mode": "perf", "display": "Performance"}
```

`network set <mode>`; failure is an error envelope (`network-switch-failed`):

```json
{"v": 1, "switched": true, "noChange": false, "previous": "compat", "new": "perf", "restarted": ["blog"], "failed": ["shop"], "elapsedMs": 4000}
```

When the mode is already set: `"switched": false, "noChange": true` and no `restarted` or `failed` keys.

### `doctor`

A report command. It exits 1 when any check is not ok-or-warn, and still prints this document. `status` is `ok`, `warn` or `fail`; a check that was skipped because an earlier layer failed is reported as `fail`, and a skipped check with no hint of its own gets `skipped: <detail>` as its hint (an existing hint wins), so a failing check is never unexplained. `detail` and `fixed` appear only when set. The top-level `fixed` array is present only when `--fix` was given (it may be empty). `stoppedPods` appears only when non-zero.

```json
{
  "v": 1,
  "checks": [
    {"name": "daemon", "status": "ok", "detail": "reachable", "hint": ""},
    {"name": "pods", "status": "fail", "detail": "blog down", "hint": "tainer restart blog", "fixed": true},
    {"name": "agent", "status": "fail", "detail": "daemon is down", "hint": "skipped: daemon is down"}
  ],
  "fixed": ["pods"],
  "stoppedPods": 2,
  "healthy": false
}
```

The `fixed` array above appears because the example was run with `--fix`.

### `config`

The parsed manifest, with the same key names as `tainer.yaml`, under `project` (so the manifest's own `project` section is `project.project`):

```json
{"v":1,"project":{"version":2,"project":{"name":"blog","type":"wordpress","domain":"blog.tainer.me"},"runtime":{"php":"8.3","database":"mariadb"},"pod":{"size":"small"}}}
```

Without `--json`, `config` prints `tainer.yaml` as written.

### `logs` (NDJSON)

One line per log line. A pod without a role argument streams every container, interleaved; each line is a whole record.

```json
{"v":1,"pod":"blog","part":"app","ts":"2026-10-31T10:11:12.345Z","line":"2026-10-31T10:11:12.345678Z GET /"}
{"v":1,"pod":"blog","part":"app","ts":"","line":"plain line without stamp"}
```

`ts` is non-empty only when the line itself starts with an RFC 3339 timestamp, which it usually does not, so it is usually `""`. `line` is the raw text. Blank lines are preserved. A line over 4 MiB ends the stream with a `logs-unavailable` envelope; records already written stay valid. `--follow` keeps the stream open.

### `exec` (NDJSON)

Non-interactive only. `--tty` and `--interactive` under `--json` are refused with `exec-tty-unsupported`. Output is chunked as it arrives, then one final exit record.

```json
{"v":1,"stream":"stdout","data":"hello\n"}
{"v":1,"stream":"stderr","data":"oops"}
{"v":1,"exit":3}
```

`data` is UTF-8; bytes that are not valid UTF-8 are replaced with U+FFFD, so binary output is not representable. Chunk boundaries are arbitrary and need not align with lines. The remote command's own exit status is in the final record; the `tainer exec` process itself exits with that status too.

## 6. Streams in general

`watch`, `logs` and `exec` are streams: read line by line, parse each line separately, and stop at end of input. An error envelope can appear as the last line of any stream.

## 7. Progress

Long operations report to the daemon, which republishes them as `progress` events (section 3). Six `op` values exist in the schema: `init`, `start`, `stop`, `db-export`, `db-import`, `update`. Today `init`, `start`, `update`, `db-export` and `db-import` emit them; `stop` is reserved. Treat `op` as an open set.

- Events for one operation share `op` and `pod`. The last one has `"done":true`.
- `update` emits its `done` event only after the pod restart has completed.
- There are no error events. A failed operation ends with the command's own result (error envelope, or the document for report commands). Do not wait for a `done` event that will not come.
- Progress is best effort and may be missing entirely (older daemon, daemon down). Never depend on it for correctness; use it to animate.
- Daemons accept progress documents with unknown fields, so a newer CLI can talk to an older daemon.

## 8. The control gateway

For tools running in a pod (the control webapp), CyberStack exposes the CLI over HTTP. Every response body is the CLI's own output, unmodified.

### Routes

| Route | Behaviour |
| --- | --- |
| `POST /v1/cli/{cmd}` | Body `{"args":["..."]}`. Runs `tainer {cmd} --json <args>` (argv exec, no shell) and relays stdout. `logs`, `exec` and `watch` stream as `application/x-ndjson`; the rest return `application/json`. |
| `GET /v1/watch` | The event stream (section 3), same lines as `tainer watch --json`. |

`{cmd}` must be one of: `status`, `list`, `init`, `start`, `stop`, `restart`, `destroy`, `db`, `update`, `network`, `doctor`, `logs`, `exec`, `config`, `api`, `watch`, `login`, `logout`, `whoami`. The request body may also carry an optional `"stdin"` string, which the gateway writes to the CLI's standard input; it is permitted only for `login` (which reads its credentials there) and refused for every other command. Anything else (`ssh`, `menu`, ...) is refused. The gateway adds `--json` itself and drops any `--json` or `--json=...` in your args that comes before a bare `--`.

### Authentication

Every request needs `Authorization: Bearer <token>`. The token is minted fresh each time the daemon boots and written (mode 0600) to `control.token` in the daemon's state directory. A bad or missing token gets `401` with `{"v":1,"error":{"code":"unauthorized",...}}` before any routing, so paths cannot be probed.

Inside the control pod, tainer mounts the `control/` subdirectory of the daemon state directory (which holds only the gateway files; the state directory root is never mounted) read-only at `/run/tainer/control` and sets `TAINER_CONTROL_TOKEN_FILE` and `TAINER_GATEWAY_ADDR_FILE` to the token and address files within it (`control.addr` holds the base URL). It also sets `TAINER_PROJECT_PATH` to the control project's absolute host directory (the folder holding its `tainer.yaml`), for building file paths to hand to the CLI. Read the paths from those variables rather than assuming them.

The token file exists only after the listener is up, and is removed when the daemon stops. A client must wait for the file (and re-read it after a daemon restart); it must not assume it exists when a pod is created.

The gateway can be permanently absent: with backend none, an unknown network mode, no state directory, or no host address found, it is skipped and no token file ever appears. Clients must therefore bound their wait with a timeout. In vmnet-helper mode the daemon polls up to 60 seconds for the VM's address before binding, so allow a minute or more before giving up.

### Address and port

Port 7754. The address the pod uses depends on the network mode:

| Mode | Address from inside a pod | Notes |
| --- | --- | --- |
| vmnet-helper (macOS) | The host's address on the VM network, discovered at runtime; normally `192.168.105.1` | Do not hard-code it. |
| external-gvproxy and hybrid (macOS) | `192.168.127.254:7754` | The listener is bound on the host's loopback; gvproxy maps its host alias to it. `host.containers.internal` is unverified, so use the IP. |
| Linux native | `10.113.0.1:7754` | The bridge gateway. Host firewall reachability is on the bring-up checklist. |

### Outcomes

- `exit != 0` from the CLI is still HTTP `200`, and the body is the CLI's own output: an error envelope, or a report document. Use section 2's discriminator, not the HTTP status.
- `400` `unsupported-command` (not allowlisted) or `bad-request` (body not `{"args":[...]}`, trailing data, more than 256 args, NUL byte); `413` `bad-request` (body over 1 MiB).
- `429` `too-busy` when 8 CLI processes are already running. There is no `Retry-After`; back off and retry.
- `502` `cli-unavailable` when the CLI could not be started.
- `cli-failed` (HTTP 200) when the CLI died without printing anything: `{"v":1,"error":{"code":"cli-failed","message":"tainer exited without output"}}`.
- Closing the connection terminates the CLI process (SIGTERM, then SIGKILL after 5 seconds).
- Streaming responses are written with a 15 second per-write deadline; a client that stops reading is dropped.

## 9. Versioning

- Additive changes keep `"v":1`: new keys in documents, new event types, new `progress` ops, new slugs. Consumers must ignore what they do not recognise.
- A breaking change (removing or renaming a key, changing a type or meaning) bumps `v`. The release that introduces `v:2` keeps `v:1` available for one more minor release so consumers can move.
- Daemons tolerate unknown fields in progress documents posted by the CLI, so CLI and daemon can be one minor version apart.

## 10. Accounts

`login`, `logout` and `whoami` manage the tainer account the CLI and the apps share. The signed-in session lives in `account.json` (mode 0600) in the CLI's accounts directory: `~/.cyberstack`, overridable via `TAINER_ACCOUNTS_DIR`. The gateway spawns the CLI with a scrubbed environment: `PATH`, `NO_COLOR`, the user's `HOME` (so dashboard logins and terminal logins share one session) and `DOCKER_HOST`, plus `TAINER_ID_URL` and `TAINER_ACCOUNTS_DIR` only when the daemon itself was started with them set and non-empty (a development aid for pointing at a local accounts service; unset in production, where the CLI uses its built-in accounts endpoint). Nothing else is inherited. The CLI refreshes and rotates the session as needed. None of these commands runs against pods, and none needs the stack to be up.

### `login`

`tainer login --json` reads exactly one JSON object from stdin; `deviceName` is optional and defaults to `<hostname> · tainer`.

```json
{"email":"leni@example.com","password":"...","deviceName":"My Mac"}
```

Success never echoes tokens:

```json
{
  "v": 1,
  "loggedIn": true,
  "userId": "u-1",
  "email": "leni@example.com"
}
```

Errors: `login-failed`, `email-unverified`, `too-many-attempts`, `id-unreachable`, and `invalid-arguments` (stdin is not one object, or the email or password is missing). Without `--json`, `login` prompts for the email and the password (typed without echo).

### `whoami`

`tainer whoami --json` always exits 0 for a document, signed in or not.

```json
{
  "v": 1,
  "loggedIn": false
}
```

```json
{
  "v": 1,
  "loggedIn": true,
  "userId": "u-1",
  "email": "leni@example.com",
  "accessToken": "...",
  "accessTokenExpiresAt": "2026-11-01T09:00:00.000Z"
}
```

The access token is refreshed first when it has less than 60 seconds left. If the accounts service says the session has expired or been revoked, the stored session is removed and the answer is `loggedIn:false`. If the service cannot be reached but the stored access token has not expired, the document is returned with `"offline":true` added; if it has expired the result is the `id-unreachable` error and the stored session is kept.

### `logout`

`tainer logout --json` revokes the session at the accounts service on a best-effort basis, then always removes the local session, even when the service is unreachable or nobody was signed in.

```json
{
  "v": 1,
  "loggedOut": true
}
```
