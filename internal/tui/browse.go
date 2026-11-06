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
//	│ beans: project       / query · F filters · … │
//	│ ──────────────────────────────────────────── │
//	│ ╭─ Status ─╮ ╭─ Type ─╮ ╭─ Tags ─╮ ╭─ Done ─╮ │  filter bar (one line on short terminals)
//	│ ╭─ Beans ──────╮ ╭──────────────────────────╮ │
//	│ │ tree         │ │ detail boxes             │ │  side by side (wide terminals)
//	│ ╰──────────────╯ ╰──────────────────────────╯ │
//	│ ──────────────────────────────────────────── │
//	│ enter details · e edit · …                    │  footer (1-2 lines)
//	╰──────────────────────────────────────────────╯
//
// On narrow or short terminals only the tree is shown; the detail pane
// replaces it while it has focus (enter). Detail boxes that do not fit make
// the detail pane scroll as a whole (detail_scroll.go). Below
// minAppWidth x minAppHeight only a "terminal too small" message is shown.

const (
	// wideMinWidth and wideMinHeight are the minimum terminal size for
	// side-by-side panes.
	wideMinWidth  = 100
	wideMinHeight = 16
	// filterBarMinHeight is the minimum terminal height showing the full
	// filter bar; shorter terminals show it as a single line.
	filterBarMinHeight = 28
	// twoLineFooterMinHeight is the minimum terminal height for a two-line footer.
	twoLineFooterMinHeight = 20
	// Below this size only a hint to enlarge the terminal is shown.
	minAppWidth  = 40
	minAppHeight = 12
)

// focusArea is the pane that receives keys in the browse view.
type focusArea int

const (
	focusTree focusArea = iota
	focusDetail
	focusFilter
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
	l.wide = a.width >= wideMinWidth && a.height >= wideMinHeight
	l.showBar = a.height >= filterBarMinHeight
	l.footer = a.footerLines(l.inner, l.wide)
	fixed := 2 + 1 + 1 + 1 + len(l.footer) // frame, header, two rules, footer
	if l.showBar {
		fixed += 3
	} else {
		fixed++ // one-line filter bar
	}
	l.paneH = max(a.height-fixed, 3)
	l.treeW = l.inner
	if l.wide {
		l.treeW = max(36, l.inner*2/5)
		if a.activeList() != nil {
			l.treeW = max(36, l.inner/2) // view rows carry more columns
		}
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
	a.syncView(max(l.treeW-2, 1), a.viewRowsHeight(l.paneH))
	if l.wide {
		a.detail.setSize(max(l.detailW-4, 1), max(l.paneH-2, 1))
	} else {
		a.detail.setSize(l.inner, l.paneH)
	}
	a.syncDetail()
	a.list.ensureVisible()
}

// syncDetail shows the bean selected in the active view in the detail pane,
// or the bean opened from a relation if the view does not list it.
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
	a.detail.setBean(a.currentBean(), force)
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
func footerHintSeparator() string { return glyphs().sep() }

// fitHintLines lays out hints on lines of the given widths (one entry per
// line). Hints are given in order of importance and keep their order; a hint
// that does not fit the current line starts the next one, and on the last
// line hints that do not fit are dropped. Pinned hints are always shown at
// the end of the last line. A width <= 0 means unlimited.
func fitHintLines(hints, pinned []keyHint, widths []int) []string {
	if len(widths) == 0 {
		return nil
	}
	sepW := lipgloss.Width(footerHintSeparator())
	sep := mutedStyle().Render(footerHintSeparator())
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
	switch {
	case a.focus == focusFilter:
		hints = a.filterBarHints()
	case a.focus == focusDetail && a.detail.bean != nil:
		hints = a.detail.hints()
	case a.activeList() != nil:
		hints = a.viewHints(wide)
	default:
		hints = a.list.treeHints(wide)
	}

	prefix := ""
	if n := len(a.list.selectedBeans); n > 0 && a.focus == focusTree && a.views.active == viewTree {
		prefix = fg(colWarning()).Bold(true).Render(pluralize(n, "bean", "beans")+" selected") + mutedStyle().Render(footerHintSeparator())
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
		return a.tooSmallView()
	}
	l := a.layout()
	var content []string
	content = append(content, a.headerLine(l.inner))
	content = append(content, borderStyle().Render(strings.Repeat(glyphs().Horizontal, l.inner)))
	if l.showBar {
		content = append(content, a.filterBar(l.inner)...)
	} else {
		line, _ := a.filterSummaryLine(l.inner)
		content = append(content, line)
	}
	content = append(content, a.paneLines(l)...)
	content = a.overlayDropdown(content, len(content)-l.paneH, l.inner, !l.showBar)
	content = append(content, borderStyle().Render(strings.Repeat(glyphs().Horizontal, l.inner)))
	content = append(content, l.footer...)
	return strings.Join(frameLines(content, a.width, a.height), "\n")
}

// tooSmallView asks to enlarge a terminal below the minimum size, centered
// and cut to the terminal size.
func (a *App) tooSmallView() string {
	msg := []string{
		fg(colWarning()).Bold(true).Render("Terminal too small"),
		mutedStyle().Render(fmt.Sprintf("%dx%d, beans needs %dx%d", a.width, a.height, minAppWidth, minAppHeight)),
	}
	w, h := max(a.width, 1), max(a.height, 1)
	if len(msg) > h {
		msg = msg[:h]
	}
	lines := make([]string, h)
	top := (h - len(msg)) / 2
	for i := range lines {
		if j := i - top; j >= 0 && j < len(msg) {
			l := clampWidth(msg[j], w)
			lines[i] = fitWidth(strings.Repeat(" ", (w-lipgloss.Width(l))/2)+l, w, lipgloss.NewStyle())
		} else {
			lines[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(lines, "\n")
}

// frameLines draws the app's outer frame around content lines, padding them
// to exactly width x height.
func frameLines(content []string, width, height int) []string {
	frame := borderStyle()
	g := glyphs()
	inner := width - 4
	out := make([]string, 0, height)
	out = append(out, frame.Render(g.TopLeft+strings.Repeat(g.Horizontal, width-2)+g.TopRight))
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		out = append(out, frame.Render(g.Vertical)+" "+fitWidth(line, inner, lipgloss.NewStyle())+" "+frame.Render(g.Vertical))
	}
	out = append(out, frame.Render(g.BottomLeft+strings.Repeat(g.Horizontal, width-2)+g.BottomRight))
	return out
}

// headerLine renders "beans: <project>", the view tabs and the global key
// hints.
func (a *App) headerLine(width int) string {
	left := accentStyle().Bold(true).Render("beans:")
	if name := a.projectName(); name != "" {
		left += " " + accentStyle().Render(name)
	}
	if tabs := a.headerTabs(left, width); tabs != "" {
		left += "  " + tabs
	}
	left = clampWidth(left, width)
	room := width - lipgloss.Width(left) - 2
	right := ""
	if room > 0 {
		right = renderHints(
			[]keyHint{cmdHint("palette"), cmdHint("query"), cmdHint("focus-filter")},
			[]keyHint{cmdHint("help"), cmdHint("quit")},
			room,
		)
		if lipgloss.Width(right) > room {
			right = ""
		}
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return left + strings.Repeat(" ", gap) + right
}

// paneLines renders the tree and detail panes.
func (a *App) paneLines(l browseLayout) []string {
	if !l.wide && a.focus == focusDetail {
		return a.detail.viewLines()
	}
	if a.activeList() != nil {
		return a.joinDetail(l, a.viewPaneLines(l))
	}
	var rows []string
	if a.list.showSearchLine() {
		rows = append(rows, a.list.searchLine(l.treeW-2))
	}
	rows = append(rows, a.list.viewLines()...)
	tree := box{
		label:    a.list.title(),
		botRight: mutedStyle().Render(a.list.position()),
		lines:    rows,
		width:    l.treeW,
		height:   l.paneH,
		focused:  a.focus == focusTree,
	}.render()
	return a.joinDetail(l, tree)
}

// joinDetail places the detail pane beside the list pane on wide terminals.
func (a *App) joinDetail(l browseLayout, tree []string) []string {
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
