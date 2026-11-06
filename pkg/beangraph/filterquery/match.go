package filterquery

import (
	"slices"
	"strings"

	"github.com/hmans/beans/pkg/bean"
)

// Context holds the lookups needed to evaluate relational clauses (parent,
// blocked, ready, done). Build it from all known beans, including archived
// ones and beans not being filtered.
type Context struct {
	byID map[string]*bean.Bean
	// incomingBlockers holds, for each bean ID, the beans that list it in
	// their Blocking.
	incomingBlockers map[string][]*bean.Bean
	order            []*bean.Bean
}

// NewContext creates a context for beans.
func NewContext(beans []*bean.Bean) *Context {
	ctx := &Context{
		byID:             make(map[string]*bean.Bean, len(beans)),
		incomingBlockers: make(map[string][]*bean.Bean),
		order:            beans,
	}
	for _, b := range beans {
		ctx.byID[b.ID] = b
		for _, id := range b.Blocking {
			ctx.incomingBlockers[id] = append(ctx.incomingBlockers[id], b)
		}
	}
	return ctx
}

// ancestors calls fn for each ancestor of b (excluding b), nearest first,
// stopping at cycles, missing beans or when fn returns false.
func (c *Context) ancestors(b *bean.Bean, fn func(*bean.Bean) bool) {
	seen := map[string]bool{b.ID: true}
	id := b.Parent
	for id != "" && !seen[id] {
		p := c.byID[id]
		if p == nil {
			return
		}
		if !fn(p) {
			return
		}
		seen[id] = true
		id = p.Parent
	}
}

// IsResolvedStatus reports whether a status is terminal (completed or scrapped).
func IsResolvedStatus(status string) bool {
	return status == "completed" || status == "scrapped"
}

func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// RefersTo reports whether ref (an ID, short ID, slug or title,
// case-insensitive) identifies b.
func RefersTo(b *bean.Bean, ref string) bool {
	r := strings.ToLower(ref)
	return strings.ToLower(b.ID) == r ||
		strings.ToLower(shortID(b.ID)) == r ||
		(b.Slug != "" && strings.ToLower(b.Slug) == r) ||
		strings.ToLower(b.Title) == r
}

// MatchesText reports whether one free-text term occurs (case-insensitive)
// in the bean's title, type, status, ID or tags.
func MatchesText(b *bean.Bean, text string) bool {
	if text == "" {
		return true
	}
	lower := strings.ToLower(text)
	contains := func(s string) bool { return strings.Contains(strings.ToLower(s), lower) }
	return contains(b.Title) || contains(b.Type) || contains(b.Status) || contains(b.ID) ||
		slices.ContainsFunc(b.Tags, contains)
}

func priorityRank(priority string) int {
	if priority == "" {
		priority = defaultPriority
	}
	if i := slices.Index(Priorities, priority); i >= 0 {
		return i
	}
	return len(Priorities)
}

func matchesPriority(b *bean.Bean, t Term) bool {
	priority := b.Priority
	if priority == "" {
		priority = defaultPriority
	}
	if t.Op == OpEq || t.Op == "" {
		return slices.Contains(t.Values, priority)
	}
	rank := priorityRank(priority)
	for _, v := range t.Values {
		target := slices.Index(Priorities, v)
		if target < 0 {
			continue
		}
		// Lower rank = more urgent, so ">= high" means rank <= rank(high).
		var ok bool
		switch t.Op {
		case OpGE:
			ok = rank <= target
		case OpGT:
			ok = rank < target
		case OpLE:
			ok = rank >= target
		case OpLT:
			ok = rank > target
		}
		if ok {
			return true
		}
	}
	return false
}

// Predicate reports whether a bean matches a compiled query.
type Predicate func(*bean.Bean) bool

// matcher evaluates relational properties with memoization.
type matcher struct {
	ctx      *Context
	blocked  map[string]bool
	implicit map[string]bool
}

func (m *matcher) isActiveBlocker(b *bean.Bean) bool {
	return b != nil && !IsResolvedStatus(b.Status)
}

func (m *matcher) hasDirectBlockers(b *bean.Bean) bool {
	for _, id := range b.BlockedBy {
		if m.isActiveBlocker(m.ctx.byID[id]) {
			return true
		}
	}
	return slices.ContainsFunc(m.ctx.incomingBlockers[b.ID], m.isActiveBlocker)
}

// isBlocked mirrors beancore.IsBlocked: explicitly blocked, or an ancestor is.
func (m *matcher) isBlocked(b *bean.Bean) bool {
	if v, ok := m.blocked[b.ID]; ok {
		return v
	}
	v := m.hasDirectBlockers(b)
	if !v {
		m.ctx.ancestors(b, func(a *bean.Bean) bool {
			v = m.hasDirectBlockers(a)
			return !v
		})
	}
	m.blocked[b.ID] = v
	return v
}

// hasResolvedAncestor mirrors beancore.ImplicitStatus != "".
func (m *matcher) hasResolvedAncestor(b *bean.Bean) bool {
	if v, ok := m.implicit[b.ID]; ok {
		return v
	}
	v := false
	m.ctx.ancestors(b, func(a *bean.Bean) bool {
		v = IsResolvedStatus(a.Status)
		return !v
	})
	m.implicit[b.ID] = v
	return v
}

// IsReady mirrors `beans list --ready`: not blocked (explicitly or via an
// ancestor), not draft/in-progress/completed/scrapped and no completed or
// scrapped ancestor.
func (m *matcher) isReady(b *bean.Bean) bool {
	switch b.Status {
	case "draft", "in-progress", "completed", "scrapped":
		return false
	}
	return !m.hasResolvedAncestor(b) && !m.isBlocked(b)
}

func (m *matcher) isDone(b *bean.Bean) bool {
	return IsResolvedStatus(b.Status) || m.hasResolvedAncestor(b)
}

func matchesBool(t Term, actual bool) bool {
	for _, v := range t.Values {
		if want, ok := boolValues[v]; ok && want == actual {
			return true
		}
	}
	return false
}

// Compile compiles a query into a predicate. Relational lookups (parent
// references, blocked state) are resolved once per compiled query and
// memoized, so the predicate must not be used after the context's beans
// change.
func Compile(q Query, ctx *Context) Predicate {
	clauses := q.ActiveClauses()
	texts := q.Texts()
	if len(clauses) == 0 && len(texts) == 0 {
		return func(*bean.Bean) bool { return true }
	}
	if ctx == nil {
		ctx = NewContext(nil)
	}
	m := &matcher{ctx: ctx, blocked: map[string]bool{}, implicit: map[string]bool{}}

	// Resolve parent references to the set of bean IDs they identify.
	refTargets := make([]map[string]bool, len(clauses))
	for i, c := range clauses {
		if !c.Field.IsParentField() {
			continue
		}
		targets := map[string]bool{}
		for _, b := range ctx.order {
			if c.Field != FieldParent && b.Type != string(c.Field) {
				continue
			}
			if slices.ContainsFunc(c.Values, func(ref string) bool { return RefersTo(b, ref) }) {
				targets[b.ID] = true
			}
		}
		refTargets[i] = targets
	}

	matchesClause := func(b *bean.Bean, i int) bool {
		c := clauses[i]
		switch c.Field {
		case FieldStatus:
			return slices.Contains(c.Values, b.Status)
		case FieldType:
			return slices.Contains(c.Values, b.Type)
		case FieldPriority:
			return matchesPriority(b, c)
		case FieldTag:
			return slices.ContainsFunc(b.Tags, func(tag string) bool {
				return slices.Contains(c.Values, strings.ToLower(tag))
			})
		case FieldBlocked:
			return matchesBool(c, m.isBlocked(b))
		case FieldReady:
			return matchesBool(c, m.isReady(b))
		case FieldDone:
			return matchesBool(c, m.isDone(b))
		case FieldArchived:
			return matchesBool(c, b.IsArchived())
		case FieldParent, FieldMilestone, FieldEpic:
			targets := refTargets[i]
			found := false
			if len(targets) > 0 {
				ctx.ancestors(b, func(a *bean.Bean) bool {
					found = targets[a.ID]
					return !found
				})
			}
			return found
		}
		return false
	}

	return func(b *bean.Bean) bool {
		for i, c := range clauses {
			if matchesClause(b, i) == c.Negate {
				return false
			}
		}
		for _, t := range texts {
			if !MatchesText(b, t) {
				return false
			}
		}
		return true
	}
}

// Filter returns the beans matching the query, preserving order. ctx should
// be built from all beans (see NewContext); if nil, it is built from beans.
func Filter(beans []*bean.Bean, q Query, ctx *Context) []*bean.Bean {
	if q.IsEmpty() {
		return beans
	}
	if ctx == nil {
		ctx = NewContext(beans)
	}
	match := Compile(q, ctx)
	out := make([]*bean.Bean, 0, len(beans))
	for _, b := range beans {
		if match(b) {
			out = append(out, b)
		}
	}
	return out
}
