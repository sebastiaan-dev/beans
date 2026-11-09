---
# beans-omee
title: Build incoming link index to replace O(n) FindIncomingLinks scan
status: completed
type: task
priority: high
created_at: 2026-03-13T17:49:00Z
updated_at: 2026-11-09T08:59:41Z
parent: beans-oyic
---

FindIncomingLinks() iterates ALL beans to find who points at a given bean. It's called from Children, BlockedBy, and Blocking GraphQL resolvers — loading 100 beans with children scans 100×n beans. Build and maintain a reverse map during load and on mutations to make this O(1).

## Summary of Changes

Resolved in the fork: Core keeps an incoming-link index under the lock; relation fields, FindIncomingLinks and blocker checks use it (PR #55).
