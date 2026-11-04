package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
)

var (
	// List title style
	listTitleStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)

	// Detail title style (badge: reverse video keeps theme contrast, see ui.badgeStyle)
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ui.ColorPrimary).
				Reverse(true).
				Padding(0, 1)

	// Help text style
	helpStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	// Help key style
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)

	// Dimmed background behind modals
	modalDimStyle = lipgloss.NewStyle().
			Foreground(ui.ColorSubtle)
)

// applyListTheme replaces the bubbles list defaults (which use fixed hex and
// 256-color values) with ANSI palette styles so the list follows the terminal
// theme. Callers may further customize l.Styles afterwards, but must call
// this function again (or set l.FilterInput styles directly) if they change
// FilterPrompt or FilterCursor, because list.New copies those into the filter
// input at construction time.
func applyListTheme(l *list.Model) {
	s := list.DefaultStyles()

	s.Title = listTitleStyle
	s.Spinner = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	s.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	s.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	s.StatusBar = s.StatusBar.Foreground(ui.ColorMuted)
	s.StatusEmpty = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	s.StatusBarActiveFilter = lipgloss.NewStyle()
	s.StatusBarFilterCount = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	s.NoItems = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	s.ArabicPagination = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	s.ActivePaginationDot = s.ActivePaginationDot.Foreground(ui.ColorDefault)
	s.InactivePaginationDot = s.InactivePaginationDot.Foreground(ui.ColorMuted)
	s.DividerDot = s.DividerDot.Foreground(ui.ColorMuted)
	l.Styles = s

	l.FilterInput.PromptStyle = s.FilterPrompt
	l.FilterInput.Cursor.Style = s.FilterCursor
	l.FilterInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	l.FilterInput.CompletionStyle = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	l.Paginator.ActiveDot = s.ActivePaginationDot.String()
	l.Paginator.InactiveDot = s.InactivePaginationDot.String()

	keyStyle := lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	descStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	sepStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	l.Help.Styles.ShortKey = keyStyle
	l.Help.Styles.ShortDesc = descStyle
	l.Help.Styles.ShortSeparator = sepStyle
	l.Help.Styles.Ellipsis = sepStyle
	l.Help.Styles.FullKey = keyStyle
	l.Help.Styles.FullDesc = descStyle
	l.Help.Styles.FullSeparator = sepStyle
}
