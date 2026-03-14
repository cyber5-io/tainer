---
name: conflict-resolver
description: Resolve merge conflicts in Tainer fork with context from conflict zones map
---

## Conflict Resolution

1. Read `.claude/conflict-zones.md` for known brand-modified files
2. For each conflict:
   - If file is in conflict zones → preserve brand changes (ours), integrate upstream structural changes
   - If file is NOT in conflict zones → accept upstream changes (theirs)
3. Run test suite after resolution
4. If resolution is uncertain, create PR with analysis for human review
