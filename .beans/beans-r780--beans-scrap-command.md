---
# beans-r780
title: beans scrap command
status: completed
type: task
priority: normal
created_at: 2025-12-27T21:44:04Z
updated_at: 2026-11-04T14:38:49Z
order: VVv
parent: beans-mmyp
---

Add `beans scrap <id> --reason <text>` command.

## Behavior

- Sets status to `scrapped`
- **Required** `--reason` flag to document why the bean was scrapped
- Adds a `## Reason for Scrapping` section to the bean body (preserves project memory)
- Shows confirmation message

## Example

```bash
beans scrap beans-abc --reason "Superseded by beans-xyz approach"
```

## Summary of Changes

- Added `beans scrap <id> --reason/-m text` (internal/commands/scrap.go): sets status to scrapped via UpdateBean.
- --reason is required (structured VALIDATION_ERROR in --json); appended as `## Reasons for Scrapping` (prompt.tmpl convention); '-' reads stdin.
- Already-scrapped beans are left unchanged with a warning.
