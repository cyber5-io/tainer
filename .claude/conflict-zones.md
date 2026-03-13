# Tainer Conflict Zones

Files modified by the brand branch. Check these during upstream merges.

## Binary Name
- `cmd/podman/main.go` — user-facing strings renamed to "tainer"
- `cmd/podman/root.go` — error message referencing "tainer" CLI commands
- `cmd/podman/system/version.go` — version output: "Tainer Engine"
- `Makefile` — primary build target `bin/tainer` (was `bin/podman`)

## Documentation
- `docs/**` — rebranded (future task)

## License
- `NOTICE` — Tainer/Cyber5 attribution (on main, not brand)
