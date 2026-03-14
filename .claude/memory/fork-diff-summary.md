# Fork Diff Summary

## Brand Branch (vs main)
Binary rename and build system rebrand:

- `cmd/podman/main.go` — error strings: "podman" → "tainer"
- `cmd/podman/root.go` — connection error message references tainer CLI
- `cmd/podman/system/version.go` — version output: "Tainer Engine"
- `Makefile` — comprehensive rename: all binary outputs, build targets, variables, install paths, clean patterns, release artifacts (Go source paths and on-disk file refs preserved as "podman")
- `.claude/conflict-zones.md` — documents all brand-modified files

## Main Branch (vs upstream)
Infrastructure and compliance additions:

- `NOTICE` — Apache 2.0 attribution to Podman/Red Hat
- `CLAUDE.md` — agent configuration and coding standards
- `.github/workflows/rebase-brand.yml` — auto-rebase brand on main push
- `.claude/skills/` — upstream-sync, conflict-resolver, changelog-analyzer
- `.claude/memory/` — upstream-cadence, fork-diff-summary
- `.claude/settings.json` — MCP server config (GitHub)
