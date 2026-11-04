---
# beans-0ajg
title: beans complete command
status: completed
type: task
priority: normal
created_at: 2025-12-27T21:44:04Z
updated_at: 2026-11-04T14:38:49Z
order: VV
parent: beans-mmyp
---

Add `beans complete <id> [--summary <text>]` command.

## Behavior

- Sets status to `completed`
- Optional `--summary` flag to add a completion note to the bean body
- Shows confirmation message with bean title

## Example

```bash
beans complete beans-abc --summary "Implemented via PR #42"
```

## Summary of Changes

- Added `beans complete <id> [--summary/-m text]` (internal/commands/complete.go): sets status to completed via UpdateBean.
- Summary is appended as a `## Summary of Changes` section (prompt.tmpl convention); '-' reads stdin; empty summary rejected.
- Already-completed beans are left unchanged (no duplicate section) with a warning.
- Shared transition helper in workflow.go; tests in workflow_test.go.
