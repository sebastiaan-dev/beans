package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

// The picker: one modal for choosing a bean's status, type, priority,
// parent or the beans it blocks (s t P p b).
//
//	╭─ Select Status ──────────────────────╮
//	│ Check the error texts of the login … │  bean title (or "N selected beans")
//	│ demo-t002                            │  bean ID (single bean only)
//	│ ──────────────────────────────────── │
//	│ › press / to filter                  │  type-to-filter input
//	│▌in-progress                          │  items, one line each, cut with …
//	│  todo (current)                      │
//	│ ──────────────────────────────────── │
//	│ Currently being worked on            │  description (status/type/priority)
//	│ enter select · / filter · esc cancel │
//	╰──────────────────────────────────────╯
//
// j/k (↑↓) move, enter chooses, esc/backspace cancels, / filters the items
// (fuzzy, like the command palette; enter keeps the filter, esc clears it).
// The blocking picker toggles beans with space and applies the changes with
// enter. The result is sent as a message (statusSelectedMsg, …) that the App
// applies, so every change goes through one place and is recorded for undo.

// pickerKind is what a picker sets.
type pickerKind int

const (
	pickStatus pickerKind = iota
	pickType
	pickPriority
	pickParent
	pickBlocking
)

// pickerKeys maps the edit keys of the panes to the picker they open.
var pickerKeys = map[string]pickerKind{
	"s": pickStatus,
	"t": pickType,
	"P": pickPriority,
	"p": pickParent,
	"b": pickBlocking,
}

// title is the picker's box label.
func (k pickerKind) title() string {
	return [...]string{"Select Status", "Select Type", "Select Priority", "Select Parent", "Manage Blocking"}[k]
}

// listsBeans reports whether the picker's items are beans (rather than the
// statuses, types or priorities, which come with a description).
func (k pickerKind) listsBeans() bool { return k == pickParent || k == pickBlocking }

// Messages -------------------------------------------------------------------------

// openPickerMsg asks the App to open a picker.
type openPickerMsg struct {
	kind    pickerKind
	beanIDs []string   // beans to update
	bean    *bean.Bean // the bean being edited; nil for a batch (selection)
}

// openPickerCmd opens a picker for the beans with the given IDs; b is the
// bean being edited, nil when editing a selection of beans.
func openPickerCmd(kind pickerKind, ids []string, b *bean.Bean) tea.Cmd {
	msg := openPickerMsg{kind: kind, beanIDs: ids, bean: b}
	return func() tea.Msg { return msg }
}

// statusSelectedMsg, typeSelectedMsg, prioritySelectedMsg and
// parentSelectedMsg carry the choice of a picker for beanIDs.
type statusSelectedMsg struct {
	beanIDs []string
	status  string
}

type typeSelectedMsg struct {
	beanIDs  []string
	beanType string
}

type prioritySelectedMsg struct {
	beanIDs  []string
	priority string
}

type parentSelectedMsg struct {
	beanIDs  []string // the beans being modified
	parentID string   // the new parent ID (empty string to clear parent)
}

// blockingConfirmedMsg carries the changes made in the blocking picker.
type blockingConfirmedMsg struct {
	beanID   string   // the bean we're modifying
	toAdd    []string // IDs to add to blocking
	toRemove []string // IDs to remove from blocking
}

// Model ---------------------------------------------------------------------------

// pickerItem is a choice of a picker.
type pickerItem struct {
	value   string     // status, type or priority name, bean ID; "" = no parent
	filter  string     // text the type-to-filter input matches
	desc    string     // description shown below the list (enum pickers)
	color   string     // config color name (enum pickers)
	archive bool       // archive status (rendered without bold)
	bean    *bean.Bean // bean pickers; nil for "(No Parent)"
	current bool       // the value the bean has now
}

// pickerModel is the state of the open picker.
type pickerModel struct {
	kind      pickerKind
	beanIDs   []string
	beanTitle string
	beanID    string // shown below the title; "" for a batch

	items   []pickerItem
	visible []int // indices into items matching the filter, in display order
	cursor  int   // index into visible
	offset  int   // first visible item shown

	query     string // type-to-filter text
	filtering bool   // typed characters go into the query

	// Blocking picker: the beans blocked when it opened and now.
	blocking, marked map[string]bool
}

// openPicker opens the picker requested by msg. A parent picker for a bean
// whose type can't have a parent (e.g. a milestone) does not open.
func (a *App) openPicker(msg openPickerMsg) tea.Cmd {
	p := pickerModel{kind: msg.kind, beanIDs: msg.beanIDs}
	if b := msg.bean; b != nil {
		p.beanTitle, p.beanID = b.Title, b.ID
	} else {
		p.beanTitle = fmt.Sprintf("%d selected beans", len(msg.beanIDs))
	}
	switch msg.kind {
	case pickStatus:
		for _, s := range config.DefaultStatuses {
			p.items = append(p.items, pickerItem{value: s.Name, desc: s.Description, color: s.Color, archive: s.Archive})
		}
	case pickType:
		for _, t := range config.DefaultTypes {
			p.items = append(p.items, pickerItem{value: t.Name, desc: t.Description, color: t.Color})
		}
	case pickPriority:
		for _, pr := range config.DefaultPriorities {
			p.items = append(p.items, pickerItem{value: pr.Name, desc: pr.Description, color: pr.Color})
		}
	case pickParent:
		types := a.pickerBeanTypes(msg)
		for _, t := range types {
			if beancore.ValidParentTypes(t) == nil {
				return nil
			}
		}
		p.items = append(p.items, pickerItem{filter: "no parent clear none"})
		for _, b := range a.eligibleParents(msg.beanIDs, types) {
			p.items = append(p.items, beanPickerItem(b))
		}
	case pickBlocking:
		p.blocking, p.marked = map[string]bool{}, map[string]bool{}
		if msg.bean != nil {
			for _, id := range msg.bean.Blocking {
				p.blocking[id], p.marked[id] = true, true
			}
		}
		for _, b := range a.pickerBeans(func(b *bean.Bean) bool { return !slices.Contains(msg.beanIDs, b.ID) }) {
			p.items = append(p.items, beanPickerItem(b))
		}
	}
	if b := msg.bean; b != nil {
		current := map[pickerKind]string{pickStatus: b.Status, pickType: b.Type, pickPriority: b.Priority, pickParent: b.Parent}
		for i := range p.items {
			if v, ok := current[msg.kind]; ok && p.items[i].value == v {
				p.items[i].current = true
			}
		}
	}
	for i := range p.items {
		if it := &p.items[i]; it.filter == "" {
			it.filter = it.value + " " + it.desc
		}
	}
	p.refilter()
	// Start on the current value.
	for i, idx := range p.visible {
		if p.items[idx].current {
			p.cursor = i
		}
	}
	a.picker = p
	a.state = viewPicker
	return nil
}

// beanPickerItem returns the item of a bean picker for b.
func beanPickerItem(b *bean.Bean) pickerItem {
	return pickerItem{value: b.ID, filter: b.Title + " " + b.ID, bean: b}
}

// pickerBeanTypes returns the types of the beans a picker edits.
func (a *App) pickerBeanTypes(msg openPickerMsg) []string {
	if msg.bean != nil {
		return []string{msg.bean.Type}
	}
	var types []string
	for _, id := range msg.beanIDs {
		if b := a.list.byID[id]; b != nil {
			types = append(types, b.Type)
		}
	}
	return types
}

// pickerBeans returns the beans for which keep returns true, ordered by
// type (milestones first), then by title.
func (a *App) pickerBeans(keep func(*bean.Bean) bool) []*bean.Bean {
	all, _ := a.resolver.Beans(context.Background(), nil)
	typeOrder := map[string]int{}
	for i, t := range a.config.TypeNames() {
		typeOrder[t] = i
	}
	out := slices.DeleteFunc(slices.Clone(all), func(b *bean.Bean) bool { return !keep(b) })
	slices.SortStableFunc(out, func(x, y *bean.Bean) int {
		if c := cmp.Compare(typeOrder[x.Type], typeOrder[y.Type]); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
	return out
}

// eligibleParents returns the beans that can become the parent of all the
// beans with the given IDs and types: of a valid parent type for every one
// of them, and neither one of them nor a descendant (which would create a
// cycle).
func (a *App) eligibleParents(ids, types []string) []*bean.Bean {
	var valid []string
	for i, t := range types {
		if i == 0 {
			valid = beancore.ValidParentTypes(t)
		} else {
			valid = slices.DeleteFunc(valid, func(v string) bool { return !slices.Contains(beancore.ValidParentTypes(t), v) })
		}
	}
	all, _ := a.resolver.Beans(context.Background(), nil)
	excluded := map[string]bool{}
	for _, id := range ids {
		excluded[id] = true
		for d := range collectDescendants(id, all) {
			excluded[d] = true
		}
	}
	return a.pickerBeans(func(b *bean.Bean) bool { return !excluded[b.ID] && slices.Contains(valid, b.Type) })
}

// collectDescendants returns the IDs of all descendants of the bean.
func collectDescendants(beanID string, allBeans []*bean.Bean) map[string]bool {
	children := make(map[string][]string)
	for _, b := range allBeans {
		if b.Parent != "" {
			children[b.Parent] = append(children[b.Parent], b.ID)
		}
	}
	descendants := make(map[string]bool)
	queue := children[beanID]
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if !descendants[id] {
			descendants[id] = true
			queue = append(queue, children[id]...)
		}
	}
	return descendants
}

// refilter recomputes the visible items for the query, best matches first.
// The cursor moves to the best match, or back to the item it was on when
// the query is cleared.
func (p *pickerModel) refilter() {
	prev := -1
	if p.cursor >= 0 && p.cursor < len(p.visible) {
		prev = p.visible[p.cursor]
	}
	type scored struct{ idx, score int }
	var matches []scored
	for i, it := range p.items {
		if s, ok := fuzzyScore(p.query, it.filter); ok {
			matches = append(matches, scored{i, s})
		}
	}
	slices.SortStableFunc(matches, func(x, y scored) int { return cmp.Compare(y.score, x.score) })
	p.visible = p.visible[:0]
	p.cursor, p.offset = 0, 0
	for i, m := range matches {
		p.visible = append(p.visible, m.idx)
		if m.idx == prev && strings.TrimSpace(p.query) == "" {
			p.cursor = i
		}
	}
}

// visibleItem returns the i-th visible item.
func (p *pickerModel) visibleItem(i int) (pickerItem, bool) {
	if i < 0 || i >= len(p.visible) {
		return pickerItem{}, false
	}
	return p.items[p.visible[i]], true
}

func (p *pickerModel) move(delta int) {
	p.cursor = max(0, min(p.cursor+delta, len(p.visible)-1))
}

// Overlay -------------------------------------------------------------------------

func (p *pickerModel) acceptsText() bool { return p.filtering }

func (p *pickerModel) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if p.filtering {
		p.updateFilter(key)
		return nil
	}
	switch key.String() {
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "pgup":
		p.move(-a.pickerRows())
	case "pgdown":
		p.move(a.pickerRows())
	case "home", "g":
		p.move(-len(p.visible))
	case "end", "G":
		p.move(len(p.visible))
	case "/":
		p.filtering = true
	case " ":
		p.toggle()
	case "enter":
		return p.choose()
	case "esc", "backspace":
		return a.closeOverlay()
	case "q":
		return tea.Quit
	}
	return nil
}

// updateFilter handles a key while the query is typed.
func (p *pickerModel) updateFilter(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEnter:
		p.filtering = false
		if len(p.visible) == 0 {
			p.query = ""
			p.refilter()
		}
	case tea.KeyEsc:
		p.filtering = false
		p.query = ""
		p.refilter()
	case tea.KeyUp, tea.KeyCtrlP:
		p.move(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		p.move(1)
	case tea.KeyBackspace:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.refilter()
		}
	case tea.KeyRunes, tea.KeySpace:
		p.query += string(key.Runes)
		if key.Type == tea.KeySpace {
			p.query += " "
		}
		p.refilter()
	}
}

// toggle flips the blocking of the bean under the cursor (blocking picker).
func (p *pickerModel) toggle() {
	it, ok := p.visibleItem(p.cursor)
	if p.kind != pickBlocking || !ok {
		return
	}
	if p.marked[it.value] {
		delete(p.marked, it.value)
	} else {
		p.marked[it.value] = true
	}
}

// choose returns the message applying the item under the cursor, or the
// blocking changes.
func (p *pickerModel) choose() tea.Cmd {
	var msg tea.Msg
	it, ok := p.visibleItem(p.cursor)
	switch p.kind {
	case pickBlocking:
		m := blockingConfirmedMsg{beanID: p.beanIDs[0]}
		for _, it := range p.items {
			switch id := it.value; {
			case p.marked[id] && !p.blocking[id]:
				m.toAdd = append(m.toAdd, id)
			case !p.marked[id] && p.blocking[id]:
				m.toRemove = append(m.toRemove, id)
			}
		}
		msg = m
	case pickStatus:
		msg = statusSelectedMsg{beanIDs: p.beanIDs, status: it.value}
	case pickType:
		msg = typeSelectedMsg{beanIDs: p.beanIDs, beanType: it.value}
	case pickPriority:
		msg = prioritySelectedMsg{beanIDs: p.beanIDs, priority: it.value}
	case pickParent:
		msg = parentSelectedMsg{beanIDs: p.beanIDs, parentID: it.value}
	}
	if !ok && p.kind != pickBlocking {
		return nil
	}
	return func() tea.Msg { return msg }
}

// click chooses visible item i (the blocking picker toggles it).
func (p *pickerModel) click(_ *App, i int) tea.Cmd {
	if _, ok := p.visibleItem(i); !ok {
		return nil
	}
	p.filtering = false
	p.cursor = i
	if p.kind == pickBlocking {
		p.toggle()
		return nil
	}
	return p.choose()
}

func (p *pickerModel) scroll(_ *App, delta int) { p.move(delta) }

// Rendering -------------------------------------------------------------------------

// pickerWidth is the picker's outer width.
func (a *App) pickerWidth() int {
	pct, maxW := 50, 60
	if a.picker.kind.listsBeans() {
		pct, maxW = 60, 80
	}
	return min(max(40, min(maxW, a.width*pct/100))+2, max(a.width-2, 20))
}

// pickerChromeLines is the number of lines of the picker besides its items.
func (a *App) pickerChromeLines() int {
	n := 2 + 1 + 1 + 1 + 1 + 1 // borders, title, two rules, filter, footer
	if a.picker.beanID != "" {
		n++
	}
	if !a.picker.kind.listsBeans() {
		n++ // description
	}
	return n
}

// pickerRows is the number of item lines shown.
func (a *App) pickerRows() int {
	maxRows := 10
	if a.picker.kind.listsBeans() {
		maxRows = 12
	}
	return max(min(len(a.picker.items), maxRows, a.height-2-a.pickerChromeLines()), 1)
}

func (p *pickerModel) view(a *App, rec hitRecorder) string {
	outer := a.pickerWidth()
	inner := outer - 4
	rows := a.pickerRows()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	p.offset = max(0, min(p.offset, len(p.visible)-rows))

	rule := borderStyle().Render(strings.Repeat(glyphs().Horizontal, inner))
	lines := []string{truncateText(lipgloss.NewStyle().Bold(true).Render(p.beanTitle), inner)}
	if p.beanID != "" {
		lines = append(lines, mutedStyle().Render(p.beanID))
	}
	lines = append(lines, rule, p.filterLine())
	end := len(lines) + rows
	if len(p.visible) == 0 {
		lines = append(lines, mutedStyle().Render("No matches"))
	}
	for i := p.offset; i < len(p.visible) && i < p.offset+rows; i++ {
		// Inside the border (1) and the padding (1).
		rec.add(2, 1+len(lines), inner, 1, hitTarget{kind: hitModalItem, index: i})
		lines = append(lines, a.pickerItemLine(p.items[p.visible[i]], i == p.cursor, inner))
	}
	for len(lines) < end {
		lines = append(lines, "")
	}
	lines = append(lines, rule)
	if !p.kind.listsBeans() {
		desc := ""
		if it, ok := p.visibleItem(p.cursor); ok {
			desc = it.desc
		}
		lines = append(lines, mutedStyle().Render(truncateText(desc, inner)))
	}
	lines = append(lines, renderHints(p.hints(), nil, inner))

	pos := ""
	if len(p.visible) > rows {
		pos = mutedStyle().Render(fmt.Sprintf("%d/%d", p.cursor+1, len(p.visible)))
	}
	return strings.Join(box{
		label:    p.kind.title(),
		botRight: pos,
		lines:    lines,
		width:    outer,
		focused:  true,
		padding:  1,
	}.render(), "\n")
}

// filterLine renders the type-to-filter input.
func (p *pickerModel) filterLine() string {
	g := glyphs()
	switch {
	case p.filtering:
		return accentStyle().Render(g.Prompt+" ") + p.query + accentStyle().Render(g.TextCursor)
	case p.query != "":
		return accentStyle().Render(g.Prompt+" ") + p.query
	}
	return mutedStyle().Render(g.Prompt + " press / to filter")
}

// hints returns the picker's footer hints.
func (p *pickerModel) hints() []keyHint {
	move := keyHint{glyphs().KeyUpDown, "move"}
	switch {
	case p.filtering:
		return []keyHint{{"enter", "apply"}, {"esc", "clear"}, move}
	case p.kind == pickBlocking:
		return []keyHint{{"space", "toggle"}, {"enter", "confirm"}, {"/", "filter"}, {"esc", "cancel"}, move}
	}
	return []keyHint{{"enter", "select"}, {"/", "filter"}, {"esc", "cancel"}, move}
}

// pickerItemLine renders an item as a line of exactly width cells, cut with
// an ellipsis if it is too long.
func (a *App) pickerItemLine(it pickerItem, selected bool, width int) string {
	r := newRowStyler(selected)
	sp := r.plain().Render(" ")
	line := r.bar() + sp
	switch {
	case a.picker.kind == pickBlocking:
		mark := r.muted().Render(glyphs().NotBlocking)
		if a.picker.marked[it.value] {
			mark = r.st(fg(colDanger()).Bold(true)).Render(glyphs().Blocking)
		}
		line += mark + sp + beanRef(a.config, it.bean, r)
	case it.bean != nil:
		line += beanRef(a.config, it.bean, r)
	case a.picker.kind == pickParent:
		line += r.muted().Render("(No Parent)")
	default:
		st := r.st(fg(colNamed(it.color)))
		if a.picker.kind == pickStatus && !it.archive {
			st = st.Bold(true)
		}
		line += st.Render(it.value)
	}
	if it.current {
		line += r.muted().Render(" (current)")
	}
	return rowLine(line, width, r)
}
