package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
)

// The command palette (":" or ctrl+p): a centered overlay with an input that
// fuzzy-matches the actions of the command registry (commands.go) and the
// beans. enter runs an action (actions that need an argument open their
// picker) or jumps to a bean, revealing it in the tree.

// maxRecentCommands bounds the recently run commands shown at the top.
const maxRecentCommands = 5

// Palette sections.
const (
	sectionRecent  = "Recent"
	sectionActions = "Actions"
	sectionBeans   = "Beans"
)

// paletteEntry is a selectable row of the palette: a command or a bean.
type paletteEntry struct {
	section string
	cmd     *command
	bean    *bean.Bean
	reason  string // why the command is unavailable ("" if available)
	hidden  string // why the bean is not in the tree: "folded" or "filtered"
	score   int
	order   int // position before ranking, for ties
}

// commandPalette is the palette's state.
type commandPalette struct {
	input   textinput.Model
	entries []paletteEntry // in display order
	cursor  int            // index into entries
	offset  int            // first display row shown
	message string         // e.g. why the chosen command is unavailable
}

// openCommandPalette opens the palette over the browse view.
func (a *App) openCommandPalette() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = ": "
	ti.Placeholder = "command or bean" + glyphs().Ellipsis
	ti.Cursor.SetMode(cursor.CursorStatic)
	a.palette = commandPalette{input: ti}
	a.state = viewCommandPalette
	a.pendingKey = ""
	a.refreshPalette()
	return a.palette.input.Focus()
}

// refreshPalette recomputes the entries for the current query.
func (a *App) refreshPalette() {
	p := &a.palette
	p.entries = a.paletteEntries(p.input.Value())
	p.cursor, p.offset = 0, 0
}

// paletteEntries returns the entries matching query in display order.
//
// Without a query, the recently run commands come first, then all other
// actions (unavailable ones last) and all beans in tree order. With a query,
// both sections are ranked by score (ties: available actions and recently
// run commands first, then registry or tree order), and the section with the
// best match comes first.
func (a *App) paletteEntries(query string) []paletteEntry {
	query = strings.TrimSpace(query)
	recentRank := map[string]int{}
	for i, id := range a.recentCommands {
		recentRank[id] = i
	}

	var recent, actions, beans []paletteEntry
	for i := range commands {
		c := &commands[i]
		if c.run == nil {
			continue
		}
		score, ok := fuzzyScore(query, c.paletteTitle())
		if s, hok := fuzzyScore(query, c.help); hok && (!ok || s > score) {
			score, ok = s, true
		}
		if !ok {
			continue
		}
		e := paletteEntry{section: sectionActions, cmd: c, reason: c.unavailable(a), score: score, order: i}
		if r, isRecent := recentRank[c.id]; isRecent {
			if query == "" {
				e.section, e.order = sectionRecent, r
				recent = append(recent, e)
				continue
			}
			// Recently run commands win ties.
			e.order = r - len(commands)
		}
		actions = append(actions, e)
	}

	matched := map[string]bool{}
	if a.list.qctx != nil {
		for _, b := range filterquery.Filter(a.list.beans, a.list.filter(), a.list.qctx) {
			matched[b.ID] = true
		}
	}
	for i, b := range a.paletteBeans() {
		score, ok := a.beanScore(query, b)
		if !ok {
			continue
		}
		e := paletteEntry{section: sectionBeans, bean: b, score: score, order: i}
		if a.list.rowIndex(b.ID) < 0 {
			e.hidden = "filtered"
			if matched[b.ID] {
				e.hidden = "folded"
			}
		}
		beans = append(beans, e)
	}

	// Without a query unavailable actions come last; with one they rank by
	// score like the others, so that e.g. "undo" shows why it can't run.
	rank := func(es []paletteEntry) {
		slices.SortStableFunc(es, func(x, y paletteEntry) int {
			if c := cmp.Compare(y.score, x.score); c != 0 {
				return c
			}
			if (x.reason == "") != (y.reason == "") {
				if x.reason == "" {
					return -1
				}
				return 1
			}
			return cmp.Compare(x.order, y.order)
		})
	}
	rank(recent)
	rank(actions)
	rank(beans)

	best := func(es []paletteEntry) int {
		b := -1
		for _, e := range es {
			b = max(b, e.score)
		}
		return b
	}
	out := append([]paletteEntry{}, recent...)
	if query != "" && best(beans) > best(actions) {
		return append(append(out, beans...), actions...)
	}
	return append(append(out, actions...), beans...)
}

// paletteBeans returns all beans: those in the tree in tree order, then the
// ones hidden by the filters.
func (a *App) paletteBeans() []*bean.Bean {
	out := make([]*bean.Bean, 0, len(a.list.beans))
	seen := make(map[string]bool, len(a.list.beans))
	for _, r := range a.list.rows {
		out = append(out, r.bean)
		seen[r.bean.ID] = true
	}
	// Beans in folded subtrees and those hidden by the filters.
	for _, b := range a.list.beans {
		if !seen[b.ID] {
			out = append(out, b)
		}
	}
	return out
}

// beanScore scores a bean against the query: its ID or short ID, or its
// title. An exact ID beats everything.
func (a *App) beanScore(query string, b *bean.Bean) (int, bool) {
	if query == "" {
		return 0, true
	}
	short := shortID(b.ID, a.config.Beans.Prefix)
	q := strings.ToLower(query)
	if q == strings.ToLower(b.ID) || q == strings.ToLower(short) {
		return 10000, true
	}
	score, ok := fuzzyScore(query, b.Title)
	if s, idOK := fuzzyScore(query, b.ID); idOK && !strings.Contains(query, " ") && (!ok || s > score) {
		score, ok = s, true
	}
	return score, ok
}

// rememberCommand moves a command to the top of the recent commands.
func (a *App) rememberCommand(id string) {
	a.recentCommands = slices.DeleteFunc(a.recentCommands, func(s string) bool { return s == id })
	a.recentCommands = slices.Insert(a.recentCommands, 0, id)
	if len(a.recentCommands) > maxRecentCommands {
		a.recentCommands = a.recentCommands[:maxRecentCommands]
	}
}

func (p *commandPalette) acceptsText() bool { return true }

// click runs entry i.
func (p *commandPalette) click(a *App, i int) tea.Cmd {
	if i < 0 || i >= len(p.entries) {
		return nil
	}
	p.cursor = i
	return a.runPaletteEntry()
}

// scroll moves the cursor.
func (p *commandPalette) scroll(_ *App, delta int) { p.move(delta) }

func (p *commandPalette) move(delta int) {
	if n := len(p.entries); n > 0 {
		p.cursor = max(0, min(p.cursor+delta, n-1))
	}
	p.message = ""
}

// update handles messages while the palette is open.
func (p *commandPalette) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return cmd
	}
	move := p.move
	switch key.String() {
	case "esc":
		return a.closeOverlay()
	case "up", "ctrl+p", "ctrl+k":
		move(-1)
		return nil
	case "down", "ctrl+n", "ctrl+j", "tab":
		move(1)
		return nil
	case "pgup":
		move(-a.paletteRows())
		return nil
	case "pgdown":
		move(a.paletteRows())
		return nil
	case "enter":
		return a.runPaletteEntry()
	}
	prev := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prev {
		p.message = ""
		a.refreshPalette()
	}
	return cmd
}

// runPaletteEntry runs the selected entry and closes the palette, unless the
// selected command is unavailable.
func (a *App) runPaletteEntry() tea.Cmd {
	p := &a.palette
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return nil
	}
	e := p.entries[p.cursor]
	if e.bean != nil {
		a.state = viewList
		a.paletteJumpToBean(e.bean.ID)
		return nil
	}
	if reason := e.cmd.unavailable(a); reason != "" {
		p.message = reason
		return nil
	}
	a.rememberCommand(e.cmd.id)
	return a.runCommand(e.cmd)
}

// paletteJumpToBean moves the tree cursor to a bean, expanding folded ancestors.
// Beans hidden by the filters are shown in the focused detail pane.
func (a *App) paletteJumpToBean(id string) {
	if a.views.active != viewTree {
		a.switchView(viewTree)
	}
	a.focusTree()
	a.showBean(id, false)
	if a.detailPinned == "" {
		return
	}
	a.focus = focusDetail
	a.detail.focus()
	a.list.statusMessage = fmt.Sprintf("%s is hidden by the filters (esc clears them)", a.shortID(id))
}

// Rendering ------------------------------------------------------------------------

// paletteModalWidth is the palette's outer width.
func (a *App) paletteModalWidth() int {
	return min(max(a.width-6, 30), 80)
}

// paletteRows is the number of list rows shown.
func (a *App) paletteRows() int {
	// Border (2), input, rule, rule, footer.
	return max(min(a.height-2-6, 18), 3)
}

// paletteDisplayRows returns the display rows (section headers and entries)
// and the row of each entry.
func (a *App) paletteDisplayRows(inner int) ([]string, []int) {
	p := &a.palette
	var rows []string
	entryRow := make([]int, len(p.entries))
	section := ""
	for i, e := range p.entries {
		if e.section != section {
			section = e.section
			rows = append(rows, mutedStyle().Bold(true).Render(section))
		}
		entryRow[i] = len(rows)
		rows = append(rows, a.paletteEntryLine(e, i == p.cursor, inner))
	}
	return rows, entryRow
}

// paletteEntryLine renders an entry as a line of exactly width cells.
func (a *App) paletteEntryLine(e paletteEntry, selected bool, width int) string {
	r := newRowStyler(selected)
	plain, muted := r.plain(), r.muted()
	var left, right string
	if c := e.cmd; c != nil {
		right = r.st(keyStyle()).Render(glyphs().localize(c.paletteKey()))
		left = r.titleStyle().Render(glyphs().localize(c.paletteTitle()))
		if e.reason != "" {
			left = muted.Render(glyphs().localize(c.paletteTitle())) + muted.Render(" "+glyphs().Dash+" "+e.reason)
			right = muted.Render(glyphs().localize(c.paletteKey()))
		}
	} else {
		left = beanRef(a.config, e.bean, r)
		if e.hidden != "" {
			right = muted.Render(e.hidden)
		}
	}
	rightW := lipgloss.Width(right)
	leftW := max(width-1-rightW-1, 1)
	left = fitWidth(truncateText(left, leftW), leftW, plain)
	return fitWidth(r.bar()+left+plain.Render(" ")+right, width, plain)
}

// view renders the palette as a modal.
func (p *commandPalette) view(a *App, rec hitRecorder) string {
	modalWidth := a.paletteModalWidth()
	inner := modalWidth - 4
	rowsH := a.paletteRows()

	p.input.Width = max(inner-lipgloss.Width(p.input.Prompt)-1, 1)
	p.input.PromptStyle = accentStyle().Bold(true)
	p.input.PlaceholderStyle = mutedStyle()
	p.input.Cursor.Style = accentStyle()

	rows, entryRow := a.paletteDisplayRows(inner)
	if len(p.entries) > 0 {
		// Keep the cursor visible, with its section header when it is the
		// section's first entry.
		cur := entryRow[p.cursor]
		top := cur
		if cur > 0 && (p.cursor == 0 || p.entries[p.cursor-1].section != p.entries[p.cursor].section) {
			top = cur - 1
		}
		if p.cursor == 0 {
			top = 0
		}
		if top < p.offset {
			p.offset = top
		}
		if cur >= p.offset+rowsH {
			p.offset = cur - rowsH + 1
		}
		p.offset = max(0, min(p.offset, len(rows)-rowsH))
	}
	var body []string
	if len(rows) == 0 {
		body = append(body, mutedStyle().Render("No matching commands or beans"))
	} else {
		body = rows[p.offset:min(p.offset+rowsH, len(rows))]
		for i, r := range entryRow {
			if r >= p.offset && r < p.offset+rowsH {
				// Below the border and the input and its rule.
				rec.add(2, 3+r-p.offset, inner, 1, hitTarget{kind: hitModalItem, index: i})
			}
		}
	}
	body = padLines(body, inner)
	for len(body) < rowsH {
		body = append(body, strings.Repeat(" ", inner))
	}

	rule := borderStyle().Render(strings.Repeat(glyphs().Horizontal, inner))
	hints := []keyHint{{glyphs().KeyUpDown, "move"}, {"enter", "run"}, {"esc", "close"}}
	footer := renderHints(hints, nil, inner)
	if p.message != "" {
		footer = clampWidth(fg(colWarning()).Bold(true).Render(p.message), inner)
	} else if n := len(p.entries); n > 0 {
		pos := mutedStyle().Render(fmt.Sprintf("%d/%d", p.cursor+1, n))
		footer = renderHints(hints, nil, max(inner-lipgloss.Width(pos)-1, 1))
		footer = fitWidth(footer, inner-lipgloss.Width(pos), lipgloss.NewStyle()) + pos
	}

	content := []string{clampWidth(p.input.View(), inner), rule}
	content = append(content, body...)
	content = append(content, rule, footer)

	return strings.Join(box{
		label:   "Command palette",
		lines:   content,
		width:   modalWidth,
		focused: true,
		padding: 1,
	}.render(), "\n")
}

// opensCommandPalette reports whether key opens the palette in the current
// state. Text inputs and open dropdowns keep their meaning of ctrl+p (up).
func (a *App) opensCommandPalette(key string) bool {
	if key != ":" && key != "ctrl+p" {
		return false
	}
	return a.state == viewList && !a.typing() && !(a.focus == focusFilter && a.bar.open)
}
