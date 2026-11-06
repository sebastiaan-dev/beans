package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpOverlayModel displays the keyboard shortcuts of the command registry
// (commands.go) by section, scrollable when they don't fit.
type helpOverlayModel struct {
	width  int
	height int
	offset int // first body line shown
}

func newHelpOverlayModel(width, height int) helpOverlayModel {
	return helpOverlayModel{
		width:  width,
		height: height,
	}
}

func (m *helpOverlayModel) acceptsText() bool       { return false }
func (m *helpOverlayModel) click(*App, int) tea.Cmd { return nil }

func (m *helpOverlayModel) scroll(_ *App, delta int) { m.scrollBy(delta) }

func (m *helpOverlayModel) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	page := max(m.bodyRows()-1, 1)
	switch key.String() {
	case "?", "esc":
		return a.closeOverlay()
	case ":", "ctrl+p":
		return a.openCommandPalette()
	case "q":
		return tea.Quit
	case "up", "k":
		m.scrollBy(-1)
	case "down", "j":
		m.scrollBy(1)
	case "pgup", "ctrl+u", "ctrl+b":
		m.scrollBy(-page)
	case "pgdown", "ctrl+d", "ctrl+f", " ":
		m.scrollBy(page)
	case "home", "g":
		m.offset = 0
	case "end", "G":
		m.scrollBy(len(m.bodyLines()))
	}
	return nil
}

// view renders the help at the terminal size.
func (m *helpOverlayModel) view(a *App, _ hitRecorder) string {
	m.width, m.height = a.width, a.height
	return m.View()
}

// helpSection is a titled group of shortcuts in the help overlay.
type helpSection struct {
	title string
	keys  [][2]string // key, description
}

// helpSections returns the shortcuts of the browse view from the command
// registry, in helpSectionOrder.
//
// Keys and descriptions are kept short (see TestHelpEntriesFit) so that two
// columns fit on 70-column terminals.
func helpSections() []helpSection {
	var out []helpSection
	for _, title := range helpSectionOrder {
		sec := helpSection{title: title}
		for i := range commands {
			if c := &commands[i]; c.section == title && !c.noHelp {
				sec.keys = append(sec.keys, [2]string{c.keyText(), c.help})
			}
		}
		out = append(out, sec)
	}
	return out
}

// helpKeyWidth is the width of the key column of the help.
const helpKeyWidth = 10

// renderHelpSections renders sections as one column of the given width.
func renderHelpSections(sections []helpSection, width int) []string {
	g := glyphs()
	keyW := helpKeyWidth
	for _, sec := range sections {
		for _, k := range sec.keys {
			keyW = max(keyW, lipgloss.Width(g.localize(k[0]))+1)
		}
	}
	var lines []string
	for i, sec := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, listTitleStyle().Render(sec.title))
		for _, k := range sec.keys {
			key := keyStyle().Width(keyW).Render(g.localize(k[0]))
			// Descriptions use the terminal's default foreground so they stay
			// readable on both dark and light themes.
			lines = append(lines, clampWidth(key+g.localize(k[1]), width))
		}
	}
	return lines
}

// sectionHeight is the number of lines a section takes in a column.
func sectionHeight(s helpSection) int { return 1 + len(s.keys) }

// splitHelpColumns splits sections into two columns of similar height,
// keeping their order.
func splitHelpColumns(sections []helpSection) (left, right []helpSection) {
	total := 0
	for _, s := range sections {
		total += sectionHeight(s) + 1
	}
	used := 0
	for i, s := range sections {
		h := sectionHeight(s) + 1
		if i > 0 && used+h/2 >= total/2 {
			return sections[:i], sections[i:]
		}
		used += h
	}
	return sections, nil
}

// twoColumns reports whether the help is shown in two columns.
func (m helpOverlayModel) twoColumns() bool { return m.width >= 68 }

// modalWidth is the outer width of the help.
func (m helpOverlayModel) modalWidth() int {
	if m.twoColumns() {
		return min(m.width-2, 80)
	}
	return min(max(m.width-4, 20), 48)
}

// bodyLines returns all lines of the help's body.
func (m helpOverlayModel) bodyLines() []string {
	inner := m.modalWidth() - 4 // border and padding
	sections := helpSections()
	if !m.twoColumns() {
		return renderHelpSections(sections, inner)
	}
	colW := (inner - 3) / 2
	l, r := splitHelpColumns(sections)
	left := renderHelpSections(l, colW)
	right := renderHelpSections(r, colW)
	return joinColumns(3, padLines(left, colW), padLines(right, colW))
}

// bodyRows is the number of body lines that fit the terminal: everything
// but the border, the title line and the footer.
func (m helpOverlayModel) bodyRows() int {
	return max(min(m.height-2-4, len(m.bodyLines())), 1)
}

func (m *helpOverlayModel) scrollBy(delta int) {
	maxOffset := max(len(m.bodyLines())-m.bodyRows(), 0)
	m.offset = max(0, min(m.offset+delta, maxOffset))
}

func (m helpOverlayModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	modalWidth := m.modalWidth()
	inner := modalWidth - 4 // border and padding
	all := m.bodyLines()
	rows := m.bodyRows()
	m.scrollBy(0)
	body := all[m.offset:min(m.offset+rows, len(all))]

	title := lipgloss.NewStyle().Bold(true).Foreground(colAccent()).Render("Keyboard Shortcuts")
	palette := keyStyle().Render(":") + " " + lipgloss.NewStyle().Bold(true).Render("command palette")
	if gap := inner - lipgloss.Width(title) - lipgloss.Width(palette); gap > 0 {
		title += strings.Repeat(" ", gap) + palette
	} else {
		title = palette
	}

	hints := []keyHint{{"?/esc", "close"}, {":", "palette"}}
	scrollable := len(all) > rows
	if scrollable {
		hints = append([]keyHint{{"j/k", "scroll"}, {"pgdn", "page"}}, hints...)
	}
	footer := renderHints(hints, []keyHint{{"q", "quit"}}, inner)
	if scrollable {
		pos := mutedStyle().Render(fmt.Sprintf("%d%s%d/%d", m.offset+1, glyphs().Range, m.offset+len(body), len(all)))
		footer = renderHints(hints, []keyHint{{"q", "quit"}}, max(inner-lipgloss.Width(pos)-1, 1))
		footer = fitWidth(footer, inner-lipgloss.Width(pos), lipgloss.NewStyle()) + pos
	}
	lines := append(append([]string{title}, body...), footer)
	return strings.Join(box{lines: lines, width: modalWidth, focused: true, padding: 1}.render(), "\n")
}

// padLines pads lines to width cells.
func padLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fitWidth(l, width, lipgloss.NewStyle())
	}
	return out
}
