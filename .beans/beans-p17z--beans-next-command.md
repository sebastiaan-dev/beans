---
# beans-p17z
title: beans next command
status: completed
type: task
priority: normal
created_at: 2025-12-27T21:44:04Z
updated_at: 2026-11-04T14:38:49Z
order: VVg
parent: beans-mmyp
---

Add `beans next` command to show the single most important bean to work on.

## Behavior

- Returns the highest-priority `todo` bean that is not blocked
- Shows full bean details (like `beans show`)
- If nothing is ready, suggests checking `beans blocked` or `beans list`

## Example

```bash
beans next
# Shows the single most important bean to tackle
```

## Summary of Changes

- Added `beans next [--json]` (internal/commands/next.go): picks the most important ready bean (todo, not blocked explicitly or via ancestors, not implicitly completed/scrapped) using existing BeanFilter fields.
- Ranking: priority (critical>high>normal>low>deferred, unset=normal), then leaf items before containers (epic/milestone or beans with open children), then oldest, then ID.
- Prints details like `show`; when nothing is ready prints a hint (or {success:true,message}) and exits 0.
- Tests: TestRankNextCandidates, TestFindNextBean.
