package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

// Row rendering shared by the tree, the views' lists, the relations, the
// picker and the command palette: a row starts with the cursor bar, the
// selected row is drawn with the selection background (colSelection) and
// an accent bold title, and rows are cut with an ellipsis to their width.

// rowStyler styles the parts of a selected or unselected row.
type rowStyler struct {
	selected bool
	bg       lipgloss.Color // background of every part ("" = none)
}

// newRowStyler returns the styler of a list row: selected rows get the
// selection background. (rowStyler{selected: true} highlights a row
// without a background, e.g. inside a detail box.)
func newRowStyler(selected bool) rowStyler {
	r := rowStyler{selected: selected}
	if selected {
		r.bg = colSelection()
	}
	return r
}

func (r rowStyler) st(s lipgloss.Style) lipgloss.Style { return withBackground(s, r.bg) }
func (r rowStyler) plain() lipgloss.Style              { return r.st(lipgloss.NewStyle()) }
func (r rowStyler) muted() lipgloss.Style              { return r.st(mutedStyle()) }

// bar renders the cursor bar starting a row.
func (r rowStyler) bar() string {
	if r.selected {
		return r.st(accentStyle()).Render(glyphs().Cursor)
	}
	return r.plain().Render(" ")
}

// titleStyle is the style of a bean title (accent when selected).
func (r rowStyler) titleStyle() lipgloss.Style {
	if r.selected {
		return r.st(accentStyle().Bold(true))
	}
	return r.plain()
}

// title renders a bean title.
func (r rowStyler) title(t string) string {
	if t == "" {
		return r.muted().Render("(untitled)")
	}
	return r.titleStyle().Render(t)
}

// beanRef renders "S T id title" for a bean: colored status and type
// glyphs, short ID and title.
func beanRef(cfg *config.Config, b *bean.Bean, r rowStyler) string {
	colors := cfg.GetBeanColors(b.Status, b.Type, b.Priority)
	sp := r.plain().Render(" ")
	return r.st(fg(colNamed(colors.StatusColor))).Render(glyph(b.Status, ui.ShortStatus)) + sp +
		r.st(fg(colNamed(colors.TypeColor))).Render(glyph(b.Type, ui.ShortType)) + sp +
		r.muted().Render(shortID(b.ID, cfg.Beans.Prefix)) + sp + r.title(b.Title)
}

// rowLine fits a rendered row to width: cut with an ellipsis or padded with
// the row's background.
func rowLine(s string, width int, r rowStyler) string {
	if lipgloss.Width(s) > width && width > 1 {
		return clampWidth(s, width-1) + r.muted().Render(glyphs().Ellipsis)
	}
	return fitWidth(s, width, r.plain())
}
