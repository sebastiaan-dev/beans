package beangraph

import (
	"slices"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// ApplyFilter applies BeanFilter to a slice of beans and returns filtered results.
// This is used by both the top-level beans query and relationship field resolvers.
func ApplyFilter(beans []*bean.Bean, filter *model.BeanFilter, core *beancore.Core) []*bean.Bean {
	if filter == nil {
		return beans
	}

	result := beans

	// Status filters
	if len(filter.Status) > 0 {
		result = filterByField(result, filter.Status, func(b *bean.Bean) string { return b.Status })
	}
	if len(filter.ExcludeStatus) > 0 {
		result = excludeByField(result, filter.ExcludeStatus, func(b *bean.Bean) string { return b.Status })
	}

	// Type filters
	if len(filter.Type) > 0 {
		result = filterByField(result, filter.Type, func(b *bean.Bean) string { return b.Type })
	}
	if len(filter.ExcludeType) > 0 {
		result = excludeByField(result, filter.ExcludeType, func(b *bean.Bean) string { return b.Type })
	}

	// Priority filters (empty priority treated as "normal")
	if len(filter.Priority) > 0 {
		result = filterByPriority(result, filter.Priority)
	}
	if len(filter.ExcludePriority) > 0 {
		result = excludeByPriority(result, filter.ExcludePriority)
	}

	// Tag filters
	if len(filter.Tags) > 0 {
		result = filterByTags(result, filter.Tags)
	}
	if len(filter.ExcludeTags) > 0 {
		result = excludeByTags(result, filter.ExcludeTags)
	}

	// Parent filters
	if filter.HasParent != nil && *filter.HasParent {
		result = filterByHasParent(result)
	}
	if filter.NoParent != nil && *filter.NoParent {
		result = filterByNoParent(result)
	}
	if filter.ParentID != nil && *filter.ParentID != "" {
		result = filterByParentID(result, *filter.ParentID)
	}

	// Blocking filters. Like the Bean.blocking and Bean.blockedBy fields,
	// they use the symmetric relations: a bean is blocking B if it lists B
	// in its blocking field or B lists it in its blocked_by field.
	if filter.HasBlocking != nil && *filter.HasBlocking {
		result = filterBeans(result, core.HasBlocking)
	}
	if filter.BlockingID != nil && *filter.BlockingID != "" {
		result = filterByBlockingID(result, *filter.BlockingID, core)
	}
	if filter.NoBlocking != nil && *filter.NoBlocking {
		result = filterBeans(result, func(b *bean.Bean) bool { return !core.HasBlocking(b) })
	}
	if filter.IsBlocked != nil {
		if *filter.IsBlocked {
			result = filterByIsBlocked(result, core)
		} else {
			result = filterByNotBlocked(result, core)
		}
	}
	if filter.IsExplicitlyBlocked != nil && *filter.IsExplicitlyBlocked {
		result = filterByIsExplicitlyBlocked(result, core)
	}
	if filter.IsImplicitlyBlocked != nil && *filter.IsImplicitlyBlocked {
		result = filterByIsImplicitlyBlocked(result, core)
	}

	// Blocked-by filters (symmetric, like the blocking filters)
	if filter.HasBlockedBy != nil && *filter.HasBlockedBy {
		result = filterBeans(result, core.HasBlockedBy)
	}
	if filter.BlockedByID != nil && *filter.BlockedByID != "" {
		result = filterByBlockedByID(result, *filter.BlockedByID, core)
	}
	if filter.NoBlockedBy != nil && *filter.NoBlockedBy {
		result = filterBeans(result, func(b *bean.Bean) bool { return !core.HasBlockedBy(b) })
	}

	// Implicit status filter
	if filter.ExcludeImplicitTerminal != nil && *filter.ExcludeImplicitTerminal {
		result = filterByNoImplicitTerminal(result, core)
	}

	return result
}

// filterByField filters beans to include only those where getter returns a value in values (OR logic).
func filterByField(beans []*bean.Bean, values []string, getter func(*bean.Bean) string) []*bean.Bean {
	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*bean.Bean
	for _, b := range beans {
		if valueSet[getter(b)] {
			result = append(result, b)
		}
	}
	return result
}

// excludeByField filters beans to exclude those where getter returns a value in values.
func excludeByField(beans []*bean.Bean, values []string, getter func(*bean.Bean) string) []*bean.Bean {
	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*bean.Bean
	for _, b := range beans {
		if !valueSet[getter(b)] {
			result = append(result, b)
		}
	}
	return result
}

// filterByPriority filters beans to include only those with matching priorities (OR logic).
// Empty priority in the bean is treated as "normal" for matching purposes.
func filterByPriority(beans []*bean.Bean, priorities []string) []*bean.Bean {
	prioritySet := make(map[string]bool, len(priorities))
	for _, p := range priorities {
		prioritySet[p] = true
	}

	var result []*bean.Bean
	for _, b := range beans {
		priority := b.Priority
		if priority == "" {
			priority = "normal"
		}
		if prioritySet[priority] {
			result = append(result, b)
		}
	}
	return result
}

// excludeByPriority filters beans to exclude those with matching priorities.
// Empty priority in the bean is treated as "normal" for matching purposes.
func excludeByPriority(beans []*bean.Bean, priorities []string) []*bean.Bean {
	prioritySet := make(map[string]bool, len(priorities))
	for _, p := range priorities {
		prioritySet[p] = true
	}

	var result []*bean.Bean
	for _, b := range beans {
		priority := b.Priority
		if priority == "" {
			priority = "normal"
		}
		if !prioritySet[priority] {
			result = append(result, b)
		}
	}
	return result
}

// filterByTags filters beans to include only those with any of the given tags (OR logic).
func filterByTags(beans []*bean.Bean, tags []string) []*bean.Bean {
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[t] = true
	}

	var result []*bean.Bean
	for _, b := range beans {
		for _, t := range b.Tags {
			if tagSet[t] {
				result = append(result, b)
				break
			}
		}
	}
	return result
}

// excludeByTags filters beans to exclude those with any of the given tags.
func excludeByTags(beans []*bean.Bean, tags []string) []*bean.Bean {
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[t] = true
	}

	var result []*bean.Bean
outer:
	for _, b := range beans {
		for _, t := range b.Tags {
			if tagSet[t] {
				continue outer
			}
		}
		result = append(result, b)
	}
	return result
}

// filterByHasParent filters beans to include only those with a parent.
func filterByHasParent(beans []*bean.Bean) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if b.Parent != "" {
			result = append(result, b)
		}
	}
	return result
}

// filterByNoParent filters beans to include only those without a parent.
func filterByNoParent(beans []*bean.Bean) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if b.Parent == "" {
			result = append(result, b)
		}
	}
	return result
}

// filterByParentID filters beans with specific parent ID.
func filterByParentID(beans []*bean.Bean, parentID string) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if b.Parent == parentID {
			result = append(result, b)
		}
	}
	return result
}

// filterBeans returns the beans matching keep.
func filterBeans(beans []*bean.Bean, keep func(*bean.Bean) bool) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if keep(b) {
			result = append(result, b)
		}
	}
	return result
}

// filterByBlockingID filters beans that are blocking the bean with the given
// ID (short IDs are supported): beans listing it in their blocking field and
// beans it lists in its blocked_by field. Matches nothing if the bean doesn't
// exist, like the Bean.blocking field skips broken links.
func filterByBlockingID(beans []*bean.Bean, targetID string, core *beancore.Core) []*bean.Bean {
	target, err := core.Get(targetID)
	if err != nil {
		return nil
	}
	return filterBeans(beans, func(b *bean.Bean) bool {
		return slices.Contains(b.Blocking, target.ID) || slices.Contains(target.BlockedBy, b.ID)
	})
}

// filterByBlockedByID filters beans that are blocked by the bean with the
// given ID (short IDs are supported): beans listing it in their blocked_by
// field and beans it lists in its blocking field.
func filterByBlockedByID(beans []*bean.Bean, blockerID string, core *beancore.Core) []*bean.Bean {
	blocker, err := core.Get(blockerID)
	if err != nil {
		return nil
	}
	return filterBeans(beans, func(b *bean.Bean) bool {
		return slices.Contains(b.BlockedBy, blocker.ID) || slices.Contains(blocker.Blocking, b.ID)
	})
}

// filterByIsBlocked filters beans that are blocked by others.
// A bean is considered blocked only if it has active (non-completed, non-scrapped) blockers.
func filterByIsBlocked(beans []*bean.Bean, core *beancore.Core) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if core.IsBlocked(b.ID) {
			result = append(result, b)
		}
	}
	return result
}

// filterByIsExplicitlyBlocked filters beans that have direct active blockers.
func filterByIsExplicitlyBlocked(beans []*bean.Bean, core *beancore.Core) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if core.IsExplicitlyBlocked(b.ID) {
			result = append(result, b)
		}
	}
	return result
}

// filterByIsImplicitlyBlocked filters beans where an ancestor is blocked.
func filterByIsImplicitlyBlocked(beans []*bean.Bean, core *beancore.Core) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if core.IsImplicitlyBlocked(b.ID) {
			result = append(result, b)
		}
	}
	return result
}

// filterByNotBlocked filters beans that are NOT blocked by others.
// A bean is considered not blocked if it has no active (non-completed, non-scrapped) blockers.
func filterByNotBlocked(beans []*bean.Bean, core *beancore.Core) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		if !core.IsBlocked(b.ID) {
			result = append(result, b)
		}
	}
	return result
}

// filterByNoImplicitTerminal excludes beans that inherit a terminal status
// (scrapped or completed) from an ancestor.
func filterByNoImplicitTerminal(beans []*bean.Bean, core *beancore.Core) []*bean.Bean {
	var result []*bean.Bean
	for _, b := range beans {
		status, _ := core.ImplicitStatus(b.ID)
		if status == "" {
			result = append(result, b)
		}
	}
	return result
}
