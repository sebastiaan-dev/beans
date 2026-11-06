package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Overlays: the modals shown centered over the dimmed browse view, one at a
// time. App.state says which one is open (viewList: none); activeOverlay
// returns it. While an overlay is open it receives all keys (except ctrl+c)
// and the messages the App does not handle itself; the mouse reaches it
// through click (its hitModalItem regions) and scroll (the wheel), and a
// click outside closes it like esc.

// overlay is a modal over the browse view.
type overlay interface {
	// update handles a key or another message while the overlay is open.
	update(a *App, msg tea.Msg) tea.Cmd
	// view renders the modal. Regions are recorded at rec relative to the
	// modal's top-left corner.
	view(a *App, rec hitRecorder) string
	// acceptsText reports whether typed characters go into a text input,
	// so batched runes must be kept together (see handleKeys).
	acceptsText() bool
	// click chooses item i, recorded as a hitModalItem region.
	click(a *App, i int) tea.Cmd
	// scroll moves the overlay's cursor by delta (mouse wheel).
	scroll(a *App, delta int)
}

// viewState is the overlay shown over the browse view.
type viewState int

const (
	viewList           viewState = iota // no overlay
	viewPicker                          // pickerModel (picker.go)
	viewHelpOverlay                     // helpOverlayModel (help.go)
	viewWorkspaces                      // workspacesOverlay (workspaces.go)
	viewCommandPalette                  // commandPalette (cmdpalette.go)
)

// activeOverlay returns the open overlay, or nil.
func (a *App) activeOverlay() overlay {
	switch a.state {
	case viewPicker:
		return &a.picker
	case viewHelpOverlay:
		return &a.helpOverlay
	case viewWorkspaces:
		return &a.wsOverlay
	case viewCommandPalette:
		return &a.palette
	}
	return nil
}

// closeOverlay closes the open overlay, as esc does. Closing a picker
// reloads the beans in case they changed while it was open.
func (a *App) closeOverlay() tea.Cmd {
	if a.state == viewPicker {
		return a.closePicker(false)
	}
	a.state = viewList
	return nil
}

// overlayView renders the base view (browse view or reader) with the overlay o on top and
// records the modal and its regions.
func (a *App) overlayView(o overlay) string {
	local := &hitMap{}
	modal := o.view(a, local.recorder())
	bg := a.baseView()
	x, y := modalOrigin(modal, a.width, a.height)
	rec := a.hits.recorder()
	rec.add(x, y, lipgloss.Width(modal), len(strings.Split(modal, "\n")), hitTarget{kind: hitModal})
	rec.place(local, x, y)
	return overlayModal(bg, modal, a.width, a.height)
}

// clickOverlay handles a click while an overlay is open: a click outside
// closes it, a click on an item chooses it.
func (a *App) clickOverlay(o overlay, x, y int) tea.Cmd {
	reg, ok := a.hits.find(x, y, hitModalItem, hitModal)
	if !ok {
		return a.closeOverlay()
	}
	if reg.kind == hitModalItem {
		return o.click(a, reg.index)
	}
	return nil
}

// modalOrigin returns the top-left cell of a modal centered on a width x
// height screen (see overlayModal).
func modalOrigin(modal string, width, height int) (int, int) {
	lines := strings.Split(modal, "\n")
	x := max((width-lipgloss.Width(modal))/2, 0)
	y := max((height-len(lines))/2, 0)
	return x, y
}

// overlayModal places a modal on top of a background view, dimmed and
// stripped of its styles. Positions are terminal cells, so wide runes (CJK,
// emoji) in the background don't shift the modal.
func overlayModal(bgView, modal string, width, height int) string {
	bgLines := strings.Split(bgView, "\n")

	// Pad or truncate background to fill the screen
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	if len(bgLines) > height {
		bgLines = bgLines[:height]
	}

	// Center position (also used for mouse hit-testing)
	startX, startY := modalOrigin(modal, width, height)
	modalLines := strings.Split(modal, "\n")

	dimStyle := modalDimStyle()
	for i, line := range bgLines {
		if mi := i - startY; mi >= 0 && mi < len(modalLines) {
			bgLines[i] = overlayLine(line, modalLines[mi], startX, width)
		} else {
			bgLines[i] = dimStyle.Render(clampWidth(stripAnsi(line), width))
		}
	}
	return strings.Join(bgLines, "\n")
}

// overlayLine places a modal line on top of a background line at cell
// startX: the background, stripped, dimmed and padded to maxWidth cells,
// shows left and right of the modal. A wide rune cut by an edge of the modal
// becomes spaces.
func overlayLine(bgLine, modalLine string, startX, maxWidth int) string {
	plain := lipgloss.NewStyle()
	bg := fitWidth(stripAnsi(bgLine), maxWidth, plain)
	prefix := fitWidth(bg, startX, plain)
	suffix := dropCells(bg, startX+lipgloss.Width(modalLine))

	dimStyle := modalDimStyle()
	return dimStyle.Render(prefix) + modalLine + dimStyle.Render(suffix)
}

// stripAnsi removes ANSI escape codes from a string
func stripAnsi(s string) string {
	result := strings.Builder{}
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
