package beangraph

import (
	"context"
	"sort"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// ReadyFilter returns the filter used to find beans ready to be worked on
// (`beans next`, the TUI's "n"): todo beans that are not blocked (explicitly
// or via an ancestor) and that do not inherit a terminal status from a
// completed/scrapped ancestor.
func ReadyFilter() *model.BeanFilter {
	notBlocked := false
	excludeImplicitTerminal := true
	return &model.BeanFilter{
		Status:                  []string{"todo"},
		IsBlocked:               &notBlocked,
		ExcludeImplicitTerminal: &excludeImplicitTerminal,
	}
}

// isContainerType reports whether a bean type is a grouping container
// (milestone or epic) rather than a directly workable item.
func isContainerType(t string) bool {
	return t == "milestone" || t == "epic"
}

// OpenChildCounts returns, for each bean ID, how many direct children are
// not completed/scrapped.
func OpenChildCounts(all []*bean.Bean) map[string]int {
	counts := make(map[string]int)
	for _, b := range all {
		if b.Parent != "" && b.Status != "completed" && b.Status != "scrapped" {
			counts[b.Parent]++
		}
	}
	return counts
}

// RankNextCandidates sorts candidates in place, most important first:
//  1. priority (critical > high > normal > low > deferred; unset = normal,
//     unknown priorities last)
//  2. leaf work items before containers (epics/milestones, or any bean
//     that still has open children)
//  3. oldest created first (beans without a creation date last)
//  4. ID, for a stable deterministic order
func RankNextCandidates(candidates []*bean.Bean, priorityNames []string, openChildren map[string]int) {
	priorityOrder := make(map[string]int, len(priorityNames))
	normal := len(priorityNames)
	for i, p := range priorityNames {
		priorityOrder[p] = i
		if p == "normal" {
			normal = i
		}
	}
	prio := func(b *bean.Bean) int {
		if b.Priority == "" {
			return normal
		}
		if o, ok := priorityOrder[b.Priority]; ok {
			return o
		}
		return len(priorityNames)
	}
	container := func(b *bean.Bean) bool {
		return isContainerType(b.Type) || openChildren[b.ID] > 0
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if pa, pb := prio(a), prio(b); pa != pb {
			return pa < pb
		}
		if ca, cb := container(a), container(b); ca != cb {
			return !ca
		}
		switch {
		case a.CreatedAt != nil && b.CreatedAt != nil:
			if !a.CreatedAt.Equal(*b.CreatedAt) {
				return a.CreatedAt.Before(*b.CreatedAt)
			}
		case a.CreatedAt != nil:
			return true
		case b.CreatedAt != nil:
			return false
		}
		return a.ID < b.ID
	})
}

// NextBean returns the single most important bean that is ready to be worked
// on (see ReadyFilter and RankNextCandidates), or nil if none is.
func (r *CoreResolver) NextBean(ctx context.Context) (*bean.Bean, error) {
	candidates, err := r.Beans(ctx, ReadyFilter())
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	all, err := r.Beans(ctx, nil)
	if err != nil {
		return nil, err
	}
	var priorityNames []string
	if cfg := r.Core.Config(); cfg != nil {
		priorityNames = cfg.PriorityNames()
	}
	RankNextCandidates(candidates, priorityNames, OpenChildCounts(all))
	return candidates[0], nil
}
