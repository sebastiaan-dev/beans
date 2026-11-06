package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openHelpMsg requests opening the help overlay
type openHelpMsg struct{}

// closeHelpMsg is sent when the help overlay is closed
type closeHelpMsg struct{}

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

func (m helpOverlayModel) Init() tea.Cmd {
	return nil
}

func (m helpOverlayModel) Update(msg tea.Msg) (helpOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		page := max(m.bodyRows()-1, 1)
		switch msg.String() {
		case "?", "esc":
			return m, func() tea.Msg {
				return closeHelpMsg{}
			}
		case ":", "ctrl+p":
			return m, func() tea.Msg { return openCommandPaletteMsg{} }
		case "up", "k":
			m.scroll(-1)
		case "down", "j":
			m.scroll(1)
		case "pgup", "ctrl+u", "ctrl+b":
			m.scroll(-page)
		case "pgdown", "ctrl+d", "ctrl+f", " ":
			m.scroll(page)
		case "home", "g":
			m.offset = 0
		case "end", "G":
			m.scroll(len(m.bodyLines()))
		}
	}

	return m, nil
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

func (m *helpOverlayModel) scroll(delta int) {
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
	m.scroll(0)
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
	content := title + "\n" + strings.Join(body, "\n") + "\n" + footer

	border := lipgloss.NewStyle().
		Border(glyphs().border()).
		BorderForeground(colAccent()).
		Padding(0, 1).
		Width(modalWidth - 2)

	return border.Render(content)
}

// padLines pads lines to width cells.
func padLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fitWidth(l, width, lipgloss.NewStyle())
	}
	return out
}

// ModalView returns the help overlay as a centered modal on top of the background
func (m helpOverlayModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
