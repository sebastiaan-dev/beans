package tui

import (
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
)

// Glyphs ------------------------------------------------------------------------
//
// Every non-ASCII character the TUI draws comes from the current glyph set:
// unicodeGlyphs by default, asciiGlyphs with tui.ascii: true or
// BEANS_TUI_ASCII=1 (for terminals and fonts without these characters).
// Code must not use non-ASCII literals for UI chrome; use glyphs() instead.
//
// Glyphs used in layouts that assume a width (frame characters, the tree
// cursor and chevrons, the truncation marker, ...) have the same width in
// both sets, so both sets lay out identically.

// glyphSet is a set of the characters the TUI draws.
type glyphSet struct {
	// Box drawing (rounded corners in Unicode, + - | in ASCII).
	TopLeft, TopRight, BottomLeft, BottomRight string
	Horizontal, Vertical                       string

	// Ellipsis marks truncated text; it is one cell wide.
	Ellipsis string
	// Loading follows texts of pending actions ("Loading…").
	Loading string
	// Sep separates items in labels, hints and messages (" · ").
	Sep string
	// Range separates the ends of a range ("1–5/9") and stands for a
	// missing value.
	Range string

	// Tree rows.
	Cursor     string // current row, picker cursor
	Marked     string // row in the multi-selection
	FoldOpen   string // expanded node
	FoldClosed string // collapsed node
	Implicit   string // prefix of an inherited status ("↑completed")

	// Priority symbols in the tree, by priority name.
	Priority map[string]string

	// Workspaces.
	Workspace string // workspace badge
	Dirty     string // uncommitted changes
	Ahead     string // commits ahead of the base branch
	Behind    string // commits behind the base branch

	// Filter bar and dropdowns.
	Clear       string // clear-field button
	AtLeast     string // priority ">= high"
	AtMost      string // priority "<= low"
	ScrollLeft  string // more fields to the left
	ScrollRight string // more fields to the right
	Prompt      string // type-to-filter prompt
	TextCursor  string // text cursor of the type-to-filter input
	Radio       string // selected radio option, inside "( )"
	ActiveField string // active field in the one-line filter bar

	// Key names in hints and the help overlay.
	KeyUpDown    string
	KeyLeftRight string
	KeyUp        string
	KeyDown      string
	KeyShift     string // prefix of shifted keys ("⇧tab")

	// Blocking picker.
	Blocking    string
	NotBlocking string

	// Pagination dots of the pickers.
	PageActive, PageInactive string

	// CheckboxMarkers are candidate markers of the current checkbox in a
	// rendered body; the first one not occurring in the body is used.
	CheckboxMarkers []string
}

var unicodeGlyphs = glyphSet{
	TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
	Horizontal: "─", Vertical: "│",

	Ellipsis: "…",
	Loading:  "…",
	Sep:      "·",
	Range:    "–",

	Cursor:     "▌",
	Marked:     "•",
	FoldOpen:   "▾",
	FoldClosed: "▸",
	Implicit:   "↑",

	Priority: map[string]string{"critical": "‼", "high": "!", "low": "↓", "deferred": "→"},

	Workspace: "⎇",
	Dirty:     "●",
	Ahead:     "↑",
	Behind:    "↓",

	Clear:       "✕",
	AtLeast:     "≥",
	AtMost:      "≤",
	ScrollLeft:  "‹",
	ScrollRight: "›",
	Prompt:      "›",
	TextCursor:  "▏",
	Radio:       "•",
	ActiveField: "▸",

	KeyUpDown:    "↑↓",
	KeyLeftRight: "←→",
	KeyUp:        "↑",
	KeyDown:      "↓",
	KeyShift:     "⇧",

	Blocking:    "●",
	NotBlocking: "○",

	PageActive: "•", PageInactive: "○",

	CheckboxMarkers: []string{"▶", "►", "➤", "⮞"},
}

var asciiGlyphs = glyphSet{
	TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	Horizontal: "-", Vertical: "|",

	Ellipsis: "~",
	Loading:  "...",
	Sep:      "-",
	Range:    "-",

	Cursor:     ">",
	Marked:     "*",
	FoldOpen:   "-",
	FoldClosed: "+",
	Implicit:   "^",

	Priority: map[string]string{"critical": "!!", "high": "!", "low": "-", "deferred": "_"},

	Workspace: "@",
	Dirty:     "*",
	Ahead:     "+",
	Behind:    "-",

	Clear:       "x",
	AtLeast:     ">=",
	AtMost:      "<=",
	ScrollLeft:  "<",
	ScrollRight: ">",
	Prompt:      ">",
	TextCursor:  "_",
	Radio:       "*",
	ActiveField: ">",

	KeyUpDown:    "up/dn",
	KeyLeftRight: "lt/rt",
	KeyUp:        "up",
	KeyDown:      "dn",
	KeyShift:     "S-",

	Blocking:    "*",
	NotBlocking: "o",

	PageActive: "*", PageInactive: ".",

	CheckboxMarkers: []string{"=>", "->", ">>", "*>"},
}

// currentGlyphs is the glyph set in use (see setASCII).
var currentGlyphs = &unicodeGlyphs

// glyphs returns the glyph set in use.
func glyphs() *glyphSet { return currentGlyphs }

// setASCII switches between the Unicode and the ASCII glyph set. Call it
// before rendering, e.g. from Run.
func setASCII(on bool) {
	if on {
		currentGlyphs = &asciiGlyphs
	} else {
		currentGlyphs = &unicodeGlyphs
	}
	resetMarkdownRenderers()
}

// asciiMode reports whether the ASCII glyph set is in use.
func asciiMode() bool { return currentGlyphs == &asciiGlyphs }

// localize translates the Unicode key names, ellipses and separators of a
// static text (such as the help overlay's key table, which is defined
// before a glyph set is chosen) to the glyph set in use.
func (g *glyphSet) localize(s string) string {
	if g != &asciiGlyphs {
		return s
	}
	return asciiTextReplacer.Replace(s)
}

var asciiTextReplacer = strings.NewReplacer(
	unicodeGlyphs.KeyUpDown, asciiGlyphs.KeyUpDown,
	unicodeGlyphs.KeyLeftRight, asciiGlyphs.KeyLeftRight,
	unicodeGlyphs.KeyUp, asciiGlyphs.KeyUp,
	unicodeGlyphs.KeyDown, asciiGlyphs.KeyDown,
	unicodeGlyphs.KeyShift, asciiGlyphs.KeyShift,
	unicodeGlyphs.Loading, asciiGlyphs.Loading,
	unicodeGlyphs.Sep, asciiGlyphs.Sep,
)

// sep returns the separator surrounded by spaces (" · ").
func (g *glyphSet) sep() string { return " " + g.Sep + " " }

// prioritySymbol returns the tree symbol of a priority ("" for normal).
func (g *glyphSet) prioritySymbol(priority string) string { return g.Priority[priority] }

// border returns the lipgloss border of modals.
func (g *glyphSet) border() lipgloss.Border {
	if g == &asciiGlyphs {
		return lipgloss.Border{
			Top: "-", Bottom: "-", Left: "|", Right: "|",
			TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
			MiddleLeft: "+", MiddleRight: "+", Middle: "+", MiddleTop: "+", MiddleBottom: "+",
		}
	}
	return lipgloss.RoundedBorder()
}

// applyMarkdownGlyphs replaces the non-ASCII decorations of a glamour style
// (bullets, task marks, quote bars, rules, ...) with ASCII ones when the
// ASCII glyph set is in use.
func applyMarkdownGlyphs(s *ansi.StyleConfig) {
	if !asciiMode() {
		return
	}
	str := func(v string) *string { return &v }
	s.BlockQuote.IndentToken = str("| ")
	s.HorizontalRule.Format = "\n--------\n"
	s.Item.BlockPrefix = "* "
	s.Enumeration.BlockPrefix = ". "
	s.Task.Ticked = "[x] "
	s.Task.Unticked = "[ ] "
	s.ImageText.Format = "Image: {{.text}} ->"
	s.Table.CenterSeparator = str("|")
	s.Table.ColumnSeparator = str("|")
	s.Table.RowSeparator = str("-")
	s.DefinitionDescription.BlockPrefix = "\n* "
}
