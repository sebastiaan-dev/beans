package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
)

// setQuery replaces the tree's filter query.
func setQuery(app *App, query string) {
	app.list.setFilter(filterquery.Parse(query))
	app.syncLayout()
}

func TestFilterBarFocusCycle(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t001") // has relations

	steps := []struct {
		key     string
		focus   focusArea
		section detailSection
	}{
		{"tab", focusDetail, sectionRelations},
		{"tab", focusDetail, sectionBody},
		{"tab", focusFilter, 0},
		{"tab", focusTree, 0},
		{"shift+tab", focusFilter, 0},
		{"shift+tab", focusDetail, sectionBody},
		{"shift+tab", focusDetail, sectionRelations},
		{"shift+tab", focusTree, 0},
	}
	for i, s := range steps {
		pressKey(t, app, s.key)
		if app.focus != s.focus || (s.focus == focusDetail && app.detail.section != s.section) {
			t.Fatalf("step %d (%s): focus=%v section=%v, want %v/%v", i, s.key, app.focus, app.detail.section, s.focus, s.section)
		}
	}

	// F focuses the bar from the detail pane too; esc returns to the tree.
	pressKey(t, app, "enter")
	pressKey(t, app, "F")
	if app.focus != focusFilter || app.detail.focused {
		t.Fatalf("F should focus the filter bar: focus=%v", app.focus)
	}
	pressKey(t, app, "esc")
	if app.focus != focusTree {
		t.Errorf("esc should return to the tree, focus = %v", app.focus)
	}

	// Keys the bar does not use do not act on the tree.
	pressKey(t, app, "F")
	pressKey(t, app, "s")
	if app.state != viewList {
		t.Errorf("s in the filter bar must not open the status picker, state = %v", app.state)
	}
	// / leaves the bar for the query input.
	pressKey(t, app, "/")
	if app.focus != focusTree || !app.list.searching {
		t.Errorf("/ should start the query input: focus=%v searching=%v", app.focus, app.list.searching)
	}
}

func TestFilterBarFieldNavigation(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKey(t, app, "F")
	if app.bar.field != barStatus {
		t.Fatalf("field = %v", app.bar.field)
	}
	pressKey(t, app, "left")
	if app.bar.field != barStatus {
		t.Error("← on the first field stays")
	}
	for range barFieldCount + 2 {
		pressKey(t, app, "l")
	}
	if app.bar.field != barClear {
		t.Errorf("→ stops at the last field, field = %v", app.bar.field)
	}
	pressKey(t, app, "h")
	if app.bar.field != barDone {
		t.Errorf("h should move left, field = %v", app.bar.field)
	}
}

// TestFilterBarAndQueryStayInSync checks that the bar and the "/" input
// edit the same query.
func TestFilterBarAndQueryStayInSync(t *testing.T) {
	app := newDemoApp(t, 160, 40)

	// Typed query → bar.
	pressKey(t, app, "/")
	for _, r := range "type:task,epic login" {
		pressKey(t, app, string(r))
	}
	pressKey(t, app, "enter")
	bar := stripAnsi(strings.Join(app.filterBar(156), "\n"))
	if !strings.Contains(bar, "task, epic") {
		t.Errorf("the bar should show the typed types:\n%s", bar)
	}

	// Bar → query, keeping the terms the bar has no field for.
	pressKey(t, app, "F")
	pressKey(t, app, "l")
	pressKey(t, app, "enter") // Type dropdown, cursor on "epic" (first selected)
	if o := app.barOptions(barType, app.list.filter(), "")[app.bar.cursor]; o.value != "epic" {
		t.Fatalf("cursor on %q, want the first selected type", o.value)
	}
	pressKey(t, app, " ") // deselect epic
	pressKey(t, app, "j")
	pressKey(t, app, " ") // select feature
	pressKey(t, app, "enter")
	if got := app.list.query(); got != "type:task,feature login" {
		t.Errorf("query = %q", got)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"demo-t001", "demo-t002"}) {
		t.Errorf("matched %v", got)
	}

	// Editing the query again starts from what the bar wrote.
	pressKey(t, app, "/")
	if got := app.list.search.Value(); got != "type:task,feature login " {
		t.Errorf("the query input should hold the bar's query, got %q", got)
	}
	pressKey(t, app, "-")
	pressKey(t, app, "b")
	if got := app.list.query(); got != "type:task,feature login -b" {
		t.Errorf("query = %q", got)
	}
	// esc cancels the edit only.
	pressKey(t, app, "esc")
	if got := app.list.query(); got != "type:task,feature login" {
		t.Errorf("esc should restore the query from before the edit, got %q", got)
	}
}

func TestFilterBarToggles(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())
	pressKey(t, app, "F")

	// Ready flips directly, like beans list --ready.
	for app.bar.field != barReady {
		pressKey(t, app, "l")
	}
	pressKey(t, app, "enter")
	if app.list.query() != "ready:yes" || app.bar.open {
		t.Fatalf("enter on Ready should toggle it: query=%q open=%v", app.list.query(), app.bar.open)
	}
	// t-wip is in progress, t-child inherits completed, the rest is done.
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-open"}) {
		t.Errorf("ready: got %v", got)
	}
	pressKey(t, app, " ")
	if app.list.hasActiveFilter() {
		t.Errorf("space should toggle Ready off, query = %q", app.list.query())
	}

	// Done is the H toggle.
	pressKey(t, app, "l")
	pressKey(t, app, "enter")
	if !app.list.doneHidden() {
		t.Errorf("Done should hide done beans, query = %q", app.list.query())
	}
	pressKey(t, app, "H")
	if app.list.doneHidden() {
		t.Error("H should toggle the same state")
	}

	// Clear all.
	setQuery(app, "type:bug ready:yes some text")
	pressKey(t, app, "l")
	pressKey(t, app, "enter")
	if app.list.hasActiveFilter() {
		t.Errorf("clear should clear all filters, query = %q", app.list.query())
	}
}

func TestFilterBarArchived(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())
	pressKey(t, app, "F")
	for app.bar.field != barArchived {
		pressKey(t, app, "l")
	}
	choose := func(label string) {
		t.Helper()
		pressKey(t, app, "enter")
		for i, o := range app.barOptions(barArchived, app.list.filter(), "") {
			if o.label == label {
				app.bar.cursor = i
			}
		}
		pressKey(t, app, "enter")
	}

	choose("only")
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-arch"}) || app.list.query() != "archived:yes" {
		t.Errorf("only: query=%q matched %v", app.list.query(), got)
	}
	choose("hidden")
	if got := matchedIDs(app.list); len(got) != 5 || app.list.query() != "archived:no" {
		t.Errorf("hidden: query=%q matched %v", app.list.query(), got)
	}
	choose("shown")
	if app.list.hasActiveFilter() {
		t.Errorf("shown should drop the filter, query = %q", app.list.query())
	}
}

func TestFilterBarPriority(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKey(t, app, "F")
	pressKey(t, app, "l")
	pressKey(t, app, "l")
	pressKey(t, app, "enter")
	// Options are ordered from most to least urgent: critical, high, ...
	pressKey(t, app, "j")
	pressKey(t, app, ">")
	if got := app.list.query(); got != "priority:>=high" {
		t.Fatalf("> should select at least high, query = %q", got)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"demo-t001", "demo-t005"}) {
		t.Errorf("priority >= high matched %v", got)
	}
	pressKey(t, app, "j")
	pressKey(t, app, "j")
	pressKey(t, app, "<")
	if got := app.list.query(); got != "priority:<=low" {
		t.Errorf("< should replace the comparison, query = %q", got)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"demo-t003", "demo-t006"}) {
		t.Errorf("priority <= low matched %v", got)
	}
	// Toggling a level replaces the comparison with a plain selection.
	pressKey(t, app, " ")
	if got := app.list.query(); got != "priority:low" {
		t.Errorf("space should select the level, query = %q", got)
	}
	pressKey(t, app, "enter")
	bar := stripAnsi(strings.Join(app.filterBar(156), "\n"))
	if !strings.Contains(bar, "low") {
		t.Errorf("the bar should show the priority:\n%s", bar)
	}
	// enter on a field without a selection picks the option under the cursor.
	pressKey(t, app, "backspace")
	pressKey(t, app, "enter")
	pressKey(t, app, "enter")
	if got := app.list.query(); got != "priority:critical" {
		t.Errorf("enter should pick the cursor option, query = %q", got)
	}
}

func TestFilterBarDropdownRendering(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{160, 40}, {80, 24}, {100, 30}, {50, 16}} {
		app := newDemoApp(t, size[0], size[1])
		pressKey(t, app, "g")
		pressKey(t, app, "t")
		pressKey(t, app, "s")
		screen := stripAnsi(app.View())
		checkFrame(t, screen, size[0], size[1])
		for _, want := range []string{"─ Tags ─", "› s", "security", "[ ]", "space toggle"} {
			if !strings.Contains(screen, want) {
				t.Errorf("%dx%d: dropdown lacks %q:\n%s", size[0], size[1], want, screen)
			}
		}
		if strings.Contains(screen, "export") {
			t.Errorf("%dx%d: type-ahead should hide non-matching tags:\n%s", size[0], size[1], screen)
		}
	}
}

func TestFilterBarCollapsedLine(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 80, 24)
	screen := stripAnsi(app.View())
	if !strings.Contains(screen, "Filters none") || !strings.Contains(screen, "(F)") {
		t.Errorf("short terminals should show the one-line bar:\n%s", screen)
	}
	// Focused, it lists the fields and scrolls to the focused one.
	pressKey(t, app, "F")
	for range 6 {
		pressKey(t, app, "l")
	}
	line, xs := app.filterSummaryLine(76)
	plain := stripAnsi(line)
	if !strings.Contains(plain, "▸Done") || !strings.HasPrefix(plain, "‹ ") {
		t.Errorf("focused line = %q", plain)
	}
	if _, ok := xs[barDone]; !ok {
		t.Errorf("the focused field needs a position for its dropdown: %v", xs)
	}
	pressKey(t, app, "enter")
	if !app.list.doneHidden() {
		t.Error("the collapsed bar should toggle Done")
	}
}

func TestFilterStateSurvivesReloadAndFolds(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "z")
	pressKey(t, app, "M")
	pressKey(t, app, "F")
	pressKey(t, app, "enter") // Status
	pressKey(t, app, "j")     // todo
	pressKey(t, app, "enter")
	if app.list.query() != "status:todo" {
		t.Fatalf("query = %q", app.list.query())
	}
	// Matches inside folded parents are shown while filtering.
	if app.list.rowIndex("demo-t005") < 0 {
		t.Errorf("filtering should show matches inside folds: %v", visibleIDs(app.list))
	}
	_, cmd := app.Update(beansChangedMsg{})
	runCmd(t, app, cmd)
	if app.list.query() != "status:todo" || app.list.rowIndex("demo-t005") < 0 {
		t.Errorf("the filter should survive a reload: query=%q rows=%v", app.list.query(), visibleIDs(app.list))
	}
	// Clearing the field brings the folds back.
	pressKey(t, app, "backspace")
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, []string{"demo-m001", "demo-t008", "demo-m002"}) {
		t.Errorf("after clearing rows = %v", got)
	}
}

func TestFilterBarHintsAndBatchedText(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKey(t, app, "F")
	if got := stripAnsi(strings.Join(app.footerLines(156, true), " ")); !strings.Contains(got, "enter open") || !strings.Contains(got, "bksp clear field") {
		t.Errorf("bar hints = %q", got)
	}
	pressKey(t, app, "l")
	pressKey(t, app, "l")
	pressKey(t, app, "enter")
	if got := stripAnsi(strings.Join(app.footerLines(156, true), " ")); !strings.Contains(got, "> at least") {
		t.Errorf("priority dropdown hints = %q", got)
	}
	pressKey(t, app, "esc")

	// Batched runes are text in the tag type-ahead.
	pressKey(t, app, "l")
	pressKey(t, app, "enter")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sec")})
	runCmd(t, app, cmd)
	if app.bar.typed != "sec" {
		t.Errorf("typed = %q", app.bar.typed)
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}
