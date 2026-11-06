package tui

import (
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
)

// The Blocked view (3): open beans that are blocked, explicitly (by their
// own blockers) or implicitly (an ancestor is blocked), each followed by its
// active blockers. Selecting a blocker row shows the blocker; enter jumps to
// it (to its own row if it is blocked too, otherwise to the tree).

// blockerInfo is an active blocker of a bean.
type blockerInfo struct {
	blocker *bean.Bean
	// via is the blocked ancestor the bean inherits the blocker from (nil
	// for the bean's own blockers).
	via *bean.Bean
}

// blockersOf returns the active blockers of the bean and of its ancestors,
// mirroring beancore's IsBlocked.
func (a *App) blockersOf(b *bean.Bean) []blockerInfo {
	var out []blockerInfo
	seen := make(map[string]bool)
	add := func(owner, via *bean.Bean) {
		for _, bl := range a.core.FindActiveBlockers(owner.ID) {
			if seen[bl.ID] {
				continue
			}
			seen[bl.ID] = true
			if cur := a.list.byID[bl.ID]; cur != nil {
				bl = cur
			}
			out = append(out, blockerInfo{blocker: bl, via: via})
		}
	}
	add(b, nil)
	visited := map[string]bool{b.ID: true}
	for p := a.list.byID[b.Parent]; p != nil && !visited[p.ID]; p = a.list.byID[p.Parent] {
		visited[p.ID] = true
		add(p, p)
	}
	return out
}

// isResolved reports whether a status means the bean is done.
func isResolved(status string) bool {
	return status == "completed" || status == "scrapped"
}

// buildBlocked rebuilds the Blocked view's rows.
func (a *App) buildBlocked() {
	l := &a.views.blocked
	type blocked struct {
		bean     *bean.Bean
		blockers []blockerInfo
	}
	var all []*bean.Bean
	blockers := make(map[string][]blockerInfo)
	for _, b := range a.list.beans {
		if isResolved(b.Status) {
			continue
		}
		if bs := a.blockersOf(b); len(bs) > 0 {
			all = append(all, b)
			blockers[b.ID] = bs
		}
	}
	a.sortBeans(all)
	shown := filterquery.Filter(all, a.list.filter(), a.list.qctx)

	l.title = a.filterTitle("Blocked", len(shown), len(all))
	switch {
	case len(all) == 0:
		l.empty = "Nothing is blocked."
	case len(shown) == 0:
		l.empty = "No blocked beans match. Press esc to clear."
	default:
		l.empty = ""
	}

	var rows []flatRow
	for _, b := range shown {
		bs := blockers[b.ID]
		rows = append(rows, flatRow{
			key:    "b\x00" + b.ID,
			beanID: b.ID,
			render: func(width int, selected bool) string { return a.blockedRow(b, bs, width, selected) },
		})
		for _, info := range bs {
			rows = append(rows, flatRow{
				key:    "x\x00" + b.ID + "\x00" + info.blocker.ID,
				beanID: info.blocker.ID,
				jumpID: info.blocker.ID,
				render: func(width int, selected bool) string { return a.blockerRow(info, width, selected) },
			})
		}
	}
	l.setRows(rows)
}

// blockedRow renders a blocked bean: "▌S T id title · 2 blockers", or
// "· implicitly, via e001" if it is only blocked through an ancestor.
func (a *App) blockedRow(b *bean.Bean, bs []blockerInfo, width int, selected bool) string {
	r := newRowStyler(selected)
	note := pluralize(len(bs), "blocker", "blockers")
	if bs[0].via != nil {
		// The own blockers come first, so all blockers are inherited.
		note = "implicitly, via " + a.shortID(bs[0].via.ID)
	}
	return rowLine(r.bar()+a.beanRef(b, r)+r.muted().Render(" "+glyphs().sep()+note), width, r)
}

// blockerRow renders a blocker beneath the bean it blocks:
//
//	↳ blocked by I F t001 Two-factor authentication · in-progress
//	↳ via e001: I F t001 Two-factor authentication · in-progress
func (a *App) blockerRow(info blockerInfo, width int, selected bool) string {
	r := newRowStyler(selected)
	label := glyphs().SubItem + " blocked by "
	if info.via != nil {
		label = glyphs().SubItem + " via " + a.shortID(info.via.ID) + ": "
	}
	b := info.blocker
	return rowLine(r.bar()+r.plain().Render("    ")+r.muted().Render(label)+a.beanRef(b, r)+r.muted().Render(glyphs().sep()+b.Status), width, r)
}
