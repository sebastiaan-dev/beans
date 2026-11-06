package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// Mouse tests render the screen, look up the recorded region of what they
// click and check it against the rendered text before clicking it.

// screenLines renders the app and returns the screen as plain lines.
func screenLines(app *App) []string {
	return strings.Split(stripAnsi(app.View()), "\n")
}

// cellText returns the plain text of a screen rectangle, lines joined by
// "\n" (all fixture glyphs are one cell wide).
func cellText(screen []string, r rect) string {
	var out []string
	for y := r.y; y < r.y+r.h && y < len(screen); y++ {
		rs := []rune(screen[y])
		x0, x1 := min(r.x, len(rs)), min(r.x+r.w, len(rs))
		out = append(out, string(rs[x0:x1]))
	}
	return strings.Join(out, "\n")
}

// region renders the app and returns the first region of a kind matching
// pred.
func region(t *testing.T, app *App, kind hitKind, pred func(hitRegion) bool) hitRegion {
	t.Helper()
	app.View()
	for _, reg := range app.hits.all(kind) {
		if pred == nil || pred(reg) {
			return reg
		}
	}
	t.Fatalf("no region of kind %d matches", kind)
	return hitRegion{}
}

func withIndex(i int) func(hitRegion) bool {
	return func(r hitRegion) bool { return r.index == i }
}

func withKey(k string) func(hitRegion) bool {
	return func(r hitRegion) bool { return r.key == k }
}

// mouse sends a mouse event at (x, y) on the freshly rendered screen and
// runs the resulting commands.
func mouse(t *testing.T, app *App, x, y int, button tea.MouseButton) {
	t.Helper()
	app.View()
	_, cmd := app.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button})
	runCmd(t, app, cmd)
}

func click(t *testing.T, app *App, r rect) {
	t.Helper()
	mouse(t, app, r.x, r.y, tea.MouseButtonLeft)
}

func wheel(t *testing.T, app *App, r rect, down bool) {
	t.Helper()
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	mouse(t, app, r.x, r.y, b)
}

// clickRow clicks the tree row of a bean.
func clickRow(t *testing.T, app *App, id string) {
	t.Helper()
	i := app.list.rowIndex(id)
	if i < 0 {
		t.Fatalf("%s not in the tree", id)
	}
	click(t, app, region(t, app, hitTreeRow, withIndex(i)).r)
}

// revealBean moves the tree cursor to a bean and shows it in the detail
// pane.
func revealBean(t *testing.T, app *App, id string) {
	t.Helper()
	if !app.list.reveal(id) {
		t.Fatalf("%s not in the tree", id)
	}
	app.syncLayout()
}

// stubMouseClock makes mouseNow return *now.
func stubMouseClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := mouseNow
	mouseNow = func() time.Time { return now }
	t.Cleanup(func() { mouseNow = prev })
	return &now
}

var mouseSizes = [][2]int{{160, 40}, {100, 30}, {70, 24}}

// TestMouseGeometry checks that the recorded regions cover what is drawn
// there, in the side-by-side and stacked layouts.
func TestMouseGeometry(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			app := newDemoApp(t, size[0], size[1])
			screen := screenLines(app)

			rows := app.hits.all(hitTreeRow)
			if len(rows) == 0 {
				t.Fatal("no tree rows recorded")
			}
			for _, reg := range rows {
				b := app.list.rows[reg.index].bean
				if got := cellText(screen, reg.r); !strings.Contains(got, shortID(b.ID, "demo-")) {
					t.Errorf("row %d region %+v shows %q, want %s", reg.index, reg.r, got, b.ID)
				}
			}
			chevrons := app.hits.all(hitTreeChevron)
			if len(chevrons) == 0 {
				t.Fatal("no chevrons recorded")
			}
			for _, reg := range chevrons {
				if got := cellText(screen, reg.r); got != "▾ " && got != "▸ " {
					t.Errorf("chevron region of row %d shows %q", reg.index, got)
				}
			}
			tree := region(t, app, hitTreePane, nil)
			if got := cellText(screen, rect{tree.r.x, tree.r.y, 3, 1}); got != "╭─ " {
				t.Errorf("tree pane region starts at %q", got)
			}

			if size[1] >= filterBarMinHeight {
				for f := barField(0); f < barClear; f++ {
					reg := region(t, app, hitBarField, withIndex(int(f)))
					if got := cellText(screen, rect{reg.r.x, reg.r.y, reg.r.w, 1}); !strings.Contains(got, f.label()) || !strings.HasPrefix(got, "╭") || !strings.HasSuffix(got, "╮") {
						t.Errorf("bar field %s region shows %q", f.label(), got)
					}
				}
			} else {
				reg := region(t, app, hitBarLine, nil)
				if got := cellText(screen, reg.r); !strings.HasPrefix(got, "Filters") {
					t.Errorf("filter line region shows %q", got)
				}
			}

			// The detail pane of the two-factor bean.
			revealBean(t, app, "demo-t001")
			pressKey(t, app, "enter")
			screen = screenLines(app)
			labels := map[string]string{"e": "Title", "s": "Status", "t": "Type", "P": "Priority", "p": "Parent"}
			for key, label := range labels {
				reg := region(t, app, hitDetailField, withKey(key))
				if got := cellText(screen, rect{reg.r.x, reg.r.y, reg.r.w, 1}); !strings.Contains(got, label) || !strings.HasPrefix(got, "╭") {
					t.Errorf("field %s region top shows %q", label, got)
				}
				if got := cellText(screen, rect{reg.r.x, reg.r.y + reg.r.h - 1, reg.r.w, 1}); !strings.Contains(got, "("+key+")") || !strings.HasSuffix(got, "╯") {
					t.Errorf("field %s region bottom shows %q", label, got)
				}
			}
			for _, reg := range app.hits.all(hitRelation) {
				l := app.detail.links[reg.index]
				if got := cellText(screen, reg.r); !strings.Contains(got, shortID(l.bean.ID, "demo-")) {
					t.Errorf("relation %d region shows %q, want %s", reg.index, got, l.bean.ID)
				}
			}
			if len(app.hits.all(hitRelation)) == 0 {
				t.Error("no relations recorded")
			}
			lines := app.hits.all(hitBodyLine)
			if len(lines) == 0 {
				t.Fatal("no body lines recorded")
			}
			for _, reg := range lines {
				want := strings.TrimSpace(stripAnsi(app.detail.body[reg.index]))
				if got := cellText(screen, reg.r); !strings.Contains(got, want) {
					t.Errorf("body line %d region shows %q, want %q", reg.index, got, want)
				}
			}
		})
	}
}

func TestMouseTreeClickSelects(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		app := newDemoApp(t, size[0], size[1])
		pressKeys(t, app, "j", "enter") // focus the detail pane (replaces the tree when narrow)
		if !app.list.reveal("demo-t001") {
			t.Fatal("reveal")
		}
		pressKey(t, app, "esc")
		clickRow(t, app, "demo-t005")
		if b := app.list.currentBean(); b == nil || b.ID != "demo-t005" {
			t.Errorf("%v: cursor on %v, want demo-t005", size, b)
		}
		if app.focus != focusTree {
			t.Errorf("%v: focus = %v, want the tree", size, app.focus)
		}

		// Clicking the tree while the detail pane has the focus (wide).
		if size[0] >= wideMinWidth {
			pressKey(t, app, "enter")
			clickRow(t, app, "demo-t006")
			if b := app.list.currentBean(); app.focus != focusTree || b == nil || b.ID != "demo-t006" {
				t.Errorf("%v: focus %v on %v after clicking the tree", size, app.focus, b)
			}
		}
	}
}

func TestMouseChevronTogglesFold(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	i := app.list.rowIndex("demo-e001")
	click(t, app, region(t, app, hitTreeChevron, withIndex(i)).r)
	if app.list.rowIndex("demo-t001") >= 0 {
		t.Error("children of demo-e001 still shown after clicking its chevron")
	}
	if b := app.list.currentBean(); b == nil || b.ID != "demo-e001" {
		t.Errorf("cursor on %v, want demo-e001", b)
	}
	click(t, app, region(t, app, hitTreeChevron, withIndex(i)).r)
	if app.list.rowIndex("demo-t001") < 0 {
		t.Error("demo-e001 not expanded by the second click")
	}
	if app.focus != focusTree {
		t.Errorf("focus = %v", app.focus)
	}
}

func TestMouseDoubleClickOpensDetail(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		app := newDemoApp(t, size[0], size[1])
		now := stubMouseClock(t)
		clickRow(t, app, "demo-t002")
		*now = now.Add(time.Second)
		clickRow(t, app, "demo-t002")
		if app.focus != focusTree {
			t.Fatalf("%v: slow clicks opened the detail pane", size)
		}
		*now = now.Add(300 * time.Millisecond)
		clickRow(t, app, "demo-t002")
		if app.focus != focusDetail || app.detail.bean == nil || app.detail.bean.ID != "demo-t002" {
			t.Errorf("%v: double click: focus %v, detail %v", size, app.focus, app.detail.bean)
		}
	}
}

func TestMouseWheelScrollsTree(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 70, 16)
	tree := region(t, app, hitTreePane, nil).r
	visible := func() (int, int) {
		app.View()
		rows := app.hits.all(hitTreeRow)
		return rows[0].index, rows[len(rows)-1].index
	}
	first, _ := visible()
	if first != 0 {
		t.Fatalf("tree starts at row %d", first)
	}
	wheel(t, app, tree, true)
	first, last := visible()
	if first != wheelTreeRows {
		t.Errorf("first row after wheel down = %d, want %d", first, wheelTreeRows)
	}
	if c := app.list.cursor; c < first || c > last {
		t.Errorf("cursor %d not shown (rows %d-%d)", c, first, last)
	}
	for range 10 {
		wheel(t, app, tree, true)
	}
	if _, last := visible(); last != len(app.list.rows)-1 {
		t.Errorf("last shown row = %d, want the last row %d", last, len(app.list.rows)-1)
	}
	for range 10 {
		wheel(t, app, tree, false)
	}
	if first, _ := visible(); first != 0 || app.list.offset != 0 {
		t.Errorf("first row after wheel up = %d", first)
	}
}

func TestMouseDetailFieldRunsAction(t *testing.T) {
	withStableRendering(t)
	cases := []struct {
		key   string
		state viewState
	}{
		{"s", viewStatusPicker},
		{"t", viewTypePicker},
		{"P", viewPriorityPicker},
		{"p", viewParentPicker},
		{"b", viewBlockingPicker},
	}
	for _, size := range mouseSizes {
		for _, tc := range cases {
			app := newDemoApp(t, size[0], size[1])
			revealBean(t, app, "demo-t002")
			pressKey(t, app, "enter")
			click(t, app, region(t, app, hitDetailField, withKey(tc.key)).r)
			if app.state != tc.state {
				t.Errorf("%v: clicking the %s field: state %v, want %v", size, tc.key, app.state, tc.state)
			}
		}
	}

	// Choosing an item of the picker by clicking it.
	app := newDemoApp(t, 100, 30)
	revealBean(t, app, "demo-t002")
	pressKey(t, app, "enter")
	click(t, app, region(t, app, hitDetailField, withKey("s")).r)
	screen := screenLines(app)
	var target hitRegion
	for _, reg := range app.hits.all(hitModalItem) {
		if strings.Contains(cellText(screen, reg.r), "completed") {
			target = reg
		}
	}
	if target.kind == 0 {
		t.Fatalf("no picker item shows completed:\n%s", strings.Join(screen, "\n"))
	}
	click(t, app, target.r)
	if app.state != viewList {
		t.Errorf("state %v after choosing a status", app.state)
	}
	if b, _ := app.core.Get("demo-t002"); b.Status != "completed" {
		t.Errorf("status = %s, want completed", b.Status)
	}
}

func TestMousePickerItemsMatchScreen(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		app := newDemoApp(t, size[0], size[1])
		revealBean(t, app, "demo-t002")
		pressKey(t, app, "s")
		screen := screenLines(app)
		items := app.hits.all(hitModalItem)
		if len(items) == 0 {
			t.Fatalf("%v: no picker items recorded", size)
		}
		for _, reg := range items {
			want := app.statusPicker.list.VisibleItems()[reg.index].(statusItem).name
			if got := cellText(screen, reg.r); !strings.Contains(got, want) {
				t.Errorf("%v: item %d region shows %q, want %s", size, reg.index, got, want)
			}
		}
	}
}

func TestMouseRelationJumps(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		app := newDemoApp(t, size[0], size[1])
		revealBean(t, app, "demo-t001")
		pressKey(t, app, "enter")
		var target string
		app.View()
		for _, reg := range app.hits.all(hitRelation) {
			if l := app.detail.links[reg.index]; l.bean.ID == "demo-t003" {
				target = l.bean.ID
				click(t, app, reg.r)
				break
			}
		}
		if target == "" {
			t.Fatalf("%v: no relation to demo-t003", size)
		}
		if b := app.detail.bean; b == nil || b.ID != target || app.focus != focusDetail {
			t.Errorf("%v: detail shows %v (focus %v), want %s", size, b, app.focus, target)
		}
		if b := app.list.currentBean(); b == nil || b.ID != target {
			t.Errorf("%v: tree cursor on %v", size, b)
		}
	}

	// From the tree (wide): a click on a relation opens it directly.
	app := newDemoApp(t, 160, 40)
	revealBean(t, app, "demo-t001")
	reg := region(t, app, hitRelation, func(r hitRegion) bool { return app.detail.links[r.index].bean.ID == "demo-e001" })
	click(t, app, reg.r)
	if b := app.detail.bean; b == nil || b.ID != "demo-e001" || app.focus != focusDetail {
		t.Errorf("detail shows %v (focus %v), want demo-e001", b, app.focus)
	}
}

func TestMouseBodyCheckbox(t *testing.T) {
	withStableRendering(t)
	for _, size := range mouseSizes {
		for _, focused := range []bool{false, true} {
			if !focused && size[0] < wideMinWidth {
				continue // the body is only shown with the focus
			}
			app := newDemoApp(t, size[0], size[1])
			revealBean(t, app, "demo-t001")
			if focused {
				pressKeys(t, app, "enter", "tab") // the body, cursor on the first checkbox
			}
			// Scroll the body (short terminals) until the checkbox is shown.
			var target hitRegion
			for range 5 {
				screen := screenLines(app)
				for _, reg := range app.hits.all(hitBodyLine) {
					if strings.Contains(cellText(screen, reg.r), "Recovery codes") {
						target = reg
					}
				}
				if target.kind != 0 {
					break
				}
				wheel(t, app, region(t, app, hitBody, nil).r, true)
			}
			if target.kind == 0 {
				t.Fatalf("%v: no body line shows the second checkbox", size)
			}
			click(t, app, target.r)
			b, _ := app.core.Get("demo-t001")
			if !strings.Contains(b.Body, "- [ ] TOTP support") || !strings.Contains(b.Body, "- [x] Recovery codes") {
				t.Errorf("%v focused=%v: body after click:\n%s", size, focused, b.Body)
			}
			if app.focus != focusDetail || app.detail.section != sectionBody || app.detail.checkCursor != 1 {
				t.Errorf("%v: focus %v section %v checkbox %d", size, app.focus, app.detail.section, app.detail.checkCursor)
			}

			// A line without a checkbox only focuses the body.
			for range 5 {
				wheel(t, app, region(t, app, hitBody, nil).r, false)
			}
			reg := region(t, app, hitBodyLine, func(r hitRegion) bool {
				return !strings.Contains(stripAnsi(app.detail.body[r.index]), "[")
			})
			click(t, app, reg.r)
			if b2, _ := app.core.Get("demo-t001"); b2.Body != b.Body {
				t.Errorf("%v: clicking a text line changed the body", size)
			}
		}
	}
}

func TestMouseWheelScrollsBody(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	long := strings.Repeat("A line of the body.\n\n", 40)
	if _, err := app.resolver.UpdateBean(context.Background(), "demo-t008", model.UpdateBeanInput{Body: &long}); err != nil {
		t.Fatal(err)
	}
	runCmd(t, app, app.list.loadBeans)
	revealBean(t, app, "demo-t008")
	pane := region(t, app, hitDetailPane, nil).r
	wheel(t, app, pane, true)
	if app.detail.bodyOffset != wheelBodyLines {
		t.Errorf("body offset = %d, want %d", app.detail.bodyOffset, wheelBodyLines)
	}
	wheel(t, app, pane, false)
	wheel(t, app, pane, false)
	if app.detail.bodyOffset != 0 {
		t.Errorf("body offset = %d after scrolling back", app.detail.bodyOffset)
	}
	if app.focus != focusTree {
		t.Errorf("the wheel moved the focus to %v", app.focus)
	}
}

func TestMouseFilterBar(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 160, 40)
	click(t, app, region(t, app, hitBarField, withIndex(int(barStatus))).r)
	if app.focus != focusFilter || !app.bar.open || app.bar.field != barStatus {
		t.Fatalf("focus %v open %v field %v", app.focus, app.bar.open, app.bar.field)
	}
	screen := screenLines(app)
	pick := func(label string) hitRegion {
		t.Helper()
		screen = screenLines(app)
		for _, reg := range app.hits.all(hitDropdownItem) {
			if strings.Contains(cellText(screen, reg.r), label) {
				return reg
			}
		}
		t.Fatalf("no dropdown item %q:\n%s", label, strings.Join(screen, "\n"))
		return hitRegion{}
	}
	click(t, app, pick("todo").r)
	click(t, app, pick("draft").r)
	if got := app.list.query(); got != "status:todo,draft" && got != "status:draft,todo" {
		t.Errorf("query = %q", got)
	}
	if !app.bar.open {
		t.Error("toggling an option closed the dropdown")
	}
	click(t, app, pick("draft").r)
	if got := app.list.query(); got != "status:todo" {
		t.Errorf("query after untoggling = %q", got)
	}

	// A click outside closes the dropdown, keeping the filter.
	click(t, app, rect{x: 0, y: 0})
	if app.bar.open || app.list.query() != "status:todo" {
		t.Errorf("open %v query %q after clicking outside", app.bar.open, app.list.query())
	}

	// Another field opens its dropdown; the same field closes it.
	click(t, app, region(t, app, hitBarField, withIndex(int(barType))).r)
	if !app.bar.open || app.bar.field != barType {
		t.Errorf("type dropdown not open")
	}
	click(t, app, region(t, app, hitBarField, withIndex(int(barType))).r)
	if app.bar.open {
		t.Errorf("clicking the open field did not close its dropdown")
	}

	// Archived is a single choice that closes the dropdown.
	click(t, app, region(t, app, hitBarField, withIndex(int(barArchived))).r)
	click(t, app, pick("hidden").r)
	if app.bar.open || !strings.Contains(app.list.query(), "archived:no") {
		t.Errorf("open %v query %q after choosing archived hidden", app.bar.open, app.list.query())
	}

	// Toggles flip, clear clears.
	click(t, app, region(t, app, hitBarField, withIndex(int(barDone))).r)
	if !strings.Contains(app.list.query(), "done:no") {
		t.Errorf("query %q after clicking Done", app.list.query())
	}
	click(t, app, region(t, app, hitBarField, withIndex(int(barClear))).r)
	if app.list.query() != "" {
		t.Errorf("query %q after clicking clear", app.list.query())
	}

	// A click on the tree returns the focus to it.
	clickRow(t, app, "demo-t002")
	if app.focus != focusTree {
		t.Errorf("focus %v after clicking the tree", app.focus)
	}
}

func TestMouseFilterLineAndDropdownWheel(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 70, 12)
	click(t, app, region(t, app, hitBarLine, nil).r)
	if app.focus != focusFilter {
		t.Fatalf("focus %v after clicking the filter line", app.focus)
	}
	screen := screenLines(app)
	reg := region(t, app, hitBarField, withIndex(int(barType)))
	if got := cellText(screen, reg.r); !strings.Contains(got, "Type") {
		t.Errorf("type field region shows %q", got)
	}
	click(t, app, reg.r)
	if !app.bar.open || app.bar.field != barType {
		t.Fatalf("open %v field %v", app.bar.open, app.bar.field)
	}
	rows := app.dropdownRows()
	n := len(app.barOptions(barType, app.list.filter(), ""))
	if rows >= n {
		t.Fatalf("dropdown shows all %d options; the test needs a scrolling one", n)
	}
	dd := region(t, app, hitDropdown, nil).r
	wheel(t, app, dd, true)
	if app.bar.offset != 1 {
		t.Errorf("dropdown offset = %d after wheel down", app.bar.offset)
	}
	screen = screenLines(app)
	items := app.hits.all(hitDropdownItem)
	opts := app.barOptions(barType, app.list.filter(), "")
	for _, it := range items {
		if got := cellText(screen, it.r); !strings.Contains(got, opts[it.index].label) {
			t.Errorf("dropdown item %d shows %q, want %s", it.index, got, opts[it.index].label)
		}
	}
	if items[0].index != 1 {
		t.Errorf("first shown item = %d", items[0].index)
	}
}

func TestMouseOverlays(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)

	pressKey(t, app, "?")
	modal := region(t, app, hitModal, nil).r
	click(t, app, rect{x: modal.x + 2, y: modal.y + 2})
	if app.state != viewHelpOverlay {
		t.Error("clicking inside the help closed it")
	}
	click(t, app, rect{x: 0, y: 0})
	if app.state != viewList {
		t.Errorf("state %v after clicking outside the help", app.state)
	}

	pressKey(t, app, "t")
	if app.state != viewTypePicker {
		t.Fatalf("state %v", app.state)
	}
	before := app.typePicker.list.Index()
	wheel(t, app, region(t, app, hitModal, nil).r, true)
	if app.typePicker.list.Index() != before+1 {
		t.Errorf("wheel moved the picker cursor from %d to %d", before, app.typePicker.list.Index())
	}
	click(t, app, rect{x: 0, y: 0})
	if app.state != viewList {
		t.Errorf("state %v after clicking outside the picker", app.state)
	}
	b := app.list.currentBean()
	if stored, _ := app.core.Get(b.ID); stored.Type != b.Type {
		t.Error("closing the picker changed the type")
	}
}

func TestMouseBlockingPickerToggles(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	revealBean(t, app, "demo-t002")
	pressKey(t, app, "b")
	screen := screenLines(app)
	var target hitRegion
	for _, reg := range app.hits.all(hitModalItem) {
		if strings.Contains(cellText(screen, reg.r), "Child of scrapped") {
			target = reg
		}
	}
	if target.kind == 0 {
		t.Fatalf("no item shows demo-t007:\n%s", strings.Join(screen, "\n"))
	}
	click(t, app, target.r)
	if app.state != viewBlockingPicker {
		t.Fatalf("clicking an item closed the blocking picker")
	}
	pressKey(t, app, "enter")
	if b, _ := app.core.Get("demo-t002"); len(b.Blocking) != 1 || b.Blocking[0] != "demo-t007" {
		t.Errorf("blocking = %v, want [demo-t007]", b.Blocking)
	}
}

func TestMouseWorkspacesOverlay(t *testing.T) {
	withStableRendering(t)
	r := newWSRepo(t)
	r.startInWorkspace(t, "ws-bbbb", "other-ws")
	if _, err := r.mgr.Create("empty"); err != nil {
		t.Fatal(err)
	}
	app := r.app(t, nil)
	pressKey(t, app, "g")
	_, cmd := app.Update(keyRunes("w"))
	drainSlow(t, app, cmd)
	if app.state != viewWorkspaces {
		t.Fatalf("state = %v", app.state)
	}
	other := workspaceIndex(t, app, "other-ws")
	screen := screenLines(app)
	reg := region(t, app, hitModalItem, withIndex(other))
	if got := cellText(screen, reg.r); !strings.Contains(got, "other-ws") {
		t.Errorf("workspace item region shows %q", got)
	}
	click(t, app, reg.r)
	if app.state != viewList {
		t.Fatalf("state %v after clicking a workspace", app.state)
	}
	if b := app.list.currentBean(); b == nil || b.ID != "ws-bbbb" {
		t.Errorf("cursor on %v, want ws-bbbb", b)
	}

	pressKey(t, app, "g")
	pressKey(t, app, "w")
	click(t, app, rect{x: 0, y: 0})
	if app.state != viewList {
		t.Errorf("clicking outside did not close the overlay")
	}
}

func TestMouseIgnoredWhileTyping(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, "c")
	if app.list.inline == nil {
		t.Fatal("no inline input")
	}
	clickRow(t, app, "demo-t008")
	if app.list.inline == nil {
		t.Error("a click cancelled the inline create input")
	}
}

func TestMouseEnabled(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", true}, {"1", true}, {"0", false}, {"false", false}, {"off", false}, {"yes", true}} {
		t.Setenv("BEANS_TUI_MOUSE", tc.value)
		if got := mouseEnabled(); got != tc.want {
			t.Errorf("BEANS_TUI_MOUSE=%q: mouseEnabled() = %v", tc.value, got)
		}
		if got := len(programOptions(mouseEnabled())); got != map[bool]int{true: 2, false: 1}[tc.want] {
			t.Errorf("BEANS_TUI_MOUSE=%q: %d program options", tc.value, got)
		}
	}
}

func TestMouseRestoredAfterEditor(t *testing.T) {
	app := newDemoApp(t, 100, 30)
	if cmd := app.restoreMouse(); cmd != nil {
		t.Error("restoreMouse enables the mouse although it is off")
	}
	app.mouseOn = true
	var found bool
	for _, msg := range runCmd(t, app, app.update(editorFinishedMsg{})) {
		if fmt.Sprintf("%T", msg) == fmt.Sprintf("%T", tea.EnableMouseCellMotion()) {
			found = true
		}
	}
	if !found {
		t.Error("closing the editor does not re-enable the mouse")
	}
}

func TestMouseViewRows(t *testing.T) {
	stubMouseClock(t)
	app := newViewsApp(t, 160, 40)
	pressKey(t, app, "2") // Ready
	l := app.activeList()
	if l == nil || len(l.rows) < 2 {
		t.Fatalf("ready view has no rows")
	}
	target := -1
	for i := range l.rows {
		if i != l.cursor && l.selectable(i) {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no second selectable row")
	}
	reg := region(t, app, hitViewRow, withIndex(target))
	click(t, app, reg.r)
	if l.cursor != target || app.focus != focusTree {
		t.Errorf("click: cursor %d (want %d), focus %v", l.cursor, target, app.focus)
	}
	// A second click on the same row (within the double-click interval)
	// opens the detail pane.
	click(t, app, region(t, app, hitViewRow, withIndex(target)).r)
	if app.focus != focusDetail {
		t.Errorf("double click: focus %v, want the detail pane", app.focus)
	}
}

func TestMouseClickOutsidePaletteCloses(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKey(t, app, ":")
	if app.state != viewCommandPalette {
		t.Fatal("palette not open")
	}
	mouse(t, app, 0, 0, tea.MouseButtonLeft)
	if app.state != viewList {
		t.Errorf("state = %v after clicking outside the palette", app.state)
	}
}

// TestMouseDetailScrolledPane checks that clicks in a detail pane that scrolls
// as a whole (small terminals) hit what is shown.
func TestMouseDetailScrolledPane(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 60, 18)
	revealBean(t, app, "demo-t002")
	pressKey(t, app, "enter")
	if !app.detail.layout().scroll {
		t.Skip("detail pane doesn't scroll at this size")
	}
	app.detail.scrollBy(100) // to the bottom
	screen := screenLines(app)
	for _, reg := range app.hits.all(hitDetailField) {
		text := cellText(screen, reg.r)
		if strings.TrimSpace(text) == "" {
			t.Errorf("field region %+v (key %q) is blank on screen", reg.r, reg.key)
		}
	}
}
