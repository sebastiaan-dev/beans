package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
)

// The Audit view (4): the findings of `beans audit` (Core.RunAudit) grouped
// by kind. The audit runs in the background when the view is opened and,
// debounced, when beans change while it is open (or when it is opened after
// changes); R runs it again. The git check (landed-but-open) only runs in git
// repositories. A finding row selects the bean the finding is about; its
// sub-rows select the other beans involved (children, blockers). The filter
// query does not apply to the audit.

// auditDebounce is the delay between a bean change and re-running the audit,
// so that a burst of changes runs it once.
var auditDebounce = 500 * time.Millisecond

// auditClock returns the reference time of an audit (replaced in tests).
var auditClock = time.Now

// auditRunState tracks the background audit runs.
type auditRunState struct {
	report *beancore.AuditReport
	err    error
	ranAt  time.Time

	ranOnce   bool // a run has finished
	running   bool // a run is in progress
	dirty     bool // beans changed since the last run started
	immediate bool // run without debouncing (view opened, R, debounce done)
	ticking   bool // a debounce tick is pending
	gen       int  // debounce generation; older ticks are ignored
}

// auditDoneMsg delivers the result of a background audit run.
type auditDoneMsg struct {
	report beancore.AuditReport
	err    error
	at     time.Time
}

// auditTickMsg ends a debounce delay.
type auditTickMsg struct{ gen int }

// openAudit runs the audit when the view opens unless its result is current.
func (a *App) openAudit() {
	s := &a.views.auditRun
	if !s.ranOnce || s.dirty {
		s.immediate = true
	}
}

// rerunAudit runs the audit again now (R).
func (a *App) rerunAudit() {
	s := &a.views.auditRun
	s.dirty, s.immediate = true, true
	a.list.statusMessage = "Running the audit"+glyphs().Loading
}

// auditBeansChanged marks the audit result as outdated after beans changed.
func (a *App) auditBeansChanged() {
	s := &a.views.auditRun
	if s.ranOnce || s.running {
		s.dirty = true
	}
	a.views.audit.built = false // bean titles and statuses may have changed
}

// viewsCmd returns the command starting or scheduling an audit run, if one
// is due. It runs after every update.
func (a *App) viewsCmd() tea.Cmd {
	s := &a.views.auditRun
	if a.views.active != viewAudit || s.running || (s.ranOnce && !s.dirty) {
		return nil
	}
	if s.immediate || !s.ranOnce {
		s.immediate = false
		return a.startAudit()
	}
	if s.ticking {
		return nil
	}
	s.ticking = true
	s.gen++
	gen := s.gen
	return tea.Tick(auditDebounce, func(time.Time) tea.Msg { return auditTickMsg{gen: gen} })
}

// startAudit starts a background audit run of a snapshot of the beans.
// Beans are copied here, on the UI goroutine, because mutations modify the
// core's beans in place.
func (a *App) startAudit() tea.Cmd {
	s := &a.views.auditRun
	s.running, s.dirty = true, false
	a.views.audit.built = false

	beans := a.core.All()
	snapshot := make([]*bean.Bean, len(beans))
	for i, b := range beans {
		snapshot[i] = copyBean(b)
	}
	core := a.core
	p := beancore.AuditRunParams{
		BookkeepingPaths: []string{core.Root()},
		StaleDays:        beancore.DefaultAuditStaleDays,
		Now:              auditClock(),
		Beans:            snapshot,
	}
	if a.config != nil {
		p.BaseRef = a.config.GetWorktreeBaseRef()
		p.BookkeepingPaths = append(p.BookkeepingPaths, a.config.ConfigPath())
	}
	return func() tea.Msg {
		report, err := core.RunAudit(p)
		return auditDoneMsg{report: report, err: err, at: p.Now}
	}
}

// handleViewMsg handles the messages of the views and reports whether msg
// was one of them.
func (a *App) handleViewMsg(msg tea.Msg) bool {
	s := &a.views.auditRun
	switch msg := msg.(type) {
	case auditDoneMsg:
		s.running, s.ranOnce = false, true
		s.report, s.err, s.ranAt = &msg.report, msg.err, msg.at
		a.views.audit.built = false
	case auditTickMsg:
		s.ticking = false
		if msg.gen == s.gen {
			s.immediate = true
		}
	default:
		return false
	}
	return true
}

// buildAudit rebuilds the Audit view's rows from the last result.
func (a *App) buildAudit() {
	l := &a.views.audit
	s := &a.views.auditRun
	var rows []flatRow
	info := func(key, text string) {
		rows = append(rows, flatRow{key: key, render: func(width int, _ bool) string { return truncateText(" "+text, width) }})
	}

	title := []string{"Audit"}
	switch {
	case s.running:
		title = append(title, "running"+glyphs().Loading)
	case s.dirty:
		title = append(title, "outdated")
	}
	if s.report != nil {
		title = append(title, pluralize(s.report.Count, "finding", "findings"))
	}
	if !a.list.filter().IsEmpty() {
		title = append(title, "filters not applied")
	}
	l.title = strings.Join(title, glyphs().sep())
	l.empty = ""

	if s.report == nil {
		if s.err != nil {
			info("error", fg(colDanger()).Render("Error: "+s.err.Error()))
		} else {
			info("running", mutedStyle().Render("Running the audit"+glyphs().Loading))
		}
		l.setRows(rows)
		return
	}
	r := s.report

	if r.Clean {
		info("clean", fg(colSuccess()).Bold(true).Render(glyphs().Check+" No audit findings")+mutedStyle().Render(glyphs().sep()+"beans look up to date"))
	}
	info("summary", mutedStyle().Render(auditSummary(r, s.ranAt)))
	if s.err != nil {
		info("error", fg(colDanger()).Render("Error: "+s.err.Error()))
	}
	for i, n := range r.Notes {
		info(fmt.Sprint("note", i), fg(colWarning()).Render("! "+n))
	}

	byKind := make(map[beancore.AuditKind][]beancore.AuditFinding)
	for _, f := range r.Findings {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}
	for _, kind := range beancore.AuditKinds {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		heading := accentStyle().Bold(true).Render(fmt.Sprintf("%s (%d)", beancore.AuditKindTitles[kind], len(group))) +
			mutedStyle().Render(" ["+string(kind)+"]")
		info("kind\x00"+string(kind)+"\x00gap", "")
		info("kind\x00"+string(kind), heading)
		for _, f := range group {
			rows = append(rows, a.findingRows(f)...)
		}
	}
	l.setRows(rows)
}

// auditSummary describes a run: "Audited 14:03:05 · 120 commits on main (4
// bean-only ignored)".
func auditSummary(r *beancore.AuditReport, at time.Time) string {
	parts := []string{"Audited " + at.Format("15:04:05")}
	if g := r.Git; g != nil {
		scan := fmt.Sprintf("%s on %s", pluralize(g.CommitsScanned, "commit", "commits"), g.BaseRef)
		if g.BookkeepingCommits > 0 {
			scan += fmt.Sprintf(" (%d bean-only ignored)", g.BookkeepingCommits)
		}
		parts = append(parts, scan)
	}
	return strings.Join(parts, glyphs().sep())
}

// findingRows returns the rows of a finding: the finding (selecting its
// bean) and the other beans or commits involved.
func (a *App) findingRows(f beancore.AuditFinding) []flatRow {
	id := f.BeanIDs[0]
	key := "f\x00" + string(f.Kind) + "\x00" + id
	rows := []flatRow{{
		key:    key,
		beanID: id,
		render: func(width int, selected bool) string { return a.findingRow(f, width, selected) },
	}}
	sub := func(subKey, beanID string, render func(r rowStyler) string) {
		rows = append(rows, flatRow{
			key:    key + "\x00" + subKey,
			beanID: beanID,
			render: func(width int, selected bool) string {
				r := newRowStyler(selected)
				return rowLine(r.bar()+r.plain().Render("      ")+render(r), width, r)
			},
		})
	}
	related := func(label string, ids []string) {
		for _, rid := range ids {
			b := a.list.byID[rid]
			if b == nil {
				continue
			}
			sub(rid, rid, func(r rowStyler) string {
				return r.muted().Render(glyphs().SubItem+" "+label+" ") + a.beanRef(b, r) + r.muted().Render(glyphs().sep()+b.Status)
			})
		}
	}
	switch f.Kind {
	case beancore.AuditLandedButOpen:
		const maxCommits = 3
		for i, c := range f.Details.Commits[:min(len(f.Details.Commits), maxCommits)] {
			sub(fmt.Sprint("c", i), id, func(r rowStyler) string {
				return r.muted().Render(glyphs().SubItem+" ") + r.st(fg(colWarning())).Render(c.ShortHash) + r.plain().Render(" "+c.Subject)
			})
		}
		if more := f.Details.CommitCount - maxCommits; more > 0 {
			sub("more", id, func(r rowStyler) string { return r.muted().Render(fmt.Sprintf("%s +%d more", glyphs().SubItem, more)) })
		}
	case beancore.AuditContainerDoneChildrenOpen:
		related("open child", f.Details.OpenChildren)
	case beancore.AuditContainerReadyToClose:
		related("done child", f.Details.ClosedChildren)
	case beancore.AuditBlockedByDone:
		var ids []string
		for _, bl := range f.Details.Blockers {
			if !slices.Contains(ids, bl.ID) {
				ids = append(ids, bl.ID)
			}
		}
		related("done blocker", ids)
	}
	return rows
}

// findingRow renders a finding: "▌ ✗ S T id title — reason".
func (a *App) findingRow(f beancore.AuditFinding, width int, selected bool) string {
	r := newRowStyler(selected)
	id := f.BeanIDs[0]
	ref := r.muted().Render(a.shortID(id)) + r.plain().Render(" ") + r.title(f.Details.Title)
	if b := a.list.byID[id]; b != nil {
		ref = a.beanRef(b, r)
	}
	return rowLine(r.bar()+r.plain().Render("  ")+r.st(fg(colDanger())).Render(glyphs().Cross)+r.plain().Render(" ")+ref+r.muted().Render(" "+glyphs().Dash+" "+findingReason(f)), width, r)
}

// findingReason summarizes why a finding was reported.
func findingReason(f beancore.AuditFinding) string {
	d := f.Details
	switch f.Kind {
	case beancore.AuditLandedButOpen:
		if len(d.Commits) == 0 {
			return d.Status + " but referenced on the base branch"
		}
		s := fmt.Sprintf("%s but referenced by %s", d.Status, d.Commits[0].ShortHash)
		if d.CommitCount > 1 {
			s += fmt.Sprintf(" (+%d)", d.CommitCount-1)
		}
		return s
	case beancore.AuditContainerDoneChildrenOpen:
		return fmt.Sprintf("%s with %s open", d.Status, pluralize(len(d.OpenChildren), "child", "children"))
	case beancore.AuditContainerReadyToClose:
		if len(d.ClosedChildren) == 1 {
			return d.Status + ", its only child is done"
		}
		return fmt.Sprintf("%s, all %d children done", d.Status, len(d.ClosedChildren))
	case beancore.AuditStaleInProgress:
		return fmt.Sprintf("in-progress, no updates for %s", pluralize(d.IdleDays, "day", "days"))
	case beancore.AuditBlockedByDone:
		return "still linked as blocked, all blockers done"
	}
	return f.Message
}
