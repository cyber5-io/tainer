# Tainer

A local-first development tool for containerised projects on macOS.

`tainer` is a small CLI that drives **CyberStack** — a Docker-compatible container engine — to handle the full lifecycle of dev environments (init, start, stop, exec, build, deploy) for WordPress, Node.js, Next.js, Nuxt.js, NestJS, React, PHP, and Kompozi projects. No daemon configuration, no `docker-compose.yml` to write, no podman. Just `tainer init` → `tainer start` → working dev environment.

## Status

This branch (`dev/v1`) is a **fresh rebuild** towards `1.0.0`. The previous tree (a podman fork that shipped as tainer 0.2.x) lives under [`legacy/`](./legacy) for reference and will be removed from `main` after the 1.0.0 merge.

The rebuild plan and design are in [`docs/superpowers/`](./docs/superpowers/).

CyberStack lives in a separate repo: [`cyber5-io/cyber-stack`](https://github.com/cyber5-io/cyber-stack) (private). Its 0.4.0 cold-pull P50 is 494ms, 27ms behind OrbStack on the same Apple Silicon hardware.

## License

BSL 1.1 — see [LICENSE](./LICENSE).

The `legacy/` tree is Apache 2.0 (inherited from podman) — see [legacy/LICENSE](./legacy/LICENSE).
