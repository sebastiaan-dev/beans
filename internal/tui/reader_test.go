package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// addLongBean creates a child of demo-e001 with a long body: paragraphs,
// a list of checkboxes in the middle and more paragraphs.
func addLongBean(t *testing.T, app *App) string {
	t.Helper()
	var body strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&body, "Paragraph %d of a long description that goes on for a while.\n\n", i)
	}
	body.WriteString("## Checklist\n\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&body, "- [ ] step %d\n", i)
	}
	body.WriteString("\n")
	for i := 21; i <= 40; i++ {
		fmt.Fprintf(&body, "Paragraph %d of a long description that goes on for a while.\n\n", i)
	}
	text, typ, parent := body.String(), "task", "demo-e001"
	b, err := app.resolver.CreateBean(context.Background(), model.CreateBeanInput{Title: "Long read", Type: &typ, Parent: &parent, Body: &text})
	if err != nil {
		t.Fatal(err)
	}
	app.reload()
	app.syncLayout()
	return b.ID
}

// openReaderOn moves the tree cursor to id and presses f.
func openReaderOn(t *testing.T, app *App, id string) {
	t.Helper()
	moveTo(t, app, id)
	pressKey(t, app, "f")
	if app.reader == nil || app.reader.id != id {
		t.Fatalf("f should open the reader on %s: %+v", id, app.reader)
	}
}

// checkReaderScreen asserts that the screen is exactly width x height.
func checkReaderScreen(t *testing.T, screen string, width, height int) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != height {
		t.Errorf("screen has %d lines, want %d", len(lines), height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != width {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, w, width, l)
		}
	}
}

func TestReaderGolden(t *testing.T) {
	withStableRendering(t)
	cases := []struct {
		name          string
		width, height int
		ascii         bool
	}{
		{"reader-160x40", 160, 40, false},
		{"reader-80x24", 80, 24, false},
		{"reader-40x12", 40, 12, false},
		{"reader-ascii-80x24", 80, 24, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ascii {
				withASCII(t)
			}
			app := newDemoApp(t, tc.width, tc.height)
			openReaderOn(t, app, "demo-t001")
			screen := stripAnsi(app.View())
			checkReaderScreen(t, screen, tc.width, tc.height)
			if tc.ascii {
				if bad, found := nonASCII(screen); found {
					t.Errorf("ASCII reader renders non-ASCII: %q", bad)
				}
			}
			checkGolden(t, tc.name, screen)
		})
	}
}

func TestReaderLayout(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{240, 60}, {160, 40}, {100, 30}, {80, 24}, {60, 20}, {50, 16}, {40, 12}} {
		app := newDemoApp(t, size[0], size[1])
		id := addLongBean(t, app)
		openReaderOn(t, app, id)
		screen := stripAnsi(app.View())
		checkReaderScreen(t, screen, size[0], size[1])
		g := app.readerGeometry()
		if g.col > readerMaxWidth || g.col > size[0]-4 {
			t.Errorf("%v: column %d too wide", size, g.col)
		}
		if diff := (size[0] - g.col - g.left) - g.left; diff < 0 || diff > 1 {
			t.Errorf("%v: column not centered: left %d col %d", size, g.left, g.col)
		}
		for _, clutter := range []string{"╭", "beans: demo", "[1 Tree]", "Status"} {
			if strings.Contains(screen, clutter) {
				t.Errorf("%v: reader shows %q:\n%s", size, clutter, screen)
			}
		}
		lines := strings.Split(screen, "\n")
		if !strings.Contains(lines[g.top], "Long read") {
			t.Errorf("%v: title not on top: %q", size, lines[g.top])
		}
		if footer := lines[size[1]-1]; !strings.Contains(footer, "esc back") || !strings.Contains(footer, "%") {
			t.Errorf("%v: footer = %q", size, footer)
		}
		// The page keys and a picker keep the screen size.
		pressKeys(t, app, " ", "s")
		checkReaderScreen(t, stripAnsi(app.View()), size[0], size[1])
	}
}

func TestReaderEnterAndLeave(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t002")
	pressKey(t, app, "f")
	if app.reader == nil || app.reader.id != "demo-t002" {
		t.Fatal("f should open the reader on the tree's bean")
	}
	if !strings.Contains(stripAnsi(app.View()), "Check the error texts of the login mask") {
		t.Error("reader should show the title")
	}
	pressKey(t, app, "}")
	want := app.reader.id
	if want == "demo-t002" {
		t.Fatal("} should move to the next bean")
	}
	pressKey(t, app, "esc")
	if app.reader != nil || cursorID(app) != want || app.focus != focusTree {
		t.Errorf("esc should return to the tree on %s: reader=%v cursor=%s focus=%v", want, app.reader, cursorID(app), app.focus)
	}

	// f and q close it as well; q does not quit.
	for _, key := range []string{"f", "q"} {
		pressKey(t, app, "f")
		_, cmd := app.Update(keyMsgFor(key))
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("%s in the reader quits", key)
			}
		}
		if app.reader != nil {
			t.Errorf("%s should close the reader", key)
		}
	}

	// From the detail pane, back to the detail pane on the last bean.
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	pressKey(t, app, "f")
	if app.reader == nil || app.reader.id != "demo-t001" {
		t.Fatal("f should open the reader from the detail pane")
	}
	pressKey(t, app, "}")
	want = app.reader.id
	pressKey(t, app, "f")
	if app.reader != nil || app.focus != focusDetail || app.detail.bean == nil || app.detail.bean.ID != want || cursorID(app) != want {
		t.Errorf("leaving should return to the detail pane on %s: focus=%v cursor=%s", want, app.focus, cursorID(app))
	}

	// The palette command is the key.
	app = newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t002")
	typePalette(t, app, "reader mode")
	if e := paletteFirst(t, app); e.cmd == nil || e.cmd.id != "reader" {
		t.Fatalf("palette should offer the reader: %+v", e)
	}
	pressKey(t, app, "enter")
	if app.reader == nil || app.reader.id != "demo-t002" {
		t.Error("the palette should open the reader")
	}
}

func TestReaderScrolling(t *testing.T) {
	app := newDemoApp(t, 40, 12)
	openReaderOn(t, app, addLongBean(t, app))
	r := app.reader
	last := r.docLen - r.viewH
	if last <= 0 {
		t.Fatalf("document should not fit: %d lines, %d rows", r.docLen, r.viewH)
	}
	pressKey(t, app, "k")
	if r.offset != 0 {
		t.Errorf("k at the top: offset %d", r.offset)
	}
	pressKeys(t, app, "j", "j")
	if r.offset != 2 {
		t.Errorf("j j: offset %d", r.offset)
	}
	pressKey(t, app, " ")
	if want := 2 + r.viewH - 1; r.offset != want {
		t.Errorf("space: offset %d, want %d", r.offset, want)
	}
	app.Update(keyMsgFor("pgup"))
	if r.offset != 2 {
		t.Errorf("pgup: offset %d", r.offset)
	}
	app.Update(keyMsgFor("ctrl+d"))
	if want := 2 + r.viewH/2; r.offset != want {
		t.Errorf("ctrl+d: offset %d, want %d", r.offset, want)
	}
	app.Update(keyMsgFor("ctrl+u"))
	if r.offset != 2 {
		t.Errorf("ctrl+u: offset %d", r.offset)
	}
	pressKey(t, app, "G")
	if r.offset != last {
		t.Errorf("G: offset %d, want %d", r.offset, last)
	}
	if footer := stripAnsi(app.View()); !strings.Contains(footer, "100%") {
		t.Errorf("at the end the footer should say 100%%:\n%s", footer)
	}
	pressKeys(t, app, "j", "j")
	app.Update(keyMsgFor("pgdown"))
	if r.offset != last {
		t.Errorf("past the end: offset %d, want %d", r.offset, last)
	}
	pressKeys(t, app, "g", "g")
	if r.offset != 0 {
		t.Errorf("gg: offset %d", r.offset)
	}
	checkReaderScreen(t, stripAnsi(app.View()), 40, 12)
}

func TestReaderStepsThroughTreeOrder(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	pressKeys(t, app, "z", "M") // fold everything: only top-level rows
	order := visibleIDs(app.list)
	app.list.cursor = 0
	pressKey(t, app, "f")
	var seen []string
	for range order {
		seen = append(seen, app.reader.id)
		pressKey(t, app, "}")
	}
	if !slices.Equal(seen, order) {
		t.Errorf("} should follow the tree as shown:\n got %v\nwant %v", seen, order)
	}
	if app.reader.id != order[len(order)-1] || !strings.Contains(app.list.statusMessage, "Last bean") {
		t.Errorf("} at the end: %s %q", app.reader.id, app.list.statusMessage)
	}
	pressKey(t, app, "{")
	if app.reader.id != order[len(order)-2] {
		t.Errorf("{ should go back: %s", app.reader.id)
	}
	pressKey(t, app, "esc")
	if cursorID(app) != order[len(order)-2] {
		t.Errorf("the cursor should follow the reader: %s", cursorID(app))
	}
}

func TestReaderStepsThroughReadyView(t *testing.T) {
	app := newViewsApp(t, 160, 40)
	pressKey(t, app, "2")
	order := app.readerOrder()
	if len(order) < 3 {
		t.Fatalf("ready view too short: %v", order)
	}
	selectRow(t, app, order[0], 0)
	pressKey(t, app, "f")
	pressKeys(t, app, "}", "}")
	if app.reader.id != order[2] {
		t.Errorf("} } in the Ready view: %s, want %s", app.reader.id, order[2])
	}
	pressKey(t, app, "{")
	pressKey(t, app, "esc")
	if r, ok := app.activeList().current(); !ok || r.beanID != order[1] || app.views.active != viewReady {
		t.Errorf("leaving should select %s in the Ready view: %+v", order[1], r)
	}

	// The Blocked view steps through the blocked beans, not their blockers.
	pressKey(t, app, "3")
	for _, row := range app.activeList().rows {
		if row.jumpID != "" && slices.Contains(app.readerOrder(), row.beanID) && !slices.ContainsFunc(app.activeList().rows, func(o flatRow) bool { return o.beanID == row.beanID && o.jumpID == "" }) {
			t.Errorf("blocker %s should not be in the order", row.beanID)
		}
	}
}

func TestReaderCheckboxToggleAndUndo(t *testing.T) {
	app := newDemoApp(t, 80, 24)
	openReaderOn(t, app, "demo-t001")
	pressKey(t, app, "x") // the first checkbox on screen
	if b := getBean(t, app, "demo-t001"); !strings.Contains(b.Body, "- [x] TOTP support") {
		t.Fatalf("x should toggle the first checkbox:\n%s", b.Body)
	}
	if app.reader == nil || !strings.Contains(stripAnsi(app.View()), "[✓] ▶ TOTP support") {
		t.Errorf("the reader should show the toggled checkbox:\n%s", stripAnsi(app.View()))
	}
	pressKeys(t, app, "]", "x")
	if b := getBean(t, app, "demo-t001"); !strings.Contains(b.Body, "- [x] Recovery codes") {
		t.Errorf("] x should toggle the second checkbox:\n%s", b.Body)
	}
	pressKeys(t, app, "u", "u")
	if b := getBean(t, app, "demo-t001"); strings.Contains(b.Body, "[x]") {
		t.Errorf("u u should undo both toggles:\n%s", b.Body)
	}
	if app.reader == nil || app.reader.id != "demo-t001" {
		t.Error("undo should keep the reader open")
	}
}

func TestReaderCheckboxScrollsIntoView(t *testing.T) {
	app := newDemoApp(t, 60, 16)
	openReaderOn(t, app, addLongBean(t, app))
	r := app.reader
	// No checkbox on the first page: x selects the next one without toggling.
	pressKey(t, app, "x")
	id := r.id
	if b := getBean(t, app, id); strings.Contains(b.Body, "[x]") {
		t.Fatal("x must not toggle a checkbox that is not on screen")
	}
	doc := app.readerDocument(getBean(t, app, id), app.readerGeometry().col)
	line := doc.checks[r.checkCursor]
	if r.checkCursor != 0 || line < r.offset || line >= r.offset+r.viewH {
		t.Errorf("the first checkbox should be scrolled into view: cursor %d line %d offset %d", r.checkCursor, line, r.offset)
	}
	pressKey(t, app, "x")
	if b := getBean(t, app, id); !strings.Contains(b.Body, "- [x] step 1") {
		t.Errorf("x should now toggle step 1:\n%s", b.Body)
	}
}

func TestReaderRelations(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	openReaderOn(t, app, "demo-t001")
	screen := stripAnsi(app.View())
	if !strings.Contains(screen, "Related") || !strings.Contains(screen, "Login overhaul") {
		t.Fatalf("reader should list the relations:\n%s", screen)
	}
	pressKeys(t, app, "tab", "enter")
	if app.reader.id != "demo-e001" {
		t.Fatalf("tab enter should open the parent: %s", app.reader.id)
	}
	pressKey(t, app, "backspace")
	if app.reader.id != "demo-t001" {
		t.Errorf("backspace should return: %s", app.reader.id)
	}
}

func TestReaderEditAndPickers(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "sh -c 'true'")
	openReaderOn(t, app, "demo-t002")
	msgs := runCmd(t, app, app.handleReaderKey(keyMsgFor("e")))
	if len(msgs) == 0 || app.editingBeanID != "demo-t002" {
		t.Fatalf("e should open the editor: %v", msgs)
	}
	// Simulate the editor round trip.
	body := "Edited in the editor."
	externalUpdate(t, app, "demo-t002", model.UpdateBeanInput{Body: &body})
	app.editingBeanModTime = app.editingBeanModTime.Add(-1e9)
	_, cmd := app.Update(editorFinishedMsg{})
	runCmd(t, app, cmd)
	if app.reader == nil || app.reader.id != "demo-t002" || !strings.Contains(stripAnsi(app.View()), "Edited in the editor.") {
		t.Errorf("the reader should show the edited body:\n%s", stripAnsi(app.View()))
	}

	// A picker opens over the reader and returns to it.
	pressKey(t, app, "s")
	if app.state != viewPicker || app.picker.kind != pickStatus || app.picker.beanIDs[0] != "demo-t002" {
		t.Fatalf("s should open the status picker: state %v", app.state)
	}
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "Select Status") || strings.Contains(screen, "[1 Tree]") {
		t.Errorf("the picker should be drawn over the reader:\n%s", screen)
	}
	pressKeys(t, app, "/", "c", "o", "m", "p")
	pressKey(t, app, "enter") // keep the filter
	pressKey(t, app, "enter") // choose
	if app.state != viewList || app.reader == nil {
		t.Fatalf("choosing should return to the reader: state %v", app.state)
	}
	if b := getBean(t, app, "demo-t002"); b.Status != "completed" {
		t.Errorf("status = %q", b.Status)
	}
	if !strings.Contains(stripAnsi(app.View()), "completed") {
		t.Error("the reader should show the new status")
	}
}

func TestReaderLiveUpdates(t *testing.T) {
	app := newDemoApp(t, 40, 12)
	id := addLongBean(t, app)
	openReaderOn(t, app, id)
	pressKeys(t, app, "j", "j", "j")
	b := getBean(t, app, id)
	body := "Changed on disk.\n\n" + b.Body
	externalUpdate(t, app, id, model.UpdateBeanInput{Body: &body})
	_, cmd := app.Update(beansChangedMsg{})
	runCmd(t, app, cmd)
	if app.reader == nil || app.reader.offset != 3 {
		t.Fatalf("a change should keep the reader and its scroll position: %+v", app.reader)
	}
	pressKeys(t, app, "g", "g")
	if !strings.Contains(stripAnsi(app.View()), "Changed on disk.") {
		t.Error("the reader should show the changed body")
	}

	if _, err := app.resolver.DeleteBean(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_, cmd = app.Update(beansChangedMsg{})
	runCmd(t, app, cmd)
	if app.reader != nil || !strings.Contains(app.list.statusMessage, "was deleted") {
		t.Errorf("deleting the bean should close the reader: %v %q", app.reader, app.list.statusMessage)
	}
	checkFrame(t, stripAnsi(app.View()), 40, 12)
}

func TestReaderMouse(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 40, 12)
	openReaderOn(t, app, addLongBean(t, app))
	wheel(t, app, rect{x: 20, y: 5}, true)
	if app.reader.offset != wheelBodyLines {
		t.Errorf("wheel down: offset %d", app.reader.offset)
	}
	wheel(t, app, rect{x: 20, y: 5}, false)
	wheel(t, app, rect{x: 20, y: 5}, false)
	if app.reader.offset != 0 {
		t.Errorf("wheel up: offset %d", app.reader.offset)
	}

	// Clicking a checkbox line toggles it.
	app = newDemoApp(t, 80, 24)
	openReaderOn(t, app, "demo-t001")
	reg := region(t, app, hitReaderCheckbox, withIndex(1))
	if text := cellText(screenLines(app), reg.r); !strings.Contains(text, "Recovery codes") {
		t.Fatalf("checkbox region shows %q", text)
	}
	click(t, app, reg.r)
	if b := getBean(t, app, "demo-t001"); !strings.Contains(b.Body, "- [x] Recovery codes") {
		t.Errorf("clicking should toggle the checkbox:\n%s", b.Body)
	}

	// Clicking a relation opens it.
	reg = region(t, app, hitReaderRelation, withIndex(0))
	if text := cellText(screenLines(app), reg.r); !strings.Contains(text, "Login overhaul") {
		t.Fatalf("relation region shows %q", text)
	}
	click(t, app, reg.r)
	if app.reader.id != "demo-e001" {
		t.Errorf("clicking a relation should open it: %s", app.reader.id)
	}
}

func TestReaderFooterHintsMatchRegistry(t *testing.T) {
	tokens := registryKeyTokens()
	app := newDemoApp(t, 160, 40)
	openReaderOn(t, app, "demo-t001")
	pressKeys(t, app, "tab", "enter") // history
	for _, h := range app.readerHints() {
		for _, f := range strings.FieldsFunc(h.key, func(r rune) bool { return r == ' ' || r == '/' }) {
			if !tokens[f] && !tokens[h.key] {
				t.Errorf("reader hint %q %q is not a registered key", h.key, h.desc)
			}
		}
	}
}
