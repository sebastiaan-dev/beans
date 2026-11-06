package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Markdown task-list checkboxes ("- [ ] item", "- [x] item") in bean bodies.
//
// The detail pane lets the user step through the checkboxes of the body and
// toggle them. Toggling changes a single byte of the body, so everything
// else (spacing, line endings, other markdown) is preserved exactly.

// checkboxItem is a task-list checkbox in a markdown body.
type checkboxItem struct {
	// state is the byte offset of the character between the brackets
	// (' ', 'x' or 'X').
	state   int
	checked bool
	// text is the item's text after "] ", trimmed (for messages).
	text string
}

// fenceStart returns the fence character and length if line (without
// indentation) opens or closes a fenced code block.
func fenceStart(trimmed string) (byte, int) {
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0
	}
	c := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	// A backtick fence's info string must not contain backticks.
	if c == '`' && strings.IndexByte(trimmed[n:], '`') >= 0 {
		return 0, 0
	}
	return c, n
}

// stripContainerPrefix removes leading whitespace and blockquote markers.
func stripContainerPrefix(line string, offset int) (string, int) {
	for {
		trimmed := strings.TrimLeft(line, " \t")
		offset += len(line) - len(trimmed)
		line = trimmed
		if !strings.HasPrefix(line, ">") {
			return line, offset
		}
		line, offset = line[1:], offset+1
	}
}

// listCheckbox parses a list item line (without container prefix) and
// returns the offset of the checkbox state character within it, or -1.
func listCheckbox(s string) int {
	i := 0
	switch {
	case len(s) > 0 && (s[0] == '-' || s[0] == '*' || s[0] == '+'):
		i = 1
	default:
		for i < len(s) && i < 9 && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i >= len(s) || (s[i] != '.' && s[i] != ')') {
			return -1
		}
		i++
	}
	// At least one space or tab after the marker.
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j == i || j+3 > len(s) {
		return -1
	}
	if s[j] != '[' || s[j+2] != ']' || (s[j+1] != ' ' && s[j+1] != 'x' && s[j+1] != 'X') {
		return -1
	}
	// The checkbox must be followed by whitespace or the end of the line.
	if k := j + 3; k < len(s) && s[k] != ' ' && s[k] != '\t' {
		return -1
	}
	return j + 1
}

// findCheckboxes returns the task-list checkboxes of a markdown body in
// order. Checkboxes in fenced code blocks are ignored; nested lists and
// lists in blockquotes are supported.
func findCheckboxes(body string) []checkboxItem {
	var items []checkboxItem
	var fenceChar byte
	fenceLen := 0
	for start := 0; start < len(body); {
		end := strings.IndexByte(body[start:], '\n')
		if end < 0 {
			end = len(body)
		} else {
			end += start
		}
		line := strings.TrimSuffix(body[start:end], "\r")
		content, offset := stripContainerPrefix(line, start)

		if c, n := fenceStart(content); c != 0 {
			switch {
			case fenceChar == 0:
				fenceChar, fenceLen = c, n
			case c == fenceChar && n >= fenceLen && strings.TrimSpace(content[n:]) == "":
				fenceChar, fenceLen = 0, 0
			}
		} else if fenceChar == 0 {
			if i := listCheckbox(content); i >= 0 {
				state := offset + i
				items = append(items, checkboxItem{
					state:   state,
					checked: body[state] != ' ',
					text:    strings.TrimSpace(content[i+2:]),
				})
			}
		}
		start = end + 1
	}
	return items
}

// toggleCheckbox flips the idx-th checkbox of body. Only the state byte
// changes. It reports false if there is no such checkbox.
func toggleCheckbox(body string, idx int) (string, bool) {
	items := findCheckboxes(body)
	if idx < 0 || idx >= len(items) {
		return body, false
	}
	it := items[idx]
	state := byte('x')
	if it.checked {
		state = ' '
	}
	return body[:it.state] + string(state) + body[it.state+1:], true
}

// markCheckbox inserts a marker after the idx-th checkbox of body, so that
// its rendered line can be found and highlighted. It returns the marked body
// and the marker, or "" if there is no such checkbox or no free marker.
func markCheckbox(body string, idx int) (string, string) {
	items := findCheckboxes(body)
	if idx < 0 || idx >= len(items) {
		return body, ""
	}
	for _, mk := range glyphs().CheckboxMarkers {
		if !strings.Contains(body, mk) {
			at := items[idx].state + 2 // after "]"
			return body[:at] + " " + mk + body[at:], mk
		}
	}
	return body, ""
}

// Checkbox cursor of the detail pane --------------------------------------------

// toggleCheckboxMsg asks the App to toggle a checkbox of a bean's body.
type toggleCheckboxMsg struct {
	id    string
	index int
}

// showCheckboxCursor reports whether the current checkbox is highlighted:
// while the body has the focus and has checkboxes.
func (m detailModel) showCheckboxCursor() bool {
	return m.focused && m.section == sectionBody && len(m.checks) > 0
}

// checkboxKey returns the part of the body cache key depending on the
// checkbox cursor.
func (m detailModel) checkboxKey() string {
	if !m.showCheckboxCursor() {
		return "-"
	}
	return fmt.Sprint(m.checkCursor)
}

// renderBody renders the bean's body, highlighting the current checkbox.
func (m *detailModel) renderBody() {
	m.checkLine = -1
	body := m.bean.Body
	if strings.TrimSpace(body) == "" {
		m.body = []string{mutedStyle().Render("(no body)")}
		return
	}
	marker := ""
	if m.showCheckboxCursor() {
		body, marker = markCheckbox(body, m.checkCursor)
	}
	lines := renderMarkdown(body, m.bodyWidth())
	if marker != "" {
		for i, l := range lines {
			if strings.Contains(l, marker) {
				lines[i] = strings.Replace(l, marker, accentStyle().Bold(true).Render(marker), 1)
				m.checkLine = i
				break
			}
		}
	}
	m.body = lines
}

// scrollToCheckbox scrolls the body so that the current checkbox is visible.
func (m *detailModel) scrollToCheckbox(rows int) {
	if m.checkLine < 0 || rows <= 0 {
		return
	}
	if m.checkLine < m.bodyOffset {
		m.bodyOffset = m.checkLine
	} else if m.checkLine >= m.bodyOffset+rows {
		m.bodyOffset = m.checkLine - rows + 1
	}
}

// handleCheckboxKey handles the checkbox keys of the focused body: "]" and
// "[" move the cursor, "x" and space toggle the current checkbox.
func (m *detailModel) handleCheckboxKey(key string) (bool, tea.Cmd) {
	if m.bean == nil || m.section != sectionBody || len(m.checks) == 0 {
		return false, nil
	}
	switch key {
	case "]", "[":
		if key == "]" {
			m.checkCursor = min(m.checkCursor+1, len(m.checks)-1)
		} else {
			m.checkCursor = max(m.checkCursor-1, 0)
		}
		m.checkScroll = true
		m.prepare()
		return true, nil
	case "x", " ":
		msg := toggleCheckboxMsg{id: m.bean.ID, index: m.checkCursor}
		m.checkScroll = true
		return true, func() tea.Msg { return msg }
	}
	return false, nil
}
