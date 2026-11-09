---
# beans-26we
title: Fix TOCTOU race in GraphQL mutation validation
status: completed
type: bug
priority: high
created_at: 2026-03-14T15:06:18Z
updated_at: 2026-11-09T08:59:41Z
parent: beans-5txp
---

Resolver mutations call Get() (returns a copy outside the lock), run validations, then call Update() (acquires the lock). Between Get and Update, another goroutine can modify the bean, making validations stale. The ETag check in Update() catches conflicts but validators could pass on state that's no longer valid.

## Proposed Fix

Move validation inside Update() under the lock, or introduce a CompareAndSwap-style method that validates + updates atomically.

## Affected Files

- internal/graph/schema.resolvers.go (lines ~240-356)
- pkg/beancore/core.go (Update method)

## Summary of Changes

Resolved in the fork: copy-on-write mutations — Core.Mutate validates ifMatch, parent, blocking and cycles and writes under one lock (PR #52).
