package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
)

// Views of the browse view, switched with 1-4 and shown as tabs in the
// header. Every view keeps the detail pane on the right for the selected
// bean, and the bean actions (s t P p b e y w W u) work in all of them:
//
//	1 Tree     the bean tree (list.go), the default
//	2 Ready    beans ready to work on, ranked like `beans next` (view_ready.go)
//	3 Blocked  blocked beans with their blockers (view_blocked.go)
//	4 Audit    `beans audit` findings, refreshed in the background (view_audit.go)
//
// Ready and Blocked list the beans matching the filter query; the audit
// covers all beans. The active view is kept for the session only.

// appView identifies a view of the browse view.
type appView int

const (
	viewTree appView = iota
	viewReady
	viewBlocked
	viewAudit
)

// viewNames are the tab labels of the views, in key order (1-4).
var viewNames = [...]string{"Tree", "Ready", "Blocked", "Audit"}

// viewForKey returns the view switched to by key.
func viewForKey(key string) (appView, bool) {
	if len(key) == 1 && key[0] >= '1' && key[0] < '1'+byte(len(viewNames)) {
		return appView(key[0] - '1'), true
	}
	return 0, false
}

// viewsState holds the state of the Ready, Blocked and Audit views.
type viewsState struct {
	active appView

	ready   flatList
	blocked flatList
	audit   flatList

	// dataGen counts bean loads; lists remember the generation and the
	// filter query they were built for and are rebuilt when either changes.
	dataGen int

	auditRun auditRunState // see view_audit.go
}

// flatRow is a one-line row of a view's list.
type flatRow struct {
	// key identifies the row across rebuilds, to keep the cursor on it.
	key string
	// beanID is the bean shown in the detail pane while the row is selected;
	// rows without one (headings, notes) are skipped by the cursor.
	beanID string
	// jumpID, if set, is the bean enter jumps to (e.g. a blocker).
	jumpID string
	// render renders the row at the given width.
	render func(width int, selected bool) string
}

// flatList is the scrollable list of rows of a view.
type flatList struct {
	rows   []flatRow
	cursor int
	offset int
	width  int
	height int

	builtGen   int
	builtQuery string
	built      bool

	// title is the pane label; empty is "beans" ("No beans" etc.).
	title string
	// empty is the text shown when there are no rows.
	empty string
}

// setRows replaces the rows, keeping the cursor on the row with the same key
// (or the same bean), else at the same position.
func (l *flatList) setRows(rows []flatRow) {
	prevKey, prevBean := "", ""
	if r, ok := l.current(); ok {
		prevKey, prevBean = r.key, r.beanID
	}
	prev := l.cursor
	l.rows = rows
	l.cursor = -1
	for i, r := range rows {
		if r.key == prevKey && prevKey != "" {
			l.cursor = i
			break
		}
	}
	if l.cursor < 0 && prevBean != "" {
		l.cursor = l.indexOf(prevBean)
	}
	if l.cursor < 0 {
		l.cursor = max(0, min(prev, len(rows)-1))
	}
	l.snap(1)
	l.ensureVisible()
}

// selectable reports whether row i can hold the cursor.
func (l *flatList) selectable(i int) bool {
	return i >= 0 && i < len(l.rows) && l.rows[i].beanID != ""
}

// snap moves the cursor to the nearest selectable row, preferring dir.
func (l *flatList) snap(dir int) {
	if l.selectable(l.cursor) {
		return
	}
	for _, d := range []int{dir, -dir} {
		for i := l.cursor + d; i >= 0 && i < len(l.rows); i += d {
			if l.selectable(i) {
				l.cursor = i
				return
			}
		}
	}
}

// current returns the row under the cursor.
func (l *flatList) current() (flatRow, bool) {
	if !l.selectable(l.cursor) {
		return flatRow{}, false
	}
	return l.rows[l.cursor], true
}

// indexOf returns the first selectable row showing the bean (rows that
// jump elsewhere last), or -1.
func (l *flatList) indexOf(id string) int {
	fallback := -1
	for i, r := range l.rows {
		if r.beanID != id {
			continue
		}
		if r.jumpID == "" {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// selectBean moves the cursor to the bean's row and reports whether it has
// one.
func (l *flatList) selectBean(id string) bool {
	i := l.indexOf(id)
	if i < 0 {
		return false
	}
	l.cursor = i
	l.ensureVisible()
	return true
}

// move moves the cursor by delta selectable rows (at least one row in the
// direction of delta, clamped to the ends).
func (l *flatList) move(delta int) {
	dir := 1
	if delta < 0 {
		dir, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		next := -1
		for i := l.cursor + dir; i >= 0 && i < len(l.rows); i += dir {
			if l.selectable(i) {
				next = i
				break
			}
		}
		if next < 0 {
			break
		}
		l.cursor = next
	}
	l.ensureVisible()
}

// moveToEnd moves the cursor to the first (dir < 0) or last selectable row.
func (l *flatList) moveToEnd(dir int) {
	l.move(dir * len(l.rows))
	if dir < 0 {
		l.offset = 0 // show headings above the first row
	}
	l.ensureVisible()
}

// page returns the number of rows of a page.
func (l *flatList) page() int { return max(l.height-1, 1) }

func (l *flatList) setSize(width, height int) {
	l.width, l.height = width, height
	l.ensureVisible()
}

func (l *flatList) ensureVisible() {
	if l.height <= 0 {
		return
	}
	l.offset = max(0, min(l.offset, len(l.rows)-l.height))
	if l.cursor < l.offset {
		l.offset = l.cursor
		// Keep the heading right above the first row of a group in view.
		if l.offset > 0 && !l.selectable(l.offset-1) {
			l.offset--
		}
	}
	if l.cursor >= l.offset+l.height {
		l.offset = l.cursor - l.height + 1
	}
	l.offset = max(l.offset, 0)
}

// lines renders exactly height lines of width cells.
func (l *flatList) lines(width, height int) []string {
	var out []string
	if len(l.rows) == 0 && l.empty != "" {
		out = append(out, mutedStyle().Render(truncateText(" "+l.empty, width)))
	}
	for i := l.offset; i < len(l.rows) && len(out) < height; i++ {
		out = append(out, fitWidth(l.rows[i].render(width, i == l.cursor), width, lipgloss.NewStyle()))
	}
	for len(out) < height {
		out = append(out, "")
	}
	for i := range out {
		out[i] = fitWidth(out[i], width, lipgloss.NewStyle())
	}
	return out
}

// position returns "n/total" of the selectable rows for the pane border.
func (l *flatList) position() string {
	n, pos := 0, 0
	for i := range l.rows {
		if l.selectable(i) {
			n++
			if i == l.cursor {
				pos = n
			}
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", pos, n)
}

// App integration --------------------------------------------------------------

// activeList returns the list of the active view (nil for the tree).
func (a *App) activeList() *flatList {
	switch a.views.active {
	case viewReady:
		return &a.views.ready
	case viewBlocked:
		return &a.views.blocked
	case viewAudit:
		return &a.views.audit
	}
	return nil
}

// currentBean returns the bean selected in the active view.
func (a *App) currentBean() *bean.Bean {
	l := a.activeList()
	if l == nil {
		return a.list.currentBean()
	}
	if r, ok := l.current(); ok {
		return a.list.byID[r.beanID]
	}
	return nil
}

// viewFilterApplies reports whether the filter query narrows the active
// view's list.
func (a *App) viewFilterApplies() bool {
	return a.views.active != viewAudit
}

// switchView makes v the active view. The focus moves from the detail pane
// to the view's list.
func (a *App) switchView(v appView) {
	if v == a.views.active {
		return
	}
	a.views.active = v
	if a.focus == focusDetail {
		a.focusTree()
	}
	a.detailPinned = ""
	if v == viewAudit {
		a.openAudit()
	}
}

// onBeansLoaded marks the views' lists for a rebuild after a bean load.
func (a *App) onBeansLoaded() {
	a.views.dataGen++
	a.auditBeansChanged()
}

// syncView rebuilds the active view's list if the beans or the filter query
// changed since it was built, and sizes it. It runs after every update.
func (a *App) syncView(width, height int) {
	l := a.activeList()
	if l == nil {
		return
	}
	// The audit ignores the query but its title mentions it.
	query := a.list.filter().String()
	if !l.built || l.builtGen != a.views.dataGen || l.builtQuery != query {
		l.built, l.builtGen, l.builtQuery = true, a.views.dataGen, query
		switch a.views.active {
		case viewReady:
			a.buildReady()
		case viewBlocked:
			a.buildBlocked()
		case viewAudit:
			a.buildAudit()
		}
	}
	l.setSize(width, height)
}

// handleViewKey handles 1-4 (switching views) and, in the Ready, Blocked
// and Audit views, the keys of their list. It reports whether key was
// handled; unhandled keys continue to the focus keys (enter, tab), the
// quick keys and the workspace keys.
func (a *App) handleViewKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	key := msg.String()
	if v, ok := viewForKey(key); ok {
		a.switchView(v)
		return nil, true
	}
	l := a.activeList()
	if l == nil {
		return nil, false
	}
	if a.focus == focusDetail {
		switch key {
		case "/", "H", "c", "C", "r", "K", "J", "R":
			// List-wide keys return to the list, like in the tree.
			a.focusTree()
		default:
			return nil, false
		}
	}
	if a.focus != focusTree {
		return nil, false
	}
	switch key {
	case "up", "k":
		l.move(-1)
	case "down", "j":
		l.move(1)
	case "pgup", "ctrl+b":
		l.move(-l.page())
	case "pgdown", "ctrl+f":
		l.move(l.page())
	case "ctrl+u":
		l.move(-max(l.page()/2, 1))
	case "ctrl+d":
		l.move(max(l.page()/2, 1))
	case "home":
		l.moveToEnd(-1)
	case "end", "G":
		l.moveToEnd(1)
	case "enter":
		r, ok := l.current()
		if !ok || r.jumpID == "" {
			return nil, false // focus the detail pane
		}
		a.jumpToBean(r.jumpID)
	case "n":
		a.nextInView()
	case "/":
		if !a.viewFilterApplies() {
			a.list.statusMessage = "Filters don't apply to the audit"
			return nil, true
		}
		return a.list.startSearch(), true
	case "H":
		if !a.viewFilterApplies() {
			a.list.statusMessage = "Filters don't apply to the audit"
			return nil, true
		}
		a.list.toggleHideDone()
	case "esc", "backspace":
		if a.viewFilterApplies() && a.list.search.Value() != "" {
			a.list.clearFilter()
		}
	case "R":
		if a.views.active != viewAudit {
			return nil, false
		}
		a.rerunAudit()
	case "s", "t", "P", "p", "b", "e", "y":
		// The bean actions of the detail pane, for the selected bean.
		if a.currentBean() == nil {
			return nil, true
		}
		a.syncDetail()
		var cmd tea.Cmd
		a.detail, cmd = a.detail.Update(msg)
		return cmd, true
	case "c", "C", "r", "K", "J", " ", "left", "right", "h", "l":
		a.list.statusMessage = "Creating, renaming, moving, folding and selecting work in the tree (1)"
	default:
		return nil, false
	}
	return nil, true
}

// handleViewSearchKey moves a view's cursor with ↑↓ while the query is being
// typed (the tree is hidden).
func (a *App) handleViewSearchKey(key string) bool {
	l := a.activeList()
	if l == nil || !a.list.searching || a.list.inline != nil {
		return false
	}
	switch key {
	case "up":
		l.move(-1)
	case "down":
		l.move(1)
	default:
		return false
	}
	return true
}

// selectInView moves the active view's cursor to the bean and reports
// whether the view lists it.
func (a *App) selectInView(id string) bool {
	if l := a.activeList(); l != nil {
		return l.selectBean(id)
	}
	return a.list.reveal(id)
}

// jumpToBean selects a bean: on its own row in the active view if it has
// one (not a row referencing it, like a blocker row), otherwise in the tree.
func (a *App) jumpToBean(id string) {
	if l := a.activeList(); l != nil {
		for i, r := range l.rows {
			if r.beanID == id && r.jumpID == "" {
				l.cursor = i
				l.ensureVisible()
				return
			}
		}
	}
	from := a.views.active
	a.switchView(viewTree)
	if a.list.reveal(id) {
		a.list.statusMessage = fmt.Sprintf("%s in the tree"+glyphs().sep()+"%d back to %s", a.shortID(id), int(from)+1, viewNames[from])
	} else {
		a.detailPinned = id
		a.list.statusMessage = fmt.Sprintf("%s is hidden by the filters"+glyphs().sep()+"%d back to %s", a.shortID(id), int(from)+1, viewNames[from])
	}
}

// nextInView handles n outside the tree: the Ready view selects the bean
// `beans next` suggests (its top row unless filtered out); other views
// jump to it in the tree.
func (a *App) nextInView() {
	if a.views.active != viewReady {
		a.switchView(viewTree)
		a.jumpToNext()
		return
	}
	l := a.activeList()
	r, ok := a.nextBean()
	switch {
	case !ok:
		a.list.statusMessage = "No beans ready to work on"
	case l.selectBean(r.ID):
		a.list.statusMessage = fmt.Sprintf("Next: %s %s", a.shortID(r.ID), r.Title)
	default:
		a.list.statusMessage = fmt.Sprintf("Next is %s %s, hidden by the filters (esc clears them)", a.shortID(r.ID), r.Title)
	}
}

// Header tabs and footer hints -------------------------------------------------

// viewTabs renders the view tabs, e.g. "[1 Tree] 2 Ready 3 Blocked 4 Audit".
// Compact tabs label only the active view: "[1 Tree] 2 3 4".
func (a *App) viewTabs(compact bool) string {
	parts := make([]string, len(viewNames))
	for i, name := range viewNames {
		n := fmt.Sprint(i + 1)
		if appView(i) == a.views.active {
			parts[i] = accentStyle().Bold(true).Render("[" + n + " " + name + "]")
			continue
		}
		if compact {
			parts[i] = keyStyle().Render(n)
		} else {
			parts[i] = keyStyle().Render(n) + " " + mutedStyle().Render(name)
		}
	}
	return strings.Join(parts, " ")
}

// headerTabs returns the tabs that fit next to the title of the given
// width while leaving room for the pinned header hints (? help · q quit),
// or "" if none fit.
func (a *App) headerTabs(title string, width int) string {
	pinned := lipgloss.Width(renderHints(nil, []keyHint{{"?", "help"}, {"q", "quit"}}, 0))
	for _, compact := range []bool{false, true} {
		tabs := a.viewTabs(compact)
		if lipgloss.Width(title)+2+lipgloss.Width(tabs)+2+pinned <= width {
			return tabs
		}
	}
	return ""
}

// viewHints returns the footer hints of the active view's list.
func (a *App) viewHints(wide bool) []keyHint {
	if a.list.searching {
		return searchHints()
	}
	detail := "details"
	if !wide {
		detail = "open"
	}
	// The hints refer to the command registry (commands.go).
	var hints []keyHint
	if a.viewFilterApplies() && a.list.search.Value() != "" {
		hints = append(hints, cmdHint("clear-filters"))
	}
	if r, ok := a.activeList().current(); ok && r.jumpID != "" {
		hints = append(hints, cmdHint("focus-detail", "jump to "+a.shortID(r.jumpID)))
	} else {
		hints = append(hints, cmdHint("focus-detail", detail))
	}
	workspace := cmdHint("start-workspace", "workspace")
	switch a.views.active {
	case viewReady:
		hints = append(hints, cmdHint("next"), cmdHint("status"), workspace)
	case viewBlocked:
		hints = append(hints, cmdHint("blocking"), cmdHint("status"))
	case viewAudit:
		hints = append(hints, cmdHint("status"), cmdHint("audit-rerun", "re-run"), cmdHint("parent"), cmdHint("blocking"))
	}
	hints = append(hints,
		cmdHint("edit"),
		cmdHint("type"),
		cmdHint("priority"),
		cmdHint("parent"),
		cmdHint("blocking"),
		cmdHint("undo"),
		cmdHint("views"),
	)
	if a.viewFilterApplies() {
		hints = append(hints, cmdHint("query"), cmdHint("focus-filter"))
	}
	hints = append(hints, workspace, cmdHint("copy-id"))
	return dedupeHints(hints)
}

// dedupeHints drops repeated keys, keeping the first (most important) one.
func dedupeHints(hints []keyHint) []keyHint {
	seen := make(map[string]bool, len(hints))
	out := hints[:0]
	for _, h := range hints {
		if !seen[h.key] {
			seen[h.key] = true
			out = append(out, h)
		}
	}
	return out
}

// Rendering ----------------------------------------------------------------------

// viewPaneLines renders the active view's list pane of the given outer size.
func (a *App) viewPaneLines(l browseLayout, rec hitRecorder) []string {
	fl := a.activeList()
	var rows []string
	if a.viewFilterApplies() && a.list.showSearchLine() {
		rows = append(rows, a.list.searchLine(l.treeW-2))
	}
	rec.add(0, 0, l.treeW, l.paneH, hitTarget{kind: hitTreePane})
	height := max(l.paneH-2-len(rows), 0)
	for k := 0; k < height && fl.offset+k < len(fl.rows); k++ {
		rec.add(1, 1+len(rows)+k, max(l.treeW-2, 1), 1, hitTarget{kind: hitViewRow, index: fl.offset + k})
	}
	rows = append(rows, fl.lines(max(l.treeW-2, 1), height)...)
	return box{
		label:    fl.title,
		botRight: mutedStyle().Render(fl.position()),
		lines:    rows,
		width:    l.treeW,
		height:   l.paneH,
		focused:  a.focus == focusTree,
	}.render()
}

// viewRowsHeight returns the number of row lines of the active view's pane.
func (a *App) viewRowsHeight(paneH int) int {
	h := paneH - 2
	if a.viewFilterApplies() && a.list.showSearchLine() {
		h--
	}
	return max(h, 1)
}

// filterTitle returns a view's pane label: name, counts and the active
// filters.
func (a *App) filterTitle(name string, shown, total int) string {
	q := a.list.filter()
	if q.IsEmpty() {
		return fmt.Sprintf("%s %d", name, total)
	}
	parts := append([]string{fmt.Sprintf("%s %d/%d", name, shown, total)}, filterSummary(q)...)
	return strings.Join(parts, glyphs().sep())
}
