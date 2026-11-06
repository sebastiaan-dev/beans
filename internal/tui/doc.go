// Package tui is the Bubble Tea terminal UI of beans (`beans tui`).
//
// # Architecture
//
// Components. App (tui.go) is the tea.Model. It owns three panes and the
// overlays and coordinates them; the panes never talk to each other:
//
//   - listModel (list.go, inline_input.go): the loaded beans, the filter
//     query (the single source of truth for all filters), the tree rows,
//     folding, the cursor, the multi-selection and the footer's status
//     message.
//   - detailModel (detail.go, detail_scroll.go, checkbox.go): the bean
//     shown beside (or instead of) the tree as labelled field boxes.
//   - filterBarModel (filterbar.go): the focused field and the dropdown;
//     it edits the query owned by listModel.
//   - The views Ready, Blocked and Audit (views.go, view_*.go) replace the
//     tree with a flatList of rows; their state is App.views.
//   - Overlays (overlay.go): one modal at a time, selected by App.state
//     and implementing the overlay interface: the picker (picker.go; status,
//     type, priority, parent, blocking), the help (help.go), the workspace
//     list (workspaces.go) and the command palette (cmdpalette.go).
//
// Message flow. App.Update runs update, then the views' background command
// (viewsCmd) and syncLayout, which sizes the panes for the terminal and
// shows the active view's bean in the detail pane. Keys go through
// handleKey (keys.go): ctrl+c quits; an open overlay gets every other key;
// otherwise handleBrowseKey tries, in order, chords, the palette, the
// focused filter bar, the view keys (1-4, the views' lists), the focus
// keys, the quick actions (quickactions.go) and the workspace keys, and
// the focused pane handles the rest. Keys that move the focus or open
// something are handled synchronously so the next key reaches the right
// pane; keys that need a command's message first (e.g. "s" opens the
// picker through openPickerMsg) hold back the keys after them (handleKeys).
// Panes ask the App for anything that changes beans or other panes with a
// message (openPickerMsg, openEditorMsg, copyBeanIDMsg, statusSelectedMsg,
// ...). Every change of beans is made by the App through the GraphQL
// resolver and recorded for undo (trackChange, undo.go); bean files
// changing on disk arrive as beansChangedMsg and reload the list.
//
// Focus model. In the browse view the focus (App.focus) is on the filter
// bar, the tree (or the active view's list) or the detail pane; tab and
// shift+tab cycle through them, F focuses the filter bar and esc returns
// to the tree. The detail pane has two sections, relations and body.
// Commands act on App.focusedBean: the detail pane's bean while it has the
// focus, else the selection or the bean under the cursor. On narrow or
// short terminals the focused detail pane replaces the tree.
//
// Rendering and hit-testing. Renderers return lines of exactly the width
// they are given (fitWidth, clampWidth, truncateText in frame.go), so
// panes can be joined and framed without measuring. Bordered boxes are
// drawn with box (frame.go), rows with rowStyler, beanRef and rowLine
// (rows.go): a cursor bar, the selection background and an accent title,
// cut with an ellipsis instead of wrapping (tree rows wrap their title onto
// up to three lines instead). Overlays are centered over the dimmed browse view by
// overlayModal. While rendering, every clickable region is recorded into
// App.hits through a hitRecorder (layout.go) passed down explicitly and
// offset to where the block ends up; the mouse handlers (mouse.go)
// hit-test against what was drawn instead of recomputing the layout.
// Overlays record their items as hitModalItem relative to the modal.
//
// Glyphs and colors. Every non-ASCII character of the UI comes from
// glyphs() (glyphs.go), so that tui.ascii / BEANS_TUI_ASCII=1 produce pure
// ASCII output; static texts with Unicode key names go through
// glyphs().localize. Every color is resolved at render time through the
// accessors of palette.go (colAccent, mutedStyle, colNamed for the
// configured status/type/priority colors, ...): never capture a color in a
// package-level style, since the theme can change at runtime
// (theme_reload.go).
//
// The registry. commands.go lists every action of the browse view once:
// its keys, help text, palette title, footer hint, scope and availability.
// The help overlay is generated from it, the palette lists and runs its
// commands by pressing their keys (so it uses exactly the keyboard's code
// paths), and the footers of the panes build their hints with cmdHint, so
// a key shown anywhere in the browse view is a registered key (checked by
// commands_test.go). Keys that only exist inside an overlay (the picker's
// space, the palette's arrows, ...) are not commands; overlays render their
// own hints with renderHints.
//
// Known debts. The footer's status message lives in listModel
// (statusMessage) although the App and every pane set it; the $EDITOR
// session is spread over App (editingBeanID, editingBeanModTime) and
// undoState.editing; the palette's recently run commands live in App
// because the palette is recreated whenever it opens.
package tui
