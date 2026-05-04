<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/tainer-logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/tainer-logo-light.svg">
  <img alt="Tainer" src="docs/assets/tainer-logo-light.svg" width="280">
</picture>

A developer-friendly local development environment tool. Create, manage, and run containerised projects with a single command.

Tainer wraps the complexity of containers, networking, TLS, SSH, and databases into a simple CLI that just works.

## Quick Start

```bash
# Create a new project
cd my-project
tainer init

# Start it
tainer start

# Access it
https://my-project.tainer.me
ssh my-project@ssh.tainer.me
```

## Features

- **One-command setup** — `tainer init` walks you through project creation
- **Automatic HTTPS** — every project gets a `.tainer.me` domain with TLS
- **SSH access** — SSH into any project container via `ssh.tainer.me`
- **Multiple runtimes** — WordPress, PHP, Node.js, Next.js, Nuxt.js
- **Database included** — MariaDB or PostgreSQL, your choice
- **Offline support** — cached images work without internet
- **Config backup** — automatic backup and restore of project configuration
- **Self-update** — `tainer update` keeps images and the binary up to date

## Supported Project Types

| Type | Runtime | Default Database |
|------|---------|-----------------|
| WordPress | PHP 7.4 - 8.5 | MariaDB |
| PHP | PHP 7.4 - 8.5 | MariaDB |
| Node.js | Node 18 - 23 | PostgreSQL |
| Next.js | Node 18 - 23 | PostgreSQL |
| Nuxt.js | Node 18 - 23 | PostgreSQL |

## Commands

| Command | Description |
|---------|-------------|
| `tainer init` | Create a new project (interactive wizard) |
| `tainer start` | Start a project |
| `tainer stop` | Stop a project |
| `tainer restart` | Restart a project |
| `tainer destroy` | Remove a project's containers |
| `tainer open` | Open project URL in browser |
| `tainer ssh` | SSH into project container |
| `tainer update` | Update project images |
| `tainer update core` | Self-update the tainer binary |
| `tainer config backup` | Backup project configuration |
| `tainer config restore` | Restore project configuration |
| `tainer up` / `tainer down` | Aliases for start/stop |

## Origin

Tainer started as a fork of [Podman](https://github.com/containers/podman) and builds on its container engine to provide a higher-level developer experience. The Podman project is licensed under the Apache License 2.0 and is Copyright (c) the Podman contributors.

## License

Tainer is licensed under the [Business Source License 1.1](LICENSE).

- **Free for all developers and teams** — use it however you like
- **Restriction** — you may not use it to offer a commercial hosted development environment service
- **Change date** — each release converts to Apache 2.0 after 7 years
