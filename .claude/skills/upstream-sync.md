---
name: upstream-sync
description: Sync upstream Podman releases into Tainer's upstream branch, merge into main, and trigger brand rebase
---

## Upstream Sync Procedure

1. `git fetch origin-upstream`
2. `git checkout upstream && git merge origin-upstream/main` (fast-forward)
3. `git push origin upstream`
4. `git checkout main && git merge upstream`
5. Run test suite: `make localsystem`
6. If clean + tests pass + no conflict zone files touched → push main (triggers brand rebase via CI)
7. If conflicts or conflict zone files touched → create PR for human review
8. Create Jira task (TAIN) logging the sync

## Conflict Zone Check

Before auto-merging, check if any files listed in `.claude/conflict-zones.md` were modified in the upstream diff. If so, escalate to human review.
