---
name: changelog-analyzer
description: Analyze upstream Podman release notes and flag changes affecting Tainer customizations
---

## Changelog Analysis

1. Fetch upstream release notes / changelog from GitHub releases
2. Identify changes affecting files in `.claude/conflict-zones.md`
3. Flag breaking changes, deprecations, and security patches
4. Create summary with risk assessment (low/medium/high)
5. For security patches: flag as immediate priority
