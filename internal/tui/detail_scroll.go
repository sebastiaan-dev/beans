package tui

import "github.com/charmbracelet/lipgloss"

// Scrolling the detail pane as a whole.
//
// When the detail boxes do not fit the pane even after shrinking them (see
// detailModel.layout), clipping would hide the lower boxes. Instead every
// box is shown in full (the body unscrolled, all relations, the history)
// and the whole pane scrolls: ↑↓ and the page keys scroll it while the body
// has the focus, and the relation cursor, a newly focused section and the
// current checkbox are kept in view.

// scrollLayout returns the layout of a pane that scrolls as a whole.
func (m detailModel) scrollLayout() detailLayout {
	l := detailLayout{scroll: true, history: true}
	l.titleLines = wrapTokens(wordTokens(m.bean.Title, lipgloss.NewStyle().Bold(true)), max(m.width-4, 1), 2, lipgloss.NewStyle())
	if len(l.titleLines) == 0 {
		l.titleLines = []string{mutedStyle().Render("(untitled)")}
	}
	l.metaRows = rowsFor(m.width, 3)
	l.extraRows = rowsFor(m.width, 2)
	l.relRows = max(len(m.links), 1)
	l.bodyRows = max(len(m.body), 1)
	return l
}

// bodyTop returns the first line of the body box.
func (l detailLayout) bodyTop() int {
	return 2 + len(l.titleLines) + 3*l.metaRows + 3*l.extraRows
}

// relationsTop returns the first line of the relations box.
func (l detailLayout) relationsTop() int {
	return l.bodyTop() + l.bodyRows + 2
}

// totalHeight returns the height of all boxes.
func (m detailModel) totalHeight(l detailLayout) int {
	n := l.relationsTop() + l.relRows + 2
	if l.history {
		n += m.historyHeight()
	}
	return n
}

// scrollBy scrolls the pane (if it scrolls as a whole) or the body by delta
// lines.
func (m *detailModel) scrollBy(delta int) {
	l := m.layout()
	if l.scroll {
		m.paneOffset = max(0, min(m.paneOffset+delta, m.totalHeight(l)-m.height))
		return
	}
	m.bodyOffset = max(0, min(m.bodyOffset+delta, len(m.body)-l.bodyRows))
}

// scrollHome scrolls to the top of the pane or the body.
func (m *detailModel) scrollHome() {
	m.paneOffset, m.bodyOffset = 0, 0
}

// scrollEnd scrolls to the end of the pane or the body.
func (m *detailModel) scrollEnd() {
	m.scrollBy(len(m.body) + m.totalHeight(m.layout()))
}

// syncPaneOffset keeps the focused part of a pane that scrolls as a whole
// in view: a newly focused section (as much of it as fits), the relation cursor and,
// after moving it, the current checkbox. It resets the offset when the
// boxes fit.
func (m *detailModel) syncPaneOffset(l detailLayout, checkScroll bool) {
	if !l.scroll {
		m.paneOffset = 0
		m.scrollSection, m.scrollFocused = m.section, m.focused
		return
	}
	h := max(m.height, 1)
	show := func(top, bottom int) {
		if bottom > m.paneOffset+h {
			m.paneOffset = bottom - h
		}
		if top < m.paneOffset {
			m.paneOffset = top
		}
	}
	if m.focused && (m.section != m.scrollSection || !m.scrollFocused) {
		switch m.section {
		case sectionBody:
			show(l.bodyTop(), min(l.bodyTop()+l.bodyRows+2, l.bodyTop()+h))
		case sectionRelations:
			show(l.relationsTop(), min(l.relationsTop()+l.relRows+2, l.relationsTop()+h))
		}
	}
	m.scrollSection, m.scrollFocused = m.section, m.focused
	if m.focused && m.section == sectionRelations && len(m.links) > 0 {
		line := l.relationsTop() + 1 + m.relCursor
		show(line, line+1)
	}
	if checkScroll && m.checkLine >= 0 {
		line := l.bodyTop() + 1 + m.checkLine
		show(line, line+1)
	}
	m.paneOffset = max(0, min(m.paneOffset, m.totalHeight(l)-h))
}
