package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

func TestSortBeans(t *testing.T) {
	// Define the expected order from DefaultStatuses, DefaultPriorities, and DefaultTypes
	statusNames := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
	priorityNames := []string{"critical", "high", "normal", "low", "deferred"}
	typeNames := []string{"milestone", "epic", "bug", "feature", "task"}

	t.Run("sorts by status order first", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "completed", Type: "task", Title: "A"},
			{ID: "2", Status: "draft", Type: "task", Title: "B"},
			{ID: "3", Status: "in-progress", Type: "task", Title: "C"},
			{ID: "4", Status: "todo", Type: "task", Title: "D"},
			{ID: "5", Status: "scrapped", Type: "task", Title: "E"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		expected := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
		for i, want := range expected {
			if beans[i].Status != want {
				t.Errorf("index %d: got status %q, want %q", i, beans[i].Status, want)
			}
		}
	})

	t.Run("sorts by priority within same status", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "task", Priority: "low", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Priority: "critical", Title: "B"},
			{ID: "3", Status: "todo", Type: "task", Priority: "high", Title: "C"},
			{ID: "4", Status: "todo", Type: "task", Priority: "", Title: "D"}, // empty = normal
			{ID: "5", Status: "todo", Type: "task", Priority: "deferred", Title: "E"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		// Order: critical, high, normal (empty), low, deferred
		expectedPriorities := []string{"critical", "high", "", "low", "deferred"}
		for i, want := range expectedPriorities {
			if beans[i].Priority != want {
				t.Errorf("index %d: got priority %q, want %q", i, beans[i].Priority, want)
			}
		}
	})

	t.Run("sorts by type order within same status and priority", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "task", Title: "A"},
			{ID: "2", Status: "todo", Type: "milestone", Title: "B"},
			{ID: "3", Status: "todo", Type: "bug", Title: "C"},
			{ID: "4", Status: "todo", Type: "epic", Title: "D"},
			{ID: "5", Status: "todo", Type: "feature", Title: "E"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		expected := []string{"milestone", "epic", "bug", "feature", "task"}
		for i, want := range expected {
			if beans[i].Type != want {
				t.Errorf("index %d: got type %q, want %q", i, beans[i].Type, want)
			}
		}
	})

	t.Run("sorts by title within same status, priority, and type", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "task", Title: "Zebra"},
			{ID: "2", Status: "todo", Type: "task", Title: "Apple"},
			{ID: "3", Status: "todo", Type: "task", Title: "Mango"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		expected := []string{"Apple", "Mango", "Zebra"}
		for i, want := range expected {
			if beans[i].Title != want {
				t.Errorf("index %d: got title %q, want %q", i, beans[i].Title, want)
			}
		}
	})

	t.Run("title sort is case-insensitive", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "task", Title: "zebra"},
			{ID: "2", Status: "todo", Type: "task", Title: "Apple"},
			{ID: "3", Status: "todo", Type: "task", Title: "MANGO"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		expected := []string{"Apple", "MANGO", "zebra"}
		for i, want := range expected {
			if beans[i].Title != want {
				t.Errorf("index %d: got title %q, want %q", i, beans[i].Title, want)
			}
		}
	})

	t.Run("combined sort order: status > priority > type > title", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "completed", Type: "bug", Title: "Z"},
			{ID: "2", Status: "todo", Type: "task", Priority: "low", Title: "A"},
			{ID: "3", Status: "todo", Type: "bug", Priority: "high", Title: "B"},
			{ID: "4", Status: "todo", Type: "bug", Priority: "high", Title: "A"},
			{ID: "5", Status: "draft", Type: "epic", Title: "X"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		// Expected order:
		// 1. draft/epic/X (ID: 5)
		// 2. todo/high/bug/A (ID: 4)
		// 3. todo/high/bug/B (ID: 3)
		// 4. todo/low/task/A (ID: 2)
		// 5. completed/bug/Z (ID: 1)
		expectedIDs := []string{"5", "4", "3", "2", "1"}
		for i, want := range expectedIDs {
			if beans[i].ID != want {
				t.Errorf("index %d: got ID %q, want %q (status=%s, priority=%s, type=%s, title=%s)",
					i, beans[i].ID, want, beans[i].Status, beans[i].Priority, beans[i].Type, beans[i].Title)
			}
		}
	})

	t.Run("unrecognized status sorts last", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "unknown", Type: "task", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Title: "B"},
			{ID: "3", Status: "draft", Type: "task", Title: "C"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		// unknown status should be last
		if beans[2].Status != "unknown" {
			t.Errorf("unrecognized status should be last, got %q at position 2", beans[2].Status)
		}
	})

	t.Run("unrecognized type sorts last within status", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "unknown", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Title: "B"},
			{ID: "3", Status: "todo", Type: "bug", Title: "C"},
		}

		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)

		// unknown type should be last within todo status
		if beans[2].Type != "unknown" {
			t.Errorf("unrecognized type should be last, got %q at position 2", beans[2].Type)
		}
	})

	t.Run("empty slice does not panic", func(t *testing.T) {
		beans := []*bean.Bean{}
		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)
		// No assertion needed, just checking it doesn't panic
	})

	t.Run("single bean does not panic", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "1", Status: "todo", Type: "task", Title: "A"},
		}
		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)
		if beans[0].ID != "1" {
			t.Error("single bean should remain unchanged")
		}
	})
}

func TestCompareBeansByStatusPriorityAndType(t *testing.T) {
	statusNames := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
	priorityNames := []string{"critical", "high", "normal", "low", "deferred"}
	typeNames := []string{"milestone", "epic", "bug", "feature", "task"}

	t.Run("compares by status first", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "draft", Type: "task", Title: "B"}

		// draft < todo, so b should come before a
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("draft bean should come before todo bean")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("draft bean should come before todo bean")
		}
	})

	t.Run("compares by priority within same status", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Priority: "low", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "task", Priority: "high", Title: "B"}

		// high < low, so b should come before a
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("high priority bean should come before low priority bean")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("high priority bean should come before low priority bean")
		}
	})

	t.Run("compares by type within same status and priority", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "bug", Title: "B"}

		// bug < task, so b should come before a
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("bug bean should come before task bean")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("bug bean should come before task bean")
		}
	})

	t.Run("compares by title within same status, priority, and type", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Title: "Zebra"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "task", Title: "Apple"}

		// Apple < Zebra, so b should come before a
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("Apple bean should come before Zebra bean")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("Apple bean should come before Zebra bean")
		}
	})

	t.Run("title comparison is case-insensitive", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Title: "zebra"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "task", Title: "APPLE"}

		// apple < zebra (case-insensitive), so b should come before a
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("APPLE bean should come before zebra bean (case-insensitive)")
		}
	})

	t.Run("empty priority treated as normal", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "task", Priority: "", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "task", Priority: "normal", Title: "B"}

		// Both should be equivalent in priority ordering
		// Since titles differ, A < B, so a should come before b
		if !compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("empty priority should be treated as normal")
		}
	})

	t.Run("unrecognized status sorts last", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "unknown", Type: "task", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "scrapped", Type: "task", Title: "B"}

		// scrapped is last known status, unknown should be after it
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("unknown status should sort after scrapped")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("scrapped should sort before unknown")
		}
	})

	t.Run("unrecognized type sorts last within status", func(t *testing.T) {
		a := &bean.Bean{ID: "1", Status: "todo", Type: "unknown", Title: "A"}
		b := &bean.Bean{ID: "2", Status: "todo", Type: "task", Title: "B"}

		// task is last known type, unknown should be after it
		if compareBeansByStatusPriorityAndType(a, b, statusNames, priorityNames, typeNames) {
			t.Error("unknown type should sort after task")
		}
		if !compareBeansByStatusPriorityAndType(b, a, statusNames, priorityNames, typeNames) {
			t.Error("task should sort before unknown")
		}
	})
}

// setupListTestModel creates a core in a temp dir with a mix of open, resolved,
// implicitly resolved and archived beans, and returns a sized list model.
func setupListTestModel(t *testing.T) (*App, *beancore.Core) {
	t.Helper()
	beansDir := filepath.Join(t.TempDir(), ".beans")
	if err := os.MkdirAll(beansDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	core := beancore.New(beansDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}

	for _, b := range []*bean.Bean{
		{ID: "t-open", Title: "Open bug", Status: "todo", Type: "bug", Tags: []string{"frontend"}},
		{ID: "t-wip", Title: "In progress task", Status: "in-progress", Type: "task", Tags: []string{"backend"}},
		{ID: "t-done", Title: "Done epic", Status: "completed", Type: "epic"},
		{ID: "t-child", Title: "Child of done", Status: "todo", Type: "task", Parent: "t-done"},
		{ID: "t-scrap", Title: "Scrapped feature", Status: "scrapped", Type: "feature", Tags: []string{"frontend"}},
		{ID: "t-arch", Title: "Archived bug", Status: "completed", Type: "bug"},
	} {
		b.Slug = bean.Slugify(b.Title)
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}
	if err := core.Archive("t-arch"); err != nil {
		t.Fatal(err)
	}

	app := New(core, cfg)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.list, _ = app.list.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return app, core
}

// runCmd executes a command and feeds the resulting message back into the app,
// following batches one level deep. Returns the produced messages.
func runCmd(t *testing.T, app *App, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	var msgs []tea.Msg
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	} else {
		msgs = append(msgs, msg)
	}
	for _, m := range msgs {
		_, next := app.Update(m)
		msgs = append(msgs, runCmd(t, app, next)...)
	}
	return msgs
}

// matchedIDs returns the IDs of list items that matched the filters (not context ancestors).
func matchedIDs(m listModel) []string {
	var ids []string
	for _, it := range m.list.Items() {
		if bi, ok := it.(beanItem); ok && bi.matched {
			ids = append(ids, bi.bean.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func pressKey(t *testing.T, app *App, key string) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	_, cmd := app.Update(msg)
	runCmd(t, app, cmd)
}

func TestListHideResolvedToggle(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	all := []string{"t-arch", "t-child", "t-done", "t-open", "t-scrap", "t-wip"}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, all) {
		t.Fatalf("default should show all beans, got %v", got)
	}
	if app.list.title() != "Beans" {
		t.Errorf("title = %q, want plain title by default", app.list.title())
	}

	pressKey(t, app, "H")
	// Completed, scrapped, archived-completed and the child of a completed epic are hidden.
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-open", "t-wip"}) {
		t.Errorf("hide resolved: got %v", got)
	}
	if got := app.list.title(); got != "Beans 2/6 · done hidden" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(app.list.Footer(), "show done") {
		t.Error("footer should offer to show done beans while hidden")
	}

	pressKey(t, app, "H")
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, all) {
		t.Errorf("toggle back should show all, got %v", got)
	}
}

func TestListFiltersSurviveReload(t *testing.T) {
	app, core := setupListTestModel(t)
	runCmd(t, app, app.Init())

	app.list.setFilters(listFilters{tags: []string{"frontend"}, hideResolved: true})
	runCmd(t, app, app.list.loadBeans)
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-open"}) {
		t.Fatalf("filtered: got %v", got)
	}

	// A file change triggers a reload; the filters must still apply.
	nb := &bean.Bean{ID: "t-new", Title: "New frontend bug", Slug: "new-frontend-bug", Status: "todo", Type: "bug", Tags: []string{"frontend"}}
	if err := core.Create(nb); err != nil {
		t.Fatal(err)
	}
	_, cmd := app.Update(beansChangedMsg{})
	runCmd(t, app, cmd)
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-new", "t-open"}) {
		t.Errorf("after reload: got %v", got)
	}
	if !app.list.hasActiveFilter() {
		t.Error("filters should still be active after reload")
	}
}

func TestListEscClearsFilters(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	app.list.setFilters(listFilters{types: []string{"bug"}, hideResolved: true})
	runCmd(t, app, app.list.loadBeans)
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-open"}) {
		t.Fatalf("filtered: got %v", got)
	}

	pressKey(t, app, "esc")
	if app.list.hasActiveFilter() {
		t.Error("esc should clear all filters")
	}
	if got := matchedIDs(app.list); len(got) != 6 {
		t.Errorf("after clear: got %v", got)
	}
}

func TestListTagChordReplacesTagFilter(t *testing.T) {
	app, _ := setupListTestModel(t)
	app.list.setFilters(listFilters{tags: []string{"a", "b"}, types: []string{"bug"}})
	_, cmd := app.Update(tagSelectedMsg{tag: "frontend"})
	runCmd(t, app, cmd)
	if !reflect.DeepEqual(app.list.filters, listFilters{tags: []string{"frontend"}, types: []string{"bug"}}) {
		t.Errorf("filters = %+v", app.list.filters)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-open"}) {
		t.Errorf("got %v", got)
	}
}

func TestListFilterModalFlow(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	pressKey(t, app, "F")
	if app.state != viewFilterPicker {
		t.Fatalf("F should open the filter modal, state = %v", app.state)
	}
	pressKey(t, app, "tab")
	pressKey(t, app, "tab")
	// q must not quit while typing a tag search
	if _, cmd := app.Update(keyRunes("q")); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q in the tag search must not quit")
		}
	}
	app.Update(keyType(tea.KeyBackspace))
	for _, k := range []string{"b", "a", "c", "k", " "} {
		pressKey(t, app, k)
	}
	pressKey(t, app, "enter")

	if app.state != viewList {
		t.Fatalf("enter should return to the list, state = %v", app.state)
	}
	if got := matchedIDs(app.list); !reflect.DeepEqual(got, []string{"t-wip"}) {
		t.Errorf("tag backend: got %v", got)
	}
	if got := app.list.title(); got != "Beans 1/6 · tag:backend" {
		t.Errorf("title = %q", got)
	}

	// Cancelling keeps the applied filters
	pressKey(t, app, "F")
	pressKey(t, app, "esc")
	if app.state != viewList || !reflect.DeepEqual(app.list.filters.tags, []string{"backend"}) {
		t.Errorf("cancel: state=%v filters=%+v", app.state, app.list.filters)
	}
}

func TestListMarksArchivedBeans(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())

	var archived []string
	for _, it := range app.list.list.Items() {
		if bi := it.(beanItem); bi.archived {
			archived = append(archived, bi.bean.ID)
		}
	}
	if !reflect.DeepEqual(archived, []string{"t-arch"}) {
		t.Errorf("archived items = %v, want [t-arch]", archived)
	}

	view := stripAnsi(app.list.View())
	for _, line := range strings.Split(view, "\n") {
		hasMarker := strings.Contains(line, strings.TrimSpace(archivedMarkerText))
		if strings.Contains(line, "t-arch") != hasMarker {
			t.Errorf("archived marker mismatch on line %q", line)
		}
	}
	if !strings.Contains(view, "t-arch") {
		t.Error("archived bean should be listed")
	}
}

func TestListRowsDoNotWrapOnNarrowTerminal(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())
	const width = 44
	app.list, _ = app.list.Update(tea.WindowSizeMsg{Width: width, Height: 30})

	view := stripAnsi(app.list.View())
	// Each bean, including the archived one and the child with an inherited
	// status annotation, must render on a single row that fits the width.
	for _, id := range []string{"t-arch", "t-child", "t-open"} {
		found := false
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, id) {
				found = true
				if w := lipgloss.Width(line); w > width {
					t.Errorf("row for %s is %d columns wide: %q", id, w, line)
				}
			}
		}
		if !found {
			t.Errorf("%s not listed", id)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "│ "))
		if strings.HasPrefix(trimmed, "↑") {
			t.Errorf("implicit status annotation wrapped onto its own line: %q", line)
		}
	}
}

// pressKeyQuits sends a key to the app and reports whether it (transitively)
// produced a tea.QuitMsg.
func pressKeyQuits(t *testing.T, app *App, msg tea.KeyMsg) bool {
	t.Helper()
	_, cmd := app.Update(msg)
	for _, m := range runCmd(t, app, cmd) {
		if _, ok := m.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func TestListEscDoesNotQuit(t *testing.T) {
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	t.Run("unfiltered list", func(t *testing.T) {
		app, _ := setupListTestModel(t)
		runCmd(t, app, app.Init())
		if pressKeyQuits(t, app, esc) {
			t.Fatal("esc on the unfiltered list must not quit")
		}
		if app.state != viewList {
			t.Errorf("state = %v, want list", app.state)
		}
	})

	t.Run("clears selection then filters, never quits", func(t *testing.T) {
		app, _ := setupListTestModel(t)
		runCmd(t, app, app.Init())
		app.list.setFilters(listFilters{types: []string{"bug"}})
		runCmd(t, app, app.list.loadBeans)
		app.list.selectedBeans["t-open"] = true

		if pressKeyQuits(t, app, esc) {
			t.Fatal("esc must not quit while beans are selected")
		}
		if len(app.list.selectedBeans) != 0 {
			t.Error("first esc should clear the selection")
		}
		if !app.list.hasActiveFilter() {
			t.Error("first esc should keep the filters")
		}

		if pressKeyQuits(t, app, esc) {
			t.Fatal("esc must not quit while filters are active")
		}
		if app.list.hasActiveFilter() {
			t.Error("second esc should clear the filters")
		}

		if pressKeyQuits(t, app, esc) {
			t.Fatal("esc on the cleared list must not quit")
		}
	})

	t.Run("q and ctrl+c still quit", func(t *testing.T) {
		for _, k := range []tea.KeyMsg{keyRunes("q"), {Type: tea.KeyCtrlC}} {
			app, _ := setupListTestModel(t)
			runCmd(t, app, app.Init())
			if !pressKeyQuits(t, app, k) {
				t.Errorf("%s should quit", k)
			}
		}
	})
}
