package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Box drawing for the browse view.
//
// The labelled boxes (label in the top border, shortcut key in the bottom
// border) follow the design of the field boxes in rierek/lif's TUI
// (internal/tui/box_dropdown.go, boxTopBorder/boxBottomBorder; Apache-2.0).
// The implementation here is independent.

// clampWidth truncates s (which may contain ANSI escapes) to at most w cells.
func clampWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// fitWidth truncates or pads s to exactly w cells. The padding uses pad's
// style, so a background can extend to the edge.
func fitWidth(s string, w int, pad lipgloss.Style) string {
	s = clampWidth(s, w)
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += pad.Render(strings.Repeat(" ", gap))
	}
	return s
}

// truncateText shortens s (which may contain ANSI escapes) to at most w cells, ending with "…" if it
// had to be cut.
func truncateText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return clampWidth(s, w-1) + "…"
}

// box describes a bordered box with a label in its top border and optional
// badges in the borders.
type box struct {
	label    string // shown in the top border, left
	topRight string // badge in the top border, right (pre-rendered)
	hotkey   string // shortcut shown as "(key)" in the bottom border, right
	botRight string // badge in the bottom border, right (pre-rendered), used if hotkey is empty
	lines    []string
	width    int  // outer width
	height   int  // outer height; 0 means fit the content
	focused  bool // accent border
	padding  int  // horizontal padding inside the border
}

// render draws the box. Every line is exactly b.width cells wide and, if
// b.height > 0, the box is exactly b.height lines tall (content is cut or
// padded with blank lines).
func (b box) render() []string {
	w := max(b.width, 4)
	frame := borderStyle()
	if b.focused {
		frame = fg(colFocusBorder())
	}

	inner := w - 2 - 2*b.padding
	lines := b.lines
	if b.height > 0 {
		n := max(b.height-2, 0)
		if len(lines) > n {
			lines = lines[:n]
		}
		for len(lines) < n {
			lines = append(lines, "")
		}
	}

	out := make([]string, 0, len(lines)+2)
	out = append(out, borderLine(frame, "╭", "╮", w, b.labelSegment(), b.topRight))
	pad := strings.Repeat(" ", b.padding)
	for _, l := range lines {
		out = append(out, frame.Render("│")+pad+fitWidth(l, inner, lipgloss.NewStyle())+pad+frame.Render("│"))
	}
	right := b.botRight
	if b.hotkey != "" {
		right = keyStyle().Render("(" + b.hotkey + ")")
	}
	out = append(out, borderLine(frame, "╰", "╯", w, "", right))
	return out
}

func (b box) labelSegment() string {
	if b.label == "" {
		return ""
	}
	label := b.label
	if b.focused {
		return accentStyle().Bold(true).Render(label)
	}
	return mutedStyle().Render(label)
}

// borderLine renders a horizontal border of width w:
// <left>─ <label> ───…─── <right> ─<rightCorner>. The label is cut to fit; the
// right badge is dropped if it does not fit next to the label.
func borderLine(frame lipgloss.Style, left, rightCorner string, w int, label, right string) string {
	interior := w - 2
	if interior < 0 {
		return ""
	}
	var seg string
	used := 0
	if label != "" && interior >= 4 {
		label = clampWidth(label, interior-3)
		seg = frame.Render("─ ") + label + " "
		used = 3 + lipgloss.Width(label)
	}
	var tail string
	tailW := 0
	if right != "" {
		tailW = lipgloss.Width(right) + 2 + 1 // " badge " + "─"
		if used+tailW+1 > interior {
			right, tailW = "", 0
		} else {
			tail = " " + right + " " + frame.Render("─")
		}
	}
	fill := interior - used - tailW
	return frame.Render(left) + seg + frame.Render(strings.Repeat("─", max(fill, 0))) + tail + frame.Render(rightCorner)
}

// joinColumns places blocks of lines side by side, separated by gap spaces.
// Each block must have lines of equal width; shorter blocks are padded with
// blank lines of their width.
func joinColumns(gap int, blocks ...[]string) []string {
	height := 0
	widths := make([]int, len(blocks))
	for i, b := range blocks {
		height = max(height, len(b))
		for _, l := range b {
			widths[i] = max(widths[i], lipgloss.Width(l))
		}
	}
	sep := strings.Repeat(" ", gap)
	out := make([]string, height)
	for row := range out {
		parts := make([]string, len(blocks))
		for i, b := range blocks {
			if row < len(b) {
				parts[i] = fitWidth(b[row], widths[i], lipgloss.NewStyle())
			} else {
				parts[i] = strings.Repeat(" ", widths[i])
			}
		}
		out[row] = strings.Join(parts, sep)
	}
	return out
}

// splitWidths divides total cells into n widths separated by gap cells.
func splitWidths(total, n, gap int) []int {
	if n <= 0 {
		return nil
	}
	avail := total - gap*(n-1)
	ws := make([]int, n)
	for i := range ws {
		ws[i] = avail / n
		if i < avail%n {
			ws[i]++
		}
	}
	return ws
}
