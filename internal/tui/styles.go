package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Shared styles. They are functions rather than package-level variables so
// that they pick up the current palette (see palette.go) when rendering.

// listTitleStyle renders list and section titles.
func listTitleStyle() lipgloss.Style { return fg(colAccent()).Bold(true) }

// helpStyle renders hint descriptions.
func helpStyle() lipgloss.Style { return mutedStyle() }

// helpKeyStyle renders hint keys.
func helpKeyStyle() lipgloss.Style { return keyStyle() }

// modalDimStyle renders the dimmed background behind modals.
func modalDimStyle() lipgloss.Style { return fg(colDim()) }

// applyListTheme replaces the bubbles list defaults (which use fixed hex and
// 256-color values) with ANSI palette styles so the list follows the terminal
// theme. Callers may further customize l.Styles afterwards, but must call
// this function again (or set l.FilterInput styles directly) if they change
// FilterPrompt or FilterCursor, because list.New copies those into the filter
// input at construction time.
func applyListTheme(l *list.Model) {
	s := list.DefaultStyles()

	s.Title = listTitleStyle()
	s.Spinner = mutedStyle()
	s.FilterPrompt = accentStyle()
	s.FilterCursor = accentStyle()
	s.StatusBar = s.StatusBar.Foreground(colMuted())
	s.StatusEmpty = mutedStyle()
	s.StatusBarActiveFilter = lipgloss.NewStyle()
	s.StatusBarFilterCount = mutedStyle()
	s.NoItems = mutedStyle()
	s.ArabicPagination = mutedStyle()
	s.ActivePaginationDot = s.ActivePaginationDot.Foreground(colFg())
	s.InactivePaginationDot = s.InactivePaginationDot.Foreground(colMuted())
	s.DividerDot = s.DividerDot.Foreground(colMuted())
	l.Styles = s

	l.FilterInput.PromptStyle = s.FilterPrompt
	l.FilterInput.Cursor.Style = s.FilterCursor
	l.FilterInput.PlaceholderStyle = mutedStyle()
	l.FilterInput.CompletionStyle = mutedStyle()
	l.Paginator.ActiveDot = s.ActivePaginationDot.String()
	l.Paginator.InactiveDot = s.InactivePaginationDot.String()

	l.Help.Styles.ShortKey = accentStyle()
	l.Help.Styles.ShortDesc = mutedStyle()
	l.Help.Styles.ShortSeparator = mutedStyle()
	l.Help.Styles.Ellipsis = mutedStyle()
	l.Help.Styles.FullKey = accentStyle()
	l.Help.Styles.FullDesc = mutedStyle()
	l.Help.Styles.FullSeparator = mutedStyle()
	applyListGlyphs(l)
}

// applyListGlyphs replaces the bubbles list's Unicode bullets, ellipses and
// arrow key names when the ASCII glyph set is in use.
func applyListGlyphs(l *list.Model) {
	if !asciiMode() {
		return
	}
	g := glyphs()
	l.Styles.ActivePaginationDot = l.Styles.ActivePaginationDot.SetString(g.PageActive)
	l.Styles.InactivePaginationDot = l.Styles.InactivePaginationDot.SetString(g.PageInactive)
	l.Styles.DividerDot = l.Styles.DividerDot.SetString(g.sep())
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	l.Help.ShortSeparator = g.sep()
	l.Help.Ellipsis = g.Loading
	l.KeyMap.CursorUp.SetHelp(g.KeyUp+"/k", "up")
	l.KeyMap.CursorDown.SetHelp(g.KeyDown+"/j", "down")
	l.KeyMap.PrevPage.SetHelp("h/pgup", "prev page")
	l.KeyMap.NextPage.SetHelp("l/pgdn", "next page")
}
