package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mouse support. Clicks and the wheel act on the regions recorded while the
// last screen was rendered (layout.go):
//
//   - tree: click a row selects it, click its fold chevron toggles the fold,
//     a double click opens the detail pane; the wheel scrolls the tree
//   - detail: click a relation opens it, a field box with a key hint runs
//     that key's action (the borders of the body and relations boxes run
//     e and b), click the body focuses it and a checkbox line toggles the
//     checkbox; the wheel scrolls the body
//   - filter bar: click a field focuses it and opens its dropdown (or flips
//     a toggle); click an option toggles or chooses it; the wheel scrolls
//     the dropdown; a click elsewhere closes it
//   - overlays (overlay.go): click an item chooses it (the blocking picker
//     toggles it), a click outside closes the overlay, the wheel moves the
//     cursor (or scrolls the help)
//
// Mouse reporting is on by default; BEANS_TUI_MOUSE=0 turns it off. While it
// is on, terminals select text only with Shift (or Option on macOS) held.

// doubleClickInterval is the maximum time between the clicks of a double
// click.
const doubleClickInterval = 400 * time.Millisecond

// Wheel steps: tree rows and body lines per wheel event.
const (
	wheelTreeRows  = 3
	wheelBodyLines = 3
)

// mouseNow returns the current time (replaced in tests).
var mouseNow = time.Now

// mouseState is the state of the mouse handling between events.
type mouseState struct {
	lastRowID string    // bean of the last clicked tree row
	lastAt    time.Time // time of that click
}

// mouseEnabled reports whether the TUI enables mouse reporting:
// BEANS_TUI_MOUSE=0 (or false/no/off) turns it off.
func mouseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BEANS_TUI_MOUSE"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// programOptions returns the options of the TUI program.
func programOptions(mouse bool) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	return opts
}

// restoreMouse re-enables mouse reporting after the TUI was suspended for
// another program (the editor, a workspace agent): Bubble Tea turns it off
// when releasing the terminal but does not turn it back on.
func (a *App) restoreMouse() tea.Cmd {
	if !a.mouseOn {
		return nil
	}
	return tea.EnableMouseCellMotion
}

// handleMouse handles a mouse event.
func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if a.holdingKeys || msg.Action != tea.MouseActionPress {
		return nil
	}
	if a.hits == nil {
		a.view() // record the regions of the current screen
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return a.handleWheel(msg.X, msg.Y, -1)
	case tea.MouseButtonWheelDown:
		return a.handleWheel(msg.X, msg.Y, 1)
	case tea.MouseButtonLeft:
		a.pendingKey = ""
		a.list.statusMessage = ""
		if o := a.activeOverlay(); o != nil {
			return a.clickOverlay(o, msg.X, msg.Y)
		}
		return a.clickBrowse(msg.X, msg.Y)
	}
	return nil
}

// Browse view ----------------------------------------------------------------

// clickBrowse handles a click in the browse view.
func (a *App) clickBrowse(x, y int) tea.Cmd {
	if a.list.inline != nil {
		return nil // keep the title being typed
	}
	if a.focus == focusFilter && a.bar.open {
		reg, ok := a.hits.find(x, y, hitDropdownItem, hitDropdown, hitBarField)
		switch {
		case !ok:
			a.closeDropdown(false)
			return nil
		case reg.kind == hitDropdownItem:
			a.clickDropdownOption(reg.index)
			return nil
		case reg.kind == hitDropdown:
			return nil
		}
	}
	reg, ok := a.hits.find(x, y,
		hitBarField, hitBarLine,
		hitTreeChevron, hitTreeRow, hitViewRow, hitTreePane,
		hitDetailField, hitRelation, hitBodyLine, hitBody, hitDetailPane)
	if !ok {
		return nil
	}
	if a.list.searching {
		// Apply the query being typed, like enter.
		a.list, _ = a.list.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	switch reg.kind {
	case hitBarField:
		a.clickBarField(barField(reg.index))
	case hitBarLine:
		a.focusFilterBar()
	case hitTreeRow, hitTreeChevron:
		a.clickTreeRow(reg.index, reg.kind == hitTreeChevron)
	case hitViewRow:
		a.clickViewRow(reg.index)
	case hitTreePane:
		a.focusTree()
	case hitDetailField:
		return a.clickDetailField(reg.key)
	case hitRelation:
		a.clickRelation(reg.index)
	case hitBodyLine:
		return a.clickBody(reg.index)
	case hitBody:
		return a.clickBody(-1)
	case hitDetailPane:
		if a.focus != focusDetail {
			a.focusDetailPane(true)
		}
	}
	return nil
}

// clickTreeRow selects a tree row, toggling its fold for a click on the
// chevron and opening the detail pane for a double click.
func (a *App) clickTreeRow(i int, chevron bool) {
	if i < 0 || i >= len(a.list.rows) {
		return
	}
	r := a.list.rows[i]
	a.focusTree()
	a.list.cursor = i
	a.list.ensureVisible()
	if chevron {
		a.mouse = mouseState{}
		a.list.setFolded(r.bean.ID, !r.collapsed)
		return
	}
	now := mouseNow()
	double := a.mouse.lastRowID == r.bean.ID && now.Sub(a.mouse.lastAt) <= doubleClickInterval
	if double {
		a.mouse = mouseState{}
		a.focusDetailPane(true)
		return
	}
	a.mouse = mouseState{lastRowID: r.bean.ID, lastAt: now}
}

// clickViewRow selects a row of the Ready, Blocked or Audit list; a double
// click acts like enter (jump to a blocker, else open the detail pane).
func (a *App) clickViewRow(i int) {
	l := a.activeList()
	if l == nil || i < 0 || i >= len(l.rows) || !l.selectable(i) {
		return
	}
	a.focusTree()
	l.cursor = i
	l.ensureVisible()
	key := fmt.Sprintf("view:%d:%s", a.views.active, l.rows[i].key)
	now := mouseNow()
	if a.mouse.lastRowID == key && now.Sub(a.mouse.lastAt) <= doubleClickInterval {
		a.mouse = mouseState{}
		if r := l.rows[i]; r.jumpID != "" {
			a.jumpToBean(r.jumpID)
		} else {
			a.focusDetailPane(true)
		}
		return
	}
	a.mouse = mouseState{lastRowID: key, lastAt: now}
}

// clickDetailField runs the action of a field box's key hint, as if the key
// was pressed in the detail pane.
func (a *App) clickDetailField(key string) tea.Cmd {
	if a.detail.bean == nil {
		return nil
	}
	var cmd tea.Cmd
	a.detail, cmd = a.detail.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return cmd
}

// clickRelation opens a relation of the detail pane.
func (a *App) clickRelation(i int) {
	if a.focus != focusDetail && !a.focusDetailPane(true) {
		return
	}
	if i < 0 || i >= len(a.detail.links) {
		return
	}
	a.detail.section = sectionRelations
	a.detail.relCursor = i
	if id := a.detail.selectedRelation(); id != "" {
		a.showBean(id, true)
	}
}

// clickBody focuses the body and toggles the checkbox on the clicked
// rendered line (-1 = no line).
func (a *App) clickBody(line int) tea.Cmd {
	if a.detail.bean == nil {
		return nil
	}
	// Map the line on the body as it was shown, before the focus changes
	// its rendering.
	check := -1
	if line >= 0 {
		check = a.detail.checkboxAtLine(line)
	}
	if a.focus != focusDetail {
		if !a.focusDetailPane(false) {
			return nil
		}
	}
	a.detail.section = sectionBody
	if check < 0 {
		return nil
	}
	a.detail.checkCursor = check
	msg := toggleCheckboxMsg{id: a.detail.bean.ID, index: check}
	return func() tea.Msg { return msg }
}

// clickBarField focuses a filter bar field and opens its dropdown, flips
// it (toggles) or clears all filters (the clear field). A click on the
// field whose dropdown is open closes it.
func (a *App) clickBarField(f barField) {
	if a.focus == focusFilter && a.bar.open && a.bar.field == f {
		a.closeDropdown(false)
		return
	}
	a.focusFilterBar()
	a.bar.field = f
	switch {
	case f == barClear:
		a.list.clearFilter()
	case f.isToggle():
		a.applyBarFilter(toggleBarField(a.list.filter(), f))
	default:
		a.openDropdown()
	}
}

// clickDropdownOption toggles an option of the open dropdown, or chooses it
// and closes the dropdown for the single-choice Archived field.
func (a *App) clickDropdownOption(i int) {
	f := a.bar.field
	q := a.list.filter()
	opts := a.barOptions(f, q, a.bar.typed)
	if i < 0 || i >= len(opts) {
		return
	}
	a.bar.cursor = i
	a.bar.changed = true
	if f == barArchived {
		a.applyBarFilter(setArchived(q, opts[i].value))
		a.closeDropdown(false)
		return
	}
	a.applyBarFilter(toggleOption(q, f, opts[i]))
	a.ensureDropdownCursor()
}

// handleWheel scrolls what is under the mouse by delta steps.
func (a *App) handleWheel(x, y, delta int) tea.Cmd {
	if o := a.activeOverlay(); o != nil {
		o.scroll(a, delta)
		return nil
	}
	reg, ok := a.hits.find(x, y, hitDropdown, hitTreePane, hitDetailPane)
	if !ok {
		return nil
	}
	switch reg.kind {
	case hitDropdown:
		if a.bar.open {
			a.scrollDropdown(delta)
		}
	case hitTreePane:
		if l := a.activeList(); l != nil {
			l.move(delta * wheelTreeRows)
		} else if a.list.inline == nil {
			a.list.scrollBy(delta * wheelTreeRows)
		}
	case hitDetailPane:
		a.detail.scrollBy(delta * wheelBodyLines)
	}
	return nil
}

// scrollDropdown scrolls the open dropdown's options, keeping the cursor
// on a shown option.
func (a *App) scrollDropdown(delta int) {
	n := len(a.barOptions(a.bar.field, a.list.filter(), a.bar.typed))
	rows := a.dropdownRows()
	a.bar.offset = max(0, min(a.bar.offset+delta, n-rows))
	a.bar.cursor = max(a.bar.offset, min(a.bar.cursor, a.bar.offset+rows-1))
	a.ensureDropdownCursor()
}

// scrollBy scrolls the tree by delta rows, moving the cursor along only as
// far as needed to keep it on a fully shown row.
func (m *listModel) scrollBy(delta int) {
	if len(m.rows) == 0 || m.height <= 0 {
		return
	}
	m.offset = max(0, min(m.offset+delta, len(m.rows)-1))
	// Don't scroll past the end: keep the last page full.
	used := 0
	for i := m.offset; i < len(m.rows); i++ {
		used += m.rowHeight(i)
	}
	for m.offset > 0 && used+m.rowHeight(m.offset-1) <= m.height {
		m.offset--
		used += m.rowHeight(m.offset)
	}
	last := m.offset
	used = 0
	for i := m.offset; i < len(m.rows); i++ {
		used += m.rowHeight(i)
		if used > m.height {
			break
		}
		last = i
	}
	m.cursor = max(m.offset, min(m.cursor, last))
}

// Recording ------------------------------------------------------------------

// recordRow records tree row i, rendered as n lines at line y of the rows
// area of height h.
func (m listModel) recordRow(i, y, w, n, h int) {
	n = min(n, h-y)
	if n <= 0 {
		return
	}
	m.hits.add(0, y, w, n, hitTarget{kind: hitTreeRow, index: i})
	if r := m.rows[i]; r.hasChildren {
		// " " or the cursor bar, the indent, then the chevron (see renderRow).
		m.hits.add(1+2*r.depth, y, 2, 1, hitTarget{kind: hitTreeChevron, index: i})
	}
}

// recordScrollBox records a detail box of rows content lines at line top:
// its content area (area, if not 0), its borders running key and its first
// n content lines as items index offset, offset+1, …
func (m detailModel) recordScrollBox(top, w, rows int, key string, area, item hitKind, offset, n int) {
	if area != 0 {
		m.hits.add(0, top+1, w, rows, hitTarget{kind: area})
	}
	m.hits.add(0, top, w, 1, hitTarget{kind: hitDetailField, key: key})
	m.hits.add(0, top+rows+1, w, 1, hitTarget{kind: hitDetailField, key: key})
	for j := 0; j < n; j++ {
		m.hits.add(1, top+1+j, w-2, 1, hitTarget{kind: item, index: offset + j})
	}
}

// recordSummaryLine records the one-line filter bar at rec: the whole line
// while unfocused, otherwise each shown field (xs: start columns).
func (a *App) recordSummaryLine(rec hitRecorder, width int, xs map[barField]int) {
	if !a.filterBarFocused() {
		rec.add(0, 0, width, 1, hitTarget{kind: hitBarLine})
		return
	}
	sepW := lipgloss.Width(" · ")
	for f, x := range xs {
		end := width
		for g, gx := range xs {
			if g > f && gx-sepW < end {
				end = gx - sepW
			}
		}
		rec.add(x, 0, end-x, 1, hitTarget{kind: hitBarField, index: int(f)})
	}
}

// Checkboxes -----------------------------------------------------------------

// checkboxLineMarker is the first of the private-use runes marking the
// checkboxes when mapping rendered body lines to checkboxes.
const checkboxLineMarker = 0xF0000

// checkboxAtLine returns the index of the checkbox whose item starts on
// rendered body line, or -1.
//
// The body is rendered once more as it is shown, with the first character
// of each checkbox item's text replaced by a marker rune of the same width,
// so the lines wrap exactly as shown.
func (m detailModel) checkboxAtLine(line int) int {
	if m.bean == nil || len(m.checks) == 0 || line < 0 || line >= len(m.body) {
		return -1
	}
	body := m.bean.Body
	items := findCheckboxes(body)
	if strings.ContainsRune(body, checkboxLineMarker) {
		return -1
	}
	// Replace from the end so earlier offsets stay valid.
	for k := len(items) - 1; k >= 0; k-- {
		at := items[k].state + 2
		for at < len(body) && (body[at] == ' ' || body[at] == '\t') {
			at++
		}
		size := 0
		if at < len(body) && body[at] != '\n' && body[at] != '\r' {
			_, size = utf8.DecodeRuneInString(body[at:])
		}
		body = body[:at] + string(rune(checkboxLineMarker+k)) + body[at+size:]
	}
	if m.showCheckboxCursor() {
		body, _ = markCheckbox(body, m.checkCursor)
	}
	lines := renderMarkdown(body, m.bodyWidth())
	if line >= len(lines) {
		return -1
	}
	for _, r := range lines[line] {
		if k := int(r) - checkboxLineMarker; k >= 0 && k < len(items) {
			return k
		}
	}
	return -1
}
