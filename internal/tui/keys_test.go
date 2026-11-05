package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSplitBatchedRunes(t *testing.T) {
	tests := []struct {
		name   string
		msg    tea.KeyMsg
		want   []string
		wantOK bool
	}{
		{name: "chord", msg: keyRunes("gt"), want: []string{"g", "t"}, wantOK: true},
		{name: "unicode", msg: keyRunes("äß"), want: []string{"ä", "ß"}, wantOK: true},
		{name: "alt kept", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab"), Alt: true}, want: []string{"alt+a", "alt+b"}, wantOK: true},
		{name: "single rune", msg: keyRunes("g")},
		{name: "bracketed paste", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gt"), Paste: true}},
		{name: "special key", msg: keyType(tea.KeyEnter)},
		{name: "space", msg: keyType(tea.KeySpace)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, ok := splitBatchedRunes(tt.msg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			var got []string
			for _, k := range keys {
				if len(k.Runes) != 1 || k.Type != tea.KeyRunes {
					t.Errorf("split key %+v is not a single rune", k)
				}
				got = append(got, k.String())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

// drainCmd executes cmd like the bubbletea runtime would, feeding resulting
// messages back into the app in order (sequences in order, batches one by
// one). Commands that do not finish promptly (cursor blink ticks and other
// timers) are dropped so that the test terminates.
func drainCmd(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	drainCmdDepth(t, app, cmd, 0)
}

func drainCmdDepth(t *testing.T, app *App, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 50 {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(50 * time.Millisecond):
		return
	}
	if msg == nil {
		return
	}
	// tea.BatchMsg and bubbletea's unexported sequence message are both
	// slices of commands.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		for i := 0; i < v.Len(); i++ {
			drainCmdDepth(t, app, v.Index(i).Interface().(tea.Cmd), depth+1)
		}
		return
	}
	_, next := app.Update(msg)
	drainCmdDepth(t, app, next, depth+1)
}

func sendBatched(t *testing.T, app *App, s string) {
	t.Helper()
	_, cmd := app.Update(keyRunes(s))
	drainCmd(t, app, cmd)
}

func TestBatchedRunesTriggerChord(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	sendBatched(t, app, "gt")
	if app.state != viewTagPicker {
		t.Fatalf("batched \"gt\" should open the tag picker, state = %v", app.state)
	}
}

func TestBatchedRunesTriggerShortcuts(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "H" hides resolved beans, "F" then opens the filter modal.
	sendBatched(t, app, "HF")
	if !app.list.filters.hideResolved {
		t.Error("batched \"H\" should toggle hiding resolved beans")
	}
	if app.state != viewFilterPicker {
		t.Errorf("batched \"F\" should open the filter modal, state = %v", app.state)
	}
}

func TestBatchedRunesAreTextInListFilter(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "/" starts filtering; the remaining runes of the same batch are text,
	// and so are later batches.
	sendBatched(t, app, "/op")
	sendBatched(t, app, "en q")
	if app.state != viewList {
		t.Fatalf("state = %v, want list", app.state)
	}
	if got := app.list.list.FilterValue(); got != "open q" {
		t.Errorf("filter value = %q, want %q", got, "open q")
	}
}

func TestBatchedRunesAreTextInCreateModal(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "c" opens the create modal asynchronously; the rest is the title.
	sendBatched(t, app, "cgt q")
	if app.state != viewCreateModal {
		t.Fatalf("state = %v, want create modal", app.state)
	}
	if got := app.createModal.textInput.Value(); got != "gt q" {
		t.Errorf("title = %q, want %q", got, "gt q")
	}
	sendBatched(t, app, "Fix")
	if got := app.createModal.textInput.Value(); got != "gt qFix" {
		t.Errorf("title = %q, want %q", got, "gt qFix")
	}
}

func TestKeysArrivingWhileBatchIsPendingKeepOrder(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// The terminal delivers "cFi" and then "x" before the create modal has
	// opened; the later key must not overtake the held ones.
	_, cmd := app.Update(keyRunes("cFi"))
	if _, later := app.Update(keyRunes("x")); later != nil {
		t.Error("keys arriving while a batch is pending should be held")
	}
	drainCmd(t, app, cmd)

	if app.state != viewCreateModal {
		t.Fatalf("state = %v, want create modal", app.state)
	}
	if got := app.createModal.textInput.Value(); got != "Fix" {
		t.Errorf("title = %q, want %q", got, "Fix")
	}
	if app.holdingKeys || len(app.heldKeys) > 0 {
		t.Error("held keys should be released")
	}
}

func TestBracketedPasteIsNotSplit(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gt"), Paste: true})
	drainCmd(t, app, cmd)
	if app.state != viewList {
		t.Errorf("a pasted \"gt\" must not trigger the chord, state = %v", app.state)
	}
}

func TestListFooterFitsWidth(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	modes := map[string]func(m *listModel){
		"default": func(m *listModel) {},
		"filtered": func(m *listModel) {
			m.filters = listFilters{tags: []string{"frontend"}, hideResolved: true}
		},
		"selected": func(m *listModel) {
			m.selectedBeans = map[string]bool{"t-open": true, "t-wip": true}
		},
		"status": func(m *listModel) {
			m.statusMessage = "Copied " + strings.Repeat("t-open, ", 30)
		},
	}

	for name, setup := range modes {
		for _, width := range []int{200, 160, 120, 80, 40, 10} {
			m := app.list
			m.selectedBeans = map[string]bool{}
			m.width = width
			setup(&m)

			footer := m.Footer()
			plain := stripAnsi(footer)
			if strings.Contains(footer, "\n") {
				t.Errorf("%s/%d: footer wraps: %q", name, width, plain)
			}
			if w := lipgloss.Width(footer); w > width {
				t.Errorf("%s/%d: footer is %d columns wide: %q", name, width, w, plain)
			}
			if name != "status" && width >= 40 && !strings.HasSuffix(plain, "? help  q quit") {
				t.Errorf("%s/%d: footer should end with help and quit hints: %q", name, width, plain)
			}
		}
	}

	m := app.list
	m.width = 80
	if plain := stripAnsi(m.Footer()); !strings.HasPrefix(plain, "enter view  / search  F filters") {
		t.Errorf("most important hints should come first: %q", plain)
	}
	m.width = 300
	if plain := stripAnsi(m.Footer()); !strings.Contains(plain, "H hide done") || !strings.Contains(plain, "space select") {
		t.Errorf("wide footer should show all hints: %q", plain)
	}
}
