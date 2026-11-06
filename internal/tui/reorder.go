package tui

import (
	"github.com/hmans/beans/pkg/bean"
)

// Manual ordering of siblings (K/J), using the fractional "order" keys the
// web UI's drag and drop writes (frontend/src/lib/dragOrder.ts). Beans are
// sorted by status first and by order key within a status (see
// bean.SortByStatusPriorityAndType), so a bean only moves among siblings of
// its status.

// orderKeysAscending reports whether keys are non-empty and strictly
// increasing.
func orderKeysAscending(keys []string) bool {
	for i, k := range keys {
		if k == "" || (i > 0 && keys[i-1] >= k) {
			return false
		}
	}
	return true
}

// sequentialOrderKeys returns n strictly increasing order keys.
func sequentialOrderKeys(n int) []string {
	keys := make([]string, n)
	prev := ""
	for i := range keys {
		keys[i] = bean.OrderBetween(prev, "")
		prev = keys[i]
	}
	return keys
}

// planOrderMove returns the order keys to write so that the bean id moves
// to just before (after = false) or just after (after = true) the bean
// targetID. group holds the beans the two belong to (siblings of the same
// status) in their current display order.
//
// Like the web UI, beans of the group without an order key get one first,
// keeping the current positions (ensureOrdered). Only beans whose key
// changes are returned. It returns nil if id or targetID is not in group.
func planOrderMove(group []*bean.Bean, id, targetID string, after bool) map[string]string {
	var moving *bean.Bean
	var rest []*bean.Bean
	for _, b := range group {
		if b.ID == id {
			moving = b
		} else {
			rest = append(rest, b)
		}
	}
	target := -1
	for i, b := range rest {
		if b.ID == targetID {
			target = i
		}
	}
	if moving == nil || target < 0 {
		return nil
	}

	// The desired display order.
	pos := target
	if after {
		pos++
	}
	desired := make([]*bean.Bean, 0, len(group))
	desired = append(desired, rest[:pos]...)
	desired = append(desired, moving)
	desired = append(desired, rest[pos:]...)

	// Keys of the others, filling in missing keys in place (ensureOrdered).
	keys := make(map[string]string, len(group))
	ordered := make([]string, len(group))
	for i, b := range group {
		ordered[i] = b.Order
	}
	prev := ""
	for i, b := range group {
		if ordered[i] == "" {
			next := ""
			if i+1 < len(group) {
				next = ordered[i+1]
			}
			ordered[i] = bean.OrderBetween(prev, next)
		}
		keys[b.ID] = ordered[i]
		prev = ordered[i]
	}

	// The moving bean's key goes between its new neighbours.
	before, next := "", ""
	if pos > 0 {
		before = keys[rest[pos-1].ID]
	}
	if pos < len(rest) {
		next = keys[rest[pos].ID]
	}
	keys[id] = bean.OrderBetween(before, next)

	// Duplicate or malformed keys leave no room in between: renumber.
	final := make([]string, len(desired))
	for i, b := range desired {
		final[i] = keys[b.ID]
	}
	if !orderKeysAscending(final) {
		for i, k := range sequentialOrderKeys(len(desired)) {
			keys[desired[i].ID] = k
		}
	}

	changes := make(map[string]string)
	for _, b := range group {
		if keys[b.ID] != b.Order {
			changes[b.ID] = keys[b.ID]
		}
	}
	return changes
}
