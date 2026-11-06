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
	if app.focus != focusFilter || !app.bar.open || app.bar.field != barTags {
		t.Fatalf("batched \"gt\" should open the tags dropdown: focus=%v bar=%+v", app.focus, app.bar)
	}
}

func TestBatchedRunesTriggerShortcuts(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "H" hides resolved beans, "F" then focuses the filter bar and "ll"
	// moves to its third field.
	sendBatched(t, app, "HFll")
	if !app.list.doneHidden() {
		t.Error("batched \"H\" should toggle hiding resolved beans")
	}
	if app.focus != focusFilter || app.bar.field != barPriority {
		t.Errorf("batched \"Fll\" should focus the bar's priority field: focus=%v field=%v", app.focus, app.bar.field)
	}
}

func TestBatchedRunesAreTextInListFilter(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "/" starts searching; the remaining runes of the same batch are text,
	// and so are later batches.
	sendBatched(t, app, "/op")
	sendBatched(t, app, "en q")
	if app.state != viewList {
		t.Fatalf("state = %v, want list", app.state)
	}
	if got := app.list.search.Value(); got != "open q" {
		t.Errorf("filter value = %q, want %q", got, "open q")
	}
}

func TestBatchedRunesAreTextInInlineCreate(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "c" opens the inline title input; the rest is the title.
	sendBatched(t, app, "cgt q")
	if app.list.inline == nil {
		t.Fatal("c should open the inline create input")
	}
	if got := app.list.inline.input.Value(); got != "gt q" {
		t.Errorf("title = %q, want %q", got, "gt q")
	}
	sendBatched(t, app, "Fix")
	if got := app.list.inline.input.Value(); got != "gt qFix" {
		t.Errorf("title = %q, want %q", got, "gt qFix")
	}
}

func TestKeysArrivingWhileBatchIsPendingKeepOrder(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	// "H" reloads the beans asynchronously; the terminal delivers "HcFi" and
	// then "x" before that has happened. The later key must not overtake the
	// held ones, which open the inline create input and type into it.
	_, cmd := app.Update(keyRunes("HcFi"))
	if _, later := app.Update(keyRunes("x")); later != nil {
		t.Error("keys arriving while a batch is pending should be held")
	}
	drainCmd(t, app, cmd)

	if app.list.inline == nil {
		t.Fatal("c should open the inline create input")
	}
	if got := app.list.inline.input.Value(); got != "Fix" {
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

func TestFooterFitsWidth(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	modes := map[string]func(a *App){
		"default": func(a *App) {},
		"filtered": func(a *App) {
			setQuery(a, "tag:frontend done:no")
		},
		"filter bar": func(a *App) {
			pressKey(t, a, "F")
		},
		"dropdown": func(a *App) {
			pressKey(t, a, "F")
			pressKey(t, a, "l")
			pressKey(t, a, "l")
			pressKey(t, a, "enter")
		},
		"selected": func(a *App) {
			a.list.selectedBeans = map[string]bool{"t-open": true, "t-wip": true}
		},
		"status": func(a *App) {
			a.list.statusMessage = "Copied " + strings.Repeat("t-open, ", 30)
		},
		"detail": func(a *App) {
			_, cmd := a.Update(keyType(tea.KeyEnter))
			runCmd(t, a, cmd)
		},
	}

	for name, setup := range modes {
		for _, width := range []int{200, 160, 120, 80, 40, 10} {
			app, _ := setupListTestModel(t)
			runCmd(t, app, app.Init())
			app.height = 40
			setup(app)

			lines := app.footerLines(width, width >= wideMinWidth)
			if len(lines) == 0 || len(lines) > 2 {
				t.Fatalf("%s/%d: footer has %d lines", name, width, len(lines))
			}
			for _, line := range lines {
				if strings.Contains(line, "\n") {
					t.Errorf("%s/%d: footer line wraps: %q", name, width, stripAnsi(line))
				}
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%s/%d: footer line is %d columns wide: %q", name, width, w, stripAnsi(line))
				}
			}
		}
	}

	app.height = 40
	lines := app.footerLines(80, false)
	if plain := stripAnsi(lines[0]); !strings.HasPrefix(plain, "enter open · e edit · c create") {
		t.Errorf("most important hints should come first: %q", plain)
	}
	plain := stripAnsi(strings.Join(app.footerLines(300, true), "\n"))
	if strings.Contains(plain, "\n") || !strings.Contains(plain, "H hide done") || !strings.Contains(plain, "space select") {
		t.Errorf("wide footer should show all hints on one line: %q", plain)
	}

	// Short terminals get a single footer line.
	app.height = 15
	if n := len(app.footerLines(40, false)); n != 1 {
		t.Errorf("short terminal footer has %d lines, want 1", n)
	}
}

func TestFitHintLines(t *testing.T) {
	hints := []keyHint{{"a", "aaaa"}, {"b", "bbbb"}, {"c", "cccccccccccccccc"}, {"d", "dd"}}
	render := func(widths ...int) []string {
		var out []string
		for _, l := range fitHintLines(hints, nil, widths) {
			out = append(out, stripAnsi(l))
		}
		return out
	}

	// Everything fits on one line.
	if got := render(100); !reflect.DeepEqual(got, []string{"a aaaa · b bbbb · c cccccccccccccccc · d dd"}) {
		t.Errorf("one line: %q", got)
	}
	// Hints continue on the second line in order.
	if got := render(16, 30); !reflect.DeepEqual(got, []string{"a aaaa · b bbbb", "c cccccccccccccccc · d dd"}) {
		t.Errorf("two lines: %q", got)
	}
	// On the last line, hints that do not fit are dropped, smaller later
	// ones are still shown.
	if got := render(16, 10); !reflect.DeepEqual(got, []string{"a aaaa · b bbbb", "d dd"}) {
		t.Errorf("dropped: %q", got)
	}
	// Pinned hints are kept at the end.
	got := fitHintLines(hints, []keyHint{{"q", "quit"}}, []int{20})
	if len(got) != 1 || stripAnsi(got[0]) != "a aaaa · q quit" {
		t.Errorf("pinned: %q", got)
	}
}
