package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// newViewsApp creates the demo app with a fixed audit clock (making the two
// in-progress demo beans stale) and a short audit debounce.
func newViewsApp(t *testing.T, width, height int) *App {
	t.Helper()
	prevClock, prevDebounce := auditClock, auditDebounce
	auditClock = func() time.Time { return time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC) }
	auditDebounce = time.Millisecond
	t.Cleanup(func() { auditClock, auditDebounce = prevClock, prevDebounce })
	return newDemoApp(t, width, height)
}

// rowBeans returns the bean IDs of the active view's selectable rows, with
// "↳" prefixed to rows that jump elsewhere.
func rowBeans(app *App) []string {
	var ids []string
	for _, r := range app.activeList().rows {
		switch {
		case r.beanID == "":
		case r.jumpID != "":
			ids = append(ids, "↳"+r.beanID)
		default:
			ids = append(ids, r.beanID)
		}
	}
	return ids
}

// viewText renders the active view's list rows as plain text.
func viewText(app *App) string {
	l := app.activeList()
	var lines []string
	for i, r := range l.rows {
		lines = append(lines, stripAnsi(r.render(120, i == l.cursor)))
	}
	return strings.Join(lines, "\n")
}

// selectRow moves the active view's cursor to the n-th row (0-based) whose
// bean is id.
func selectRow(t *testing.T, app *App, id string, n int) {
	t.Helper()
	l := app.activeList()
	for i, r := range l.rows {
		if r.beanID == id {
			if n == 0 {
				l.cursor = i
				app.syncLayout()
				return
			}
			n--
		}
	}
	t.Fatalf("no row for %s: %v", id, rowBeans(app))
}

func TestViewGolden(t *testing.T) {
	withStableRendering(t)
	cases := []struct {
		name string
		keys []string
	}{
		{"tree", []string{"1"}},
		{"ready", []string{"2"}},
		{"blocked", []string{"3"}},
		{"audit", []string{"4", "j"}},
	}
	for _, tc := range cases {
		for _, size := range [][2]int{{160, 40}, {80, 24}} {
			name := fmt.Sprintf("view-%s-%dx%d", tc.name, size[0], size[1])
			t.Run(name, func(t *testing.T) {
				app := newViewsApp(t, size[0], size[1])
				for _, k := range tc.keys {
					pressKey(t, app, k)
				}
				screen := stripAnsi(app.View())
				checkFrame(t, screen, size[0], size[1])
				checkGolden(t, name, screen)
			})
		}
	}
}

// checkGolden compares screen with testdata/<name>.golden (-update writes it).
func checkGolden(t *testing.T, name, screen string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(screen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got := screen + "\n"; got != string(want) {
		t.Errorf("screen differs from %s (run with -update to accept):\n%s", path, got)
	}
}

func TestViewFramesAtManySizes(t *testing.T) {
	withStableRendering(t)
	for _, key := range []string{"2", "3", "4"} {
		for _, size := range [][2]int{{200, 60}, {120, 35}, {100, 30}, {99, 27}, {70, 24}, {50, 16}, {40, 12}} { // below 40x12 the "too small" screen has no frame
			app := newViewsApp(t, size[0], size[1])
			pressKey(t, app, key)
			pressKey(t, app, "j")
			checkFrame(t, stripAnsi(app.View()), size[0], size[1])
			pressKey(t, app, "enter")
			checkFrame(t, stripAnsi(app.View()), size[0], size[1])
		}
	}
}

func TestViewSwitching(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)
	header := func() string { return stripAnsi(app.headerLine(156)) }
	if h := header(); !strings.Contains(h, "beans: demo  [1 Tree] 2 Ready 3 Blocked 4 Audit") {
		t.Fatalf("header = %q", h)
	}
	for i, name := range viewNames {
		pressKey(t, app, strconv.Itoa(i+1))
		if app.views.active != appView(i) {
			t.Errorf("key %d: active view = %v", i+1, app.views.active)
		}
		if h := header(); !strings.Contains(h, "["+strconv.Itoa(i+1)+" "+name+"]") {
			t.Errorf("key %d: header = %q", i+1, h)
		}
	}

	// Compact tabs on narrower terminals, none when there is no room.
	if h := stripAnsi(app.headerLine(50)); !strings.Contains(h, "1 2 3 [4 Audit]") {
		t.Errorf("compact header = %q", h)
	}
	if h := stripAnsi(app.headerLine(36)); strings.Contains(h, "[4") {
		t.Errorf("tiny header should drop the tabs: %q", h)
	}

	// Each view keeps its cursor for the session; switching from the detail
	// pane returns the focus to the list.
	pressKey(t, app, "2")
	pressKey(t, app, "j")
	pressKey(t, app, "j")
	want := app.currentBean().ID
	pressKey(t, app, "enter")
	if app.focus != focusDetail || app.detail.bean.ID != want {
		t.Fatalf("enter should focus the details of %s, focus %v bean %v", want, app.focus, app.detail.bean)
	}
	pressKey(t, app, "1")
	if app.focus != focusTree || app.views.active != viewTree {
		t.Errorf("1 from the details: focus %v view %v", app.focus, app.views.active)
	}
	if app.detail.bean.ID != app.list.currentBean().ID {
		t.Errorf("tree view should show the tree's bean, got %s", app.detail.bean.ID)
	}
	pressKey(t, app, "2")
	if got := app.currentBean().ID; got != want {
		t.Errorf("Ready view cursor = %s, want %s (kept for the session)", got, want)
	}

	// Digits typed into the query are text, not view switches.
	pressKey(t, app, "/")
	pressKey(t, app, "3")
	if app.views.active != viewReady || !strings.Contains(app.list.search.Value(), "3") {
		t.Errorf("typing 3 in the query: view %v, query %q", app.views.active, app.list.search.Value())
	}
}

func TestReadyView(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)
	pressKey(t, app, "2")

	// Ranked like `beans next`: critical first, then leaves before
	// containers, then by creation date and ID. Blocked beans (t003) and
	// beans below a scrapped milestone (t007) are not ready.
	want := []string{"demo-t005", "demo-t002", "demo-t008", "demo-e002", "demo-m001"}
	if got := rowBeans(app); !reflect.DeepEqual(got, want) {
		t.Fatalf("ready rows = %v, want %v", got, want)
	}
	next, err := app.resolver.NextBean(context.Background())
	if err != nil || next.ID != want[0] {
		t.Fatalf("NextBean = %v, %v; the top row must be what n jumps to", next, err)
	}
	if app.detail.bean == nil || app.detail.bean.ID != "demo-t005" {
		t.Errorf("detail pane shows %v, want the top row", app.detail.bean)
	}
	text := viewText(app)
	for _, w := range []string{"1  ‼ critical  task       t005 Export as CSV  Release 1.0 › Export", "5    normal    milestone  m001 Release 1.0"} {
		if !strings.Contains(text, w) {
			t.Errorf("ready rows lack %q:\n%s", w, text)
		}
	}
	if got := app.activeList().title; got != "Ready 5" {
		t.Errorf("title = %q", got)
	}

	// n selects the top-ranked bean.
	pressKey(t, app, "G")
	pressKey(t, app, "n")
	if got := app.currentBean().ID; got != "demo-t005" {
		t.Errorf("n selected %s", got)
	}

	// Actions work on the selected bean.
	pressKey(t, app, "j")
	pressKey(t, app, "s")
	if app.state != viewStatusPicker || !reflect.DeepEqual(app.statusPicker.beanIDs, []string{"demo-t002"}) {
		t.Fatalf("s should open the status picker for t002: state %v ids %v", app.state, app.statusPicker.beanIDs)
	}
	pressKey(t, app, "esc")
	if b := app.workspaceKeyBean(); b == nil || b.ID != "demo-t002" {
		t.Errorf("w would act on %v", b)
	}
	// Tree-only actions explain themselves and change nothing.
	before := len(app.list.beans)
	pressKey(t, app, "c")
	if app.list.inline != nil || len(app.list.beans) != before || !strings.Contains(app.list.statusMessage, "tree (1)") {
		t.Errorf("c in the Ready view: inline %v, message %q", app.list.inline, app.list.statusMessage)
	}

	// Changing a bean updates the ranking: starting t005 makes it not ready.
	status := "in-progress"
	if _, err := app.resolver.UpdateBean(context.Background(), "demo-t005", model.UpdateBeanInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	app.reload()
	app.syncLayout()
	if got := rowBeans(app); !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("after starting t005: %v", got)
	}
}

func TestReadyViewFilter(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)
	pressKey(t, app, "2")

	setQuery(app, "type:epic,milestone")
	if got := rowBeans(app); !reflect.DeepEqual(got, []string{"demo-e002", "demo-m001"}) {
		t.Errorf("filtered rows = %v", got)
	}
	// Ranks stay those of the full ranking.
	if text := viewText(app); !strings.Contains(text, "4    normal    epic") || !strings.Contains(text, "5    normal    milestone") {
		t.Errorf("filtered rows should keep their ranks:\n%s", text)
	}
	if got := app.activeList().title; got != "Ready 2/5 · type:epic,milestone" {
		t.Errorf("title = %q", got)
	}
	// n reports a next bean hidden by the filter.
	pressKey(t, app, "n")
	if !strings.Contains(app.list.statusMessage, "hidden by the filters") {
		t.Errorf("n with t005 filtered out: %q", app.list.statusMessage)
	}

	// esc clears the filter; the query typed with / filters live, and ↑↓
	// move the view's cursor meanwhile.
	pressKey(t, app, "esc")
	if app.list.query() != "" || len(rowBeans(app)) != 5 {
		t.Fatalf("esc should clear the filter: %q %v", app.list.query(), rowBeans(app))
	}
	pressKey(t, app, "/")
	for _, k := range "csv" {
		pressKey(t, app, string(k))
	}
	if got := rowBeans(app); !reflect.DeepEqual(got, []string{"demo-t005"}) {
		t.Errorf("query csv: %v", got)
	}
	pressKey(t, app, "enter")
	if got := app.activeList().title; got != "Ready 1/5 · csv" {
		t.Errorf("title = %q", got)
	}
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "/ csv") {
		t.Errorf("the query line should show in the Ready view:\n%s", screen)
	}

	// The filter bar edits the same query.
	setQuery(app, "")
	pressKey(t, app, "H") // done:no — no effect on ready beans
	if !app.list.doneHidden() || len(rowBeans(app)) != 5 {
		t.Errorf("H in the Ready view: hidden %v rows %v", app.list.doneHidden(), rowBeans(app))
	}
}

func TestBlockedView(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)
	// A child of the blocked t003 is blocked implicitly; t002 blocks t008
	// via a blocking link on t002, and t001 blocks t002. Rows are sorted
	// like the tree (t003 has a low priority).
	ctx := context.Background()
	if err := app.core.Create(&bean.Bean{ID: "demo-t009", Title: "Reset mail template", Status: "todo", Type: "task", Parent: "demo-t003"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolver.AddBlocking(ctx, "demo-t002", "demo-t008", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolver.AddBlocking(ctx, "demo-t001", "demo-t002", nil); err != nil {
		t.Fatal(err)
	}
	app.reload()
	pressKey(t, app, "3")

	want := []string{
		"demo-t002", "↳demo-t001",
		"demo-t008", "↳demo-t002",
		"demo-t009", "↳demo-t001",
		"demo-t003", "↳demo-t001",
	}
	if got := rowBeans(app); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocked rows = %v, want %v", got, want)
	}
	text := viewText(app)
	for _, w := range []string{
		"T T t003 Password reset", "· 1 blocker",
		"↳ blocked by I F t001 Two-factor authentication for the login form · in-progress",
		"T T t009 Reset mail template  · implicitly, via t003",
		"↳ via t003: I F t001",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("blocked rows lack %q:\n%s", w, text)
		}
	}

	// A blocker row shows the blocker; enter jumps to its own row if it is
	// blocked too...
	selectRow(t, app, "demo-t002", 1) // t002 blocking t008
	if app.detail.bean.ID != "demo-t002" {
		t.Errorf("blocker row shows %s", app.detail.bean.ID)
	}
	pressKey(t, app, "enter")
	if app.views.active != viewBlocked || app.activeList().cursor != 0 || app.focus != focusTree {
		t.Errorf("enter on blocker t002: view %v cursor %d focus %v", app.views.active, app.activeList().cursor, app.focus)
	}
	// ...or to the tree otherwise.
	pressKey(t, app, "j")
	pressKey(t, app, "enter")
	if app.views.active != viewTree || app.list.currentBean().ID != "demo-t001" {
		t.Errorf("enter on blocker t001: view %v bean %v", app.views.active, app.list.currentBean())
	}
	if !strings.Contains(app.list.statusMessage, "3 back to Blocked") {
		t.Errorf("status = %q", app.list.statusMessage)
	}

	// The filter narrows the blocked beans (not their blockers).
	pressKey(t, app, "3")
	setQuery(app, "priority:low")
	if got := rowBeans(app); !reflect.DeepEqual(got, []string{"demo-t003", "↳demo-t001"}) {
		t.Errorf("filtered blocked rows = %v", got)
	}
	setQuery(app, "type:bug")
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "No blocked beans match") {
		t.Errorf("empty filtered view:\n%s", screen)
	}

	// Completing the blocker unblocks the beans.
	setQuery(app, "")
	status := "completed"
	if _, err := app.resolver.UpdateBean(ctx, "demo-t001", model.UpdateBeanInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	app.reload()
	app.syncLayout()
	if got := rowBeans(app); !reflect.DeepEqual(got, []string{"demo-t008", "↳demo-t002"}) {
		t.Errorf("after completing t001: %v", got)
	}
}

func TestAuditView(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)

	// Opening the view starts the audit in the background: Update returns
	// at once, with the run as a command.
	_, cmd := app.Update(keyMsg("4"))
	if cmd == nil || !app.views.auditRun.running {
		t.Fatal("opening the audit view should start a run")
	}
	if !strings.Contains(app.activeList().title, "running…") || !strings.Contains(viewText(app), "Running the audit…") {
		t.Errorf("while running: %q\n%s", app.activeList().title, viewText(app))
	}
	runCmd(t, app, cmd)
	if app.views.auditRun.running || app.views.auditRun.report == nil {
		t.Fatal("the run should have finished")
	}

	text := viewText(app)
	for _, w := range []string{
		"Audited 12:00:00",
		"! landed-but-open skipped: not a git repository",
		"Completed/scrapped beans with open children (1) [container-done-children-open]",
		"✗ S M m002 Scrapped milestone — scrapped with 1 child open",
		"↳ open child T T t007 Child of scrapped · todo",
		"Stale in-progress beans (2) [stale-in-progress]",
		"✗ I E e001 Login overhaul — in-progress, no updates for 29 days",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("audit rows lack %q:\n%s", w, text)
		}
	}
	if got := rowBeans(app); !reflect.DeepEqual(got, []string{"demo-m002", "demo-t007", "demo-e001", "demo-t001"}) {
		t.Errorf("audit rows = %v", got)
	}
	if got := app.activeList().title; got != "Audit · 3 findings" {
		t.Errorf("title = %q", got)
	}

	// Rows select their beans for the detail pane.
	if app.detail.bean == nil || app.detail.bean.ID != "demo-m002" {
		t.Errorf("first finding shows %v", app.detail.bean)
	}
	pressKey(t, app, "j")
	if app.detail.bean.ID != "demo-t007" {
		t.Errorf("sub-row shows %v", app.detail.bean.ID)
	}

	// Filters don't apply to the audit.
	setQuery(app, "type:bug")
	if got := rowBeans(app); len(got) != 4 || !strings.Contains(app.activeList().title, "filters not applied") {
		t.Errorf("audit with a filter: %v %q", got, app.activeList().title)
	}
	pressKey(t, app, "/")
	if app.list.searching || !strings.Contains(app.list.statusMessage, "don't apply") {
		t.Errorf("/ in the audit view: searching %v, message %q", app.list.searching, app.list.statusMessage)
	}
	setQuery(app, "")

	// Fixing the findings (debounced re-run on bean changes) leads to the
	// clean state.
	ctx := context.Background()
	scrapped, todo := "scrapped", "todo"
	for id, status := range map[string]*string{"demo-t007": &scrapped, "demo-e001": &todo, "demo-t001": &todo} {
		if _, err := app.resolver.UpdateBean(ctx, id, model.UpdateBeanInput{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	_, cmd = app.Update(beansChangedMsg{})
	msgs := runCmd(t, app, cmd)
	ticks := 0
	for _, m := range msgs {
		if _, ok := m.(auditTickMsg); ok {
			ticks++
		}
	}
	if ticks != 1 {
		t.Errorf("a bean change should re-run the audit after one debounce tick, got %d ticks", ticks)
	}
	text = viewText(app)
	if !strings.Contains(text, "✓ No audit findings") || app.activeList().title != "Audit · 0 findings" {
		t.Errorf("clean state: %q\n%s", app.activeList().title, text)
	}
	if app.detail.bean != nil {
		t.Errorf("clean audit selects %v", app.detail.bean.ID)
	}

	// Changes while another view is active re-run the audit when it is
	// opened again, without waiting for the debounce.
	pressKey(t, app, "1")
	if _, err := app.resolver.UpdateBean(ctx, "demo-t001", model.UpdateBeanInput{Status: ptr("in-progress")}); err != nil {
		t.Fatal(err)
	}
	app.reload()
	if cmd := app.viewsCmd(); cmd != nil {
		t.Error("the audit should not run while its view is hidden")
	}
	_, cmd = app.Update(keyMsg("4"))
	if !app.views.auditRun.running {
		t.Error("reopening after changes should re-run at once")
	}
	runCmd(t, app, cmd)

	// R re-runs on demand.
	_, cmd = app.Update(keyMsg("R"))
	if !app.views.auditRun.running || cmd == nil {
		t.Error("R should re-run the audit")
	}
	runCmd(t, app, cmd)
}

// TestAuditRunsOffTheUIGoroutine runs the audit command concurrently with
// bean mutations and rendering on the UI goroutine (run with -race). The
// project is a git repository, so the run takes a while.
func TestAuditRunsOffTheUIGoroutine(t *testing.T) {
	app := newViewsApp(t, 120, 35)
	initGitRepo(t, filepath.Dir(app.core.Root()), "feat: error texts (Refs: demo-t002)")
	_, cmd := app.Update(keyMsg("4"))
	if cmd == nil || app.views.auditRun.report != nil {
		t.Fatal("opening the view should start a run without waiting for it")
	}
	done := make(chan tea.Msg, 1)
	go func() {
		for _, m := range collectMsgs(cmd) {
			if d, ok := m.(auditDoneMsg); ok {
				done <- d
			}
		}
	}()
	ctx := context.Background()
	statuses := []string{"in-progress", "todo"}
	var msg tea.Msg
	for i := 0; msg == nil; i++ {
		title := "Renamed " + strconv.Itoa(i)
		if _, err := app.resolver.UpdateBean(ctx, "demo-t002", model.UpdateBeanInput{Title: &title, Status: &statuses[i%2]}); err != nil {
			t.Fatal(err)
		}
		_ = app.View()
		select {
		case msg = <-done:
		default:
		}
	}
	app.Update(msg)
	if app.views.auditRun.report == nil || app.views.auditRun.running {
		t.Fatal("the result should be applied")
	}
	// Bean changes make the result outdated: it runs again after the
	// debounce.
	app.reload()
	if !app.views.auditRun.dirty {
		t.Error("changes should outdate the result")
	}
	if cmd := app.viewsCmd(); cmd == nil || !app.views.auditRun.ticking {
		t.Error("changes should schedule a debounced run")
	}
}

// initGitRepo makes dir a git repository with the beans committed and an
// empty commit for each message.
func initGitRepo(t *testing.T, dir string, messages ...string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("add", ".beans")
	git("commit", "-q", "-m", "chore: add beans (demo-t002)") // bookkeeping: ignored
	for _, m := range messages {
		git("commit", "-q", "--allow-empty", "-m", m)
	}
}

// collectMsgs runs a command and its batched commands, without an App.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestAuditViewGitFindings(t *testing.T) {
	withStableRendering(t)
	app := newViewsApp(t, 160, 40)
	initGitRepo(t, filepath.Dir(app.core.Root()), "feat: error texts\n\nRefs: demo-t002")

	pressKey(t, app, "4")
	text := viewText(app)
	for _, w := range []string{
		"Open beans already referenced on the base branch (1) [landed-but-open]",
		"✗ T T t002 Check the error texts of the login mask — todo but referenced by",
		"feat: error texts",
		"2 commits on main (1 bean-only ignored)",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("audit rows lack %q:\n%s", w, text)
		}
	}
	if strings.Contains(text, "not a git repository") {
		t.Errorf("git check should run:\n%s", text)
	}
	// The commit row selects the finding's bean.
	selectRow(t, app, "demo-t002", 1)
	if app.detail.bean.ID != "demo-t002" {
		t.Errorf("commit row shows %s", app.detail.bean.ID)
	}
}

func TestAuditSnapshotIsolation(t *testing.T) {
	app := newViewsApp(t, 120, 35)
	_, cmd := app.Update(keyMsg("4"))
	// Changing a bean after the run started does not affect its result.
	if _, err := app.resolver.UpdateBean(context.Background(), "demo-m002", model.UpdateBeanInput{Status: ptr("todo")}); err != nil {
		t.Fatal(err)
	}
	var report *beancore.AuditReport
	for _, m := range collectMsgs(cmd) {
		if d, ok := m.(auditDoneMsg); ok {
			report = &d.report
		}
	}
	if report == nil || report.Count != 3 {
		t.Fatalf("report = %+v, want the 3 findings of the snapshot", report)
	}
}

func keyMsg(k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func ptr[T any](v T) *T { return &v }

func TestASCIIViewTabs(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	for _, key := range []string{"2", "3", "4"} {
		for _, size := range [][2]int{{160, 40}, {80, 24}} {
			app := newViewsApp(t, size[0], size[1])
			pressKey(t, app, key)
			pressKey(t, app, "j")
			for _, screen := range []string{stripAnsi(app.View()), func() string { pressKey(t, app, "enter"); return stripAnsi(app.View()) }()} {
				if bad, found := nonASCII(screen); found {
					t.Errorf("view %s at %dx%d renders non-ASCII %q", key, size[0], size[1], bad)
				}
			}
		}
	}
}
