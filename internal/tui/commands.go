package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
)

// The command registry: one table of every action of the browse view.
//
// The help overlay is generated from it, the footer hints refer to its
// entries (cmdHint) and the command palette lists and runs them. A command
// runs by pressing its key binding (pressCommandKeys), so the palette goes
// through exactly the same code paths as the keyboard; key dispatch itself
// stays in the panes' handlers. Tests check that the registry, the help and
// the footers agree (commands_test.go).

// cmdScope is where a command's keys work.
type cmdScope int

const (
	// scopeGlobal commands work from the tree and the detail pane, acting
	// on the bean shown in the focused pane.
	scopeGlobal cmdScope = iota
	// scopeTree commands act on the tree; the palette focuses the tree
	// before running them.
	scopeTree
	// scopeDetail keys work in the focused detail pane.
	scopeDetail
	// scopeFilter keys work in the focused filter bar.
	scopeFilter
)

// command is an action of the browse view.
type command struct {
	id      string
	section string   // help section (helpSectionOrder)
	keys    []string // key bindings as reported by tea.KeyMsg.String(); chords as "g t"
	label   string   // keys as shown in the help (default: keyLabels(keys))
	help    string   // description in the help overlay
	title   string   // palette title (default: help); "…" marks a picker
	hintKey string   // key shown in footer hints (default: the first key's label)
	hint    string   // footer hint description
	scope   cmdScope
	// noHelp hides the command from the help overlay (it is listed through
	// a combined entry, e.g. "1 2 3 4 Switch view").
	noHelp bool
	// avail reports why the command can't run now ("" if it can).
	avail func(a *App) string
	// run runs the command from the palette; nil for keys that only make
	// sense in their pane (moving the cursor, ...), which the palette
	// doesn't list.
	run func(a *App, c *command) tea.Cmd
}

// helpSectionOrder is the order of the help overlay's sections.
var helpSectionOrder = []string{"General", "Navigate", "Details", "Filter", "Edit", "Workspaces"}

// commands is the registry. It is built in init because the run functions
// reach code that looks commands up (the footer hints), which a package
// level initializer would turn into an initialization cycle.
var commands []command

func init() {
	commands = buildCommands()
}

func buildCommands() []command {
	press := pressCommandKeys
	return []command{
		// General
		{id: "palette", section: "General", keys: []string{":", "ctrl+p"}, label: ": ^p", help: "Command palette", hint: "commands"},
		{id: "help", section: "General", keys: []string{"?"}, help: "Keyboard shortcuts", title: "Show keyboard shortcuts", hint: "help", run: press},
		{id: "undo", section: "General", keys: []string{"u"}, help: "Undo last change", hint: "undo", avail: canUndo, run: press},
		{id: "quit", section: "General", keys: []string{"q"}, help: "Quit", title: "Quit beans", hint: "quit", run: press},

		// Navigate
		{id: "move", section: "Navigate", keys: []string{"up", "k", "down", "j"}, label: "↑↓ j k", help: "Move", hintKey: "↑↓", hint: "move", scope: scopeTree},
		{id: "page", section: "Navigate", keys: []string{"pgup", "pgdown", "ctrl+b", "ctrl+f", "ctrl+u", "ctrl+d"}, label: "pgup pgdn", help: "Page up / down", scope: scopeTree},
		{id: "views", section: "Navigate", keys: []string{"1", "2", "3", "4"}, label: "1 2 3 4", help: "Switch view"},
		{id: "view-tree", section: "Navigate", keys: []string{"1"}, help: "Tree view", title: "Show the tree", noHelp: true, run: press},
		{id: "view-ready", section: "Navigate", keys: []string{"2"}, help: "Ready view", title: "Show ready beans", noHelp: true, run: press},
		{id: "view-blocked", section: "Navigate", keys: []string{"3"}, help: "Blocked view", title: "Show blocked beans", noHelp: true, run: press},
		{id: "view-audit", section: "Navigate", keys: []string{"4"}, help: "Audit view", title: "Show audit findings", noHelp: true, run: press},
		{id: "audit-rerun", section: "Navigate", keys: []string{"R"}, help: "Re-run the audit", title: "Re-run the audit", noHelp: true, avail: inAuditView, run: press},
		{id: "first", section: "Navigate", keys: []string{"g g", "home"}, label: "gg home", help: "First bean", title: "Go to first bean", scope: scopeTree, avail: needsRows, run: press},
		{id: "last", section: "Navigate", keys: []string{"G", "end"}, label: "G end", help: "Last bean", title: "Go to last bean", scope: scopeTree, avail: needsRows, run: press},
		{id: "next", section: "Navigate", keys: []string{"n"}, help: "Next bean to do", title: "Jump to next bean to do", hint: "next", run: press},
		{id: "fold", section: "Navigate", keys: []string{"left", "h", "right", "l"}, label: "←→ h l", help: "Collapse / expand", hintKey: "←→", hint: "fold", scope: scopeTree},
		{id: "fold-open", section: "Navigate", keys: []string{"z o"}, label: "zo", help: "Open fold", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "fold-close", section: "Navigate", keys: []string{"z c"}, label: "zc", help: "Close fold", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "fold-toggle", section: "Navigate", keys: []string{"z a"}, label: "za", help: "Toggle fold", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "open-all", section: "Navigate", keys: []string{"z R"}, label: "zR", help: "Open all folds", hintKey: "zR", scope: scopeTree, avail: needsRows, run: press},
		{id: "close-all", section: "Navigate", keys: []string{"z M"}, label: "zM", help: "Close all folds", hintKey: "zM", scope: scopeTree, avail: needsRows, run: press},
		{id: "focus-detail", section: "Navigate", keys: []string{"enter", "tab"}, help: "Focus details", title: "Focus the details pane", hint: "details", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "select", section: "Navigate", keys: []string{" "}, help: "Select for batch", title: "Toggle selection of bean", hint: "select", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "clear-selection", section: "Navigate", keys: []string{"esc"}, help: "Clear selection", hint: "clear selection", scope: scopeTree, avail: needsSelection, run: press},

		// Details
		{id: "detail-sections", section: "Details", keys: []string{"tab", "shift+tab"}, help: "Sections / panes", hintKey: "tab", hint: "body", scope: scopeDetail},
		{id: "detail-move", section: "Details", keys: []string{"up", "k", "down", "j"}, label: "↑↓ j k", help: "Move / scroll", hintKey: "↑↓", hint: "scroll", scope: scopeDetail},
		{id: "detail-page", section: "Details", keys: []string{"pgup", "pgdown"}, label: "pgup pgdn", help: "Scroll body", hintKey: "pgup/pgdn", hint: "scroll body", scope: scopeDetail},
		{id: "checkbox-select", section: "Details", keys: []string{"[", "]"}, label: "[ ]", help: "Select checkbox", hintKey: "[ ]", hint: "checkbox", scope: scopeDetail},
		{id: "checkbox-toggle", section: "Details", keys: []string{"x"}, help: "Toggle checkbox", hint: "toggle", scope: scopeDetail},
		{id: "open-relation", section: "Details", keys: []string{"enter"}, help: "Open related bean", hint: "open", scope: scopeDetail},
		{id: "previous-bean", section: "Details", keys: []string{"backspace"}, help: "Previous bean", hintKey: "bksp", hint: "previous", scope: scopeDetail},
		{id: "back", section: "Details", keys: []string{"esc"}, help: "Back to the tree", title: "Focus the tree", hint: "back", scope: scopeDetail, avail: needsFocusAway, run: func(a *App, _ *command) tea.Cmd { a.focusTree(); return nil }},

		// Filter
		{id: "focus-filter", section: "Filter", keys: []string{"F", "shift+tab"}, help: "Focus filter bar", hintKey: "F", hint: "filters", run: press},
		{id: "filter-fields", section: "Filter", keys: []string{"left", "right", "enter"}, label: "←→ enter", help: "Field / dropdown", hintKey: "←→", hint: "field", scope: scopeFilter},
		{id: "filter-clear-field", section: "Filter", keys: []string{"backspace", "delete"}, label: "bksp del", help: "Clear field", hintKey: "bksp", hint: "clear field", scope: scopeFilter},
		{id: "filter-leave", section: "Filter", keys: []string{"esc"}, help: "Back to the tree", hint: "tree", scope: scopeFilter},
		{id: "query", section: "Filter", keys: []string{"/"}, help: "Query, e.g. tag:ui", title: "Edit the filter query", hint: "query", scope: scopeTree, run: press},
		{id: "hide-done", section: "Filter", keys: []string{"H"}, help: "Hide/show done", title: "Toggle hiding done beans", hint: "hide done", run: press},
		{id: "filter-tag", section: "Filter", keys: []string{"g t"}, help: "Filter by tag", hint: "tag", run: press},
		{id: "clear-filters", section: "Filter", keys: []string{"esc"}, help: "Clear filters", hint: "clear filter", scope: scopeTree, avail: needsFilter,
			run: func(a *App, _ *command) tea.Cmd { a.list.clearFilter(); return nil }},

		// Edit
		{id: "create", section: "Edit", keys: []string{"c"}, help: "New bean", title: "Create bean", hint: "create", scope: scopeTree, run: press},
		{id: "create-child", section: "Edit", keys: []string{"C"}, help: "New child bean", title: "Create child bean", hint: "child", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "rename", section: "Edit", keys: []string{"r"}, help: "Rename", title: "Rename bean", hint: "rename", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "edit", section: "Edit", keys: []string{"e"}, help: "Edit in $EDITOR", title: "Edit bean in $EDITOR", hint: "edit", avail: needsBean, run: press},
		{id: "status", section: "Edit", keys: []string{"s"}, help: "Set status", title: "Set status…", hint: "status", avail: needsBean, run: press},
		{id: "type", section: "Edit", keys: []string{"t"}, help: "Set type", title: "Set type…", hint: "type", avail: needsBean, run: press},
		{id: "priority", section: "Edit", keys: []string{"P"}, help: "Set priority", title: "Set priority…", hint: "priority", avail: needsBean, run: press},
		{id: "parent", section: "Edit", keys: []string{"p"}, help: "Set parent", title: "Set parent…", hint: "parent", avail: canSetParent, run: press},
		{id: "blocking", section: "Edit", keys: []string{"b"}, help: "Manage blocking", title: "Manage blocking…", hint: "blocking", avail: needsBean, run: press},
		{id: "move-up", section: "Edit", keys: []string{"K"}, help: "Move up", title: "Move bean up", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "move-down", section: "Edit", keys: []string{"J"}, help: "Move down", title: "Move bean down", scope: scopeTree, avail: needsTreeBean, run: press},
		{id: "copy-id", section: "Edit", keys: []string{"y"}, help: "Copy bean ID", hint: "copy id", avail: needsBean, run: press},

		// Workspaces
		{id: "start-workspace", section: "Workspaces", keys: []string{"w"}, help: "Start in workspace", title: "Start bean in a workspace", avail: needsWorkspaceBean, run: press},
		{id: "start-agent", section: "Workspaces", keys: []string{"W"}, help: "…and run agent", title: "Start bean in a workspace and run the agent", avail: needsWorkspaceBean, run: press},
		{id: "workspaces", section: "Workspaces", keys: []string{"g w"}, help: "List workspaces", title: "Show workspaces", avail: needsWorkspaces, run: press},
	}
}

// commandByID returns the registered command with the given ID. It panics
// for unknown IDs, which are programming errors caught by the tests.
func commandByID(id string) *command {
	for i := range commands {
		if commands[i].id == id {
			return &commands[i]
		}
	}
	panic("tui: unknown command " + id)
}

// keyLabel returns how a key is shown to the user.
func keyLabel(k string) string {
	switch k {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "shift+tab":
		return "⇧tab"
	case "pgdown":
		return "pgdn"
	case "backspace":
		return "bksp"
	case "delete":
		return "del"
	case " ":
		return "space"
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "^" + rest
	}
	return k
}

// keyLabels returns the labels of keys separated by spaces.
func keyLabels(keys []string) string {
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = keyLabel(k)
	}
	return strings.Join(labels, " ")
}

// keyText returns the command's keys as shown in the help.
func (c *command) keyText() string {
	if c.label != "" {
		return c.label
	}
	return keyLabels(c.keys)
}

// paletteTitle returns the command's title in the palette.
func (c *command) paletteTitle() string {
	if c.title != "" {
		return c.title
	}
	return c.help
}

// paletteKey returns the key shown next to the command in the palette: its
// primary binding.
func (c *command) paletteKey() string {
	if len(c.keys) == 0 {
		return ""
	}
	return strings.ReplaceAll(keyLabel(c.keys[0]), " ", "")
}

// unavailable reports why the command can't run now ("" if it can).
func (c *command) unavailable(a *App) string {
	if c.avail == nil {
		return ""
	}
	return c.avail(a)
}

// cmdHint returns the footer hint of a command, with desc replacing its
// default description if given.
func cmdHint(id string, desc ...string) keyHint {
	c := commandByID(id)
	h := keyHint{key: c.hintKey, desc: c.hint}
	if h.key == "" {
		h.key = keyLabel(c.keys[0])
	}
	if h.desc == "" {
		h.desc = strings.ToLower(c.help)
	}
	if len(desc) > 0 {
		h.desc = desc[0]
	}
	h.key = glyphs().localize(h.key)
	return h
}

// cmdPairHint returns one footer hint for two commands, e.g. "K J move".
func cmdPairHint(desc, first, second string) keyHint {
	return keyHint{key: cmdHint(first).key + " " + cmdHint(second).key, desc: desc}
}

// runCommand runs a command from the palette in the browse view.
func (a *App) runCommand(c *command) tea.Cmd {
	a.state = viewList
	if a.focus == focusFilter && c.scope != scopeFilter {
		a.focusTree()
	}
	if c.scope == scopeTree && a.focus != focusTree {
		a.focusTree()
	}
	return c.run(a, c)
}

// pressCommandKeys runs a command by pressing its primary key binding (each
// key of a chord in turn), exactly as if the user had typed it.
func pressCommandKeys(a *App, c *command) tea.Cmd {
	if len(c.keys) == 0 {
		return nil
	}
	parts := commandKeySequence(c.keys[0])
	msgs := make([]tea.KeyMsg, len(parts))
	for i, p := range parts {
		msgs[i] = keyMsgFor(p)
	}
	return a.handleKeys(msgs)
}

// commandKeySequence returns the keys of a binding: one key, or the keys of
// a chord ("g t").
func commandKeySequence(binding string) []string {
	if binding == " " {
		return []string{" "}
	}
	return strings.Split(binding, " ")
}

// keyTypesByName maps key names (tea.KeyType.String()) to key types.
var keyTypesByName = func() map[string]tea.KeyType {
	m := map[string]tea.KeyType{}
	for t := tea.KeyType(-512); t < 512; t++ {
		if t == tea.KeyRunes {
			continue
		}
		if name := t.String(); name != "" {
			if _, dup := m[name]; !dup {
				m[name] = t
			}
		}
	}
	return m
}()

// keyMsgFor returns the key message whose String() is k.
func keyMsgFor(k string) tea.KeyMsg {
	if t, ok := keyTypesByName[k]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// Availability -------------------------------------------------------------------

const noBeanReason = "No bean selected"

// focusedBean returns the bean the focused pane's commands act on.
func (a *App) focusedBean() *bean.Bean {
	if a.focus == focusDetail && a.detail.bean != nil {
		return a.detail.bean
	}
	return a.list.currentBean()
}

func needsBean(a *App) string {
	if ids, _ := a.list.selectionOrCurrent(); a.focusedBean() == nil && len(ids) == 0 {
		return noBeanReason
	}
	return ""
}

func needsTreeBean(a *App) string {
	if a.list.currentBean() == nil {
		return "No bean in the tree"
	}
	return ""
}

func needsRows(a *App) string {
	if len(a.list.rows) == 0 {
		return "The tree is empty"
	}
	return ""
}

func needsSelection(a *App) string {
	if len(a.list.selectedBeans) == 0 {
		return "No beans selected for batch editing"
	}
	return ""
}

func needsFilter(a *App) string {
	if !a.list.hasActiveFilter() {
		return "No filters active"
	}
	return ""
}

func needsFocusAway(a *App) string {
	if a.focus == focusTree {
		return "The tree has the focus"
	}
	return ""
}

func canUndo(a *App) string {
	if len(a.undo.entries) == 0 {
		return "Nothing to undo"
	}
	return ""
}

func canSetParent(a *App) string {
	if r := needsBean(a); r != "" {
		return r
	}
	var types []string
	if b := a.detail.bean; a.focus == focusDetail && b != nil {
		types = []string{b.Type}
	} else {
		ids, _ := a.list.selectionOrCurrent()
		for _, id := range ids {
			if b := a.list.byID[id]; b != nil {
				types = append(types, b.Type)
			}
		}
	}
	for _, t := range types {
		if beancore.ValidParentTypes(t) == nil {
			return fmt.Sprintf("A %s can't have a parent", t)
		}
	}
	return ""
}

func needsWorkspaces(a *App) string {
	if !a.ws.enabled() {
		return workspacesUnavailable
	}
	return ""
}

func needsWorkspaceBean(a *App) string {
	if r := needsWorkspaces(a); r != "" {
		return r
	}
	if a.focusedBean() == nil {
		return noBeanReason
	}
	return ""
}

// inAuditView reports why the audit can't be re-run outside the Audit view.
func inAuditView(a *App) string {
	if a.views.active != viewAudit {
		return "Only in the Audit view (4)"
	}
	return ""
}
