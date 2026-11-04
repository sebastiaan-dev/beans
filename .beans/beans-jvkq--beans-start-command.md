---
# beans-jvkq
title: beans start command
status: completed
type: task
priority: normal
created_at: 2025-12-27T21:44:04Z
updated_at: 2026-11-04T14:38:49Z
order: VVa
parent: beans-mmyp
---

Add `beans start <id>` command.

## Behavior

- Sets status to `in-progress`
- Displays the bean details (like `beans show`) so you can see what you're working on
- Could optionally check if another bean is already in-progress and warn

## Example

```bash
beans start beans-abc
# Sets to in-progress and shows the bean
```

## Summary of Changes

- Added `beans start <id>` (internal/commands/start.go): sets status to in-progress via the UpdateBean resolver and prints bean details like `show`.
- Warns (stderr, or `warnings` in --json) when other work items are in-progress (ignoring the bean's ancestors and epics/milestones) and when the bean is blocked.
- Unarchives archived beans; no-op with a warning if already in-progress.
- Tests in workflow_test.go (TestOtherInProgress, TestTransitionBean*).
