package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Key routing. Every key goes through handleKey:
//
//  1. Keys arriving while earlier keys of a batch wait for a view change
//     are held back (handleKeys); several runes delivered at once are split
//     into single keys unless a text input receives them.
//  2. ctrl+c quits from anywhere.
//  3. An open overlay (overlay.go) gets all other keys.
//  4. Otherwise handleBrowseKey dispatches in the browse view: chords
//     ("g t", "z o"), the palette, the focused filter bar, the view keys
//     (1-4 and the Ready/Blocked/Audit lists), the focus keys, the quick
//     actions and the workspace keys, then the focused pane (tree or
//     detail) handles what is left.
//
// Keys that move the focus or open something are handled synchronously
// here, so that the next key already reaches the right pane.

// handleKey handles a key message.
func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Keep the order of keys while earlier keys of a batch are pending.
	if a.holdingKeys {
		a.heldKeys = append(a.heldKeys, msg)
		return nil
	}
	// Several runes delivered at once are individual shortcuts, unless a
	// text input is receiving them.
	if keys, ok := splitBatchedRunes(msg); ok && !a.acceptsTextInput() {
		return a.handleKeys(keys)
	}

	// Clear status messages on any keypress
	a.list.statusMessage = ""

	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if o := a.activeOverlay(); o != nil {
		a.pendingKey = ""
		return o.update(a, msg)
	}
	return a.handleBrowseKey(msg)
}

// handleBrowseKey handles a key in the browse view.
func (a *App) handleBrowseKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if !a.typing() {
		if first := a.pendingKey; first != "" {
			a.pendingKey = ""
			return a.handleChord(first, msg)
		}
		switch {
		case key == "g", key == "z" && a.focus == focusTree && a.views.active == viewTree:
			a.pendingKey = key
			return nil
		}
	}
	a.pendingKey = ""

	if a.opensCommandPalette(key) {
		return a.openCommandPalette()
	}
	if a.handleViewSearchKey(key) {
		return nil
	}
	if !a.list.typing() {
		if a.focus == focusFilter {
			if cmd, handled := a.updateFilterBar(msg); handled {
				return cmd
			}
		}
		if !a.typing() {
			if cmd, handled := a.handleViewKey(msg); handled {
				return cmd
			}
			if a.handleFocusKey(key) {
				return nil
			}
		}
		if cmd, handled := a.handleQuickKey(key); handled {
			return cmd
		}
		if cmd, handled := a.handleWorkspaceKey(key); handled {
			return cmd
		}
	}
	if !a.typing() {
		switch key {
		case "?":
			a.helpOverlay = newHelpOverlayModel(a.width, a.height)
			a.state = viewHelpOverlay
			return nil
		case "q":
			return tea.Quit
		}
	}

	// The focused pane handles the rest.
	var cmd tea.Cmd
	switch a.focus {
	case focusDetail:
		a.detail, cmd = a.detail.Update(msg)
	case focusFilter:
		// Keys not handled by the filter bar must not act on the tree.
	default:
		// Outside the tree view, keys only reach the tree's query input.
		if a.views.active == viewTree || a.list.typing() {
			a.list, cmd = a.list.Update(msg)
		}
	}
	return cmd
}

// handleChord handles the second key of a chord ("g t", "g g", "z o", ...).
func (a *App) handleChord(first string, msg tea.KeyMsg) tea.Cmd {
	switch first + " " + msg.String() {
	case "g t":
		a.openFilterField(barTags)
	case "g w":
		return a.openWorkspacesOverlay()
	case "g g":
		if l := a.activeList(); l != nil && a.focus == focusTree {
			l.moveToEnd(-1)
		} else if a.focus == focusTree {
			a.list.moveCursor(-len(a.list.rows))
		} else {
			a.detail.scrollHome()
		}
	case "z o":
		a.list.foldOpen()
	case "z c":
		a.list.foldClose()
	case "z a":
		a.list.foldToggle()
	case "z R":
		a.list.openAll()
	case "z M":
		a.list.closeAll()
	}
	// Other second keys cancel the chord and are ignored.
	return nil
}

// handleFocusKey handles the keys moving the focus between the filter bar,
// the tree and the detail pane, and reports whether key was one of them.
//
// tab and shift+tab cycle filter bar → tree → detail (relations, body) →
// filter bar; F focuses the filter bar from anywhere.
func (a *App) handleFocusKey(key string) bool {
	if key == "F" {
		a.focusFilterBar()
		return true
	}
	switch a.focus {
	case focusFilter:
		switch key {
		case "tab":
			a.focusTree()
		case "shift+tab":
			if !a.focusDetailPane(false) {
				a.focusTree()
			}
		default:
			return false
		}
		return true
	case focusTree:
		switch key {
		case "enter", "tab":
			if !a.focusDetailPane(true) && key == "tab" {
				a.focusFilterBar()
			}
		case "shift+tab":
			a.focusFilterBar()
		default:
			return false
		}
		return true
	}
	switch key {
	case "tab":
		if a.detail.section == sectionRelations {
			a.detail.section = sectionBody
		} else {
			a.focusFilterBar()
		}
	case "shift+tab":
		if a.detail.section == sectionBody && len(a.detail.links) > 0 {
			a.detail.section = sectionRelations
		} else {
			a.focusTree()
		}
	case "esc":
		a.focusTree()
	case "/", "H", "c", "C", "r", "K", "J":
		// Tree-wide actions return to the tree and are handled there.
		a.focusTree()
		return false
	case "enter":
		if id := a.detail.selectedRelation(); id != "" {
			a.showBean(id, true)
		}
	case "backspace":
		if id, ok := a.detail.popHistory(); ok {
			a.showBean(id, false)
		} else {
			a.focusTree()
		}
	default:
		return false
	}
	return true
}

// typing reports whether the browse view routes typed characters into a
// text field (the query input or the tag type-ahead).
func (a *App) typing() bool {
	return a.list.typing() || (a.focus == focusFilter && a.bar.typing())
}

// acceptsTextInput reports whether the active view currently routes typed
// characters into a text field, so batched runes must be kept intact.
func (a *App) acceptsTextInput() bool {
	if o := a.activeOverlay(); o != nil {
		return o.acceptsText()
	}
	return a.typing()
}

// Batched keys -------------------------------------------------------------------

// splitBatchedRunes splits a key message that carries several runes into one
// single-rune key message per rune.
//
// When a terminal delivers several characters in a single read (fast typing,
// tmux send-keys, or a paste without bracketed paste mode), bubbletea emits a
// single KeyRunes message containing all of them, e.g. "gt". Shortcut handling
// compares msg.String() against single keys, so such messages would otherwise
// be ignored. Bracketed pastes are never split, since they are explicitly text.
//
// It reports false if msg is not a splittable multi-rune message.
func splitBatchedRunes(msg tea.KeyMsg) ([]tea.KeyMsg, bool) {
	if msg.Type != tea.KeyRunes || msg.Paste || len(msg.Runes) < 2 {
		return nil, false
	}
	keys := make([]tea.KeyMsg, len(msg.Runes))
	for i, r := range msg.Runes {
		keys[i] = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt}
	}
	return keys, true
}

// releaseHeldKeysMsg delivers keys held back by handleKeys.
type releaseHeldKeysMsg struct{}

// handleKeys handles key messages one after another, as if they had been
// typed separately, splitting batched runes into single-key shortcuts.
//
// A key may return a command whose message changes the view (e.g. "s" opens
// the status picker via openPickerMsg). The remaining keys, and any keys
// arriving in the meantime, are therefore held back until that command's
// messages have been processed. Once a text input has focus, the remaining
// keys are delivered to it directly as text.
func (a *App) handleKeys(keys []tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd
	for len(keys) > 0 {
		if a.acceptsTextInput() {
			for _, k := range keys {
				_, cmd := a.Update(k)
				cmds = append(cmds, cmd)
			}
			break
		}
		if split, ok := splitBatchedRunes(keys[0]); ok {
			keys = append(split, keys[1:]...)
		}
		k := keys[0]
		keys = keys[1:]
		_, cmd := a.Update(k)
		if cmd != nil && len(keys) > 0 && !a.acceptsTextInput() {
			a.heldKeys = keys
			a.holdingKeys = true
			release := func() tea.Msg { return releaseHeldKeysMsg{} }
			cmds = append(cmds, tea.Sequence(cmd, release))
			break
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}
