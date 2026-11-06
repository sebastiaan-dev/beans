package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
)

// The Ready view (2): the beans ready to work on, ranked like `beans next`;
// the top row is what n jumps to. Each row shows the rank, priority, type
// and the bean's parents as a dim breadcrumb.

// readyRanking returns the beans ready to work on, most important first,
// with the filter and ranking of `beans next` (see beangraph.NextBean).
func (a *App) readyRanking() ([]*bean.Bean, error) {
	ctx := context.Background()
	candidates, err := a.resolver.Beans(ctx, beangraph.ReadyFilter())
	if err != nil {
		return nil, err
	}
	all, err := a.resolver.Beans(ctx, nil)
	if err != nil {
		return nil, err
	}
	var priorityNames []string
	if cfg := a.core.Config(); cfg != nil {
		priorityNames = cfg.PriorityNames()
	}
	beangraph.RankNextCandidates(candidates, priorityNames, beangraph.OpenChildCounts(all))
	return candidates, nil
}

// nextBean returns the bean `beans next` suggests.
func (a *App) nextBean() (*bean.Bean, bool) {
	ranked, err := a.readyRanking()
	if err != nil || len(ranked) == 0 {
		return nil, false
	}
	return ranked[0], true
}

// buildReady rebuilds the Ready view's rows.
func (a *App) buildReady() {
	l := &a.views.ready
	ranked, err := a.readyRanking()
	if err != nil {
		l.title, l.empty = "Ready", fmt.Sprintf("Error: %v", err)
		l.setRows(nil)
		return
	}
	rank := make(map[string]int, len(ranked))
	for i, b := range ranked {
		rank[b.ID] = i + 1
	}
	q := a.list.filter()
	shown := filterquery.Filter(ranked, q, a.list.qctx)

	l.title = a.filterTitle("Ready", len(shown), len(ranked))
	switch {
	case len(ranked) == 0:
		l.empty = "Nothing ready: no unblocked todo beans."
	case len(shown) == 0:
		l.empty = "No ready beans match. Press esc to clear."
	default:
		l.empty = ""
	}

	cols := a.readyColumns(len(ranked))
	rows := make([]flatRow, 0, len(shown))
	for _, b := range shown {
		if cur := a.list.byID[b.ID]; cur != nil {
			b = cur
		}
		b, n := b, rank[b.ID]
		rows = append(rows, flatRow{
			key:    b.ID,
			beanID: b.ID,
			render: func(width int, selected bool) string { return a.readyRow(b, n, cols, width, selected) },
		})
	}
	l.setRows(rows)
}

// readyColumns are the widths of the Ready view's columns.
type readyColumns struct {
	rank, priority, typ int
}

func (a *App) readyColumns(n int) readyColumns {
	c := readyColumns{rank: len(fmt.Sprint(max(n, 1)))}
	for _, p := range a.config.PriorityNames() {
		c.priority = max(c.priority, len(p))
	}
	for _, t := range a.config.TypeNames() {
		c.typ = max(c.typ, len(t))
	}
	return c
}

// readyRowWideMin is the minimum list width showing priority and type
// names; narrower lists show their glyphs.
const readyRowWideMin = 60

// readyRow renders a row of the Ready view:
//
//	▌ 1  ‼ critical  task     t005 Export as CSV  Release 1.0 › Export
func (a *App) readyRow(b *bean.Bean, rank int, cols readyColumns, width int, selected bool) string {
	r := newRowStyler(selected)
	sp := r.plain().Render(" ")
	colors := a.config.GetBeanColors(b.Status, b.Type, b.Priority)
	priority := b.Priority
	if priority == "" {
		priority = "normal"
	}
	ps := r.st(fg(colNamed(colors.PriorityColor)))
	if priority == "critical" || priority == "high" {
		ps = ps.Bold(true)
	}
	// Pad to the widest symbol so columns line up in both glyph sets.
	symWidth := 1
	for _, g := range glyphs().Priority {
		symWidth = max(symWidth, lipgloss.Width(g))
	}
	sym := padRight(glyphs().prioritySymbol(priority), symWidth)
	ts := r.st(fg(colNamed(colors.TypeColor)))

	var s strings.Builder
	s.WriteString(r.bar())
	s.WriteString(r.muted().Render(fmt.Sprintf("%*d", cols.rank, rank)) + sp + sp)
	if width >= readyRowWideMin {
		s.WriteString(ps.Render(sym+" "+padRight(priority, cols.priority)) + sp + sp)
		s.WriteString(ts.Render(padRight(b.Type, cols.typ)) + sp + sp)
	} else {
		s.WriteString(ps.Render(sym) + sp)
		s.WriteString(ts.Render(glyph(b.Type, ui.ShortType)) + sp)
	}
	s.WriteString(r.muted().Render(a.shortID(b.ID)) + sp + r.title(b.Title))
	if crumb := a.breadcrumb(b); crumb != "" {
		s.WriteString(sp + sp + r.muted().Render(crumb))
	}
	return rowLine(s.String(), width, r)
}

// breadcrumb returns the titles of the bean's ancestors, outermost first:
// "Release 1.0 › Export".
func (a *App) breadcrumb(b *bean.Bean) string {
	var titles []string
	seen := map[string]bool{b.ID: true}
	for p := a.list.byID[b.Parent]; p != nil && !seen[p.ID]; p = a.list.byID[p.Parent] {
		seen[p.ID] = true
		titles = append([]string{p.Title}, titles...)
	}
	return strings.Join(titles, " "+glyphs().Crumb+" ")
}

// padRight pads s with spaces to n cells.
func padRight(s string, n int) string {
	if gap := n - len([]rune(s)); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
