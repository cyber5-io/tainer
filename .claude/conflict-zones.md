# Tainer Conflict Zones

Files modified by the brand branch. Check these during upstream merges.

## Binary Name & Build
- `cmd/podman/main.go` — user-facing error strings renamed to "tainer"
- `cmd/podman/root.go` — connection error message references "tainer" CLI
- `cmd/podman/system/version.go` — version output: "Tainer Engine"
- `Makefile` — comprehensive rename: binary outputs, targets, variables, install paths, clean patterns, release artifacts (Go source paths and on-disk file refs preserved as "podman")

## Documentation
- `docs/**` — rebranded (future task)

## License
- `NOTICE` — Tainer/Cyber5 attribution (on main, not brand)
