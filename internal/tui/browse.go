package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The browse view: one rounded frame around a header, the filter bar, the
// tree and detail panes and a footer with context-sensitive key hints.
//
//	╭──────────────────────────────────────────────╮
//	│ beans: project      / search · F filters · … │
//	│ ──────────────────────────────────────────── │
//	│ ╭─ Status ─╮ ╭─ Type ─╮ ╭─ Tags ─╮ ╭─ Done ─╮ │  filter bar (tall terminals)
//	│ ╭─ Beans ──────╮ ╭──────────────────────────╮ │
//	│ │ tree         │ │ detail boxes             │ │  side by side (wide terminals)
//	│ ╰──────────────╯ ╰──────────────────────────╯ │
//	│ ──────────────────────────────────────────── │
//	│ enter details · e edit · …                    │  footer (1-2 lines)
//	╰──────────────────────────────────────────────╯
//
// On narrow terminals the detail pane replaces the tree while it has focus.

const (
	// wideMinWidth is the minimum terminal width for side-by-side panes.
	wideMinWidth = 100
	// filterBarMinHeight is the minimum terminal height showing the filter bar.
	filterBarMinHeight = 28
	// twoLineFooterMinHeight is the minimum terminal height for a two-line footer.
	twoLineFooterMinHeight = 20
	// Below this size only a hint to enlarge the terminal is shown.
	minAppWidth  = 30
	minAppHeight = 10
)

// focusArea is the pane that receives keys in the browse view.
type focusArea int

const (
	focusTree focusArea = iota
	focusDetail
)

// browseLayout is the arrangement of the browse view for a terminal size.
type browseLayout struct {
	inner   int // content width inside the frame
	wide    bool
	showBar bool
	footer  []string
	treeW   int // outer width of the tree pane
	detailW int // outer width of the detail pane (wide only)
	paneH   int // outer height of the panes
}

func (a *App) layout() browseLayout {
	l := browseLayout{inner: max(a.width-4, 1)}
	l.wide = a.width >= wideMinWidth
	l.showBar = a.height >= filterBarMinHeight
	l.footer = a.footerLines(l.inner, l.wide)
	fixed := 2 + 1 + 1 + 1 + len(l.footer) // frame, header, two rules, footer
	if l.showBar {
		fixed += 3
	}
	l.paneH = max(a.height-fixed, 3)
	l.treeW = l.inner
	if l.wide {
		l.treeW = max(36, l.inner*2/5)
		l.detailW = l.inner - l.treeW - 1
	}
	return l
}

// syncLayout pushes the current layout's sizes into the panes and shows the
// right bean in the detail pane. It runs after every update.
func (a *App) syncLayout() {
	if a.width == 0 || a.height == 0 {
		return
	}
	l := a.layout()
	rowsH := l.paneH - 2
	if a.list.showSearchLine() {
		rowsH--
	}
	a.list.setSize(max(l.treeW-2, 1), max(rowsH, 1))
	if l.wide {
		a.detail.setSize(max(l.detailW-4, 1), max(l.paneH-2, 1))
	} else {
		a.detail.setSize(l.inner, l.paneH)
	}
	a.syncDetail()
	a.list.ensureVisible()
}

// syncDetail shows the bean under the tree cursor in the detail pane, or the
// bean opened from a relation if it is not in the (filtered) tree.
func (a *App) syncDetail() {
	force := a.detailStale
	a.detailStale = false
	if a.detailPinned != "" && a.list.byID[a.detailPinned] == nil {
		a.detailPinned = ""
	}
	if a.detailPinned != "" {
		a.detail.setBean(a.list.byID[a.detailPinned], force)
		return
	}
	a.detail.setBean(a.list.currentBean(), force)
	if a.detail.bean == nil && a.focus == focusDetail {
		a.focusTree()
	}
}

func (a *App) focusTree() {
	a.focus = focusTree
	a.detail.blur()
	a.detailPinned = ""
}

// projectName returns the name shown in the header.
func (a *App) projectName() string {
	if a.config != nil {
		if n := a.config.GetProjectName(); n != "" {
			return n
		}
	}
	if a.core != nil {
		return filepath.Base(filepath.Dir(a.core.Root()))
	}
	return ""
}

// Key hints --------------------------------------------------------------------

// keyHint is a single "key description" entry in a footer.
type keyHint struct {
	key  string
	desc string
}

func (h keyHint) render() string {
	return keyStyle().Render(h.key) + " " + mutedStyle().Render(h.desc)
}

// footerHintSeparator separates hints in a footer.
const footerHintSeparator = " · "

// fitHintLines lays out hints on lines of the given widths (one entry per
// line). Hints are given in order of importance and keep their order; a hint
// that does not fit the current line starts the next one, and on the last
// line hints that do not fit are dropped. Pinned hints are always shown at
// the end of the last line. A width <= 0 means unlimited.
func fitHintLines(hints, pinned []keyHint, widths []int) []string {
	if len(widths) == 0 {
		return nil
	}
	sepW := lipgloss.Width(footerHintSeparator)
	sep := mutedStyle().Render(footerHintSeparator)
	join := func(hs []keyHint) string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = h.render()
		}
		return strings.Join(parts, sep)
	}
	hintW := func(h keyHint) int { return lipgloss.Width(h.key) + 1 + lipgloss.Width(h.desc) }

	lines := make([][]keyHint, len(widths))
	used := make([]int, len(widths))
	last := len(widths) - 1
	pinnedW := 0
	for i, p := range pinned {
		if i > 0 {
			pinnedW += sepW
		}
		pinnedW += hintW(p)
	}
	limit := func(i int) int {
		if widths[i] <= 0 {
			return int(^uint(0) >> 1)
		}
		if i == last && pinnedW > 0 {
			return widths[i] - pinnedW - sepW
		}
		return widths[i]
	}

	li := 0
	for _, h := range hints {
		w := hintW(h)
		for {
			add := w
			if len(lines[li]) > 0 {
				add += sepW
			}
			if used[li]+add <= limit(li) {
				lines[li] = append(lines[li], h)
				used[li] += add
				break
			}
			if li == last || len(lines[li]) == 0 {
				break // drop it
			}
			li++
		}
	}

	out := make([]string, 0, len(widths))
	for i, hs := range lines {
		s := join(hs)
		if i == last && len(pinned) > 0 {
			if s != "" {
				s += sep
			}
			s += join(pinned)
		}
		if s == "" {
			continue
		}
		if widths[i] > 0 {
			s = clampWidth(s, widths[i])
		}
		out = append(out, s)
	}
	return out
}

// renderHints renders hints on a single line that fits width (see
// fitHintLines).
func renderHints(hints, pinned []keyHint, width int) string {
	return strings.Join(fitHintLines(hints, pinned, []int{width}), "")
}

// footerLines renders the footer: the status message or selection count and
// the hints of the focused pane, on at most two lines.
func (a *App) footerLines(width int, wide bool) []string {
	maxLines := 2
	if a.height > 0 && a.height < twoLineFooterMinHeight {
		maxLines = 1
	}
	var hints []keyHint
	if a.focus == focusDetail && a.detail.bean != nil {
		hints = a.detail.hints()
	} else {
		hints = a.list.treeHints(wide)
	}

	prefix := ""
	if n := len(a.list.selectedBeans); n > 0 && a.focus == focusTree {
		prefix = fg(colWarning()).Bold(true).Render(pluralize(n, "bean", "beans")+" selected") + mutedStyle().Render(footerHintSeparator)
	}

	var lines []string
	if a.list.statusMessage != "" {
		lines = append(lines, clampWidth(prefix+fg(colSuccess()).Bold(true).Render(a.list.statusMessage), width))
		if maxLines > 1 {
			lines = append(lines, fitHintLines(hints, nil, []int{width})...)
		}
		return lines
	}
	widths := make([]int, maxLines)
	for i := range widths {
		widths[i] = width
	}
	widths[0] = max(width-lipgloss.Width(prefix), 1)
	lines = fitHintLines(hints, nil, widths)
	if len(lines) == 0 {
		lines = []string{""}
	}
	lines[0] = clampWidth(prefix+lines[0], width)
	return lines
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// View -------------------------------------------------------------------------

// browseView renders the browse view at the terminal size.
func (a *App) browseView() string {
	if a.width < minAppWidth || a.height < minAppHeight {
		return mutedStyle().Render("Terminal too small for beans; please enlarge it.")
	}
	l := a.layout()
	var content []string
	content = append(content, a.headerLine(l.inner))
	content = append(content, borderStyle().Render(strings.Repeat("─", l.inner)))
	if l.showBar {
		content = append(content, a.filterBar(l.inner)...)
	}
	content = append(content, a.paneLines(l)...)
	content = append(content, borderStyle().Render(strings.Repeat("─", l.inner)))
	content = append(content, l.footer...)
	return strings.Join(frameLines(content, a.width, a.height), "\n")
}

// frameLines draws the app's outer frame around content lines, padding them
// to exactly width x height.
func frameLines(content []string, width, height int) []string {
	frame := borderStyle()
	inner := width - 4
	out := make([]string, 0, height)
	out = append(out, frame.Render("╭"+strings.Repeat("─", width-2)+"╮"))
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		out = append(out, frame.Render("│")+" "+fitWidth(line, inner, lipgloss.NewStyle())+" "+frame.Render("│"))
	}
	out = append(out, frame.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return out
}

// headerLine renders "beans: <project>" and the global key hints.
func (a *App) headerLine(width int) string {
	left := accentStyle().Bold(true).Render("beans:")
	if name := a.projectName(); name != "" {
		left += " " + accentStyle().Render(name)
	}
	left = clampWidth(left, width)
	room := width - lipgloss.Width(left) - 2
	right := ""
	if room > 0 {
		right = renderHints(
			[]keyHint{{"/", "search"}, {"F", "filters"}},
			[]keyHint{{"?", "help"}, {"q", "quit"}},
			room,
		)
		if lipgloss.Width(right) > room {
			right = ""
		}
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return left + strings.Repeat(" ", gap) + right
}

// filterValue renders the selected values of a filter category.
func filterValue(values []string, color func(string) lipgloss.Color) string {
	if len(values) == 0 {
		return mutedStyle().Render("(any)")
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fg(color(v)).Render(v)
	}
	return strings.Join(parts, ", ")
}

// filterBar renders the current filters as a row of labelled boxes.
func (a *App) filterBar(width int) []string {
	f := a.list.filters
	statusColor := func(v string) lipgloss.Color {
		if s := a.config.GetStatus(v); s != nil {
			return colNamed(s.Color)
		}
		return colFg()
	}
	typeColor := func(v string) lipgloss.Color {
		if t := a.config.GetType(v); t != nil {
			return colNamed(t.Color)
		}
		return colFg()
	}
	done := mutedStyle().Render("shown")
	if f.hideResolved {
		done = fg(colWarning()).Render("hidden")
	}
	type field struct{ label, value, hotkey string }
	fields := []field{
		{"Status", filterValue(f.statuses, statusColor), "F"},
		{"Type", filterValue(f.types, typeColor), "F"},
		{"Tags", filterValue(f.tags, func(string) lipgloss.Color { return colFg() }), "g t"},
		{"Completed", done, "H"},
	}
	if q := a.list.query(); q != "" {
		fields = append(fields, field{"Search", q, "/"})
	}
	ws := splitWidths(width, len(fields), 1)
	blocks := make([][]string, len(fields))
	for i, fl := range fields {
		blocks[i] = box{label: fl.label, hotkey: fl.hotkey, lines: []string{fl.value}, width: ws[i], padding: 1}.render()
	}
	return joinColumns(1, blocks...)
}

// treeLabel returns the label of the tree pane. Without the filter bar it
// carries the filter summary.
func (a *App) treeLabel(showBar bool) string {
	if !showBar {
		return a.list.title()
	}
	return strings.SplitN(a.list.title(), " · ", 2)[0]
}

// paneLines renders the tree and detail panes.
func (a *App) paneLines(l browseLayout) []string {
	if !l.wide && a.focus == focusDetail {
		return a.detail.viewLines()
	}
	var rows []string
	if a.list.showSearchLine() {
		rows = append(rows, a.list.searchLine(l.treeW-2))
	}
	rows = append(rows, a.list.viewLines()...)
	tree := box{
		label:    a.treeLabel(l.showBar),
		botRight: mutedStyle().Render(a.list.position()),
		lines:    rows,
		width:    l.treeW,
		height:   l.paneH,
		focused:  a.focus == focusTree,
	}.render()
	if !l.wide {
		return tree
	}
	detail := box{
		lines:   a.detail.viewLines(),
		width:   l.detailW,
		height:  l.paneH,
		focused: a.focus == focusDetail,
		padding: 1,
	}.render()
	return joinColumns(1, tree, detail)
}
