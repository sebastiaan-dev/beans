---
# beans-p2pw
title: Fix navigator.platform access at module scope in FilterInput
status: completed
type: bug
priority: normal
created_at: 2026-03-13T17:49:18Z
updated_at: 2026-11-09T09:00:33Z
parent: beans-oyic
---

FilterInput.svelte accesses navigator.platform at module scope, which will break during SSR/build. Needs a browser guard from $app/environment.

## Summary of Changes

FilterInput reads navigator.platform behind a typeof check, like Sidebar does.
