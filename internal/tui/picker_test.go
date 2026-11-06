package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerValues returns the values of the picker's visible items.
func pickerValues(app *App) []string {
	var out []string
	for i := range app.picker.visible {
		it, _ := app.picker.visibleItem(i)
		out = append(out, it.value)
	}
	return out
}

func TestPickerOpensOnCurrentValue(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t003") // todo, low, blocked by t001
	for _, tc := range []struct {
		key  string
		kind pickerKind
		want string
	}{
		{"s", pickStatus, "todo"},
		{"t", pickType, "task"},
		{"P", pickPriority, "low"},
		{"p", pickParent, "demo-e001"},
	} {
		pressKey(t, app, tc.key)
		if app.state != viewPicker || app.picker.kind != tc.kind {
			t.Fatalf("%s: state %v kind %v", tc.key, app.state, app.picker.kind)
		}
		it, _ := app.picker.visibleItem(app.picker.cursor)
		if it.value != tc.want || !it.current {
			t.Errorf("%s: cursor on %q (current %v), want %q", tc.key, it.value, it.current, tc.want)
		}
		if !strings.Contains(stripAnsi(app.View()), "(current)") {
			t.Errorf("%s: the current value is not marked", tc.key)
		}
		pressKey(t, app, "esc")
		if app.state != viewList {
			t.Fatalf("%s: esc should close the picker", tc.key)
		}
	}
}

// TestPickerItemsFitOneLine checks that long items are cut with an ellipsis
// instead of wrapping onto a second line (repos-941q).
func TestPickerItemsFitOneLine(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{160, 40}, {100, 30}, {80, 24}, {40, 12}} {
		app := newDemoApp(t, size[0], size[1])
		revealBean(t, app, "demo-t001")
		pressKey(t, app, "b")
		screen := stripAnsi(app.View())
		checkFrame(t, screen, size[0], size[1])
		items := app.hits.all(hitModalItem)
		if len(items) == 0 {
			t.Fatalf("%v: no picker items", size)
		}
		lines := screenLines(app)
		for _, reg := range items {
			if reg.r.h != 1 {
				t.Errorf("%v: item %d spans %d lines", size, reg.index, reg.r.h)
			}
			it, _ := app.picker.visibleItem(reg.index)
			text := cellText(lines, reg.r)
			if !strings.Contains(text, shortID(it.value, "demo-")) {
				t.Errorf("%v: item %d shows %q, want %s", size, reg.index, text, it.value)
			}
		}
		// The long title of t003 is cut with an ellipsis.
		for i, v := range pickerValues(app) {
			if v != "demo-t003" {
				continue
			}
			if i < app.picker.offset || i >= app.picker.offset+app.pickerRows() {
				break
			}
			reg := region(t, app, hitModalItem, withIndex(i))
			if text := cellText(lines, reg.r); !strings.Contains(text, "…") || strings.Contains(text, "tree pane") {
				t.Errorf("%v: t003 not truncated: %q", size, text)
			}
		}
		modal := region(t, app, hitModal, nil).r
		if modal.w > size[0] {
			t.Errorf("%v: picker is %d cells wide", size, modal.w)
		}
	}
}

func TestPickerFilter(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKeys(t, app, "s", "/")
	if !app.acceptsTextInput() {
		t.Fatal("/ should start typing the filter")
	}
	// Typed runes, also batched ones and q, go into the filter.
	app.Update(keyRunes("pro"))
	if app.picker.query != "pro" || !slices.Equal(pickerValues(app), []string{"in-progress"}) {
		t.Fatalf("query %q items %v", app.picker.query, pickerValues(app))
	}
	pressKey(t, app, "backspace")
	pressKey(t, app, "backspace")
	pressKey(t, app, "backspace")
	if len(pickerValues(app)) != 5 {
		t.Errorf("clearing the query should show all statuses: %v", pickerValues(app))
	}
	if pressKeyQuits(t, app, keyRunes("q")) || app.picker.query != "q" {
		t.Errorf("q should be typed into the filter: %q", app.picker.query)
	}
	pressKey(t, app, "backspace")
	pressKeys(t, app, "c", "o", "m", "p")
	pressKey(t, app, "enter") // keeps the filter
	if app.picker.filtering || app.picker.query != "comp" {
		t.Fatalf("enter should apply the filter: filtering %v query %q", app.picker.filtering, app.picker.query)
	}
	got := pickerValues(app)
	if len(got) == 0 || got[0] != "completed" {
		t.Fatalf("filtered items %v", got)
	}
	pressKey(t, app, "enter") // chooses
	if app.state != viewList || getBean(t, app, "demo-t002").Status != "completed" {
		t.Errorf("enter should choose the filtered item: state %v status %s", app.state, getBean(t, app, "demo-t002").Status)
	}

	// esc while typing clears the filter; enter without matches does too.
	pressKeys(t, app, "s", "/", "x", "y", "z")
	if len(pickerValues(app)) != 0 || !strings.Contains(stripAnsi(app.View()), "No matches") {
		t.Errorf("xyz should match nothing: %v", pickerValues(app))
	}
	pressKey(t, app, "enter")
	if app.picker.query != "" || len(pickerValues(app)) != 5 {
		t.Errorf("enter without matches should clear the filter: %q", app.picker.query)
	}
	pressKeys(t, app, "/", "t", "o")
	pressKey(t, app, "esc")
	if app.state != viewPicker || app.picker.query != "" || app.picker.filtering {
		t.Errorf("esc should clear the filter but keep the picker open")
	}
	pressKey(t, app, "esc")
	if app.state != viewList {
		t.Errorf("esc should close the picker")
	}
}

func TestPickerQuitsOnQ(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "t")
	if !pressKeyQuits(t, app, keyRunes("q")) {
		t.Error("q should quit from the picker")
	}
}

func TestPickerBatch(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, " ")
	moveTo(t, app, "demo-t008")
	pressKey(t, app, " ")
	pressKey(t, app, "P")
	if app.picker.beanID != "" || app.picker.beanTitle != "2 selected beans" {
		t.Errorf("batch picker: id %q title %q", app.picker.beanID, app.picker.beanTitle)
	}
	if strings.Contains(stripAnsi(app.View()), "(current)") {
		t.Error("a batch has no current value")
	}
	pressKeys(t, app, "g", "j") // critical, high
	pressKey(t, app, "enter")
	for _, id := range []string{"demo-t002", "demo-t008"} {
		if p := getBean(t, app, id).Priority; p != "high" {
			t.Errorf("%s: priority %q", id, p)
		}
	}
	if len(app.list.selectedBeans) != 0 {
		t.Error("a batch edit should clear the selection")
	}
}

func TestParentPicker(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "p")
	got := pickerValues(app)
	// No parent first, then the beans of a parent type (milestones, epics,
	// features); not the bean itself or other tasks.
	if got[0] != "" || !slices.Contains(got, "demo-e002") || !slices.Contains(got, "demo-t001") || slices.Contains(got, "demo-t002") || slices.Contains(got, "demo-t008") {
		t.Errorf("parent items %v", got)
	}
	pressKeys(t, app, "g")
	pressKey(t, app, "enter")
	if p := getBean(t, app, "demo-t002").Parent; p != "" {
		t.Errorf("(No Parent) should clear the parent: %q", p)
	}

	// Milestones can't have a parent: no picker.
	moveTo(t, app, "demo-m001")
	pressKey(t, app, "p")
	if app.state != viewList {
		t.Errorf("the parent picker opened for a milestone")
	}
}

func TestBlockingPicker(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "b")
	if slices.Contains(pickerValues(app), "demo-t002") {
		t.Error("a bean can't block itself")
	}
	pressKeys(t, app, "/", "c", "s", "v")
	pressKey(t, app, "enter")
	pressKey(t, app, " ")
	if !strings.Contains(stripAnsi(app.View()), "● T T t005") {
		t.Errorf("space should mark t005:\n%s", stripAnsi(app.View()))
	}
	pressKey(t, app, "enter")
	if b := getBean(t, app, "demo-t002"); !slices.Equal(b.Blocking, []string{"demo-t005"}) {
		t.Fatalf("blocking = %v", b.Blocking)
	}
	// Unmarking removes it.
	pressKeys(t, app, "b", "/", "c", "s", "v")
	pressKey(t, app, "enter")
	pressKey(t, app, " ")
	pressKey(t, app, "enter")
	if b := getBean(t, app, "demo-t002"); len(b.Blocking) != 0 {
		t.Errorf("blocking = %v after unmarking", b.Blocking)
	}
}

func TestPickerKeepsCursorVisible(t *testing.T) {
	app := newDemoApp(t, 100, 16)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "b")
	rows := app.pickerRows()
	if rows >= len(app.picker.items) {
		t.Fatalf("test needs a scrolling picker: %d rows for %d items", rows, len(app.picker.items))
	}
	pressKey(t, app, "G")
	app.View()
	last := len(app.picker.visible) - 1
	found := false
	for _, reg := range app.hits.all(hitModalItem) {
		found = found || reg.index == last
	}
	if !found {
		t.Errorf("the last item is not shown after G")
	}
	if pos := stripAnsi(app.View()); !strings.Contains(pos, "11/11") {
		t.Errorf("the position is not shown:\n%s", pos)
	}
	pressKeys(t, app, "g")
	if app.picker.cursor != 0 {
		t.Errorf("g: cursor %d", app.picker.cursor)
	}
}

func TestPickerASCII(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	for _, size := range [][2]int{{160, 40}, {80, 24}, {40, 12}} {
		for _, keys := range [][]string{
			{"s", "/", "t"},
			{"s", "/", "x", "y", "enter"},
			{"b", "/", "e", "x", "p", "enter", " "},
			{"p", "/"},
		} {
			app := newDemoApp(t, size[0], size[1])
			moveTo(t, app, "demo-t003")
			pressKeys(t, app, keys...)
			view := app.View()
			if l, bad := nonASCII(view); bad {
				t.Errorf("%v %v: non-ASCII output: %q", size, keys, stripAnsi(l))
			}
			for _, l := range strings.Split(view, "\n") {
				if w := lipgloss.Width(l); w != size[0] {
					t.Fatalf("%v %v: line of %d cells", size, keys, w)
				}
			}
		}
	}
}

func TestPickerMouseWhileFiltering(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKeys(t, app, "s", "/", "s", "c")
	reg := region(t, app, hitModalItem, withIndex(0))
	if it, _ := app.picker.visibleItem(0); it.value != "scrapped" {
		t.Fatalf("first match %q", it.value)
	}
	click(t, app, reg.r)
	if getBean(t, app, "demo-t002").Status != "scrapped" || app.state != viewList {
		t.Errorf("clicking a filtered item should choose it")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
}
