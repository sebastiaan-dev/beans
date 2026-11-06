package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

// Inline title input in the tree: creating a bean ("c" sibling, "C" child)
// and renaming one ("r") without leaving the tree.

// inlineKind is what an inline input does.
type inlineKind int

const (
	inlineCreateSibling inlineKind = iota
	inlineCreateChild
	inlineRename
)

// inlineEdit is an active inline title input.
type inlineEdit struct {
	kind     inlineKind
	anchorID string // selected bean when the input started ("" = empty tree)
	parentID string // parent of the new bean
	beanType string // type of the new bean
	input    textinput.Model
}

// inlineCreateMsg asks the App to create a bean.
type inlineCreateMsg struct {
	title    string
	parentID string
	beanType string
	afterID  string // sibling to place the new bean right after ("" = none)
}

// inlineRenameMsg asks the App to change a bean's title.
type inlineRenameMsg struct {
	id    string
	title string
}

// childTypeFor returns the type of a new child of a bean of parentType:
// one level down the hierarchy (milestone → epic → feature → task), else the
// configured default type, as long as it may have such a parent. It reports
// false if beans of parentType can't have children.
func childTypeFor(cfg *config.Config, parentType string) (string, bool) {
	var candidates []string
	switch parentType {
	case "milestone":
		candidates = append(candidates, "epic")
	case "epic":
		candidates = append(candidates, "feature")
	case "feature":
		candidates = append(candidates, "task")
	}
	def := config.DefaultType
	if cfg != nil {
		def = cfg.GetDefaultType()
	}
	candidates = append(candidates, def, "task")
	for _, t := range candidates {
		if cfg != nil && cfg.GetType(t) == nil {
			continue
		}
		if slices.Contains(beancore.ValidParentTypes(t), parentType) {
			return t, true
		}
	}
	return "", false
}

// typing reports whether a text input of the tree (search or inline title)
// has the focus.
func (m listModel) typing() bool {
	return m.searching || m.inline != nil
}

// newInlineInput returns a focused single-line input.
func newInlineInput(value, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 200
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Focus()
	return ti
}

// startCreate starts an inline input for a new bean below the cursor: a
// sibling of the selected bean (same parent and type), or (child) a child of
// it, typed one level down the hierarchy (see childTypeFor). Without a
// selected bean, a top-level bean of the default type is created.
func (m *listModel) startCreate(child bool) {
	cur := m.currentBean()
	in := &inlineEdit{kind: inlineCreateSibling}
	def := config.DefaultType
	if m.config != nil {
		def = m.config.GetDefaultType()
	}
	in.beanType = def
	switch {
	case child && cur == nil:
		return
	case child:
		t, ok := childTypeFor(m.config, cur.Type)
		if !ok {
			m.statusMessage = cur.Type + " beans can't have children"
			return
		}
		in.kind, in.anchorID, in.parentID, in.beanType = inlineCreateChild, cur.ID, cur.ID, t
		// Show the new child among the existing ones.
		if r, ok := m.currentRow(); ok && r.collapsed {
			m.setFolded(cur.ID, false)
		}
	case cur != nil:
		// Siblings resemble each other: same parent, same type.
		in.anchorID = cur.ID
		if p := m.byID[cur.Parent]; p != nil {
			in.parentID = p.ID
		}
		if m.config == nil || m.config.GetType(cur.Type) != nil {
			in.beanType = cur.Type
		}
	}
	in.input = newInlineInput("", "title of the new "+in.beanType)
	m.inline = in
	m.heights = nil
	m.ensureVisible()
}

// startRename starts an inline input for the selected bean's title.
func (m *listModel) startRename() {
	cur := m.currentBean()
	if cur == nil {
		return
	}
	m.inline = &inlineEdit{kind: inlineRename, anchorID: cur.ID, input: newInlineInput(cur.Title, "title")}
	m.heights = nil
	m.ensureVisible()
}

func (m *listModel) stopInline() {
	m.inline = nil
	m.heights = nil
	m.ensureVisible()
}

// syncInline cancels the inline input if its bean disappeared.
func (m *listModel) syncInline() {
	if m.inline != nil && m.inline.anchorID != "" && m.rowIndex(m.inline.anchorID) < 0 {
		m.stopInline()
	}
}

// updateInline handles keys while the inline input has the focus.
func (m listModel) updateInline(msg tea.KeyMsg) (listModel, tea.Cmd) {
	in := m.inline
	switch msg.String() {
	case "esc":
		m.stopInline()
		return m, nil
	case "enter":
		title := strings.TrimSpace(in.input.Value())
		m.stopInline()
		if title == "" {
			return m, nil
		}
		if in.kind == inlineRename {
			if b := m.byID[in.anchorID]; b == nil || b.Title == title {
				return m, nil
			}
			return m, func() tea.Msg { return inlineRenameMsg{id: in.anchorID, title: title} }
		}
		create := inlineCreateMsg{title: title, parentID: in.parentID, beanType: in.beanType}
		if in.kind == inlineCreateSibling {
			create.afterID = in.anchorID
		}
		return m, func() tea.Msg { return create }
	}
	var cmd tea.Cmd
	in.input, cmd = in.input.Update(msg)
	return m, cmd
}

// creating reports whether an inline create input is shown.
func (m listModel) creating() bool {
	return m.inline != nil && m.inline.kind != inlineRename
}

// renaming reports whether row i is being renamed.
func (m listModel) renaming(i int) bool {
	return m.inline != nil && m.inline.kind == inlineRename && i < len(m.rows) && m.rows[i].bean.ID == m.inline.anchorID
}

// inlineInsertRow returns the row after which the create input is shown, or
// -1 for the top: right below the new child's parent, or below the selected
// bean's subtree for a sibling (where the new bean will be placed).
func (m listModel) inlineInsertRow() int {
	if !m.creating() {
		return -1
	}
	i := m.rowIndex(m.inline.anchorID)
	if i < 0 || m.inline.kind == inlineCreateChild {
		return i
	}
	d := m.rows[i].depth
	for i+1 < len(m.rows) && m.rows[i+1].depth > d {
		i++
	}
	return i
}

// inlineInput renders the inline text input fitting width.
func (m listModel) inlineInput(width int) string {
	ti := m.inline.input
	ti.Width = max(width-1, 1)
	ti.PlaceholderStyle = mutedStyle()
	ti.TextStyle = accentStyle().Bold(true)
	ti.Cursor.Style = accentStyle()
	return ti.View()
}

// inlineCreateLine renders the input row for a new bean.
func (m listModel) inlineCreateLine(width int) string {
	in := m.inline
	depth := 0
	if i := m.rowIndex(in.anchorID); i >= 0 {
		depth = m.rows[i].depth
		if in.kind == inlineCreateChild {
			depth++
		}
	}
	colors := m.config.GetBeanColors(m.config.GetDefaultStatus(), in.beanType, "")
	prefix := accentStyle().Render(glyphs().Cursor) + strings.Repeat("  ", depth) +
		accentStyle().Render("+ ") +
		fg(colNamed(colors.StatusColor)).Render(glyph(m.config.GetDefaultStatus(), ui.ShortStatus)) + " " +
		fg(colNamed(colors.TypeColor)).Render(glyph(in.beanType, ui.ShortType)) + " "
	return fitWidth(prefix+m.inlineInput(width-lipgloss.Width(prefix)), width, lipgloss.NewStyle())
}

// inlineHints returns the footer hints while the inline input has focus.
func (m listModel) inlineHints() []keyHint {
	switch m.inline.kind {
	case inlineRename:
		return []keyHint{{"enter", "save title"}, {"esc", "cancel"}}
	case inlineCreateChild:
		return []keyHint{{"enter", "create child " + m.inline.beanType}, {"esc", "cancel"}}
	default:
		return []keyHint{{"enter", "create " + m.inline.beanType}, {"esc", "cancel"}}
	}
}

// treeParentID returns the parent of b in the tree: its parent, or "" if it
// has none or the parent does not exist.
func treeParentID(b *bean.Bean, byID map[string]*bean.Bean) string {
	if b.Parent != "" && byID[b.Parent] != nil {
		return b.Parent
	}
	return ""
}
