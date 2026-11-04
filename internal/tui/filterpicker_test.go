package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/bean"
)

func TestListFiltersMatches(t *testing.T) {
	todoBug := &bean.Bean{ID: "a", Status: "todo", Type: "bug", Tags: []string{"frontend", "ux"}}
	completedTask := &bean.Bean{ID: "b", Status: "completed", Type: "task"}
	scrappedFeature := &bean.Bean{ID: "c", Status: "scrapped", Type: "feature", Tags: []string{"backend"}}

	tests := []struct {
		name     string
		filters  listFilters
		bean     *bean.Bean
		implicit string
		want     bool
	}{
		{"empty filters match everything", listFilters{}, completedTask, "", true},
		{"hide resolved hides completed", listFilters{hideResolved: true}, completedTask, "", false},
		{"hide resolved hides scrapped", listFilters{hideResolved: true}, scrappedFeature, "", false},
		{"hide resolved hides implicitly resolved", listFilters{hideResolved: true}, todoBug, "completed", false},
		{"hide resolved keeps open beans", listFilters{hideResolved: true}, todoBug, "", true},
		{"status filter matches", listFilters{statuses: []string{"draft", "todo"}}, todoBug, "", true},
		{"status filter rejects", listFilters{statuses: []string{"draft"}}, todoBug, "", false},
		{"type filter matches", listFilters{types: []string{"bug"}}, todoBug, "", true},
		{"type filter rejects", listFilters{types: []string{"task"}}, todoBug, "", false},
		{"tag filter matches any tag", listFilters{tags: []string{"backend", "ux"}}, todoBug, "", true},
		{"tag filter rejects untagged", listFilters{tags: []string{"ux"}}, completedTask, "", false},
		{"categories are combined with AND", listFilters{types: []string{"bug"}, tags: []string{"backend"}}, todoBug, "", false},
		{"status filter can select resolved beans", listFilters{statuses: []string{"completed"}}, completedTask, "", true},
		{"hide resolved wins over status filter", listFilters{statuses: []string{"completed"}, hideResolved: true}, completedTask, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filters.matches(tt.bean, tt.implicit); got != tt.want {
				t.Errorf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListFiltersApply(t *testing.T) {
	beans := []*bean.Bean{
		{ID: "parent", Status: "completed", Type: "epic"},
		{ID: "child", Status: "todo", Type: "task", Parent: "parent"},
		{ID: "open", Status: "todo", Type: "bug"},
		{ID: "gone", Status: "scrapped", Type: "bug"},
	}
	implicit := map[string]string{"child": "completed"}

	ids := func(bs []*bean.Bean) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.ID)
		}
		return out
	}

	if got := ids(listFilters{}.apply(beans, implicit)); !reflect.DeepEqual(got, []string{"parent", "child", "open", "gone"}) {
		t.Errorf("no filters: got %v", got)
	}
	if got := ids(listFilters{hideResolved: true}.apply(beans, implicit)); !reflect.DeepEqual(got, []string{"open"}) {
		t.Errorf("hide resolved: got %v", got)
	}
	if got := ids(listFilters{types: []string{"bug"}}.apply(beans, implicit)); !reflect.DeepEqual(got, []string{"open", "gone"}) {
		t.Errorf("type bug: got %v", got)
	}
}

func TestListFiltersSummary(t *testing.T) {
	if got := (listFilters{}).summary(); len(got) != 0 {
		t.Errorf("empty filters summary = %v, want none", got)
	}
	f := listFilters{
		statuses:     []string{"todo", "in-progress"},
		types:        []string{"bug"},
		tags:         []string{"a", "b"},
		hideResolved: true,
	}
	want := []string{"done hidden", "status:todo,in-progress", "type:bug", "tags:a,b"}
	if got := f.summary(); !reflect.DeepEqual(got, want) {
		t.Errorf("summary() = %v, want %v", got, want)
	}
	if got := (listFilters{tags: []string{"a"}}).summary(); !reflect.DeepEqual(got, []string{"tag:a"}) {
		t.Errorf("single tag summary = %v", got)
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// sendKeys feeds keys to the picker and returns the message of the last command, if any.
func sendKeys(m filterPickerModel, keys ...tea.KeyMsg) (filterPickerModel, tea.Msg) {
	var last tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		last = nil
		if cmd != nil {
			last = cmd()
		}
	}
	return m, last
}

func testTags() []tagWithCount {
	return []tagWithCount{
		{tag: "frontend", count: 5},
		{tag: "backend", count: 3},
		{tag: "infra", count: 3},
		{tag: "docs", count: 1},
	}
}

func TestFilterPickerStartsFromCurrentFilters(t *testing.T) {
	current := listFilters{
		statuses:     []string{"todo"},
		types:        []string{"bug"},
		tags:         []string{"orphaned"}, // no longer used by any bean
		hideResolved: true,
	}
	m := newFilterPickerModel(current, testTags(), 120, 40)

	_, msg := sendKeys(m, keyType(tea.KeyEnter))
	applied, ok := msg.(filtersAppliedMsg)
	if !ok {
		t.Fatalf("enter should emit filtersAppliedMsg, got %T", msg)
	}
	if !reflect.DeepEqual(applied.filters, current) {
		t.Errorf("unchanged picker applied %+v, want %+v", applied.filters, current)
	}
}

func TestFilterPickerSelectsStatusTypeAndTags(t *testing.T) {
	m := newFilterPickerModel(listFilters{}, testTags(), 120, 40)

	m, msg := sendKeys(m,
		// Status section: in-progress is first, todo second
		keyRunes(" "),                       // in-progress
		keyType(tea.KeyDown), keyRunes(" "), // todo
		// Type section: milestone, epic, bug ...
		keyType(tea.KeyTab), keyRunes("j"), keyRunes("j"), keyRunes(" "), // bug
		// Tags section: type-ahead narrows the list
		keyType(tea.KeyTab), keyRunes("i"), keyRunes("n"), keyRunes(" "), // infra
		keyType(tea.KeyBackspace), keyType(tea.KeyBackspace),
		keyRunes("d"), keyRunes("o"), keyRunes(" "), // docs
		keyType(tea.KeyEnter),
	)
	applied, ok := msg.(filtersAppliedMsg)
	if !ok {
		t.Fatalf("enter should emit filtersAppliedMsg, got %T", msg)
	}
	want := listFilters{
		statuses: []string{"in-progress", "todo"},
		types:    []string{"bug"},
		tags:     []string{"docs", "infra"},
	}
	if !reflect.DeepEqual(applied.filters, want) {
		t.Errorf("applied %+v, want %+v", applied.filters, want)
	}
	if m.tagQuery != "do" {
		t.Errorf("tagQuery = %q, want %q", m.tagQuery, "do")
	}
}

func TestFilterPickerToggleTwiceDeselects(t *testing.T) {
	m := newFilterPickerModel(listFilters{statuses: []string{"in-progress"}}, nil, 120, 40)
	_, msg := sendKeys(m, keyRunes(" "), keyType(tea.KeyEnter))
	if got := msg.(filtersAppliedMsg).filters.statuses; len(got) != 0 {
		t.Errorf("statuses = %v, want none", got)
	}
}

func TestFilterPickerTagQueryCapturesLetters(t *testing.T) {
	m := newFilterPickerModel(listFilters{}, testTags(), 120, 40)
	// Letters that are shortcuts elsewhere (q, j, k, /) are part of the query in the tag section.
	m, msg := sendKeys(m, keyType(tea.KeyShiftTab), keyRunes("q"), keyRunes("j"), keyRunes("k"), keyRunes("/"))
	if msg != nil {
		t.Errorf("typing in tag search should not emit messages, got %T", msg)
	}
	if m.section != filterSectionTags || m.tagQuery != "qjk/" {
		t.Errorf("section=%v query=%q, want tags section and %q", m.section, m.tagQuery, "qjk/")
	}
	if got := m.visibleTags(); len(got) != 0 {
		t.Errorf("visibleTags() = %v, want none", got)
	}
}

func TestFilterPickerVisibleTagsOrdering(t *testing.T) {
	m := newFilterPickerModel(listFilters{}, []tagWithCount{
		{tag: "frontend", count: 9},
		{tag: "end", count: 1},
		{tag: "backend", count: 4},
	}, 120, 40)

	names := func(opts []filterOption) []string {
		var out []string
		for _, o := range opts {
			out = append(out, o.name)
		}
		return out
	}

	if got := names(m.visibleTags()); !reflect.DeepEqual(got, []string{"frontend", "backend", "end"}) {
		t.Errorf("no query: %v", got)
	}
	m.tagQuery = "END"
	if got := names(m.visibleTags()); !reflect.DeepEqual(got, []string{"end", "frontend", "backend"}) {
		t.Errorf("query END: %v (prefix matches first, then by count)", got)
	}
}

func TestFilterPickerClearAndCancel(t *testing.T) {
	m := newFilterPickerModel(listFilters{statuses: []string{"todo"}, tags: []string{"docs"}, hideResolved: true}, testTags(), 120, 40)

	_, msg := sendKeys(m, keyType(tea.KeyEsc))
	if _, ok := msg.(closeFilterPickerMsg); !ok {
		t.Errorf("esc should emit closeFilterPickerMsg, got %T", msg)
	}

	_, msg = sendKeys(m, keyType(tea.KeyCtrlX), keyType(tea.KeyEnter))
	applied := msg.(filtersAppliedMsg).filters
	// ctrl+x clears the selections but keeps the hide-resolved toggle, which is owned by H.
	if !reflect.DeepEqual(applied, listFilters{hideResolved: true}) {
		t.Errorf("after clear: %+v", applied)
	}
}

func TestFilterPickerView(t *testing.T) {
	m := newFilterPickerModel(listFilters{tags: []string{"docs"}}, testTags(), 120, 40)
	view := m.View()
	for _, want := range []string{"Filter Beans", "Status", "Type", "Tags", "search:", "todo", "bug", "frontend", "selected: docs"} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q", want)
		}
	}

	// Long tag lists are truncated with a hint to keep typing
	var many []tagWithCount
	for _, name := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11", "a12"} {
		many = append(many, tagWithCount{tag: name, count: 1})
	}
	m = newFilterPickerModel(listFilters{}, many, 120, 40)
	if !strings.Contains(m.View(), "more (type to narrow)") {
		t.Error("view should hint at hidden tags")
	}
}

func TestFilterPickerHandlesBatchedRunes(t *testing.T) {
	m := newFilterPickerModel(listFilters{}, testTags(), 120, 40)
	// Terminals may deliver quickly typed keys as one message.
	m, _ = sendKeys(m, keyType(tea.KeyTab), keyRunes("jjjj"), keyRunes(" "), keyRunes("/fro"))
	if got := m.filters().types; !reflect.DeepEqual(got, []string{"task"}) {
		t.Errorf("types = %v, want [task]", got)
	}
	if m.section != filterSectionTags || m.tagQuery != "fro" {
		t.Errorf("section=%v query=%q, want tags section and %q", m.section, m.tagQuery, "fro")
	}
}

func TestFilterPickerFitsNarrowTerminal(t *testing.T) {
	for _, width := range []int{30, 42, 60, 120} {
		m := newFilterPickerModel(listFilters{}, testTags(), width, 40)
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line is %d columns wide: %q", width, w, line)
				break
			}
		}
	}
}
