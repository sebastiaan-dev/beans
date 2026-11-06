package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// visibleIDs returns the IDs of the visible tree rows in order.
func visibleIDs(m listModel) []string {
	ids := make([]string, len(m.rows))
	for i, r := range m.rows {
		ids[i] = r.bean.ID
	}
	return ids
}

func cursorID(app *App) string {
	if b := app.list.currentBean(); b != nil {
		return b.ID
	}
	return ""
}

func moveTo(t *testing.T, app *App, id string) {
	t.Helper()
	i := app.list.rowIndex(id)
	if i < 0 {
		t.Fatalf("%s is not visible: %v", id, visibleIDs(app.list))
	}
	app.list.cursor = i
	app.list.ensureVisible()
}

var demoAll = []string{
	"demo-m001", "demo-e001", "demo-t001", "demo-t002", "demo-t003", "demo-t004",
	"demo-e002", "demo-t005", "demo-t006", "demo-t008", "demo-m002", "demo-t007",
}

func TestFoldingKeys(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, demoAll) {
		t.Fatalf("initial rows = %v", got)
	}

	// ← on an expanded parent collapses it; the cursor stays on it.
	moveTo(t, app, "demo-e001")
	pressKey(t, app, "left")
	if got := visibleIDs(app.list); reflect.DeepEqual(got, demoAll) || cursorID(app) != "demo-e001" {
		t.Fatalf("collapse: rows %v cursor %s", got, cursorID(app))
	}
	if r, _ := app.list.currentRow(); !r.collapsed || r.hidden != 4 {
		t.Errorf("row should be collapsed hiding 4 beans: %+v", r)
	}
	if !strings.Contains(stripAnsi(app.list.View()), "▸") || !strings.Contains(stripAnsi(app.list.View()), "+4") {
		t.Errorf("collapsed row should show ▸ and the hidden count:\n%s", stripAnsi(app.list.View()))
	}
	// ← again goes to the parent.
	pressKey(t, app, "h")
	if cursorID(app) != "demo-m001" {
		t.Errorf("h on a collapsed row should go to the parent, cursor = %s", cursorID(app))
	}
	// → goes into the first child of an expanded parent, then expands.
	pressKey(t, app, "right")
	if cursorID(app) != "demo-e001" {
		t.Errorf("→ on an expanded row should go to its first child, cursor = %s", cursorID(app))
	}
	pressKey(t, app, "l")
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, demoAll) {
		t.Errorf("l should expand: %v", got)
	}

	// ← on a leaf goes to its parent; zc on a leaf closes the parent.
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "left")
	if cursorID(app) != "demo-e001" {
		t.Errorf("← on a leaf should go to the parent, cursor = %s", cursorID(app))
	}
	moveTo(t, app, "demo-t005")
	pressKey(t, app, "z")
	pressKey(t, app, "c")
	if cursorID(app) != "demo-e002" || app.list.rowIndex("demo-t005") >= 0 {
		t.Errorf("zc on a leaf should close its parent: cursor %s rows %v", cursorID(app), visibleIDs(app.list))
	}
	pressKey(t, app, "z")
	pressKey(t, app, "o")
	if app.list.rowIndex("demo-t005") < 0 {
		t.Error("zo should open the fold")
	}
	pressKey(t, app, "z")
	pressKey(t, app, "a")
	if app.list.rowIndex("demo-t005") >= 0 {
		t.Error("za should toggle the fold closed")
	}
	pressKey(t, app, "z")
	pressKey(t, app, "a")
	if app.list.rowIndex("demo-t005") < 0 {
		t.Error("za should toggle the fold open")
	}

	// zM closes everything, keeping the cursor on the closest visible
	// ancestor; zR opens everything.
	moveTo(t, app, "demo-t003")
	pressKey(t, app, "z")
	pressKey(t, app, "M")
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, []string{"demo-m001", "demo-t008", "demo-m002"}) {
		t.Errorf("zM rows = %v", got)
	}
	if cursorID(app) != "demo-m001" {
		t.Errorf("zM cursor = %s, want the root ancestor", cursorID(app))
	}
	pressKey(t, app, "z")
	pressKey(t, app, "R")
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, demoAll) {
		t.Errorf("zR rows = %v", got)
	}
	// "z" followed by an unknown key does nothing, and the next key is
	// handled normally again.
	pressKey(t, app, "z")
	pressKey(t, app, "x")
	pressKey(t, app, "j")
	if cursorID(app) != "demo-e001" {
		t.Errorf("j after a cancelled chord should move, cursor = %s", cursorID(app))
	}
}

func TestFoldStatePersistsAcrossReloads(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-e001")
	pressKey(t, app, "h")
	_, cmd := app.Update(beansChangedMsg{})
	runCmd(t, app, cmd)
	if app.list.rowIndex("demo-t001") >= 0 {
		t.Error("folds should survive a reload")
	}
	// Hiding completed beans does not suspend folding.
	pressKey(t, app, "H")
	if app.list.rowIndex("demo-t001") >= 0 {
		t.Error("H should keep the folds")
	}
}

func TestFoldingWithFilters(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "z")
	pressKey(t, app, "M")

	// A filter shows matches inside folded parents.
	app.list.setFilters(listFilters{types: []string{"task"}})
	runCmd(t, app, app.list.loadBeans)
	for _, id := range []string{"demo-t002", "demo-t003", "demo-t005", "demo-t006", "demo-t007", "demo-t008"} {
		if app.list.rowIndex(id) < 0 {
			t.Errorf("%s should be visible while filtering: %v", id, visibleIDs(app.list))
		}
	}

	// Folding while filtering works, and is forgotten with the filter.
	moveTo(t, app, "demo-e002")
	pressKey(t, app, "h")
	if app.list.rowIndex("demo-t005") >= 0 {
		t.Error("folding while filtering should hide the children")
	}
	app.list.setFilters(listFilters{types: []string{"task", "bug"}})
	runCmd(t, app, app.list.loadBeans)
	if app.list.rowIndex("demo-t005") < 0 {
		t.Error("changing the filter should reset the temporary folds")
	}

	// Clearing the filter restores the session's folds.
	pressKey(t, app, "esc")
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, []string{"demo-m001", "demo-t008", "demo-m002"}) {
		t.Errorf("after clearing the filter rows = %v", got)
	}
}

func TestSearch(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "z")
	pressKey(t, app, "M")

	pressKey(t, app, "/")
	if !app.list.searching || !app.acceptsTextInput() {
		t.Fatal("/ should start a search")
	}
	// Keys are text while searching: q does not quit, j is typed.
	for _, k := range []string{"c", "s", "v"} {
		pressKey(t, app, k)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"demo-t005"}) {
		t.Errorf("search csv matched %v", got)
	}
	// Matches inside folded parents are shown with their ancestors.
	if got := visibleIDs(app.list); !reflect.DeepEqual(got, []string{"demo-m001", "demo-e002", "demo-t005"}) {
		t.Errorf("rows = %v", got)
	}
	if cursorID(app) != "demo-t005" {
		t.Errorf("the cursor should be on the first match, is on %s", cursorID(app))
	}
	if !strings.Contains(stripAnsi(app.View()), "Beans 1/12") {
		t.Error("the tree label should show the match count")
	}

	// enter keeps the search, esc in the tree clears it.
	pressKey(t, app, "enter")
	if app.list.searching || app.list.query() != "csv" {
		t.Errorf("enter should keep the query: searching=%v query=%q", app.list.searching, app.list.query())
	}
	pressKey(t, app, "esc")
	if app.list.query() != "" {
		t.Error("esc should clear the search")
	}
	if got := visibleIDs(app.list); len(got) != 3 {
		t.Errorf("the folds should be back after the search: %v", got)
	}

	// esc while typing cancels the search; ID search works.
	pressKey(t, app, "/")
	for _, k := range []string{"t", "0", "0", "7"} {
		pressKey(t, app, k)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"demo-t007"}) {
		t.Errorf("ID search matched %v", got)
	}
	pressKey(t, app, "esc")
	if app.list.searching || app.list.query() != "" {
		t.Error("esc while typing should cancel the search")
	}
}

func TestDetailFocusAndRelations(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t001")
	app.syncLayout()

	pressKey(t, app, "enter")
	if app.focus != focusDetail || app.detail.section != sectionRelations {
		t.Fatalf("enter should focus the relations: focus=%v section=%v", app.focus, app.detail.section)
	}
	// Keys go to the detail pane now: j moves in the relations.
	if got := app.detail.links[app.detail.relCursor].bean.ID; got != "demo-e001" {
		t.Errorf("first relation = %s, want the parent", got)
	}
	pressKey(t, app, "j")
	if got := app.detail.links[app.detail.relCursor].bean.ID; got != "demo-t003" {
		t.Errorf("second relation = %s, want the blocked bean", got)
	}
	if cursorID(app) != "demo-t001" {
		t.Error("the tree cursor must not move while the detail pane has focus")
	}

	// enter jumps to the related bean: the tree follows and folded
	// ancestors are opened.
	app.list.folds["demo-e001"] = true
	pressKey(t, app, "enter")
	if cursorID(app) != "demo-t003" || app.detail.bean.ID != "demo-t003" {
		t.Errorf("jump: tree %s detail %s", cursorID(app), app.detail.bean.ID)
	}
	if app.focus != focusDetail {
		t.Error("the detail pane should keep the focus after a jump")
	}
	if got := app.detail.links[0]; got.bean.ID != "demo-e001" || formatLinkLabel(got.linkType, got.incoming) != "parent" {
		t.Errorf("relations of t003 start with %+v", got)
	}
	if !strings.Contains(stripAnsi(app.View()), "blocked by") {
		t.Error("t003 should list its blocker")
	}

	// backspace goes back to the previous bean.
	pressKey(t, app, "backspace")
	if app.detail.bean.ID != "demo-t001" || cursorID(app) != "demo-t001" {
		t.Errorf("back: detail %s tree %s", app.detail.bean.ID, cursorID(app))
	}

	// tab switches to the body, which scrolls with j/k.
	pressKey(t, app, "tab")
	if app.detail.section != sectionBody {
		t.Error("tab should switch to the body")
	}

	// esc returns to the tree.
	pressKey(t, app, "esc")
	if app.focus != focusTree {
		t.Error("esc should return to the tree")
	}
	pressKey(t, app, "j")
	if cursorID(app) != "demo-t002" {
		t.Errorf("j should move the tree cursor again, cursor = %s", cursorID(app))
	}
}

func TestDetailJumpOutsideFilter(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	app.list.setFilters(listFilters{types: []string{"feature"}})
	runCmd(t, app, app.list.loadBeans)
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	pressKey(t, app, "j") // demo-t003, filtered out of the tree
	pressKey(t, app, "enter")
	if app.detail.bean.ID != "demo-t003" {
		t.Fatalf("the detail pane should show the opened bean, shows %s", app.detail.bean.ID)
	}
	if cursorID(app) != "demo-t001" {
		t.Errorf("the tree cursor should stay, cursor = %s", cursorID(app))
	}
	pressKey(t, app, "esc")
	if app.detail.bean.ID != "demo-t001" {
		t.Errorf("leaving the detail pane should show the tree's bean again, shows %s", app.detail.bean.ID)
	}
}

func TestDetailBodyScroll(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	pressKey(t, app, "tab")
	if app.detail.section != sectionBody {
		t.Fatal("tab should focus the body")
	}
	pressKey(t, app, "j")
	if app.detail.bodyOffset != 1 {
		t.Errorf("j should scroll the body, offset = %d", app.detail.bodyOffset)
	}
	screen := stripAnsi(app.View())
	if !strings.Contains(screen, "Body · 2–") {
		t.Errorf("the body label should show the scroll position:\n%s", screen)
	}
	for i := 0; i < 20; i++ {
		pressKey(t, app, "j")
	}
	l := app.detail.layout()
	if want := len(app.detail.body) - l.bodyRows; app.detail.bodyOffset != want {
		t.Errorf("scrolling should stop at the end: offset %d, want %d", app.detail.bodyOffset, want)
	}
}

func TestNarrowDetailIsFullScreen(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 70, 24)
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	screen := stripAnsi(app.View())
	if strings.Contains(screen, "─ Beans") || !strings.Contains(screen, "Title · demo-t001") {
		t.Errorf("narrow terminals should show the detail pane instead of the tree:\n%s", screen)
	}
	pressKey(t, app, "esc")
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "─ Beans") {
		t.Errorf("esc should show the tree again:\n%s", screen)
	}
}

func TestDetailEditKeysTargetDetailBean(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	pressKey(t, app, "j")
	pressKey(t, app, "enter") // now showing demo-t003

	_, cmd := app.Update(keyRunes("s"))
	msg := cmd()
	open, ok := msg.(openStatusPickerMsg)
	if !ok || !reflect.DeepEqual(open.beanIDs, []string{"demo-t003"}) {
		t.Errorf("s in the detail pane should edit the shown bean: %#v", msg)
	}
	_, cmd = app.Update(keyRunes("y"))
	if copyMsg, ok := cmd().(copyBeanIDMsg); !ok || !reflect.DeepEqual(copyMsg.ids, []string{"demo-t003"}) {
		t.Errorf("y should copy the full ID of the shown bean")
	}
}

func TestDetailRefreshesAfterEdit(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t002")
	app.syncLayout()
	_, cmd := app.Update(statusSelectedMsg{beanIDs: []string{"demo-t002"}, status: "completed"})
	runCmd(t, app, cmd)
	if app.detail.bean.Status != "completed" {
		t.Errorf("detail shows status %q after the edit", app.detail.bean.Status)
	}
	if !strings.Contains(stripAnsi(app.View()), "completed") {
		t.Error("the view should show the new status")
	}
}

func TestHelpOverlayListsNewKeys(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "?")
	if app.state != viewHelpOverlay {
		t.Fatalf("? should open the help, state = %v", app.state)
	}
	view := stripAnsi(app.View())
	for _, want := range []string{"zR zM", "Collapse", "Focus details", "Open related bean", "Create new bean", "g t"} {
		if !strings.Contains(view, want) {
			t.Errorf("help lacks %q:\n%s", want, view)
		}
	}
	// The help fits small terminals without cutting descriptions.
	small := newDemoApp(t, 70, 24)
	pressKey(t, small, "?")
	view = stripAnsi(small.View())
	checkFrame(t, view, 70, 24)
	for _, sec := range helpSections {
		for _, k := range sec.keys {
			if !strings.Contains(view, k[1]) {
				t.Errorf("70x24 help lacks %q:\n%s", k[1], view)
			}
		}
	}
	pressKey(t, app, "esc")
	if app.state != viewList {
		t.Error("esc should close the help")
	}
}

func TestCreatedBeanIsSelected(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	t.Setenv("EDITOR", "true")
	t.Setenv("VISUAL", "")
	_, cmd := app.Update(beanCreatedMsg{title: "Brand new"})
	// Load the beans; the editor command is not run in tests.
	for _, c := range cmd().(tea.BatchMsg) {
		if msg := c(); msg != nil {
			if _, isEditor := msg.(openEditorMsg); !isEditor {
				app.Update(msg)
			}
		}
	}
	if b := app.list.currentBean(); b == nil || b.Title != "Brand new" {
		t.Errorf("the new bean should be selected, cursor on %v", b)
	}
}

// TestFocusChangesAreSynchronous checks that keys typed right after a focus
// change go to the newly focused pane, even before any command has run.
func TestFocusChangesAreSynchronous(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t001")
	app.Update(keyType(tea.KeyEnter))
	app.Update(keyType(tea.KeyTab))
	if app.focus != focusDetail || app.detail.section != sectionBody {
		t.Fatalf("enter then tab: focus=%v section=%v", app.focus, app.detail.section)
	}
	app.Update(keyType(tea.KeyTab))
	app.Update(keyRunes("j"))
	app.Update(keyType(tea.KeyEnter))
	app.Update(keyType(tea.KeyEsc))
	app.Update(keyRunes("j"))
	if app.focus != focusTree || cursorID(app) != "demo-t004" {
		t.Errorf("after jumping to t003 and esc, j should move the tree: focus=%v cursor=%s", app.focus, cursorID(app))
	}
}

func TestTreeKeysFromDetailReturnToTree(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKey(t, app, "enter")
	pressKey(t, app, "/")
	if app.focus != focusTree || !app.list.searching {
		t.Errorf("/ in the detail pane should start a search in the tree: focus=%v searching=%v", app.focus, app.list.searching)
	}
}
