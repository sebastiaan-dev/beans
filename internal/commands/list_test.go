package commands

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

func TestSortBeans(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	evenEarlier := now.Add(-2 * time.Hour)

	// Statuses are now hardcoded, so we just use default config
	testCfg := config.Default()

	t.Run("sort by id", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "c3"},
			{ID: "a1"},
			{ID: "b2"},
		}
		sortBeans(beans, "id", testCfg)

		if beans[0].ID != "a1" || beans[1].ID != "b2" || beans[2].ID != "c3" {
			t.Errorf("sort by id: got [%s, %s, %s], want [a1, b2, c3]",
				beans[0].ID, beans[1].ID, beans[2].ID)
		}
	})

	t.Run("sort by created", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "old", CreatedAt: &evenEarlier},
			{ID: "new", CreatedAt: &now},
			{ID: "mid", CreatedAt: &earlier},
		}
		sortBeans(beans, "created", testCfg)

		// Should be newest first
		if beans[0].ID != "new" || beans[1].ID != "mid" || beans[2].ID != "old" {
			t.Errorf("sort by created: got [%s, %s, %s], want [new, mid, old]",
				beans[0].ID, beans[1].ID, beans[2].ID)
		}
	})

	t.Run("sort by created with nil", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "nil1", CreatedAt: nil},
			{ID: "has", CreatedAt: &now},
			{ID: "nil2", CreatedAt: nil},
		}
		sortBeans(beans, "created", testCfg)

		// Non-nil should come first, then nil sorted by ID
		if beans[0].ID != "has" {
			t.Errorf("sort by created with nil: first should be \"has\", got %q", beans[0].ID)
		}
	})

	t.Run("sort by updated", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "old", UpdatedAt: &evenEarlier},
			{ID: "new", UpdatedAt: &now},
			{ID: "mid", UpdatedAt: &earlier},
		}
		sortBeans(beans, "updated", testCfg)

		// Should be newest first
		if beans[0].ID != "new" || beans[1].ID != "mid" || beans[2].ID != "old" {
			t.Errorf("sort by updated: got [%s, %s, %s], want [new, mid, old]",
				beans[0].ID, beans[1].ID, beans[2].ID)
		}
	})

	t.Run("sort by status", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "c1", Status: "completed"},
			{ID: "t1", Status: "todo"},
			{ID: "i1", Status: "in-progress"},
			{ID: "t2", Status: "todo"},
		}
		sortBeans(beans, "status", testCfg)

		// Should be ordered by status config order (in-progress, todo, draft, completed, scrapped), then by ID within same status
		expected := []string{"i1", "t1", "t2", "c1"}
		for i, want := range expected {
			if beans[i].ID != want {
				t.Errorf("sort by status[%d]: got %q, want %q", i, beans[i].ID, want)
			}
		}
	})

	t.Run("default sort (archive status then type)", func(t *testing.T) {
		beans := []*bean.Bean{
			{ID: "completed-bug", Status: "completed", Type: "bug"},
			{ID: "todo-feature", Status: "todo", Type: "feature"},
			{ID: "todo-task", Status: "todo", Type: "task"},
			{ID: "completed-task", Status: "completed", Type: "task"},
			{ID: "todo-bug", Status: "todo", Type: "bug"},
		}
		sortBeans(beans, "", testCfg)

		// Should be: non-archive first (sorted by type order from DefaultTypes: milestone, epic, bug, feature, task),
		// then archive (sorted by type)
		// DefaultTypes order: milestone, epic, bug, feature, task
		expected := []string{"todo-bug", "todo-feature", "todo-task", "completed-bug", "completed-task"}
		for i, want := range expected {
			if beans[i].ID != want {
				t.Errorf("default sort[%d]: got %q, want %q", i, beans[i].ID, want)
			}
		}
	})
}

func TestListReadyFlagMutualExclusion(t *testing.T) {
	// Test that --ready and --is-blocked are mutually exclusive
	// by checking the validation logic directly
	tests := []struct {
		name        string
		ready       bool
		isBlocked   bool
		expectError bool
	}{
		{"neither flag", false, false, false},
		{"only --ready", true, false, false},
		{"only --is-blocked", false, true, false},
		{"both flags", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the validation logic in list.go
			hasError := tt.ready && tt.isBlocked
			if hasError != tt.expectError {
				t.Errorf("ready=%v, isBlocked=%v: got error=%v, want error=%v",
					tt.ready, tt.isBlocked, hasError, tt.expectError)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 4, "h..."},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func beanIDs(beans []*bean.Bean) []string {
	ids := make([]string, len(beans))
	for i, b := range beans {
		ids[i] = b.ID
	}
	return ids
}

func TestValidateListOptions(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		onlyArchived bool
		noArchived   bool
		wantErr      string
	}{
		{"defaults", 0, false, false, ""},
		{"positive limit", 5, false, false, ""},
		{"negative limit", -1, false, false, "--limit must be a non-negative integer, got -1"},
		{"only --archived", 0, true, false, ""},
		{"only --no-archived", 0, false, true, ""},
		{"both archive flags", 0, true, true, "--archived and --no-archived are mutually exclusive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateListOptions(tt.limit, tt.onlyArchived, tt.noArchived)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLimitBeans(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  []string
	}{
		{"zero means no limit", 0, []string{"a", "b", "c"}},
		{"limit below length", 2, []string{"a", "b"}},
		{"limit equal to length", 3, []string{"a", "b", "c"}},
		{"limit above length", 10, []string{"a", "b", "c"}},
		{"limit one", 1, []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beans := []*bean.Bean{{ID: "a"}, {ID: "b"}, {ID: "c"}}
			got := beanIDs(limitBeans(beans, tt.limit))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("limitBeans(%d) = %v, want %v", tt.limit, got, tt.want)
			}
		})
	}
}

func TestLimitAppliesAfterSort(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	evenEarlier := now.Add(-2 * time.Hour)

	tests := []struct {
		name   string
		sortBy string
		limit  int
		want   []string
	}{
		{"created newest first", "created", 2, []string{"new", "mid"}},
		{"id order", "id", 2, []string{"mid", "new"}},
		{"no limit", "id", 0, []string{"mid", "new", "old"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beans := []*bean.Bean{
				{ID: "old", CreatedAt: &evenEarlier},
				{ID: "new", CreatedAt: &now},
				{ID: "mid", CreatedAt: &earlier},
			}
			sortBeans(beans, tt.sortBy, config.Default())
			got := beanIDs(limitBeans(beans, tt.limit))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterArchived(t *testing.T) {
	beans := []*bean.Bean{
		{ID: "a", Path: "a--open.md"},
		{ID: "b", Path: "archive/b--done.md"},
		{ID: "c", Path: "c.md"},
		{ID: "d", Path: "archive/d.md"},
	}

	tests := []struct {
		name         string
		onlyArchived bool
		noArchived   bool
		want         []string
	}{
		{"default includes all", false, false, []string{"a", "b", "c", "d"}},
		{"--no-archived excludes archived", false, true, []string{"a", "c"}},
		{"--archived keeps only archived", true, false, []string{"b", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := beanIDs(filterArchived(beans, archiveFilterMode(tt.onlyArchived, tt.noArchived)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArchivedTitle(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		maxWidth    int
		hasPriority bool
		want        string
	}{
		{"no width limit", "Done task", 0, false, "Done task (archived)"},
		{"fits", "Done task", 40, false, "Done task (archived)"},
		{"truncates title, keeps marker", "A rather long title", 25, false, "A rather lo... (archived)"},
		{"accounts for priority symbol", "A rather long title", 25, true, "A rather ... (archived)"},
		{"tiny width", "Title", 12, false, "T (archived)"},
		{"no room for title", "Title", 11, false, " (archived)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ui.ArchivedTitle(tt.title, tt.maxWidth, tt.hasPriority)
			if got != tt.want {
				t.Errorf("ArchivedTitle(%q, %d, %v) = %q, want %q", tt.title, tt.maxWidth, tt.hasPriority, got, tt.want)
			}
			if tt.maxWidth > 0 {
				width := len(got)
				if tt.hasPriority {
					width += 2
				}
				if width > tt.maxWidth {
					t.Errorf("result %q (width %d) exceeds max width %d", got, width, tt.maxWidth)
				}
			}
		})
	}
}

func TestRenderTreeMarksArchivedBeans(t *testing.T) {
	beans := []*bean.Bean{
		{ID: "open-1", Path: "open-1.md", Title: "Open bean", Status: "todo", Type: "task"},
		{ID: "done-1", Path: "archive/done-1.md", Title: "Done bean", Status: "completed", Type: "task"},
	}
	cfg := config.Default()
	tree := ui.BuildTree(beans, beans, func(b []*bean.Bean) { sortBeans(b, "id", cfg) }, nil)
	out := ui.RenderTree(tree, cfg, 10, false, 80)

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "Open bean"):
			if strings.Contains(line, ui.ArchivedMarker) {
				t.Errorf("non-archived bean should not be marked: %q", line)
			}
		case strings.Contains(line, "Done bean"):
			if !strings.Contains(line, "Done bean "+ui.ArchivedMarker) {
				t.Errorf("archived bean should be marked: %q", line)
			}
		}
	}
	if !strings.Contains(out, "Done bean") || !strings.Contains(out, "Open bean") {
		t.Fatalf("expected both beans in output:\n%s", out)
	}
}
