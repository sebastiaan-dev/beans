package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
	"github.com/hmans/beans/pkg/config"
)

// archivedMarkerText is appended to rows of beans stored in the archive directory.
const archivedMarkerText = "[archived]"

// treeRowMaxLines is the maximum number of lines a tree row's title wraps onto.
const treeRowMaxLines = 3

// treeRow is a visible row of the bean tree.
type treeRow struct {
	bean           *bean.Bean
	depth          int
	hasChildren    bool   // has children in the (filtered) tree
	collapsed      bool   // children are folded away
	hidden         int    // number of descendants hidden by folding
	matched        bool   // matched the filters (vs. ancestor shown for context)
	implicitStatus string // implicit terminal status from an ancestor, if any
	archived       bool   // the bean file lives in the archive directory
}

// listModel is the bean tree of the browse view: it owns the loaded beans,
// the filter query, folding, the cursor and multi-selection.
type listModel struct {
	resolver *beangraph.CoreResolver
	config   *config.Config

	// Size of the area the rows are rendered into (set by the layout).
	width  int
	height int

	err    error
	loaded bool

	// Data from the last load.
	beans    []*bean.Bean
	byID     map[string]*bean.Bean
	implicit map[string]string // implicit terminal statuses by bean ID
	archived map[string]bool   // IDs of beans stored in the archive directory
	qctx     *filterquery.Context

	// Visible rows and the IDs of all beans with children in the tree.
	rows    []treeRow
	parents []string
	cursor  int
	offset  int // first row shown

	// Cached line counts of the rows (0 = not computed) at heightsWidth.
	heights      []int
	heightsWidth int

	// The filter query (see filterquery): the single source of truth for
	// all filters. It is edited as text with "/" (while searching) and by
	// the filter bar; searchBefore is the query from before the current
	// edit, restored by esc.
	search       textinput.Model
	searching    bool
	searchBefore string

	// Inline title input for creating or renaming a bean (inline_input.go).
	inline *inlineEdit

	// Folding. folds holds the collapsed bean IDs for the session. While a
	// filter is active, matches inside folded parents must stay
	// visible, so folding then uses filterFolds instead, a temporary set that
	// starts out expanded and is reset whenever the filters change.
	folds          map[string]bool
	filterFolds    map[string]bool
	filterFoldsKey string

	// Counts from the last build, for the title indicator
	shownCount int // beans matching the filters
	totalCount int // all beans

	// Multi-select state
	selectedBeans map[string]bool // IDs of beans marked for multi-edit

	// Status message to display in footer
	statusMessage string
}

func newListModel(resolver *beangraph.CoreResolver, cfg *config.Config) listModel {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "text or status:todo type:bug tag:ui -priority:low …"
	// A static cursor avoids redrawing the whole view for blink ticks.
	ti.Cursor.SetMode(cursor.CursorStatic)
	return listModel{
		resolver:      resolver,
		config:        cfg,
		search:        ti,
		folds:         make(map[string]bool),
		selectedBeans: make(map[string]bool),
		byID:          map[string]*bean.Bean{},
	}
}

// beansLoadedMsg is sent when beans are loaded
type beansLoadedMsg struct {
	beans    []*bean.Bean
	implicit map[string]string // implicit terminal statuses by bean ID
	archived map[string]bool   // IDs of beans stored in the archive directory
}

// errMsg is sent when an error occurs
type errMsg struct {
	err error
}

func (m listModel) Init() tea.Cmd {
	return m.loadBeans
}

func (m listModel) loadBeans() tea.Msg {
	// Query all beans; filtering happens when building the tree because it
	// depends on implicit (inherited) statuses, and unmatched ancestors are
	// still needed for tree context.
	allBeans, err := m.resolver.Beans(context.Background(), nil)
	if err != nil {
		return errMsg{err}
	}

	implicit := make(map[string]string, len(allBeans))
	archived := make(map[string]bool)
	for _, b := range allBeans {
		if status, _ := m.resolver.Core.ImplicitStatus(b.ID); status != "" {
			implicit[b.ID] = status
		}
		if m.resolver.Core.IsArchived(b.ID) {
			archived[b.ID] = true
		}
	}
	return beansLoadedMsg{beans: allBeans, implicit: implicit, archived: archived}
}

// query returns the current filter query text.
func (m listModel) query() string {
	return strings.TrimSpace(m.search.Value())
}

// filter returns the parsed filter query.
func (m listModel) filter() filterquery.Query {
	return filterquery.Parse(m.search.Value())
}

// setFilter replaces the filter query (in canonical form) and rebuilds the
// tree.
func (m *listModel) setFilter(q filterquery.Query) {
	m.search.SetValue(q.String())
	m.search.CursorEnd()
	m.rebuild()
}

// toggleHideDone toggles hiding of completed and scrapped beans, including
// beans that inherit such a status from an ancestor (done:no).
func (m *listModel) toggleHideDone() {
	q := m.filter()
	if v, ok := q.BoolSelection(filterquery.FieldDone); ok && !v {
		m.setFilter(q.SetValues(filterquery.FieldDone))
		return
	}
	m.setFilter(q.SetValues(filterquery.FieldDone, "no"))
}

// doneHidden reports whether completed and scrapped beans are hidden (H).
func (m listModel) doneHidden() bool {
	v, ok := m.filter().BoolSelection(filterquery.FieldDone)
	return ok && !v
}

// clearFilter clears the filter query.
func (m *listModel) clearFilter() {
	m.search.SetValue("")
	m.rebuild()
}

// hasActiveFilter reports whether the filter query constrains the tree.
func (m listModel) hasActiveFilter() bool {
	return !m.filter().IsEmpty()
}

// filterSummary returns short descriptions of the active filter terms.
func filterSummary(q filterquery.Query) []string {
	var parts []string
	for _, t := range q.Terms {
		if !t.IsText() && len(t.Values) == 0 {
			continue
		}
		parts = append(parts, termSummary(t))
	}
	return parts
}

// termSummary describes a filter term; the toggles of the filter bar read as
// words ("done hidden"), everything else in query syntax.
func termSummary(t filterquery.Term) string {
	if t.IsText() || !t.Field.IsBoolField() || t.Negate || len(t.Values) != 1 {
		return t.String()
	}
	v, ok := filterquery.ParseBool(t.Values[0])
	if !ok {
		return t.String()
	}
	switch t.Field {
	case filterquery.FieldDone, filterquery.FieldArchived:
		if v {
			return string(t.Field) + " only"
		}
		return string(t.Field) + " hidden"
	case filterquery.FieldReady:
		if v {
			return "ready"
		}
		return "not ready"
	}
	return t.String()
}

// title returns the tree title: the match count and the active filters.
func (m listModel) title() string {
	q := m.filter()
	if q.IsEmpty() {
		return "Beans"
	}
	parts := append([]string{fmt.Sprintf("Beans %d/%d", m.shownCount, m.totalCount)}, filterSummary(q)...)
	return strings.Join(parts, " · ")
}

// foldsSuspended reports whether a filter is active that looks for
// something, in which case matches inside folded parents are shown (see
// listModel.folds). Hiding completed or archived beans does not count: it
// narrows the tree rather than looking for something.
func (m listModel) foldsSuspended() bool {
	q := m.filter()
	if len(q.Texts()) > 0 {
		return true
	}
	for _, t := range q.ActiveClauses() {
		if t.Field != filterquery.FieldDone && t.Field != filterquery.FieldArchived {
			return true
		}
	}
	return false
}

// activeFolds returns the collapsed-ID set currently in effect.
func (m *listModel) activeFolds() map[string]bool {
	if !m.foldsSuspended() {
		return m.folds
	}
	key := strings.ToLower(m.filter().String())
	if m.filterFolds == nil || key != m.filterFoldsKey {
		m.filterFolds = make(map[string]bool)
		m.filterFoldsKey = key
	}
	return m.filterFolds
}

// currentRow returns the row under the cursor.
func (m listModel) currentRow() (treeRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return treeRow{}, false
	}
	return m.rows[m.cursor], true
}

// currentBean returns the bean under the cursor, or nil.
func (m listModel) currentBean() *bean.Bean {
	if r, ok := m.currentRow(); ok {
		return r.bean
	}
	return nil
}

// rowIndex returns the index of the row showing the bean, or -1.
func (m listModel) rowIndex(id string) int {
	for i, r := range m.rows {
		if r.bean.ID == id {
			return i
		}
	}
	return -1
}

// rebuild recomputes the visible rows from the loaded beans, filters, search
// and folds, keeping the cursor on the same bean where possible. If that bean
// got folded away, the cursor moves to its closest visible ancestor.
func (m *listModel) rebuild() {
	prevID := ""
	if b := m.currentBean(); b != nil {
		prevID = b.ID
	}
	prevCursor := m.cursor

	if m.qctx == nil {
		m.qctx = filterquery.NewContext(m.beans)
	}
	matched := filterquery.Filter(m.beans, m.filter(), m.qctx)
	m.shownCount = len(matched)
	m.totalCount = len(m.beans)

	sortFn := func(beans []*bean.Bean) {
		bean.SortByStatusPriorityAndType(beans, m.config.StatusNames(), m.config.PriorityNames(), m.config.TypeNames())
	}
	tree := ui.BuildTree(matched, m.beans, sortFn, m.implicit)
	folds := m.activeFolds()

	rows := make([]treeRow, 0, len(m.beans))
	var parents []string
	var countAll func(nodes []*ui.TreeNode) int
	countAll = func(nodes []*ui.TreeNode) int {
		n := 0
		for _, c := range nodes {
			if len(c.Children) > 0 {
				parents = append(parents, c.Bean.ID)
			}
			n += 1 + countAll(c.Children)
		}
		return n
	}
	var walk func(nodes []*ui.TreeNode, depth int)
	walk = func(nodes []*ui.TreeNode, depth int) {
		for _, n := range nodes {
			r := treeRow{
				bean:           n.Bean,
				depth:          depth,
				hasChildren:    len(n.Children) > 0,
				matched:        n.Matched,
				implicitStatus: n.ImplicitStatus,
				archived:       m.archived[n.Bean.ID],
			}
			if r.hasChildren {
				parents = append(parents, n.Bean.ID)
				r.collapsed = folds[n.Bean.ID]
			}
			if r.collapsed {
				r.hidden = countAll(n.Children)
				rows = append(rows, r)
				continue
			}
			rows = append(rows, r)
			walk(n.Children, depth+1)
		}
	}
	walk(tree, 0)
	m.rows = rows
	m.parents = parents
	m.heights = nil

	// Restore the cursor.
	idx := m.rowIndex(prevID)
	if idx < 0 && prevID != "" {
		seen := map[string]bool{}
		for b := m.byID[prevID]; b != nil && b.Parent != "" && !seen[b.Parent]; b = m.byID[b.Parent] {
			seen[b.Parent] = true
			if i := m.rowIndex(b.Parent); i >= 0 {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		idx = min(prevCursor, len(m.rows)-1)
	}
	m.cursor = max(idx, 0)
	m.ensureVisible()
}

// reveal moves the cursor to the bean, expanding folded ancestors. It
// reports false if the bean is not part of the (filtered) tree.
func (m *listModel) reveal(id string) bool {
	folds := m.activeFolds()
	seen := map[string]bool{}
	for b := m.byID[id]; b != nil && b.Parent != "" && !seen[b.Parent]; b = m.byID[b.Parent] {
		seen[b.Parent] = true
		delete(folds, b.Parent)
	}
	m.rebuild()
	i := m.rowIndex(id)
	if i < 0 {
		return false
	}
	m.cursor = i
	m.ensureVisible()
	return true
}

// Folding --------------------------------------------------------------------

// parentRow returns the index of the row of the cursor row's parent, or -1.
func (m listModel) parentRow() int {
	r, ok := m.currentRow()
	if !ok || r.bean.Parent == "" {
		return -1
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].bean.ID == r.bean.Parent {
			return i
		}
	}
	return -1
}

func (m *listModel) setFolded(id string, folded bool) {
	folds := m.activeFolds()
	if folded {
		folds[id] = true
	} else {
		delete(folds, id)
	}
	m.rebuild()
}

// collapseOrParent collapses the cursor row, or moves to its parent if it is
// a leaf or already collapsed (←/h).
func (m *listModel) collapseOrParent() {
	r, ok := m.currentRow()
	if !ok {
		return
	}
	if r.hasChildren && !r.collapsed {
		m.setFolded(r.bean.ID, true)
		return
	}
	if p := m.parentRow(); p >= 0 {
		m.cursor = p
		m.ensureVisible()
	}
}

// expandOrChild expands the cursor row, or moves to its first child if it is
// already expanded (→/l).
func (m *listModel) expandOrChild() {
	r, ok := m.currentRow()
	if !ok || !r.hasChildren {
		return
	}
	if r.collapsed {
		m.setFolded(r.bean.ID, false)
		return
	}
	m.moveCursor(1)
}

// foldOpen expands the cursor row (zo).
func (m *listModel) foldOpen() {
	if r, ok := m.currentRow(); ok && r.collapsed {
		m.setFolded(r.bean.ID, false)
	}
}

// foldClose collapses the cursor row, or the fold containing it if it has
// no children of its own, moving the cursor there (zc, like vim).
func (m *listModel) foldClose() {
	r, ok := m.currentRow()
	if !ok {
		return
	}
	if r.hasChildren && !r.collapsed {
		m.setFolded(r.bean.ID, true)
		return
	}
	if p := m.parentRow(); p >= 0 {
		m.cursor = p
		m.setFolded(m.rows[p].bean.ID, true)
	}
}

// foldToggle toggles the cursor row's fold (za).
func (m *listModel) foldToggle() {
	if r, ok := m.currentRow(); ok && r.hasChildren {
		m.setFolded(r.bean.ID, !r.collapsed)
	}
}

// openAll expands every fold (zR).
func (m *listModel) openAll() {
	clear(m.activeFolds())
	m.rebuild()
}

// closeAll collapses every bean with children (zM).
func (m *listModel) closeAll() {
	folds := m.activeFolds()
	for _, id := range m.parents {
		folds[id] = true
	}
	m.rebuild()
}

// Cursor and scrolling ---------------------------------------------------------

func (m *listModel) moveCursor(delta int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = max(0, min(len(m.rows)-1, m.cursor+delta))
	m.ensureVisible()
}

// pageRows returns how many rows fit on a page starting at the cursor.
func (m *listModel) pageRows() int {
	n, used := 0, 0
	for i := m.cursor; i < len(m.rows); i++ {
		used += m.rowHeight(i)
		if used > m.height {
			break
		}
		n++
	}
	return max(n, 1)
}

// rowHeight returns the number of lines row i renders onto. Heights only
// depend on the row and the width, so they are cached until either changes.
func (m *listModel) rowHeight(i int) int {
	if m.width <= 0 {
		return 1
	}
	if m.heightsWidth != m.width || len(m.heights) != len(m.rows) {
		m.heights = make([]int, len(m.rows))
		m.heightsWidth = m.width
	}
	if m.renaming(i) {
		return 1
	}
	if m.heights[i] == 0 {
		m.heights[i] = len(m.renderRow(i, m.width))
	}
	if m.creating() && i == m.inlineInsertRow() {
		return m.heights[i] + 1 // the inline create input
	}
	return m.heights[i]
}

// ensureVisible scrolls so that the cursor row is entirely visible, without
// leaving blank lines at the bottom while rows are scrolled away at the top.
func (m *listModel) ensureVisible() {
	// While creating, the inline input row must be visible.
	target := m.cursor
	if i := m.inlineInsertRow(); i >= 0 {
		target = i
	}
	m.offset = max(0, min(m.offset, len(m.rows)-1))
	if target < m.offset {
		m.offset = target
	}
	if m.height <= 0 || len(m.rows) == 0 {
		return
	}
	used := 0
	for i := m.offset; i <= target; i++ {
		used += m.rowHeight(i)
	}
	for m.offset < target && used > m.height {
		used -= m.rowHeight(m.offset)
		m.offset++
	}
	below := used
	for i := target + 1; i < len(m.rows) && below <= m.height; i++ {
		below += m.rowHeight(i)
	}
	for m.offset > 0 && below+m.rowHeight(m.offset-1) <= m.height {
		m.offset--
		below += m.rowHeight(m.offset)
	}
}

// setSize sets the size of the rows area.
func (m *listModel) setSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width, m.height = width, height
	m.search.Width = max(width-4, 1)
	m.ensureVisible()
}

// Update -----------------------------------------------------------------------

// selectionOrCurrent returns the IDs to act on: the multi-selection if any,
// otherwise the bean under the cursor.
func (m listModel) selectionOrCurrent() ([]string, *bean.Bean) {
	if len(m.selectedBeans) > 0 {
		ids := make([]string, 0, len(m.selectedBeans))
		for _, r := range m.beans {
			if m.selectedBeans[r.ID] {
				ids = append(ids, r.ID)
			}
		}
		return ids, nil
	}
	if b := m.currentBean(); b != nil {
		return []string{b.ID}, b
	}
	return nil, nil
}

func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// The layout sets the real size; this keeps a standalone list usable.
		m.setSize(msg.Width, msg.Height)

	case beansLoadedMsg:
		m.beans = msg.beans
		m.implicit = msg.implicit
		m.archived = msg.archived
		m.qctx = filterquery.NewContext(msg.beans)
		m.byID = make(map[string]*bean.Bean, len(msg.beans))
		for _, b := range msg.beans {
			m.byID[b.ID] = b
		}
		// Forget selected beans that no longer exist.
		for id := range m.selectedBeans {
			if m.byID[id] == nil {
				delete(m.selectedBeans, id)
			}
		}
		m.loaded = true
		m.err = nil
		m.rebuild()
		m.syncInline()
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		if m.inline != nil {
			return m.updateInline(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

// startSearch focuses the query input, ready to append a term.
func (m *listModel) startSearch() tea.Cmd {
	m.searching = true
	m.searchBefore = m.search.Value()
	if v := m.search.Value(); v != "" && !strings.HasSuffix(v, " ") {
		m.search.SetValue(v + " ")
	}
	m.search.CursorEnd()
	return m.search.Focus()
}

// updateSearch handles keys while the query input has focus. The tree
// follows the query as it is typed.
func (m listModel) updateSearch(msg tea.KeyMsg) (listModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel the edit: back to the query from before.
		m.searching = false
		m.search.Blur()
		m.search.SetValue(m.searchBefore)
		m.rebuild()
		return m, nil
	case "enter":
		m.searching = false
		m.search.Blur()
		m.search.SetValue(m.query())
		return m, nil
	case "up", "ctrl+p":
		m.moveCursor(-1)
		return m, nil
	case "down", "ctrl+n":
		m.moveCursor(1)
		return m, nil
	}
	prev := m.search.Value()
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	if m.search.Value() != prev {
		m.rebuild()
		// Put the cursor on the first match rather than on context ancestors.
		for i, r := range m.rows {
			if r.matched {
				m.cursor, m.offset = i, 0
				break
			}
		}
		m.ensureVisible()
	}
	return m, cmd
}

// updateKey handles keys while the tree has focus.
func (m listModel) updateKey(msg tea.KeyMsg) (listModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup", "ctrl+b":
		m.moveCursor(-m.pageRows())
	case "pgdown", "ctrl+f":
		m.moveCursor(m.pageRows())
	case "ctrl+u":
		m.moveCursor(-max(m.pageRows()/2, 1))
	case "ctrl+d":
		m.moveCursor(max(m.pageRows()/2, 1))
	case "home":
		m.moveCursor(-len(m.rows))
	case "end", "G":
		m.moveCursor(len(m.rows))
	case "left", "h":
		m.collapseOrParent()
	case "right", "l":
		m.expandOrChild()
	case "/":
		return m, m.startSearch()
	case " ":
		// Toggle selection for multi-select, then move to next item
		if b := m.currentBean(); b != nil {
			if m.selectedBeans[b.ID] {
				delete(m.selectedBeans, b.ID)
			} else {
				m.selectedBeans[b.ID] = true
			}
			m.moveCursor(1)
		}
	case "p":
		ids, b := m.selectionOrCurrent()
		if len(ids) == 0 {
			break
		}
		msg := openParentPickerMsg{beanIDs: ids}
		if b != nil {
			msg.beanTitle, msg.beanTypes, msg.currentParent = b.Title, []string{b.Type}, b.Parent
		} else {
			msg.beanTitle = fmt.Sprintf("%d selected beans", len(ids))
			for _, id := range ids {
				msg.beanTypes = append(msg.beanTypes, m.byID[id].Type)
			}
		}
		return m, func() tea.Msg { return msg }
	case "s":
		ids, b := m.selectionOrCurrent()
		if len(ids) == 0 {
			break
		}
		msg := openStatusPickerMsg{beanIDs: ids, beanTitle: fmt.Sprintf("%d selected beans", len(ids))}
		if b != nil {
			msg.beanTitle, msg.currentStatus = b.Title, b.Status
		}
		return m, func() tea.Msg { return msg }
	case "t":
		ids, b := m.selectionOrCurrent()
		if len(ids) == 0 {
			break
		}
		msg := openTypePickerMsg{beanIDs: ids, beanTitle: fmt.Sprintf("%d selected beans", len(ids))}
		if b != nil {
			msg.beanTitle, msg.currentType = b.Title, b.Type
		}
		return m, func() tea.Msg { return msg }
	case "P":
		ids, b := m.selectionOrCurrent()
		if len(ids) == 0 {
			break
		}
		msg := openPriorityPickerMsg{beanIDs: ids, beanTitle: fmt.Sprintf("%d selected beans", len(ids))}
		if b != nil {
			msg.beanTitle, msg.currentPriority = b.Title, b.Priority
		}
		return m, func() tea.Msg { return msg }
	case "b":
		if b := m.currentBean(); b != nil {
			return m, func() tea.Msg {
				return openBlockingPickerMsg{beanID: b.ID, beanTitle: b.Title, currentBlocking: b.Blocking}
			}
		}
	case "H":
		// Toggle hiding of completed/scrapped beans
		m.toggleHideDone()
	case "c", "C":
		m.startCreate(msg.String() == "C")
	case "r":
		m.startRename()
	case "K", "J":
		if b := m.currentBean(); b != nil {
			msg := moveBeanMsg{id: b.ID, down: msg.String() == "J"}
			return m, func() tea.Msg { return msg }
		}
	case "e":
		if b := m.currentBean(); b != nil {
			return m, func() tea.Msg { return openEditorMsg{beanID: b.ID, beanPath: b.Path} }
		}
	case "y":
		// Copy the full bean ID(s) to the clipboard
		if ids, _ := m.selectionOrCurrent(); len(ids) > 0 {
			return m, func() tea.Msg { return copyBeanIDMsg{ids: ids} }
		}
	case "esc", "backspace":
		// Clear the selection, then the filters.
		if len(m.selectedBeans) > 0 {
			clear(m.selectedBeans)
			return m, nil
		}
		if m.search.Value() != "" {
			return m, func() tea.Msg { return clearFilterMsg{} }
		}
	}
	return m, nil
}

// Rendering --------------------------------------------------------------------

// shortID returns the bean ID without the configured ID prefix, so tree rows
// spend their width on titles. IDs that do not carry the prefix (e.g. created
// with a custom prefix) are kept in full.
func shortID(id, prefix string) string {
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok || prefix == "" {
		return id
	}
	if rest = strings.TrimLeft(rest, "-"); rest == "" {
		return id
	}
	return rest
}

// glyph returns the single-letter code of a status or type.
func glyph(name string, short func(string) string) string {
	if g := short(name); g != "?" {
		return g
	}
	if name == "" {
		return "?"
	}
	return strings.ToUpper(name[:1])
}

// renderRow renders row i as one or more lines of exactly width cells.
func (m listModel) renderRow(i, width int) []string {
	r := m.rows[i]
	b := r.bean
	selected := i == m.cursor && !m.creating()
	marked := m.selectedBeans[b.ID]
	dim := !r.matched

	bg := lipgloss.Color("")
	if selected {
		bg = colSelection()
	}
	st := func(s lipgloss.Style) lipgloss.Style { return withBackground(s, bg) }
	plain := st(lipgloss.NewStyle())
	muted := st(mutedStyle())

	colors := m.config.GetBeanColors(b.Status, b.Type, b.Priority)
	statusStyle := st(fg(colNamed(colors.StatusColor)))
	typeStyle := st(fg(colNamed(colors.TypeColor)))
	idStyle := muted
	titleStyle := plain
	if dim {
		statusStyle, typeStyle, titleStyle = muted, muted, muted
	}
	if selected {
		titleStyle = st(accentStyle().Bold(true))
	}
	if marked {
		idStyle = st(fg(colWarning()).Bold(true))
	}

	bar := plain.Render(" ")
	switch {
	case selected:
		bar = st(accentStyle()).Render("▌")
	case marked:
		bar = st(fg(colWarning())).Render("•")
	}
	chevron := "  "
	if r.hasChildren {
		chevron = "▾ "
		if r.collapsed {
			chevron = "▸ "
		}
	}
	id := shortID(b.ID, m.config.Beans.Prefix)
	prefix := plain.Render(strings.Repeat("  ", r.depth)) +
		muted.Render(chevron) +
		statusStyle.Render(glyph(b.Status, ui.ShortStatus)) + plain.Render(" ") +
		typeStyle.Render(glyph(b.Type, ui.ShortType)) + plain.Render(" ") +
		idStyle.Render(id) + plain.Render(" ")
	prefixW := 1 + lipgloss.Width(prefix)
	if m.renaming(i) {
		return []string{fitWidth(bar+prefix+m.inlineInput(width-prefixW), width, plain)}
	}

	var toks []token
	if sym := ui.GetPrioritySymbol(b.Priority); sym != "" && !dim {
		ps := st(fg(colNamed(colors.PriorityColor)))
		if b.Priority == "critical" || b.Priority == "high" {
			ps = ps.Bold(true)
		}
		toks = append(toks, token{text: sym, style: ps})
	}
	toks = append(toks, wordTokens(b.Title, titleStyle)...)
	if len(toks) == 0 {
		toks = append(toks, token{text: "(untitled)", style: muted})
	}
	if r.implicitStatus != "" && r.matched {
		toks = append(toks, token{text: "↑" + r.implicitStatus, style: muted})
	}
	if r.archived {
		toks = append(toks, token{text: archivedMarkerText, style: muted})
	}
	if r.collapsed && r.hidden > 0 {
		toks = append(toks, token{text: fmt.Sprintf("+%d", r.hidden), style: muted})
	}

	textW := max(width-prefixW, 1)
	lines := wrapTokens(toks, textW, treeRowMaxLines, plain)
	out := make([]string, len(lines))
	indent := plain.Render(strings.Repeat(" ", max(prefixW-1, 0)))
	for j, l := range lines {
		if j == 0 {
			out[j] = fitWidth(bar+prefix+l, width, plain)
		} else {
			out[j] = fitWidth(bar+indent+l, width, plain)
		}
	}
	return out
}

// View renders the rows area: exactly m.height lines of m.width cells.
func (m listModel) View() string {
	return strings.Join(m.viewLines(), "\n")
}

func (m listModel) viewLines() []string {
	w, h := max(m.width, 1), max(m.height, 0)
	var lines []string
	switch {
	case m.err != nil:
		lines = append(lines, fg(colDanger()).Render(clampWidth(fmt.Sprintf("Error: %v", m.err), w)))
	case !m.loaded:
		lines = append(lines, mutedStyle().Render(" Loading…"))
	case len(m.rows) == 0 && m.creating():
		lines = append(lines, m.inlineCreateLine(w))
	case len(m.rows) == 0:
		msg := " No beans yet. Press c to create one."
		if m.hasActiveFilter() {
			msg = " No beans match. Press esc to clear."
		}
		lines = append(lines, mutedStyle().Render(truncateText(msg, w)))
	default:
		insert := m.inlineInsertRow()
		if m.creating() && insert < 0 {
			lines = append(lines, m.inlineCreateLine(w))
		}
		for i := m.offset; i < len(m.rows) && len(lines) < h; i++ {
			lines = append(lines, m.renderRow(i, w)...)
			if m.creating() && i == insert {
				lines = append(lines, m.inlineCreateLine(w))
			}
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = fitWidth(l, w, lipgloss.NewStyle())
	}
	return lines
}

// searchLine renders the query line shown above the rows, flagging values
// that cannot match anything (e.g. a misspelled status).
func (m listModel) searchLine(width int) string {
	prompt := accentStyle().Render("/ ")
	var bad []string
	for _, t := range m.filter().ActiveClauses() {
		bad = append(bad, filterquery.InvalidValues(t)...)
	}
	note := ""
	if len(bad) > 0 {
		note = "  " + fg(colDanger()).Render("unknown: "+strings.Join(bad, ", "))
	}
	if !m.searching {
		return fitWidth(prompt+m.search.Value()+note+mutedStyle().Render("  (/ edit · esc clear)"), width, lipgloss.NewStyle())
	}
	m.search.Width = max(width-lipgloss.Width(prompt)-lipgloss.Width(note)-1, 1)
	return fitWidth(prompt+m.search.View()+note, width, lipgloss.NewStyle())
}

// showSearchLine reports whether the query line is shown.
func (m listModel) showSearchLine() bool {
	return m.searching || m.search.Value() != ""
}

// position returns "cursor/total" for the tree's border.
func (m listModel) position() string {
	if len(m.rows) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", m.cursor+1, len(m.rows))
}

// treeHints returns the footer hints for the tree in order of importance.
func (m listModel) treeHints(wide bool) []keyHint {
	if m.inline != nil {
		return m.inlineHints()
	}
	if m.searching {
		return []keyHint{{"enter", "apply"}, {"esc", "cancel"}, {"↑↓", "move"}}
	}
	hideLabel := "hide done"
	if m.doneHidden() {
		hideLabel = "show done"
	}
	if len(m.selectedBeans) > 0 {
		return []keyHint{
			{"esc", "clear selection"},
			{"space", "toggle"},
			{"s", "status"},
			{"t", "type"},
			{"P", "priority"},
			{"p", "parent"},
			{"y", "copy ids"},
		}
	}
	detail := "details"
	if !wide {
		detail = "open"
	}
	var hints []keyHint
	if m.search.Value() != "" {
		hints = append(hints, keyHint{"esc", "clear filter"})
	}
	return append(hints,
		keyHint{"enter", detail},
		keyHint{"e", "edit"},
		keyHint{"c", "create"},
		keyHint{"C", "child"},
		keyHint{"r", "rename"},
		keyHint{"s", "status"},
		keyHint{"t", "type"},
		keyHint{"P", "priority"},
		keyHint{"p", "parent"},
		keyHint{"b", "blocking"},
		keyHint{"n", "next"},
		keyHint{"u", "undo"},
		keyHint{"K J", "move"},
		keyHint{"←→", "fold"},
		keyHint{"space", "select"},
		keyHint{"H", hideLabel},
		keyHint{"y", "copy id"},
		keyHint{"F", "filters"},
		keyHint{"/", "query"},
		keyHint{"zR zM", "open/close all"},
		keyHint{"g t", "tag"},
	)
}
