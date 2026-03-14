# Fork Diff Summary

## Brand Branch (vs main)
User-facing renames only — minimal surface area:

- `cmd/podman/main.go` — error strings: "podman" → "tainer"
- `cmd/podman/root.go` — connection error message references tainer CLI
- `cmd/podman/system/version.go` — version output: "Tainer Engine"
- `Makefile` — build target `bin/tainer`, phony target `tainer`
- `.claude/conflict-zones.md` — documents all brand-modified files

## Main Branch (vs upstream)
Infrastructure and compliance additions:

- `NOTICE` — Apache 2.0 attribution to Podman/Red Hat
- `CLAUDE.md` — agent configuration and coding standards
- `.github/workflows/rebase-brand.yml` — auto-rebase brand on main push
- `.claude/skills/` — upstream-sync, conflict-resolver, changelog-analyzer
- `.claude/memory/` — upstream-cadence, fork-diff-summary
- `.claude/settings.json` — MCP server config (GitHub)
