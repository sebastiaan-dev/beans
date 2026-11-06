package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
)

// The TUI's palette.
//
// Every color used by this package is resolved through the accessors below
// at render time. Never capture a color in a package-level lipgloss.Style:
// the theme may change at runtime, and styles built at init would keep the
// old colors. Build styles where they are rendered instead, using these
// accessors or the style helpers further down.
//
// The accessors map semantic roles onto the shared ui colors, so pointing a
// role at a different ui color (e.g. a dedicated theme role) only takes a
// change here.

// colAccent is the accent color: focus, cursor, keys, headings.
func colAccent() lipgloss.Color { return ui.ColorPrimary }

// colBorder is the color of unfocused borders and rules.
func colBorder() lipgloss.Color { return ui.ColorBorder }

// colFocusBorder is the color of the focused pane's border (the accent
// unless the theme or tui.theme_overrides set focus_border).
func colFocusBorder() lipgloss.Color { return ui.ColorFocusBorder }

// colSelection is the background of the selected tree row. The empty color
// (terminal theme) means "no background": with the 16-color palette there is
// no background guaranteed to contrast with every theme, so the cursor bar and
// the accent title mark the selection instead. Editor themes (e.g. nvim)
// provide one (their Visual/CursorLine background).
func colSelection() lipgloss.Color { return ui.ColorSelection }

// colFg is the default foreground: the theme's text color, or the terminal's
// own when empty.
func colFg() lipgloss.Color { return ui.ColorFg }

// colMuted is used for labels, hints and secondary information.
func colMuted() lipgloss.Color { return ui.ColorMuted }

// colSuccess, colWarning and colDanger are the status message colors.
func colSuccess() lipgloss.Color { return ui.ColorSuccess }
func colWarning() lipgloss.Color { return ui.ColorWarning }
func colDanger() lipgloss.Color  { return ui.ColorDanger }

// colDim is the foreground of the dimmed background behind modals.
func colDim() lipgloss.Color { return ui.ColorSubtle }

// colNamed resolves a color name from the beans configuration (status, type
// and priority colors).
func colNamed(name string) lipgloss.Color { return ui.ResolveColor(name) }

// fg returns a style with the given foreground. An empty color leaves the
// terminal's default foreground.
func fg(c lipgloss.Color) lipgloss.Style {
	if c == "" {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(c)
}

// withBackground adds a background to s unless c is empty.
func withBackground(s lipgloss.Style, c lipgloss.Color) lipgloss.Style {
	if c == "" {
		return s
	}
	return s.Background(c)
}

func mutedStyle() lipgloss.Style  { return fg(colMuted()) }
func accentStyle() lipgloss.Style { return fg(colAccent()) }
func borderStyle() lipgloss.Style { return fg(colBorder()) }

// keyStyle renders a key in hints and help.
func keyStyle() lipgloss.Style { return fg(colAccent()).Bold(true) }
