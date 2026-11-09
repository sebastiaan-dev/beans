---
# beans-r66y
title: Deduplicate ETagMismatchError type
status: completed
type: task
priority: normal
created_at: 2026-03-14T15:06:45Z
updated_at: 2026-11-09T09:00:33Z
parent: beans-5txp
---

ETagMismatchError is defined identically in both pkg/beancore/ and internal/graph/. Should exist in one canonical location (pkg/beancore/ makes sense since that's where ETag validation happens) and be imported elsewhere.

## Affected Files

- pkg/beancore/core.go
- internal/graph/schema.resolvers.go

## Summary of Changes

beangraph.ETagMismatchError/ETagRequiredError are now aliases of the beancore types (one definition; errors.As works with either package).
