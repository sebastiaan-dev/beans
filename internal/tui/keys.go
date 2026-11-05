package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

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

// KeyMap defines the key bindings for the TUI
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Quit   key.Binding
	Help   key.Binding
	Filter key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "view"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
	}
}

// ShortHelp returns the short help for the list view
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Filter, k.Quit}
}

// FullHelp returns the full help for the list view
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Filter, k.Quit, k.Help},
	}
}

// DetailKeyMap defines key bindings specific to the detail view
type DetailKeyMap struct {
	Up   key.Binding
	Down key.Binding
	Back key.Binding
	Quit key.Binding
}

// DefaultDetailKeyMap returns the default key bindings for detail view
func DefaultDetailKeyMap() DetailKeyMap {
	return DetailKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns the short help for the detail view
func (k DetailKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit}
}

// FullHelp returns the full help for the detail view
func (k DetailKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Back, k.Quit},
	}
}
