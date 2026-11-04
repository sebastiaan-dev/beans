---
# beans-m364
title: beans progress command
status: completed
type: task
priority: normal
created_at: 2025-12-27T21:44:05Z
updated_at: 2026-11-04T14:38:49Z
order: VVd
parent: beans-mmyp
---

Add `beans progress` command to show a summary of work status.

## Behavior

- Shows counts by status (e.g., "5 in-progress, 12 todo, 8 completed")
- Could show a simple progress bar
- Optional: filter by milestone/epic to see progress on specific initiatives

## Example

```bash
beans progress
# Output:
# In Progress: 2
# Todo: 15  
# Completed: 23
# Scrapped: 3
# ━━━━━━━━━━━━━━━━ 57% complete
```

## Summary of Changes

- Added `beans progress [--parent id] [--json]` (internal/commands/progress.go): counts beans by status, total, blocked open beans, and a progress bar.
- Percent = completed / (total - scrapped); --parent scopes to all descendants of the given bean (NOT_FOUND error if missing).
- Tests: TestComputeProgress, TestDescendantsOf, TestRenderProgressBar, TestBuildProgressReport.
