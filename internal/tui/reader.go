package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
)

// Reader mode (f): one bean on the whole screen, as a centered reading
// column without the header, filter bar, tree and field boxes.
//
//	        Two-factor authentication for the login form      title (bold, accent)
//	        t001 · in-progress · feature · high · #auth · in Login overhaul
//	        ────────────────────────────────────────────      rule
//	                                                          (blank)
//	        Collection of improvements to the login flow …    body (markdown)
//	                                                          (blank)
//	        Related                                           relations
//	          parent   ● E e001 Login overhaul
//	          …
//	                                                          (blank)
//	        esc back · j/k scroll · { } prev/next · …   3/9 · 42%
//
// The whole document scrolls (j/k, space, pgup, ^d ^u, gg G, the wheel);
// { } step through the list the reader was opened from (the tree as shown,
// or the Ready/Blocked/Audit list), [ ] and x select and toggle the body's
// checkboxes, tab and enter select and open related beans (backspace goes
// back), and the bean actions (e s t P p b y u) work as in the detail pane;
// pickers open over the reader and return to it. esc, f or q return to the
// previous view with its cursor on the bean shown last. The reader follows
// changes on disk; when its bean is deleted it closes.
//
// App.reader is non-nil while the reader is open. It is not an overlay: it
// replaces the browse view, and overlays (pickers, help) open on top of it.

// readerMaxWidth is the maximum width of the reading column.
const readerMaxWidth = 88

// readerEnsure is what the next sync scrolls into view.
type readerEnsure int

const (
	ensureNothing readerEnsure = iota
	ensureCheckbox
	ensureRelation
)

// readerModel is the state of the open reader.
type readerModel struct {
	id     string // the bean shown
	offset int    // first document line shown

	// The checkbox cursor, shown once [ ] or x was pressed.
	checkCursor int
	checkActive bool
	// The related-bean cursor, shown once tab was pressed.
	relCursor int
	relActive bool
	ensure    readerEnsure

	// IDs of the beans shown before the current one (enter on a related
	// bean), for backspace.
	history []string
	// anchor is the last bean shown that is in the list the reader was
	// opened from: { } continue from it after following relations.
	anchor string

	// Caches: the relations of the bean and its rendered body.
	links      []resolvedLink
	linksStale bool
	body       []string
	bodyChecks []int // body line of each checkbox
	bodyKey    string

	// The document size and page height of the last sync, for scrolling.
	docLen int
	viewH  int
}

// readerDoc is the rendered document of the reader: its lines (each at
// most the column width) and the lines of the checkboxes and relations.
type readerDoc struct {
	lines  []string
	checks []int // document line of each checkbox (-1 = not found)
	rels   []int // document line of each relation
}

// readerGeom is the placement of the reader on the screen.
type readerGeom struct {
	left, col int // column start and width
	top       int // first screen line of the document
	viewH     int // document lines shown
	footerY   int // screen line of the footer
}

func (a *App) readerGeometry() readerGeom {
	margin := 4
	if a.width < 60 {
		margin = 2
	}
	g := readerGeom{col: max(min(readerMaxWidth, a.width-2*margin), 1)}
	g.left = (a.width - g.col) / 2
	gap := 0 // blank lines above the document and above the footer
	if a.height >= 16 {
		gap = 1
	}
	g.top = gap
	g.footerY = a.height - 1
	g.viewH = max(a.height-2*gap-1, 1)
	return g
}

// Opening and closing ---------------------------------------------------------

// openReader opens the reader for the focused bean.
func (a *App) openReader() {
	b := a.focusedBean()
	if b == nil {
		a.list.statusMessage = noBeanReason
		return
	}
	a.reader = &readerModel{}
	a.pendingKey = ""
	a.readerShow(b.ID, false)
}

// readerShow shows the bean id in the reader, from the top. record adds the
// current bean to the history for backspace.
func (a *App) readerShow(id string, record bool) {
	r := a.reader
	history, anchor := r.history, r.anchor
	if record && r.id != "" {
		history = append(history, r.id)
	}
	*r = readerModel{id: id, history: history, anchor: anchor, linksStale: true}
	if slices.Contains(a.readerOrder(), id) {
		r.anchor = id
	}
}

// closeReader returns to the view the reader was opened from, with the
// cursor (and the detail pane, if it had the focus) on the bean shown last.
func (a *App) closeReader() {
	id := a.reader.id
	a.reader = nil
	a.pendingKey = ""
	inView := a.list.byID[id] != nil && a.selectInView(id)
	if a.focus == focusDetail {
		a.detailPinned = ""
		if !inView && a.list.byID[id] != nil {
			a.detailPinned = id
		}
		a.syncDetail()
		return
	}
	if !inView && a.list.byID[id] != nil {
		a.list.statusMessage = a.shortID(id) + " is hidden by the filters"
	}
}

// readerBeansLoaded refreshes the reader after the beans were reloaded and
// closes it if its bean was deleted.
func (a *App) readerBeansLoaded() {
	r := a.reader
	if r == nil {
		return
	}
	if a.list.byID[r.id] == nil {
		id := r.id
		a.closeReader()
		a.list.statusMessage = a.shortID(id) + " was deleted"
		return
	}
	r.linksStale = true
}

// readerOrder returns the bean IDs of the list the reader steps through
// with { }: the active view's beans, or the tree's rows as shown.
func (a *App) readerOrder() []string {
	var ids []string
	if l := a.activeList(); l != nil {
		seen := map[string]bool{}
		for _, row := range l.rows {
			// Rows jumping elsewhere (blockers) reference other beans.
			if row.beanID != "" && row.jumpID == "" && !seen[row.beanID] {
				seen[row.beanID] = true
				ids = append(ids, row.beanID)
			}
		}
		return ids
	}
	for _, row := range a.list.rows {
		ids = append(ids, row.bean.ID)
	}
	return ids
}

// readerStep shows the previous (dir < 0) or next bean of the list.
func (a *App) readerStep(dir int) {
	r := a.reader
	order := a.readerOrder()
	// After following relations out of the list, step from the last bean
	// of the list that was shown.
	i := slices.Index(order, r.id)
	if i < 0 {
		i = slices.Index(order, r.anchor)
	}
	if i < 0 {
		a.list.statusMessage = a.shortID(r.id) + " is not in the list"
		return
	}
	switch next := i + dir; {
	case next < 0:
		a.list.statusMessage = "First bean of the list"
	case next >= len(order):
		a.list.statusMessage = "Last bean of the list"
	default:
		r.history = nil
		a.readerShow(order[next], false)
	}
}

// Keys ------------------------------------------------------------------------

// handleReaderKey handles a key while the reader is open.
func (a *App) handleReaderKey(msg tea.KeyMsg) tea.Cmd {
	r := a.reader
	key := msg.String()
	if first := a.pendingKey; first != "" {
		a.pendingKey = ""
		if first+" "+key == "g g" {
			r.offset = 0
		}
		return nil
	}
	b := a.list.byID[r.id]
	if b == nil {
		a.closeReader()
		return nil
	}
	page := max(r.viewH-1, 1)
	switch key {
	case "esc", "f", "q":
		a.closeReader()
	case "?":
		a.helpOverlay = newHelpOverlayModel(a.width, a.height)
		a.state = viewHelpOverlay
	case "g":
		a.pendingKey = key
	case "up", "k":
		r.scrollBy(-1)
	case "down", "j":
		r.scrollBy(1)
	case " ", "pgdown", "ctrl+f":
		r.scrollBy(page)
	case "pgup", "ctrl+b":
		r.scrollBy(-page)
	case "ctrl+d":
		r.scrollBy(max(r.viewH/2, 1))
	case "ctrl+u":
		r.scrollBy(-max(r.viewH/2, 1))
	case "home":
		r.offset = 0
	case "end", "G":
		r.offset = max(r.docLen-r.viewH, 0)
	case "{":
		a.readerStep(-1)
	case "}":
		a.readerStep(1)
	case "[", "]", "x":
		return a.readerCheckboxKey(b, key)
	case "tab", "shift+tab":
		a.readerMoveRelation(key == "tab")
	case "enter":
		if r.relActive && r.relCursor < len(r.links) {
			a.readerShow(r.links[r.relCursor].bean.ID, true)
		}
	case "backspace":
		if n := len(r.history); n > 0 {
			prev := r.history[n-1]
			r.history = r.history[:n-1]
			a.readerShow(prev, false)
		}
	case "s", "t", "P", "p", "b":
		return openPickerCmd(pickerKeys[key], []string{b.ID}, b)
	case "e":
		return func() tea.Msg { return openEditorMsg{beanID: b.ID, beanPath: b.Path} }
	case "y":
		return func() tea.Msg { return copyBeanIDMsg{ids: []string{b.ID}} }
	case "u":
		a.list.statusMessage = a.undoLast()
		a.reload()
	}
	return nil
}

// scrollBy scrolls the document by delta lines.
func (r *readerModel) scrollBy(delta int) {
	r.offset = max(0, min(r.offset+delta, r.docLen-r.viewH))
}

// readerCheckboxKey handles [ ] (select the previous/next checkbox) and x
// (toggle it). Before a checkbox was selected, ] and x start at the first
// checkbox on screen and [ at the last; x toggles it only if it is shown.
func (a *App) readerCheckboxKey(b *bean.Bean, key string) tea.Cmd {
	r := a.reader
	doc := a.readerDocument(b, a.readerGeometry().col)
	n := len(doc.checks)
	if n == 0 {
		a.list.statusMessage = "No checkboxes in this bean"
		return nil
	}
	shown := func(i int) bool {
		l := doc.checks[i]
		return l >= r.offset && l < r.offset+r.viewH
	}
	if !r.checkActive {
		r.checkActive = true
		r.ensure = ensureCheckbox
		first, last := -1, -1
		for i := range doc.checks {
			if shown(i) {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		switch {
		case key == "[" && last >= 0:
			r.checkCursor = last
		case key == "[":
			r.checkCursor = n - 1
			for i := n - 1; i >= 0 && doc.checks[i] >= r.offset; i-- {
				r.checkCursor = i
			}
		case first >= 0:
			r.checkCursor = first
		default:
			r.checkCursor = 0
			for i := range doc.checks {
				if doc.checks[i] < r.offset {
					r.checkCursor = min(i+1, n-1)
				}
			}
		}
		if key != "x" || first < 0 {
			return nil
		}
	} else {
		switch key {
		case "[":
			r.checkCursor = max(r.checkCursor-1, 0)
		case "]":
			r.checkCursor = min(r.checkCursor+1, n-1)
		}
		r.ensure = ensureCheckbox
		if key != "x" {
			return nil
		}
	}
	msg := toggleCheckboxMsg{id: b.ID, index: r.checkCursor}
	return func() tea.Msg { return msg }
}

// readerMoveRelation moves the related-bean cursor (tab: next, shift+tab:
// previous, wrapping around).
func (a *App) readerMoveRelation(next bool) {
	r := a.reader
	n := len(r.links)
	if n == 0 {
		a.list.statusMessage = "No related beans"
		return
	}
	switch {
	case !r.relActive && next:
		r.relCursor = 0
	case !r.relActive:
		r.relCursor = n - 1
	case next:
		r.relCursor = (r.relCursor + 1) % n
	default:
		r.relCursor = (r.relCursor + n - 1) % n
	}
	r.relActive = true
	r.ensure = ensureRelation
}

// readerHints returns the footer hints of the reader in order of importance.
func (a *App) readerHints() []keyHint {
	r := a.reader
	// The hints refer to the command registry (commands.go).
	hints := []keyHint{cmdHint("reader-back"), cmdHint("reader-scroll"), cmdHint("reader-prev-next")}
	if len(r.history) > 0 {
		hints = append(hints, cmdHint("previous-bean"))
	}
	hints = append(hints, cmdHint("edit"))
	if b := a.list.byID[r.id]; b != nil && len(findCheckboxes(b.Body)) > 0 {
		hints = append(hints, cmdHint("checkbox-toggle"), cmdHint("checkbox-select"))
	}
	if len(r.links) > 0 {
		hints = append(hints, cmdHint("reader-related"))
	}
	return append(hints, cmdHint("reader-page"), cmdHint("status"), cmdHint("copy-id"), cmdHint("help"))
}

// Mouse -----------------------------------------------------------------------

// clickReader handles a click in the reader: a checkbox line toggles the
// checkbox, a relation opens the bean.
func (a *App) clickReader(x, y int) tea.Cmd {
	r := a.reader
	reg, ok := a.hits.find(x, y, hitReaderCheckbox, hitReaderRelation)
	if !ok {
		return nil
	}
	switch reg.kind {
	case hitReaderCheckbox:
		r.checkCursor, r.checkActive = reg.index, true
		msg := toggleCheckboxMsg{id: r.id, index: reg.index}
		return func() tea.Msg { return msg }
	case hitReaderRelation:
		if reg.index < len(r.links) {
			a.readerShow(r.links[reg.index].bean.ID, true)
		}
	}
	return nil
}

// Rendering -------------------------------------------------------------------

// syncReader sizes the reader, refreshes its caches and keeps the selected
// checkbox or relation in view. It runs after every update.
func (a *App) syncReader() {
	r := a.reader
	if r == nil || a.width < minAppWidth || a.height < minAppHeight {
		return
	}
	b := a.list.byID[r.id]
	if b == nil {
		return
	}
	g := a.readerGeometry()
	doc := a.readerDocument(b, g.col)
	r.docLen, r.viewH = len(doc.lines), g.viewH
	line := -1
	switch r.ensure {
	case ensureCheckbox:
		if r.checkCursor < len(doc.checks) {
			line = doc.checks[r.checkCursor]
		}
	case ensureRelation:
		if r.relCursor < len(doc.rels) {
			line = doc.rels[r.relCursor]
		}
	}
	r.ensure = ensureNothing
	if line >= 0 {
		if line < r.offset {
			r.offset = line
		} else if line >= r.offset+g.viewH {
			r.offset = line - g.viewH + 1
		}
	}
	r.scrollBy(0)
}

// readerDocument renders the reader's document for bean b at the column
// width, refreshing the cached relations and body if needed.
func (a *App) readerDocument(b *bean.Bean, col int) readerDoc {
	r := a.reader
	if r.linksStale {
		r.links = resolveAllLinks(a.resolver, a.config, b)
		r.linksStale = false
	}
	checks := findCheckboxes(b.Body)
	r.checkCursor = max(0, min(r.checkCursor, len(checks)-1))
	if len(checks) == 0 {
		r.checkActive = false
	}
	r.relCursor = max(0, min(r.relCursor, len(r.links)-1))
	if len(r.links) == 0 {
		r.relActive = false
	}

	var doc readerDoc
	title := wrapTokens(wordTokens(b.Title, accentStyle().Bold(true)), col, 0, lipgloss.NewStyle())
	if len(title) == 0 {
		title = []string{mutedStyle().Render("(untitled)")}
	}
	doc.lines = append(doc.lines, title...)
	doc.lines = append(doc.lines, a.readerMetaLines(b, col)...)
	doc.lines = append(doc.lines, borderStyle().Render(strings.Repeat(glyphs().Horizontal, col)), "")

	cursor := -1
	if r.checkActive {
		cursor = r.checkCursor
	}
	a.readerBody(b, col, cursor)
	bodyTop := len(doc.lines)
	doc.lines = append(doc.lines, r.body...)
	doc.checks = make([]int, len(r.bodyChecks))
	for i, l := range r.bodyChecks {
		doc.checks[i] = -1
		if l >= 0 {
			doc.checks[i] = bodyTop + l
		}
	}

	if len(r.links) > 0 {
		doc.lines = append(doc.lines, "", mutedStyle().Bold(true).Render("Related"))
		relCursor := -1
		if r.relActive {
			relCursor = r.relCursor
		}
		for _, l := range relationLinesFor(a.config, r.links, relCursor, col) {
			doc.rels = append(doc.rels, len(doc.lines))
			doc.lines = append(doc.lines, l)
		}
	}
	return doc
}

// readerBody renders the body at width with the cursor-th checkbox
// highlighted (-1 = none), if not cached.
func (a *App) readerBody(b *bean.Bean, width, cursor int) {
	r := a.reader
	key := fmt.Sprintf("%d\x00%d\x00%v\x00%d\x00%s", width, lipgloss.ColorProfile(), asciiMode(), cursor, b.Body)
	if key == r.bodyKey {
		return
	}
	r.bodyKey = key
	if strings.TrimSpace(b.Body) == "" {
		r.body, r.bodyChecks = []string{mutedStyle().Render("(no body)")}, nil
		return
	}
	r.body, _ = renderBodyLines(b.Body, width, cursor)
	r.bodyChecks = checkboxLines(b.Body, width, cursor)
}

// readerMetaLines renders the muted metadata of b (short ID, status, type,
// priority, tags, parent, workspace), wrapped between items to width.
func (a *App) readerMetaLines(b *bean.Bean, width int) []string {
	colors := a.config.GetBeanColors(b.Status, b.Type, b.Priority)
	muted := mutedStyle()
	status := fg(colNamed(colors.StatusColor)).Render(b.Status)
	if implicit, _ := a.core.ImplicitStatus(b.ID); implicit != "" {
		status += muted.Render(" " + glyphs().Implicit + implicit)
	}
	parts := []string{muted.Render(a.shortID(b.ID)), status}
	if b.Type != "" {
		parts = append(parts, muted.Render(b.Type))
	}
	prio := b.Priority
	if prio == "" {
		prio = "normal"
	}
	parts = append(parts, muted.Render(prio))
	for _, t := range b.Tags {
		parts = append(parts, muted.Render("#"+t))
	}
	if b.Parent != "" {
		if p, _ := a.resolver.Bean(context.Background(), b.Parent); p != nil {
			parts = append(parts, muted.Render("in "+p.Title))
		}
	}
	if badge := workspaceBadge(a.core, a.ws, b.ID, ""); badge != "" {
		parts = append(parts, badge)
	}

	sep := muted.Render(glyphs().sep())
	sepW := lipgloss.Width(sep)
	var lines []string
	cur, curW := "", 0
	for _, p := range parts {
		p = truncateText(p, width)
		w := lipgloss.Width(p)
		if cur != "" && curW+sepW+w > width {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if cur != "" {
			cur += sep
			curW += sepW
		}
		cur += p
		curW += w
	}
	return append(lines, cur)
}

// readerView renders the reader at the terminal size and records its
// checkboxes and relations.
func (a *App) readerView() string {
	r := a.reader
	b := a.list.byID[r.id]
	lines := make([]string, a.height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", a.width)
	}
	if b == nil {
		return strings.Join(lines, "\n")
	}
	g := a.readerGeometry()
	doc := a.readerDocument(b, g.col)
	rec := a.hits.recorder()
	place := func(y int, s string) {
		lines[y] = strings.Repeat(" ", g.left) + fitWidth(s, a.width-g.left, lipgloss.NewStyle())
	}
	for i := 0; i < g.viewH && r.offset+i < len(doc.lines); i++ {
		place(g.top+i, doc.lines[r.offset+i])
	}
	rec.add(0, 0, a.width, a.height, hitTarget{kind: hitReader})
	record := func(docLines []int, kind hitKind) {
		for i, l := range docLines {
			if y := l - r.offset; l >= 0 && y >= 0 && y < g.viewH {
				rec.add(g.left, g.top+y, g.col, 1, hitTarget{kind: kind, index: i})
			}
		}
	}
	record(doc.checks, hitReaderCheckbox)
	record(doc.rels, hitReaderRelation)
	place(g.footerY, a.readerFooter(g.col, len(doc.lines), g.viewH))
	return strings.Join(lines, "\n")
}

// readerFooter renders the footer: the status message or the key hints,
// and the position in the list and in the document on the right.
func (a *App) readerFooter(width, docLen, viewH int) string {
	r := a.reader
	bottom := min(r.offset+viewH, docLen)
	pos := fmt.Sprintf("%d%%", bottom*100/max(docLen, 1))
	order := a.readerOrder()
	if i := slices.Index(order, r.id); i >= 0 {
		pos = fmt.Sprintf("%d/%d%s%s", i+1, len(order), glyphs().sep(), pos)
	}
	right := mutedStyle().Render(pos)
	room := max(width-lipgloss.Width(right)-2, 1)
	left := ""
	if msg := a.list.statusMessage; msg != "" {
		left = truncateText(fg(colSuccess()).Bold(true).Render(msg), room)
	} else {
		left = renderHints(a.readerHints(), nil, room)
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > width {
		return clampWidth(left, width)
	}
	return left + strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right)) + right
}
