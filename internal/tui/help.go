package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openHelpMsg requests opening the help overlay
type openHelpMsg struct{}

// closeHelpMsg is sent when the help overlay is closed
type closeHelpMsg struct{}

// helpOverlayModel displays keyboard shortcuts organized by context
type helpOverlayModel struct {
	width  int
	height int
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
		switch msg.String() {
		case "?", "esc":
			return m, func() tea.Msg {
				return closeHelpMsg{}
			}
		}
	}

	return m, nil
}

// helpSection is a titled group of shortcuts in the help overlay.
type helpSection struct {
	title string
	keys  [][2]string // key, description
}

// helpSections lists the shortcuts of the browse view. The first two
// sections form the left column, the others the right one.
//
// Descriptions are kept short so that two columns fit on 70-column
// terminals, and the columns short enough to fit 24-line terminals.
var helpSections = []helpSection{
	{"Navigate", [][2]string{
		{"↑↓ j k", "Move"},
		{"←→ h l", "Collapse / expand"},
		{"zo zc za", "Open/close/toggle"},
		{"zR zM", "Open/close all"},
		{"gg G", "First / last bean"},
		{"n", "Next bean to do"},
		{"enter tab", "Focus details"},
		{"g w", "Workspaces"},
	}},
	{"Details", [][2]string{
		{"tab ⇧tab", "Sections / panes"},
		{"↑↓ j k", "Move / scroll"},
		{"pgup pgdn", "Scroll body"},
		{"[ ] x", "Checkbox / toggle"},
		{"enter", "Open related bean"},
		{"backspace", "Previous bean"},
		{"esc", "Back to the tree"},
	}},
	{"Filter", [][2]string{
		{"F ⇧tab", "Focus filter bar"},
		{"←→ enter", "Field / dropdown"},
		{"/", "Query, e.g. tag:ui"},
		{"H", "Hide/show done"},
		{"g t", "Filter by tag"},
		{"esc", "Clear filters"},
	}},
	{"Edit", [][2]string{
		{"c C", "New bean / child"},
		{"r", "Rename"},
		{"e", "Edit in $EDITOR"},
		{"s t P", "Status/type/prio"},
		{"p", "Set parent"},
		{"b", "Manage blocking"},
		{"K J", "Move up / down"},
		{"u", "Undo last change"},
		{"space", "Select for batch"},
		{"y", "Copy bean ID"},
		{"w", "Start in workspace"},
		{"W", "…and run agent"},
	}},
}

// renderHelpSections renders sections as one column of the given width.
func renderHelpSections(sections []helpSection, width int) []string {
	const keyW = 10
	var lines []string
	for i, sec := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, listTitleStyle().Render(sec.title))
		for _, k := range sec.keys {
			key := keyStyle().Width(keyW).Render(k[0])
			// Descriptions use the terminal's default foreground so they stay
			// readable on both dark and light themes.
			lines = append(lines, clampWidth(key+k[1], width))
		}
	}
	return lines
}

func (m helpOverlayModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	twoColumns := m.width >= 68
	modalWidth := min(max(m.width-4, 20), 48)
	if twoColumns {
		modalWidth = min(m.width-2, 80)
	}
	inner := modalWidth - 4 // border and padding

	var body []string
	if twoColumns {
		colW := (inner - 3) / 2
		left := renderHelpSections(helpSections[:2], colW)
		right := renderHelpSections(helpSections[2:], colW)
		body = joinColumns(3, padLines(left, colW), padLines(right, colW))
	} else {
		body = renderHelpSections(helpSections, inner)
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(colAccent()).Render("Keyboard Shortcuts")
	footer := renderHints([]keyHint{{"?/esc", "close"}, {"q", "quit"}}, nil, inner)
	content := title + "\n" + strings.Join(body, "\n") + "\n" + footer

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
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
