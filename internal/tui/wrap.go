package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// token is a word of styled text for wrapTokens.
type token struct {
	text  string
	style lipgloss.Style
}

// wordTokens splits text into word tokens sharing one style.
func wordTokens(text string, style lipgloss.Style) []token {
	words := strings.Fields(text)
	toks := make([]token, len(words))
	for i, w := range words {
		toks[i] = token{text: w, style: style}
	}
	return toks
}

// splitToWidth splits plain text into chunks of at most w cells.
func splitToWidth(s string, w int) []string {
	var chunks []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curW+rw > w && curW > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// wrapTokens wraps tokens greedily onto lines of at most width cells, with
// single spaces (in space's style) between them. Tokens wider than a line are
// split. At most maxLines lines are returned (0 = unlimited); if the text
// does not fit, the last line ends with an ellipsis. The returned lines are
// rendered but not padded.
func wrapTokens(toks []token, width, maxLines int, space lipgloss.Style) []string {
	width = max(width, 1)

	// Split overlong tokens so every token fits on a line.
	var parts []token
	for _, t := range toks {
		if lipgloss.Width(t.text) <= width {
			parts = append(parts, t)
			continue
		}
		for _, c := range splitToWidth(t.text, width) {
			parts = append(parts, token{text: c, style: t.style})
		}
	}

	var lines [][]token
	var cur []token
	curW := 0
	for _, t := range parts {
		tw := lipgloss.Width(t.text)
		if len(cur) > 0 && curW+1+tw > width {
			lines = append(lines, cur)
			cur, curW = nil, 0
		}
		if len(cur) > 0 {
			curW++
		}
		cur = append(cur, t)
		curW += tw
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}

	if maxLines > 0 && len(lines) > maxLines {
		last := append([]token{}, lines[maxLines-1]...)
		lines = lines[:maxLines]
		// Make room for the ellipsis.
		for {
			w := tokensWidth(last)
			if w+1 <= width || len(last) == 0 {
				break
			}
			end := &last[len(last)-1]
			excess := w + 1 - width
			tw := lipgloss.Width(end.text)
			if tw <= excess {
				last = last[:len(last)-1]
				continue
			}
			end.text = clampWidth(end.text, tw-excess)
		}
		if len(last) > 0 {
			last[len(last)-1].text += glyphs().Ellipsis
		} else {
			last = []token{{text: glyphs().Ellipsis, style: space}}
		}
		lines[maxLines-1] = last
	}

	out := make([]string, len(lines))
	for i, l := range lines {
		var b strings.Builder
		for j, t := range l {
			if j > 0 {
				b.WriteString(space.Render(" "))
			}
			b.WriteString(t.style.Render(t.text))
		}
		out[i] = b.String()
	}
	return out
}

// tokensWidth returns the width of tokens joined by single spaces.
func tokensWidth(toks []token) int {
	w := 0
	for i, t := range toks {
		if i > 0 {
			w++
		}
		w += lipgloss.Width(t.text)
	}
	return w
}
