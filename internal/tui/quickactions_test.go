package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
)

// typeText sends text as one key message (like typing quickly).
func typeText(t *testing.T, app *App, text string) {
	t.Helper()
	_, cmd := app.Update(keyRunes(text))
	drainCmd(t, app, cmd)
}

// pressKeys presses each key in turn.
func pressKeys(t *testing.T, app *App, keys ...string) {
	t.Helper()
	for _, k := range keys {
		pressKey(t, app, k)
	}
}

func getBean(t *testing.T, app *App, id string) *bean.Bean {
	t.Helper()
	b, err := app.core.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return b
}

// beanByTitle returns the bean with the title, or nil.
func beanByTitle(app *App, title string) *bean.Bean {
	for _, b := range app.core.All() {
		if b.Title == title {
			return b
		}
	}
	return nil
}

// externalUpdate changes a bean like another client would.
func externalUpdate(t *testing.T, app *App, id string, input model.UpdateBeanInput) {
	t.Helper()
	if _, err := app.resolver.UpdateBean(context.Background(), id, input); err != nil {
		t.Fatal(err)
	}
	app.reload()
}

func TestChildTypeFor(t *testing.T) {
	cfg := config.Default()
	for parent, want := range map[string]string{"milestone": "epic", "epic": "feature", "feature": "task"} {
		if got, ok := childTypeFor(cfg, parent); !ok || got != want {
			t.Errorf("child of %s = %q, %v; want %q", parent, got, ok, want)
		}
	}
	for _, parent := range []string{"task", "bug"} {
		if got, ok := childTypeFor(cfg, parent); ok {
			t.Errorf("%s beans can't have children, got %q", parent, got)
		}
	}
}

func TestInlineCreateSibling(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "c")
	if app.list.inline == nil || !app.acceptsTextInput() {
		t.Fatal("c should open the inline input")
	}
	typeText(t, app, "New sibling q")
	view := stripAnsi(app.View())
	if !strings.Contains(view, "New sibling q") {
		t.Errorf("the input should show the title:\n%s", view)
	}
	pressKey(t, app, "enter")

	b := beanByTitle(app, "New sibling q")
	if b == nil {
		t.Fatal("the bean was not created")
	}
	if b.Parent != "demo-e001" || b.Type != "task" {
		t.Errorf("sibling of t002: parent=%q type=%q, want demo-e001 task (like t002)", b.Parent, b.Type)
	}
	if cursorID(app) != b.ID {
		t.Errorf("the new bean should be selected, cursor on %s", cursorID(app))
	}
	if app.list.inline != nil {
		t.Error("the input should close")
	}
	if !strings.Contains(app.list.statusMessage, "Created") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestInlineCreateChildAndTopLevel(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-m001")
	pressKey(t, app, "C")
	// The input is indented below the milestone.
	lines := strings.Split(stripAnsi(app.View()), "\n")
	found := false
	for i, l := range lines {
		if strings.Contains(l, "m001 Release 1.0") && i+1 < len(lines) && strings.Contains(lines[i+1], "+ T E") {
			found = true
		}
	}
	if !found {
		t.Errorf("the child input should be right below its parent:\n%s", strings.Join(lines, "\n"))
	}
	typeText(t, app, "Child epic")
	pressKey(t, app, "enter")
	b := beanByTitle(app, "Child epic")
	if b == nil || b.Parent != "demo-m001" || b.Type != "epic" {
		t.Fatalf("child of milestone = %+v, want an epic under demo-m001", b)
	}
	if cursorID(app) != b.ID {
		t.Errorf("the new bean should be selected, cursor on %s", cursorID(app))
	}

	// Siblings of top-level beans are top-level.
	moveTo(t, app, "demo-m002")
	pressKey(t, app, "c")
	typeText(t, app, "Top level")
	pressKey(t, app, "enter")
	if b := beanByTitle(app, "Top level"); b == nil || b.Parent != "" || b.Type != "milestone" {
		t.Errorf("top-level sibling = %+v", b)
	}

	// Tasks can't have children.
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "C")
	if app.list.inline != nil || !strings.Contains(app.list.statusMessage, "can't have children") {
		t.Errorf("C on a task: inline=%v status=%q", app.list.inline != nil, app.list.statusMessage)
	}
}

func TestInlineCreateCancelAndEmpty(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	n := len(app.core.All())
	pressKey(t, app, "c")
	typeText(t, app, "Nope")
	pressKey(t, app, "esc")
	if app.list.inline != nil || len(app.core.All()) != n {
		t.Error("esc should cancel without creating")
	}
	// q while typing is text, not quit.
	app2 := newDemoApp(t, 100, 30)
	pressKey(t, app2, "c")
	if pressKeyQuits(t, app2, keyRunes("q")) {
		t.Error("q in the inline input must not quit")
	}
	pressKey(t, app2, "esc")
	pressKey(t, app2, "c")
	pressKey(t, app2, "enter")
	if len(app2.core.All()) != n {
		t.Error("an empty title must not create a bean")
	}
}

func TestInlineCreateInEmptyProject(t *testing.T) {
	app, core := setupListTestModel(t)
	runCmd(t, app, app.Init())
	for _, b := range core.All() {
		if _, err := app.resolver.DeleteBean(context.Background(), b.ID); err != nil {
			t.Fatal(err)
		}
	}
	app.reload()
	pressKey(t, app, "c")
	typeText(t, app, "First")
	if !strings.Contains(stripAnsi(app.View()), "First") {
		t.Error("the input should be shown in an empty tree")
	}
	pressKey(t, app, "enter")
	if b := beanByTitle(app, "First"); b == nil || cursorID(app) != b.ID || b.Type != app.config.GetDefaultType() {
		t.Errorf("the first bean should be created with the default type and selected: %+v", b)
	}
}

func TestInlineCreatePlacesSiblingAfterOrderedAnchor(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	// Order t002 below t003 manually (both todo under e001).
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "J")
	if got := visibleIDs(app.list); !strings.Contains(strings.Join(got, " "), "demo-t003 demo-t002") {
		t.Fatalf("J should move t002 below t003: %v", got)
	}
	moveTo(t, app, "demo-t003")
	pressKey(t, app, "c")
	typeText(t, app, "Between")
	pressKey(t, app, "enter")
	b := beanByTitle(app, "Between")
	ids := strings.Join(visibleIDs(app.list), " ")
	if !strings.Contains(ids, "demo-t003 "+b.ID+" demo-t002") {
		t.Errorf("the new bean should be right after t003: %s", ids)
	}
}

func TestInlineRename(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "r")
	if app.list.inline == nil || app.list.inline.input.Value() != "Check the error texts of the login mask" {
		t.Fatal("r should open an input prefilled with the title")
	}
	pressKey(t, app, "esc")
	if getBean(t, app, "demo-t002").Title != "Check the error texts of the login mask" {
		t.Error("esc must not rename")
	}
	pressKey(t, app, "r")
	for range len("mask") {
		pressKey(t, app, "backspace")
	}
	typeText(t, app, "form")
	pressKey(t, app, "enter")
	if got := getBean(t, app, "demo-t002").Title; got != "Check the error texts of the login form" {
		t.Errorf("title = %q", got)
	}
	if cursorID(app) != "demo-t002" {
		t.Errorf("cursor should stay on the renamed bean, on %s", cursorID(app))
	}
	pressKey(t, app, "u")
	if got := getBean(t, app, "demo-t002").Title; got != "Check the error texts of the login mask" {
		t.Errorf("undo should restore the title, got %q", got)
	}
	if !strings.Contains(app.list.statusMessage, "Undid title of t002") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestDetailCheckboxes(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t001")
	pressKeys(t, app, "enter", "tab")
	if app.focus != focusDetail || app.detail.section != sectionBody {
		t.Fatal("the body should be focused")
	}
	view := app.View()
	if !strings.Contains(view, "▶ TOTP support") {
		t.Errorf("the first checkbox should be highlighted:\n%s", view)
	}
	pressKey(t, app, "]")
	if !strings.Contains(app.View(), "▶ Recovery codes") {
		t.Error("] should move to the second checkbox")
	}
	pressKey(t, app, "]") // stays on the last one
	pressKey(t, app, "x")
	body := getBean(t, app, "demo-t001").Body
	if !strings.Contains(body, "- [ ] TOTP support\n- [x] Recovery codes") {
		t.Errorf("x should check the second box:\n%s", body)
	}
	if !strings.Contains(app.list.statusMessage, `Checked "Recovery codes"`) {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	pressKeys(t, app, "[", " ")
	if body := getBean(t, app, "demo-t001").Body; !strings.Contains(body, "- [x] TOTP support") {
		t.Errorf("space should check the first box:\n%s", body)
	}
	pressKey(t, app, "u")
	if body := getBean(t, app, "demo-t001").Body; !strings.Contains(body, "- [ ] TOTP support\n- [x] Recovery codes") {
		t.Errorf("undo should uncheck the first box:\n%s", body)
	}
	// The highlight is only shown while the body has the focus.
	pressKey(t, app, "esc")
	if strings.Contains(app.View(), "▶") {
		t.Error("no highlight without focus")
	}
}

func TestNextJumpsToReadyBean(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	want, err := app.resolver.NextBean(context.Background())
	if err != nil || want == nil {
		t.Fatalf("NextBean = %v, %v", want, err)
	}
	// Fold everything: the next bean is revealed.
	pressKeys(t, app, "z", "M", "n")
	if cursorID(app) != want.ID {
		t.Errorf("n: cursor on %s, want %s", cursorID(app), want.ID)
	}
	if !strings.Contains(app.list.statusMessage, "Next:") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	// Filtered away: say so and keep the cursor.
	app.list.setFilter(filterquery.Parse("status:draft"))
	app.reload()
	before := cursorID(app)
	pressKey(t, app, "n")
	if !strings.Contains(app.list.statusMessage, "hidden by the filters") || cursorID(app) != before {
		t.Errorf("status = %q, cursor %s (was %s)", app.list.statusMessage, cursorID(app), before)
	}
}

func TestUndoPickerChanges(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	moveTo(t, app, "demo-t002")
	app.Update(statusSelectedMsg{beanIDs: []string{"demo-t002", "demo-t008"}, status: "completed"})
	app.Update(prioritySelectedMsg{beanIDs: []string{"demo-t002"}, priority: "high"})
	if getBean(t, app, "demo-t002").Status != "completed" || getBean(t, app, "demo-t002").Priority != "high" {
		t.Fatal("changes not applied")
	}
	pressKey(t, app, "u")
	if p := getBean(t, app, "demo-t002").Priority; p != "normal" {
		t.Errorf("undo priority: %q", p)
	}
	if !strings.Contains(app.list.statusMessage, "priority of t002") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	pressKey(t, app, "u")
	for _, id := range []string{"demo-t002", "demo-t008"} {
		if s := getBean(t, app, id).Status; s != "todo" {
			t.Errorf("undo status of %s: %q", id, s)
		}
	}
	if !strings.Contains(app.list.statusMessage, "status of 2 beans") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	pressKey(t, app, "u")
	if app.list.statusMessage != "Nothing to undo" {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestUndoParentAndBlocking(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	app.Update(parentSelectedMsg{beanIDs: []string{"demo-t008"}, parentID: "demo-e002"})
	app.Update(blockingConfirmedMsg{beanID: "demo-t008", toAdd: []string{"demo-t005"}})
	b := getBean(t, app, "demo-t008")
	if b.Parent != "demo-e002" || len(b.Blocking) != 1 {
		t.Fatalf("changes not applied: %+v", b)
	}
	pressKeys(t, app, "u", "u")
	b = getBean(t, app, "demo-t008")
	if b.Parent != "" || len(b.Blocking) != 0 {
		t.Errorf("undo should restore parent and blocking: parent=%q blocking=%v", b.Parent, b.Blocking)
	}
}

func TestUndoRefusesAfterExternalChange(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	app.Update(statusSelectedMsg{beanIDs: []string{"demo-t002"}, status: "completed"})
	app.Update(typeSelectedMsg{beanIDs: []string{"demo-t008"}, beanType: "bug"})
	title := "Changed elsewhere"
	externalUpdate(t, app, "demo-t008", model.UpdateBeanInput{Title: &title})

	pressKey(t, app, "u")
	if b := getBean(t, app, "demo-t008"); b.Type != "bug" || b.Title != title {
		t.Errorf("undo must not clobber the external change: %+v", b)
	}
	if msg := app.list.statusMessage; !strings.Contains(msg, "Can't undo type of t008") || !strings.Contains(msg, "changed since") {
		t.Errorf("status = %q", msg)
	}
	// The refused entry is dropped; the earlier change can still be undone.
	pressKey(t, app, "u")
	if s := getBean(t, app, "demo-t002").Status; s != "todo" {
		t.Errorf("earlier change should be undone, status %q", s)
	}
}

func TestUndoCreate(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	n := len(app.core.All())
	pressKey(t, app, "c")
	typeText(t, app, "Short lived")
	pressKey(t, app, "enter")
	created := beanByTitle(app, "Short lived")
	pressKey(t, app, "u")
	if len(app.core.All()) != n || beanByTitle(app, "Short lived") != nil {
		t.Error("undo should delete the created bean")
	}
	if !strings.Contains(app.list.statusMessage, "Undid create of "+app.shortID(created.ID)) {
		t.Errorf("status = %q", app.list.statusMessage)
	}

	// A created bean that changed since is not deleted.
	pressKey(t, app, "c")
	typeText(t, app, "Keeper")
	pressKey(t, app, "enter")
	keeper := beanByTitle(app, "Keeper")
	body := "notes"
	externalUpdate(t, app, keeper.ID, model.UpdateBeanInput{Body: &body})
	pressKey(t, app, "u")
	if beanByTitle(app, "Keeper") == nil {
		t.Error("a changed bean must not be deleted by undo")
	}
	if !strings.Contains(app.list.statusMessage, "Can't undo create") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestMoveAmongSiblings(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	order := func() string { return strings.Join(visibleIDs(app.list), " ") }
	moveTo(t, app, "demo-t003")
	pressKey(t, app, "K")
	if !strings.Contains(order(), "demo-t003 demo-t002") {
		t.Fatalf("K should move t003 above t002: %s", order())
	}
	if cursorID(app) != "demo-t003" {
		t.Errorf("the cursor should follow the bean, on %s", cursorID(app))
	}
	for _, id := range []string{"demo-t002", "demo-t003"} {
		if getBean(t, app, id).Order == "" {
			t.Errorf("%s should have an order key", id)
		}
	}
	// Up past an in-progress sibling is refused: status sorts first.
	pressKey(t, app, "K")
	if !strings.Contains(app.list.statusMessage, "sorted by status") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	// Moving down and undoing.
	pressKey(t, app, "J")
	if !strings.Contains(order(), "demo-t002 demo-t003") {
		t.Errorf("J should move t003 back below t002: %s", order())
	}
	pressKey(t, app, "u")
	if !strings.Contains(order(), "demo-t003 demo-t002") {
		t.Errorf("undo should move t003 above t002 again: %s", order())
	}
	pressKey(t, app, "u")
	for _, id := range []string{"demo-t002", "demo-t003"} {
		if o := getBean(t, app, id).Order; o != "" {
			t.Errorf("undoing the first move should clear %s's order, got %q", id, o)
		}
	}
	// The last top-level bean has nothing below.
	moveTo(t, app, "demo-m002")
	pressKey(t, app, "J")
	if !strings.Contains(app.list.statusMessage, "no sibling") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestUndoEditorChange(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "sh -c 'true'")
	b := getBean(t, app, "demo-t002")
	// Simulate the editor round trip without running one.
	app.Update(openEditorMsg{beanID: b.ID, beanPath: b.Path})
	body := "edited in the editor"
	externalUpdate(t, app, b.ID, model.UpdateBeanInput{Body: &body})
	app.editingBeanModTime = app.editingBeanModTime.Add(-1e9)
	app.Update(editorFinishedMsg{})
	pressKey(t, app, "u")
	if got := getBean(t, app, b.ID).Body; got != "" {
		t.Errorf("undo should restore the body, got %q", got)
	}
}

func TestQuickKeysInHelpAndFooter(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	footer := stripAnsi(strings.Join(app.footerLines(156, true), " "))
	for _, want := range []string{"C child", "r rename", "n next", "u undo", "K J move"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer lacks %q: %s", want, footer)
		}
	}
	pressKey(t, app, "c")
	footer = stripAnsi(strings.Join(app.footerLines(156, true), " "))
	if !strings.Contains(footer, "enter create") || !strings.Contains(footer, "esc cancel") {
		t.Errorf("inline footer: %s", footer)
	}
}

// TestUndoChainAcrossSeconds checks that undoing several changes of the same
// bean works when the undo writes get a newer updated_at (and so a different
// etag) than the state the older changes left behind.
func TestUndoChainAcrossSeconds(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	app.Update(statusSelectedMsg{beanIDs: []string{"demo-t002"}, status: "in-progress"})
	app.Update(prioritySelectedMsg{beanIDs: []string{"demo-t002"}, priority: "high"})
	// Wait for the next second, so that the undo writes get a new updated_at.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second + 10*time.Millisecond)))
	pressKeys(t, app, "u", "u")
	b := getBean(t, app, "demo-t002")
	if b.Status != "todo" || b.Priority != "normal" {
		t.Errorf("both changes should be undone: status=%q priority=%q (%s)", b.Status, b.Priority, app.list.statusMessage)
	}
}
