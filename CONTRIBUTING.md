# Contributing to Tainer

We welcome contributions! Whether it's bug reports, feature requests, or code contributions.

## Reporting Issues

- Check [existing issues](https://github.com/cyber5-io/tainer/issues) before opening a new one
- Include steps to reproduce, expected vs actual behavior, and your environment details
- Security issues should be reported via email to **security@cyber5.io**, not as public issues

## Pull Requests

- Fork the repo and create a branch from `main`
- Keep PRs focused — one feature or fix per PR
- Include tests where applicable
- Update documentation if your changes affect user-facing behavior

## Building from Source

```bash
make tainer-remote
```

The binary will be at `bin/darwin/tainer` (macOS) or `bin/linux/tainer` (Linux).

## License

By contributing to Tainer, you agree that your contributions will be licensed under the [Business Source License 1.1](LICENSE).

## Origin

Tainer is a fork of [Podman](https://github.com/containers/podman). The container engine internals are inherited from Podman; contributions to Tainer's developer experience layer (the `pkg/tainer/` directory) are most welcome.
