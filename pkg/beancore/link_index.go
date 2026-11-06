package beancore

import (
	"sort"

	"github.com/hmans/beans/pkg/bean"
)

// Link types as reported by IncomingLink.LinkType.
const (
	LinkTypeParent    = "parent"
	LinkTypeBlocking  = "blocking"
	LinkTypeBlockedBy = "blocked_by"
)

// linkKind indexes the outgoing link fields of a bean.
type linkKind int

const (
	linkParent    linkKind = iota // b.Parent
	linkBlocking                  // b.Blocking
	linkBlockedBy                 // b.BlockedBy
	numLinkKinds
)

var linkKindNames = [numLinkKinds]string{LinkTypeParent, LinkTypeBlocking, LinkTypeBlockedBy}

// linkIndex is the reverse index of the stored beans' links: for each link
// kind, it maps a target ID to the set of IDs of the beans whose field of
// that kind contains the target. Targets need not exist (broken links are
// indexed too); sources always are stored beans.
//
// Core keeps it in sync with c.beans: every change to the map goes through
// setBeanLocked, deleteBeanLocked or resetBeansLocked, under the write lock.
// Readers use it under the read lock.
type linkIndex [numLinkKinds]map[string]map[string]struct{}

func newLinkIndex() linkIndex {
	var idx linkIndex
	for k := range idx {
		idx[k] = make(map[string]map[string]struct{})
	}
	return idx
}

// forEachLink calls fn for every outgoing link of b. Duplicate entries in a
// list are reported once per occurrence; the index stores sets.
func forEachLink(b *bean.Bean, fn func(kind linkKind, target string)) {
	if b.Parent != "" {
		fn(linkParent, b.Parent)
	}
	for _, t := range b.Blocking {
		fn(linkBlocking, t)
	}
	for _, t := range b.BlockedBy {
		fn(linkBlockedBy, t)
	}
}

func (idx linkIndex) add(b *bean.Bean) {
	forEachLink(b, func(kind linkKind, target string) {
		sources := idx[kind][target]
		if sources == nil {
			sources = make(map[string]struct{}, 1)
			idx[kind][target] = sources
		}
		sources[b.ID] = struct{}{}
	})
}

func (idx linkIndex) remove(b *bean.Bean) {
	forEachLink(b, func(kind linkKind, target string) {
		sources := idx[kind][target]
		delete(sources, b.ID)
		if len(sources) == 0 {
			delete(idx[kind], target)
		}
	})
}

// sources returns the sorted IDs of the beans with a link of the given kind
// to target.
func (idx linkIndex) sources(kind linkKind, target string) []string {
	set := idx[kind][target]
	if len(set) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// setBeanLocked stores b as the snapshot for b.ID and updates the link
// index. Must be called with the write lock held.
func (c *Core) setBeanLocked(b *bean.Bean) {
	if old, ok := c.beans[b.ID]; ok {
		c.links.remove(old)
	}
	c.beans[b.ID] = b
	c.links.add(b)
}

// deleteBeanLocked removes the bean with the given ID and its links from the
// index. Must be called with the write lock held.
func (c *Core) deleteBeanLocked(id string) {
	if old, ok := c.beans[id]; ok {
		c.links.remove(old)
		delete(c.beans, id)
	}
}

// resetBeansLocked empties the store and the link index. Must be called with
// the write lock held.
func (c *Core) resetBeansLocked() {
	c.beans = make(map[string]*bean.Bean)
	c.links = newLinkIndex()
}

// incomingLocked returns the stored beans with a link of the given kind to
// target, sorted by ID. Must be called with c.mu held.
func (c *Core) incomingLocked(kind linkKind, target string) []*bean.Bean {
	ids := c.links.sources(kind, target)
	if len(ids) == 0 {
		return nil
	}
	result := make([]*bean.Bean, 0, len(ids))
	for _, id := range ids {
		if b, ok := c.beans[id]; ok {
			result = append(result, b)
		}
	}
	return result
}

// hasIncomingLocked reports whether any stored bean links to target with the
// given kind. Must be called with c.mu held.
func (c *Core) hasIncomingLocked(kind linkKind, target string) bool {
	return len(c.links[kind][target]) > 0
}

// The relation methods below take the bean itself rather than its ID: its own
// links (parent, blocking, blocked_by) are read from b, which may be a
// different version than the stored one (e.g. a bean about to be returned by
// a resolver), while incoming links come from the store. Returned beans are
// shared, read-only snapshots (see the package documentation).

// Parent returns b's parent, or nil if it has none or the parent doesn't
// exist.
func (c *Core) Parent(b *bean.Bean) *bean.Bean {
	if b.Parent == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.beans[b.Parent]
}

// Children returns the beans whose parent is the bean with the given ID,
// sorted by ID.
func (c *Core) Children(id string) []*bean.Bean {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incomingLocked(linkParent, id)
}

// Blocking returns the beans b is blocking: the existing beans in its
// blocking list, followed by the beans listing it in their blocked_by list
// (sorted by ID), without duplicates. It is symmetric to BlockedBy and
// includes resolved beans (FindActiveBlockers covers active blockers only).
func (c *Core) Blocking(b *bean.Bean) []*bean.Bean {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.relatedLocked(b.ID, b.Blocking, linkBlockedBy)
}

// BlockedBy returns the beans blocking b: the existing beans in its
// blocked_by list, followed by the beans listing it in their blocking list
// (sorted by ID), without duplicates.
func (c *Core) BlockedBy(b *bean.Bean) []*bean.Bean {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.relatedLocked(b.ID, b.BlockedBy, linkBlocking)
}

// relatedLocked combines the existing beans in own with the beans linking to
// id via the inverse kind. Must be called with c.mu held.
func (c *Core) relatedLocked(id string, own []string, inverse linkKind) []*bean.Bean {
	var result []*bean.Bean
	seen := make(map[string]bool)
	for _, tid := range own {
		if t, ok := c.beans[tid]; ok && !seen[tid] {
			seen[tid] = true
			result = append(result, t)
		}
	}
	for _, from := range c.incomingLocked(inverse, id) {
		if !seen[from.ID] {
			seen[from.ID] = true
			result = append(result, from)
		}
	}
	return result
}

// HasBlocking reports whether Blocking(b) is non-empty, without building it.
func (c *Core) HasBlocking(b *bean.Bean) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hasRelatedLocked(b.ID, b.Blocking, linkBlockedBy)
}

// HasBlockedBy reports whether BlockedBy(b) is non-empty, without building
// it.
func (c *Core) HasBlockedBy(b *bean.Bean) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hasRelatedLocked(b.ID, b.BlockedBy, linkBlocking)
}

// hasRelatedLocked reports whether relatedLocked would return any bean.
// Must be called with c.mu held.
func (c *Core) hasRelatedLocked(id string, own []string, inverse linkKind) bool {
	for _, tid := range own {
		if _, ok := c.beans[tid]; ok {
			return true
		}
	}
	// Sources of incoming links are stored beans by construction.
	return c.hasIncomingLocked(inverse, id)
}
