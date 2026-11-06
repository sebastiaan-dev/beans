package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
)

// The filter bar: a row of fields (status, type, priority, tags, archived,
// ready, done and "clear all") above the panes that edit the tree's filter
// query (listModel.search), the same state the "/" input edits as text. The
// bar shows whatever the query says, and every change made in the bar is
// written back into the query in canonical form, like the filter popover of
// the Beans UI.
//
// Focus it with F (or tab/shift+tab); ←/→ (h/l) move between fields,
// enter/space opens the field's dropdown below it (or flips a toggle),
// backspace/delete clears the field. In the dropdown space toggles values
// (applied live), enter applies and closes and esc restores the query from
// before it was opened. Short terminals show the bar as a single line.

// barField is a field of the filter bar.
type barField int

const (
	barStatus barField = iota
	barType
	barPriority
	barTags
	barArchived
	barReady
	barDone
	barClear
	barFieldCount
)

// label returns the field's label.
func (f barField) label() string {
	return [...]string{"Status", "Type", "Priority", "Tags", "Archived", "Ready", "Done", "Clear"}[f]
}

// queryField returns the query field the bar field edits.
func (f barField) queryField() filterquery.Field {
	switch f {
	case barStatus:
		return filterquery.FieldStatus
	case barType:
		return filterquery.FieldType
	case barPriority:
		return filterquery.FieldPriority
	case barTags:
		return filterquery.FieldTag
	case barArchived:
		return filterquery.FieldArchived
	case barReady:
		return filterquery.FieldReady
	case barDone:
		return filterquery.FieldDone
	}
	return ""
}

// isToggle reports whether the field is flipped directly instead of opening
// a dropdown.
func (f barField) isToggle() bool { return f == barReady || f == barDone }

// isMulti reports whether several values of the field can be selected.
func (f barField) isMulti() bool { return f <= barTags }

// filterBarModel is the state of the filter bar. The filter values
// themselves live in the query (listModel.search).
type filterBarModel struct {
	field barField // focused field

	// Dropdown state
	open    bool
	cursor  int    // option under the cursor
	offset  int    // first option shown
	typed   string // tag type-ahead
	before  string // query when the dropdown opened, restored by esc
	changed bool   // the selection changed since the dropdown opened
}

// typing reports whether typed characters go into the tag type-ahead.
func (b filterBarModel) typing() bool { return b.open && b.field == barTags }

// optionState is how an option of a dropdown relates to the query.
type optionState int

const (
	optionOff      optionState = iota
	optionIncluded             // field:value
	optionExcluded             // -field:value
	optionCompared             // priority:>=value etc.
)

// barOption is a choice in a dropdown.
type barOption struct {
	value string
	label string
	count int // beans with the tag (tags only)
	state optionState
	op    filterquery.Op // comparison (optionCompared)
}

// dropdownMaxRows is the maximum number of options shown at once.
const dropdownMaxRows = 8

// archivedChoices are the options of the Archived dropdown: the value of the
// archived: clause ("" = no clause).
var archivedChoices = []struct{ value, label string }{
	{"", "shown"},
	{"no", "hidden"},
	{"yes", "only"},
}

// barOptions returns the options of a dropdown field for the query.
func (a *App) barOptions(f barField, q filterquery.Query, typed string) []barOption {
	field := f.queryField()
	if f == barArchived {
		cur := archivedValue(q)
		opts := make([]barOption, len(archivedChoices))
		for i, c := range archivedChoices {
			opts[i] = barOption{value: c.value, label: c.label}
			if c.value == cur {
				opts[i].state = optionIncluded
			}
		}
		return opts
	}

	included := map[string]bool{}
	excluded := map[string]bool{}
	compared := map[string]filterquery.Op{}
	for _, t := range q.Clauses(field) {
		for _, v := range t.Values {
			switch {
			case t.Negate:
				excluded[v] = true
			case t.Op != filterquery.OpEq && t.Op != "":
				compared[v] = t.Op
			default:
				included[v] = true
			}
		}
	}

	var values []string
	counts := map[string]int{}
	switch f {
	case barStatus:
		values = filterquery.Statuses
	case barType:
		values = filterquery.Types
	case barPriority:
		values = filterquery.Priorities
	case barTags:
		values = a.tagValues(counts, included, excluded)
		values = matchTyped(values, typed)
	}
	opts := make([]barOption, 0, len(values))
	for _, v := range values {
		o := barOption{value: v, label: v, count: counts[v]}
		switch {
		case compared[v] != "":
			o.state, o.op = optionCompared, compared[v]
		case excluded[v]:
			o.state = optionExcluded
		case included[v]:
			o.state = optionIncluded
		}
		opts = append(opts, o)
	}
	return opts
}

// tagValues returns all tags in use, most used first, plus tags the query
// refers to that no bean uses (so they can be deselected). counts receives
// the usage counts.
func (a *App) tagValues(counts map[string]int, extra ...map[string]bool) []string {
	for _, b := range a.list.beans {
		for _, t := range b.Tags {
			counts[strings.ToLower(t)]++
		}
	}
	for _, m := range extra {
		for t := range m {
			if _, ok := counts[t]; !ok {
				counts[t] = 0
			}
		}
	}
	tags := make([]string, 0, len(counts))
	for t := range counts {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if counts[tags[i]] != counts[tags[j]] {
			return counts[tags[i]] > counts[tags[j]]
		}
		return tags[i] < tags[j]
	})
	return tags
}

// matchTyped returns the values matching the type-ahead text: prefix
// matches first, then substring matches, keeping the order otherwise.
func matchTyped(values []string, typed string) []string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return values
	}
	var prefix, contains []string
	for _, v := range values {
		switch {
		case strings.HasPrefix(v, typed):
			prefix = append(prefix, v)
		case strings.Contains(v, typed):
			contains = append(contains, v)
		}
	}
	return append(prefix, contains...)
}

// archivedValue returns the value of the query's archived: clause as one of
// archivedChoices ("" = archived beans are shown).
func archivedValue(q filterquery.Query) string {
	v, ok := q.BoolSelection(filterquery.FieldArchived)
	switch {
	case !ok:
		return ""
	case v:
		return "yes"
	default:
		return "no"
	}
}

// Editing ----------------------------------------------------------------------

// toggleOption toggles a value of a multi-value field: an excluded value is
// no longer excluded, otherwise the value is added to or removed from the
// field's plain clause. Toggling a priority replaces a comparison.
func toggleOption(q filterquery.Query, f barField, o barOption) filterquery.Query {
	field := f.queryField()
	if o.state == optionExcluded {
		var terms []filterquery.Term
		for _, t := range q.Terms {
			if !t.IsText() && t.Field == field && t.Negate {
				var values []string
				for _, v := range t.Values {
					if v != o.value {
						values = append(values, v)
					}
				}
				if len(values) == 0 {
					continue
				}
				t.Values = values
			}
			terms = append(terms, t)
		}
		return filterquery.Query{Terms: terms}
	}
	if f == barPriority {
		var terms []filterquery.Term
		for _, t := range q.Terms {
			if !t.IsText() && t.Field == field && t.Op != filterquery.OpEq && t.Op != "" {
				continue
			}
			terms = append(terms, t)
		}
		q = filterquery.Query{Terms: terms}
	}
	return q.ToggleValue(field, o.value)
}

// setArchived applies a choice of the Archived dropdown (see
// archivedChoices).
func setArchived(q filterquery.Query, value string) filterquery.Query {
	if value == "" {
		return q.SetValues(filterquery.FieldArchived)
	}
	return q.SetValues(filterquery.FieldArchived, value)
}

// toggleBarField flips a toggle field: ready:yes or done:no on/off.
func toggleBarField(q filterquery.Query, f barField) filterquery.Query {
	field := f.queryField()
	want := "yes"
	if f == barDone {
		want = "no"
	}
	if q.HasField(field) {
		return q.SetValues(field)
	}
	return q.SetValues(field, want)
}

// applyBarFilter writes a query edited by the bar back into the tree.
func (a *App) applyBarFilter(q filterquery.Query) {
	a.list.setFilter(q)
}

// clearBarField removes every clause of the focused field, or all filters
// for the clear-all field.
func (a *App) clearBarField(f barField) {
	if f == barClear {
		a.list.clearFilter()
		return
	}
	a.applyBarFilter(a.list.filter().SetValues(f.queryField()))
}

// Focus --------------------------------------------------------------------------

// focusFilterBar moves the focus to the filter bar.
func (a *App) focusFilterBar() {
	a.closeDropdown(false)
	if a.list.searching {
		a.list, _ = a.list.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	a.focus = focusFilter
	a.detail.blur()
	a.detailPinned = ""
}

// openFilterField focuses the filter bar on a field and opens its dropdown
// (g t opens the tags).
func (a *App) openFilterField(f barField) {
	a.focusFilterBar()
	a.bar.field = f
	a.openDropdown()
}

func (a *App) openDropdown() {
	f := a.bar.field
	if f.isToggle() || f == barClear {
		return
	}
	a.bar.open = true
	a.bar.cursor, a.bar.offset, a.bar.typed = 0, 0, ""
	a.bar.before = a.list.search.Value()
	a.bar.changed = false
	// Start on the first selected option.
	for i, o := range a.barOptions(f, a.list.filter(), "") {
		if o.state != optionOff {
			a.bar.cursor = i
			break
		}
	}
	a.ensureDropdownCursor()
}

// closeDropdown closes the dropdown, restoring the query from before it
// was opened if revert is set.
func (a *App) closeDropdown(revert bool) {
	if !a.bar.open {
		return
	}
	if revert && a.list.search.Value() != a.bar.before {
		a.list.search.SetValue(a.bar.before)
		a.list.rebuild()
	}
	a.bar.open = false
	a.bar.typed = ""
}

// dropdownRows returns how many options the dropdown shows at most.
func (a *App) dropdownRows() int {
	rows := dropdownMaxRows
	if a.height > 0 {
		l := a.layout()
		avail := l.paneH - 2 // the dropdown's borders
		if a.bar.field == barTags {
			avail-- // type-ahead line
		}
		rows = min(rows, max(avail, 1))
	}
	return rows
}

func (a *App) ensureDropdownCursor() {
	n := len(a.barOptions(a.bar.field, a.list.filter(), a.bar.typed))
	a.bar.cursor = max(0, min(a.bar.cursor, n-1))
	rows := a.dropdownRows()
	if a.bar.cursor < a.bar.offset {
		a.bar.offset = a.bar.cursor
	}
	if a.bar.cursor >= a.bar.offset+rows {
		a.bar.offset = a.bar.cursor - rows + 1
	}
	a.bar.offset = max(0, min(a.bar.offset, n-rows))
}

// Keys ---------------------------------------------------------------------------

// updateFilterBar handles a key while the filter bar has the focus. It
// reports false for keys handled elsewhere (focus changes, ?, q, /).
func (a *App) updateFilterBar(msg tea.KeyMsg) (tea.Cmd, bool) {
	if a.bar.open {
		return a.updateDropdown(msg)
	}
	key := msg.String()
	switch key {
	case "left", "h":
		a.bar.field = max(a.bar.field-1, 0)
	case "right", "l":
		a.bar.field = min(a.bar.field+1, barFieldCount-1)
	case "home":
		a.bar.field = 0
	case "end":
		a.bar.field = barFieldCount - 1
	case "enter", " ", "down", "j":
		switch f := a.bar.field; {
		case f == barClear:
			a.list.clearFilter()
		case f.isToggle():
			a.applyBarFilter(toggleBarField(a.list.filter(), f))
		default:
			a.openDropdown()
		}
	case "backspace", "delete":
		a.clearBarField(a.bar.field)
	case "H":
		a.list.toggleHideDone()
	case "esc", "up", "k":
		a.focusTree()
	case "/":
		// Edit the query as text in the tree.
		a.focusTree()
		return nil, false
	case "tab", "shift+tab", "?", "q", "F":
		return nil, false
	}
	// Other keys are ignored rather than acting on the tree.
	return nil, true
}

// updateDropdown handles a key while a dropdown is open.
func (a *App) updateDropdown(msg tea.KeyMsg) (tea.Cmd, bool) {
	f := a.bar.field
	q := a.list.filter()
	opts := a.barOptions(f, q, a.bar.typed)
	current := func() (barOption, bool) {
		if a.bar.cursor < 0 || a.bar.cursor >= len(opts) {
			return barOption{}, false
		}
		return opts[a.bar.cursor], true
	}
	apply := func(nq filterquery.Query) {
		a.applyBarFilter(nq)
		a.bar.changed = true
	}
	move := func(delta int) {
		a.bar.cursor += delta
		a.ensureDropdownCursor()
	}
	typing := f == barTags

	key := msg.String()
	switch key {
	case "esc":
		a.closeDropdown(true)
		return nil, true
	case "enter":
		if o, ok := current(); ok {
			switch {
			case f == barArchived:
				apply(setArchived(q, o.value))
			case !a.bar.changed && o.state == optionOff:
				// Nothing toggled yet: enter picks the option under the cursor.
				apply(toggleOption(q, f, o))
			}
		}
		a.closeDropdown(false)
		return nil, true
	case " ":
		if o, ok := current(); ok {
			if f == barArchived {
				apply(setArchived(q, o.value))
			} else {
				apply(toggleOption(q, f, o))
			}
		}
		return nil, true
	case "up", "ctrl+p":
		move(-1)
		return nil, true
	case "down", "ctrl+n":
		move(1)
		return nil, true
	case "pgup":
		move(-a.dropdownRows())
		return nil, true
	case "pgdown":
		move(a.dropdownRows())
		return nil, true
	case "delete":
		a.clearBarField(f)
		a.bar.changed = true
		return nil, true
	case "left", "right":
		// Keep the changes and move to the neighbouring field.
		a.closeDropdown(false)
		return a.updateFilterBar(msg)
	case "tab", "shift+tab":
		a.closeDropdown(false)
		return nil, false
	}

	if typing {
		switch msg.Type {
		case tea.KeyBackspace:
			if r := []rune(a.bar.typed); len(r) > 0 {
				a.bar.typed = string(r[:len(r)-1])
				a.bar.cursor, a.bar.offset = 0, 0
			}
		case tea.KeyRunes:
			a.bar.typed += strings.ToLower(string(msg.Runes))
			a.bar.cursor, a.bar.offset = 0, 0
		}
		return nil, true
	}

	switch key {
	case "k":
		move(-1)
	case "j":
		move(1)
	case "backspace":
		a.clearBarField(f)
		a.bar.changed = true
	case ">", "<":
		// Priority comparisons: at least / at most as urgent as the option.
		if o, ok := current(); ok && f == barPriority {
			op := filterquery.OpGE
			if key == "<" {
				op = filterquery.OpLE
			}
			if o.state == optionCompared && o.op == op {
				apply(q.SetValues(filterquery.FieldPriority))
			} else {
				apply(q.SetClause(filterquery.Term{Field: filterquery.FieldPriority, Op: op, Values: []string{o.value}}))
			}
		}
	case "?", "q":
		return nil, false
	}
	return nil, true
}

// Rendering ------------------------------------------------------------------------

// barFieldValue renders the value of a field for the bar.
func (a *App) barFieldValue(f barField, q filterquery.Query) string {
	anyValue := mutedStyle().Render("any")
	switch f {
	case barArchived:
		switch archivedValue(q) {
		case "no":
			return fg(colWarning()).Render("hidden")
		case "yes":
			return fg(colWarning()).Render("only")
		}
		return mutedStyle().Render("shown")
	case barReady:
		if v, ok := q.BoolSelection(filterquery.FieldReady); ok {
			if v {
				return fg(colSuccess()).Render("yes")
			}
			return fg(colWarning()).Render("no")
		}
		if q.HasField(filterquery.FieldReady) {
			return fg(colWarning()).Render("custom")
		}
		return anyValue
	case barDone:
		if v, ok := q.BoolSelection(filterquery.FieldDone); ok {
			if v {
				return fg(colWarning()).Render("only")
			}
			return fg(colWarning()).Render("hidden")
		}
		if q.HasField(filterquery.FieldDone) {
			return fg(colWarning()).Render("custom")
		}
		return mutedStyle().Render("shown")
	case barClear:
		if q.IsEmpty() {
			return mutedStyle().Render(glyphs().Clear)
		}
		return fg(colDanger()).Render(glyphs().Clear)
	}

	field := f.queryField()
	color := func(v string) lipgloss.Style {
		if a.config == nil {
			return lipgloss.NewStyle()
		}
		switch f {
		case barStatus:
			return fg(colNamed(a.config.GetBeanColors(v, "", "").StatusColor))
		case barType:
			return fg(colNamed(a.config.GetBeanColors("", v, "").TypeColor))
		case barPriority:
			return fg(colNamed(a.config.GetBeanColors("", "", v).PriorityColor))
		}
		return lipgloss.NewStyle()
	}
	var parts []string
	for _, t := range q.Clauses(field) {
		for _, v := range t.Values {
			s := color(v).Render(v)
			switch {
			case t.Negate:
				s = mutedStyle().Render("not ") + s
			case t.Op != filterquery.OpEq && t.Op != "":
				s = mutedStyle().Render(opSymbol(t.Op)) + s
			}
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return anyValue
	}
	return strings.Join(parts, mutedStyle().Render(", "))
}

// opSymbol renders a comparison operator.
func opSymbol(op filterquery.Op) string {
	switch op {
	case filterquery.OpGE:
		return glyphs().AtLeast
	case filterquery.OpLE:
		return glyphs().AtMost
	case filterquery.OpGT:
		return ">"
	case filterquery.OpLT:
		return "<"
	}
	return ""
}

// fieldActive reports whether a field constrains the tree.
func fieldActive(f barField, q filterquery.Query) bool {
	if f == barClear {
		return false
	}
	return q.HasField(f.queryField())
}

// filterBarWidths returns the outer widths of the field boxes for the
// rendered values: every box fits its label and value if possible, and
// status, type and tags share the remaining room. If the boxes do not fit,
// the width is split evenly.
func filterBarWidths(width int, values []string) []int {
	ws := make([]int, barFieldCount)
	used := int(barFieldCount) - 1 // gaps
	for f := barField(0); f < barFieldCount; f++ {
		labelW := 0
		if f != barClear {
			labelW = lipgloss.Width(f.label()) + 6 // "╭─ label ─╮"
		}
		ws[f] = max(labelW, lipgloss.Width(values[f])+4)
		used += ws[f]
	}
	if used > width {
		return splitWidths(width, int(barFieldCount), 1)
	}
	flexible := []barField{barStatus, barType, barTags}
	extra := width - used
	for i, f := range flexible {
		ws[f] += extra / len(flexible)
		if i < extra%len(flexible) {
			ws[f]++
		}
	}
	return ws
}

// filterBarFocused reports whether the bar has the focus in the browse view.
func (a *App) filterBarFocused() bool { return a.focus == focusFilter }

// filterBar renders the filter bar as a row of labelled boxes (3 lines),
// recording the fields at rec.
func (a *App) filterBar(width int, rec hitRecorder) []string {
	values := a.barFieldValues()
	ws := filterBarWidths(width, values)
	hotkeys := map[barField]string{barStatus: "F", barTags: "g t", barDone: "H"}
	blocks := make([][]string, barFieldCount)
	x := 0
	for f := barField(0); f < barFieldCount; f++ {
		rec.add(x, 0, ws[f], 3, hitTarget{kind: hitBarField, index: int(f)})
		x += ws[f] + 1
		label := f.label()
		if f == barClear {
			label = ""
		}
		focused := a.filterBarFocused() && a.bar.field == f
		blocks[f] = box{
			label:   label,
			hotkey:  hotkeys[f],
			lines:   []string{values[f]},
			width:   ws[f],
			padding: 1,
			focused: focused,
		}.render()
	}
	return joinColumns(1, blocks...)
}

// barFieldValues renders the values of all fields.
func (a *App) barFieldValues() []string {
	q := a.list.filter()
	values := make([]string, barFieldCount)
	for f := barField(0); f < barFieldCount; f++ {
		values[f] = a.barFieldValue(f, q)
	}
	return values
}

// barFieldX returns the column at which a field starts in the full bar.
func (a *App) barFieldX(width int, f barField) int {
	x := 0
	for i, w := range filterBarWidths(width, a.barFieldValues()) {
		if barField(i) == f {
			return x
		}
		x += w + 1
	}
	return x
}

// filterSummaryLine renders the filter bar as a single line for short
// terminals. Unfocused it lists the active fields; focused it shows every
// field, scrolled so that the focused one is visible. xs receives the start
// column of each field shown.
func (a *App) filterSummaryLine(width int) (string, map[barField]int) {
	q := a.list.filter()
	sep := mutedStyle().Render(glyphs().sep())
	sepW := lipgloss.Width(glyphs().sep())
	xs := map[barField]int{}
	focused := a.filterBarFocused()

	segment := func(f barField) string {
		label := f.label()
		value := a.barFieldValue(f, q)
		if f == barClear {
			label = "clear"
		}
		ls := mutedStyle()
		if focused && a.bar.field == f {
			ls = accentStyle().Bold(true).Underline(true)
			label = glyphs().ActiveField + label
		}
		if label == "" {
			return value
		}
		return ls.Render(label) + " " + value
	}

	if !focused {
		prefix := accentStyle().Render("Filters ")
		var parts []string
		for f := barField(0); f < barClear; f++ {
			if fieldActive(f, q) {
				parts = append(parts, segment(f))
			}
		}
		if extra := extraFilterTerms(q); len(extra) > 0 {
			parts = append(parts, mutedStyle().Render(strings.Join(extra, " ")))
		}
		body := strings.Join(parts, sep)
		if body == "" {
			body = mutedStyle().Render("none")
		}
		hint := keyStyle().Render("(F)")
		room := width - lipgloss.Width(prefix) - lipgloss.Width(hint) - 1
		body = truncateText(body, max(room, 0))
		gap := max(width-lipgloss.Width(prefix)-lipgloss.Width(body)-lipgloss.Width(hint), 1)
		return clampWidth(prefix+body+strings.Repeat(" ", gap)+hint, width), xs
	}

	segs := make([]string, barFieldCount)
	ws := make([]int, barFieldCount)
	for f := barField(0); f < barFieldCount; f++ {
		segs[f] = segment(f)
		ws[f] = lipgloss.Width(segs[f])
	}
	// Window [start, end) around the focused field that fits the width,
	// leaving room for the scroll markers.
	fits := func(start, end int) bool {
		w := 0
		for i := start; i < end; i++ {
			if i > start {
				w += sepW
			}
			w += ws[i]
		}
		if start > 0 {
			w += 2
		}
		if end < int(barFieldCount) {
			w += 2
		}
		return w <= width
	}
	cur := int(a.bar.field)
	start, end := cur, cur+1
	for start > 0 && fits(start-1, end) {
		start--
	}
	for end < int(barFieldCount) && fits(start, end+1) {
		end++
	}
	var b strings.Builder
	x := 0
	if start > 0 {
		b.WriteString(mutedStyle().Render(glyphs().ScrollLeft + " "))
		x += lipgloss.Width(glyphs().ScrollLeft) + 1
	}
	for i := start; i < end; i++ {
		if i > start {
			b.WriteString(sep)
			x += sepW
		}
		xs[barField(i)] = x
		b.WriteString(segs[i])
		x += ws[i]
	}
	if end < int(barFieldCount) {
		b.WriteString(mutedStyle().Render(" " + glyphs().ScrollRight))
	}
	return clampWidth(b.String(), width), xs
}

// extraFilterTerms returns the query terms the bar has no field for (free
// text, parent:, blocked: …).
func extraFilterTerms(q filterquery.Query) []string {
	barFields := map[filterquery.Field]bool{}
	for f := barField(0); f < barClear; f++ {
		barFields[f.queryField()] = true
	}
	var out []string
	for _, t := range q.Terms {
		if t.IsText() || (!barFields[t.Field] && len(t.Values) > 0) {
			out = append(out, t.String())
		}
	}
	return out
}

// dropdownLines renders the open dropdown, recording its options at rec
// (relative to the dropdown).
func (a *App) dropdownLines(maxWidth int, rec hitRecorder) []string {
	f := a.bar.field
	q := a.list.filter()
	opts := a.barOptions(f, q, a.bar.typed)
	rows := a.dropdownRows()

	labelW := 8
	for _, o := range opts {
		w := lipgloss.Width(o.label)
		if o.count > 0 {
			w += len(fmt.Sprint(o.count)) + 1
		}
		labelW = max(labelW, w)
	}
	width := min(max(labelW+10, 20), 40, maxWidth)

	var lines []string
	if f == barTags {
		input := accentStyle().Render(glyphs().Prompt+" ") + a.bar.typed + accentStyle().Render(glyphs().TextCursor)
		if a.bar.typed == "" {
			input = accentStyle().Render(glyphs().Prompt+" ") + mutedStyle().Render("type to filter")
		}
		lines = append(lines, input)
	}
	if len(opts) == 0 {
		msg := "no tags in use"
		if a.bar.typed != "" {
			msg = "no matching tags"
		}
		lines = append(lines, mutedStyle().Render(msg))
	}
	end := min(a.bar.offset+rows, len(opts))
	for i := a.bar.offset; i < end; i++ {
		o := opts[i]
		cursor := "  "
		if i == a.bar.cursor {
			cursor = accentStyle().Bold(true).Render(glyphs().Cursor + " ")
		}
		mark := mutedStyle().Render("[ ]")
		switch {
		case f == barArchived && o.state == optionIncluded:
			mark = keyStyle().Render("(" + glyphs().Radio + ")")
		case f == barArchived:
			mark = mutedStyle().Render("( )")
		case o.state == optionIncluded:
			mark = keyStyle().Render("[x]")
		case o.state == optionExcluded:
			mark = fg(colDanger()).Render("[-]")
		case o.state == optionCompared:
			mark = keyStyle().Render("[" + opSymbol(o.op) + "]")
		}
		label := o.label
		if i == a.bar.cursor {
			label = accentStyle().Bold(true).Render(label)
		}
		if o.count > 0 {
			label += " " + mutedStyle().Render(fmt.Sprint(o.count))
		}
		rec.add(1, 1+len(lines), width-2, 1, hitTarget{kind: hitDropdownItem, index: i})
		lines = append(lines, cursor+mark+" "+label)
	}
	pos := ""
	if len(opts) > rows {
		pos = mutedStyle().Render(fmt.Sprintf("%d/%d", a.bar.cursor+1, len(opts)))
	}
	return box{
		label:    f.label(),
		botRight: pos,
		lines:    lines,
		width:    width,
		focused:  true,
	}.render()
}

// overlayDropdown draws the open dropdown onto the content lines of the
// browse view, below the filter bar starting at line top, and records it at
// rec.
func (a *App) overlayDropdown(content []string, top, inner int, collapsed bool, rec hitRecorder) []string {
	if !a.bar.open || a.focus != focusFilter {
		return content
	}
	x := 0
	if collapsed {
		_, xs := a.filterSummaryLine(inner)
		x = xs[a.bar.field]
	} else {
		x = a.barFieldX(inner, a.bar.field)
	}
	// Record the dropdown's items relative to it, then place them.
	local := &hitMap{}
	dd := a.dropdownLines(inner, local.recorder())
	w := lipgloss.Width(dd[0])
	x = max(0, min(x, inner-w))
	rec.add(x, top, w, len(dd), hitTarget{kind: hitDropdown})
	rec.place(local, x, top)
	out := slices.Clone(content)
	for i, l := range dd {
		row := top + i
		if row >= len(out) {
			break
		}
		out[row] = spliceCells(fitWidth(out[row], inner, lipgloss.NewStyle()), l, x)
	}
	return out
}

// spliceCells replaces the cells of base starting at column x with over.
// base may contain ANSI escapes; the styles of the cells after over are
// kept.
func spliceCells(base, over string, x int) string {
	w := lipgloss.Width(over)
	left := clampWidth(base, x)
	if gap := x - lipgloss.Width(left); gap > 0 {
		left += strings.Repeat(" ", gap)
	}
	return left + "\x1b[0m" + over + "\x1b[0m" + dropCells(base, x+w)
}

// dropCells returns s without its first n cells, keeping all escape
// sequences so that styles carry over. A wide rune cut in half becomes a
// space.
func dropCells(s string, n int) string {
	var b strings.Builder
	col := 0
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == '\x1b' {
			// Copy the escape sequence: ESC [ ... final byte.
			j := i + 1
			if j < len(rs) && rs[j] == '[' {
				j++
				for j < len(rs) && !(rs[j] >= 0x40 && rs[j] <= 0x7e) {
					j++
				}
			}
			end := min(j+1, len(rs))
			b.WriteString(string(rs[i:end]))
			i = end - 1
			continue
		}
		rw := lipgloss.Width(string(r))
		switch {
		case col >= n:
			b.WriteRune(r)
		case col+rw > n:
			b.WriteString(strings.Repeat(" ", col+rw-n))
		}
		col += rw
	}
	return b.String()
}

// filterBarHints returns the footer hints while the filter bar has the focus.
func (a *App) filterBarHints() []keyHint {
	f := a.bar.field
	if a.bar.open {
		hints := []keyHint{{"space", "toggle"}, {"enter", "apply"}, {"esc", "cancel"}, {glyphs().KeyUpDown, "move"}}
		switch f {
		case barArchived:
			hints = []keyHint{{"enter", "choose"}, {"esc", "cancel"}, {glyphs().KeyUpDown, "move"}}
		case barPriority:
			hints = append(hints, keyHint{">", "at least"}, keyHint{"<", "at most"})
		case barTags:
			hints = append(hints, keyHint{"type", "filter tags"})
		}
		return append(hints, keyHint{glyphs().KeyLeftRight, "field"}, keyHint{"del", "clear field"})
	}
	action := keyHint{"enter", "open"}
	switch {
	case f == barClear:
		action = keyHint{"enter", "clear all"}
	case f.isToggle():
		action = keyHint{"enter", "toggle"}
	}
	// The hints refer to the command registry (commands.go).
	return []keyHint{
		action,
		cmdHint("filter-fields"),
		cmdHint("filter-clear-field"),
		cmdHint("filter-leave"),
		{"tab", "next pane"},
		cmdHint("query"),
		cmdHint("hide-done"),
	}
}
