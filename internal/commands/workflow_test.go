package commands

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
)

// wfBean describes a bean to create in workflow tests.
type wfBean struct {
	id, status, typ, priority, parent string
	blockedBy                         []string
	age                               time.Duration // how long ago it was created
	body                              string
}

// setupWorkflowCore creates a temporary core populated with the given beans
// and installs it (and the default config) as the package globals.
func setupWorkflowCore(t *testing.T, beans []wfBean) *beangraph.CoreResolver {
	t.Helper()
	beansDir := filepath.Join(t.TempDir(), ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Beans are written to disk directly (rather than via Core.Create) so
	// that creation timestamps can be controlled.
	now := time.Now().UTC().Truncate(time.Second)
	for _, wb := range beans {
		created := now.Add(-wb.age)
		status := wb.status
		if status == "" {
			status = "todo"
		}
		typ := wb.typ
		if typ == "" {
			typ = "task"
		}
		b := &bean.Bean{
			ID:        wb.id,
			Slug:      wb.id,
			Title:     "Title " + wb.id,
			Status:    status,
			Type:      typ,
			Priority:  wb.priority,
			Parent:    wb.parent,
			BlockedBy: wb.blockedBy,
			Body:      wb.body,
			CreatedAt: &created,
			UpdatedAt: &created,
		}
		content, err := b.Render()
		if err != nil {
			t.Fatalf("rendering %s: %v", wb.id, err)
		}
		if err := os.WriteFile(filepath.Join(beansDir, wb.id+"--"+wb.id+".md"), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	testCfg := config.Default()
	c := beancore.New(beansDir, testCfg)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}

	oldCore, oldCfg := core, cfg
	core, cfg = c, testCfg
	t.Cleanup(func() { core, cfg = oldCore, oldCfg })
	return &beangraph.CoreResolver{Core: c}
}

func ids(beans []*bean.Bean) []string {
	out := make([]string, len(beans))
	for i, b := range beans {
		out[i] = b.ID
	}
	return out
}

func TestBuildSection(t *testing.T) {
	tests := []struct {
		name, heading, text, want string
	}{
		{"empty", summarySectionHeading, "", ""},
		{"whitespace only", summarySectionHeading, "  \n ", ""},
		{"plain text", summarySectionHeading, "Did it", "## Summary of Changes\n\nDid it"},
		{"trims", scrapSectionHeading, "\n Superseded \n", "## Reasons for Scrapping\n\nSuperseded"},
		{"already has heading", summarySectionHeading, "## Summary of Changes\n\n- a", "## Summary of Changes\n\n- a"},
		{"different heading kept as content", summarySectionHeading, "## Other\n\nx", "## Summary of Changes\n\n## Other\n\nx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSection(tt.heading, tt.text); got != tt.want {
				t.Errorf("buildSection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeProgress(t *testing.T) {
	statuses := config.Default().StatusNames()
	mk := func(statuses ...string) []*bean.Bean {
		var out []*bean.Bean
		for i, s := range statuses {
			out = append(out, &bean.Bean{ID: string(rune('a' + i)), Status: s})
		}
		return out
	}
	tests := []struct {
		name                                string
		beans                               []*bean.Bean
		total, completed, scrapped, percent int
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"all scrapped", mk("scrapped", "scrapped"), 2, 0, 2, 0},
		{"half", mk("completed", "todo"), 2, 1, 0, 50},
		{"scrapped excluded from denominator", mk("completed", "todo", "scrapped"), 3, 1, 1, 50},
		{"rounds down", mk("completed", "todo", "draft"), 3, 1, 0, 33},
		{"all done", mk("completed", "completed"), 2, 2, 0, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeProgress(tt.beans, statuses)
			if got.Total != tt.total || got.Completed != tt.completed || got.Scrapped != tt.scrapped || got.Percent != tt.percent {
				t.Errorf("got total=%d completed=%d scrapped=%d percent=%d, want %d/%d/%d/%d",
					got.Total, got.Completed, got.Scrapped, got.Percent,
					tt.total, tt.completed, tt.scrapped, tt.percent)
			}
			for _, s := range statuses {
				if _, ok := got.ByStatus[s]; !ok {
					t.Errorf("ByStatus missing configured status %q", s)
				}
			}
		})
	}
}

func TestDescendantsOf(t *testing.T) {
	all := []*bean.Bean{
		{ID: "m"},
		{ID: "e", Parent: "m"},
		{ID: "t1", Parent: "e"},
		{ID: "t2", Parent: "e"},
		{ID: "s", Parent: "t1"},
		{ID: "other"},
		// cycle: x <-> y
		{ID: "x", Parent: "y"},
		{ID: "y", Parent: "x"},
	}
	tests := []struct {
		root string
		want []string
	}{
		{"m", []string{"e", "s", "t1", "t2"}},
		{"e", []string{"s", "t1", "t2"}},
		{"s", nil},
		{"missing", nil},
		{"x", []string{"y"}},
	}
	for _, tt := range tests {
		t.Run(tt.root, func(t *testing.T) {
			got := ids(descendantsOf(all, tt.root))
			slices.Sort(got)
			if !slices.Equal(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
				t.Errorf("descendantsOf(%q) = %v, want %v", tt.root, got, tt.want)
			}
		})
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		percent, width, filled int
	}{
		{0, 10, 0},
		{50, 10, 5},
		{57, 10, 5},
		{100, 10, 10},
		{150, 10, 10},
		{-5, 10, 0},
	}
	for _, tt := range tests {
		got := renderProgressBar(tt.percent, tt.width)
		if n := strings.Count(got, "━"); n != tt.filled {
			t.Errorf("renderProgressBar(%d, %d) filled = %d, want %d", tt.percent, tt.width, n, tt.filled)
		}
		if n := len([]rune(got)); n != tt.width {
			t.Errorf("renderProgressBar(%d, %d) width = %d, want %d", tt.percent, tt.width, n, tt.width)
		}
	}
	if got := renderProgressBar(50, 0); got != "" {
		t.Errorf("zero width should render empty, got %q", got)
	}
}

func TestStatusLabel(t *testing.T) {
	tests := map[string]string{"in-progress": "In Progress", "todo": "Todo", "": ""}
	for in, want := range tests {
		if got := statusLabel(in); got != want {
			t.Errorf("statusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOtherInProgress(t *testing.T) {
	all := []*bean.Bean{
		{ID: "m", Type: "milestone", Status: "in-progress"},
		{ID: "e", Type: "epic", Status: "in-progress", Parent: "m"},
		{ID: "pt", Type: "feature", Status: "in-progress"},
		{ID: "self", Type: "task", Status: "in-progress", Parent: "pt"},
		{ID: "other", Type: "task", Status: "in-progress"},
		{ID: "todo", Type: "task", Status: "todo"},
	}
	var inProgress []*bean.Bean
	for _, b := range all {
		if b.Status == "in-progress" {
			inProgress = append(inProgress, b)
		}
	}
	tests := []struct {
		name string
		self string
		want []string
	}{
		{"excludes self, ancestors and containers", "self", []string{"other"}},
		{"sibling sees self", "other", []string{"pt", "self"}},
	}
	byID := map[string]*bean.Bean{}
	for _, b := range all {
		byID[b.ID] = b
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(otherInProgress(byID[tt.self], inProgress, all))
			if !slices.Equal(got, tt.want) {
				t.Errorf("otherInProgress(%s) = %v, want %v", tt.self, got, tt.want)
			}
		})
	}
}

func TestTransitionBean(t *testing.T) {
	tests := []struct {
		name          string
		initial       wfBean
		id            string
		status        string
		section       string
		wantErr       bool
		wantUnchanged bool
		wantStatus    string
		wantBody      string
	}{
		{
			name:       "start sets in-progress",
			initial:    wfBean{id: "b1"},
			id:         "b1",
			status:     "in-progress",
			wantStatus: "in-progress",
		},
		{
			name:       "complete appends summary after body",
			initial:    wfBean{id: "b1", body: "Original."},
			id:         "b1",
			status:     "completed",
			section:    buildSection(summarySectionHeading, "Did the thing"),
			wantStatus: "completed",
			wantBody:   "Original.\n\n## Summary of Changes\n\nDid the thing",
		},
		{
			name:       "scrap appends reason to empty body",
			initial:    wfBean{id: "b1"},
			id:         "b1",
			status:     "scrapped",
			section:    buildSection(scrapSectionHeading, "Superseded"),
			wantStatus: "scrapped",
			wantBody:   "## Reasons for Scrapping\n\nSuperseded",
		},
		{
			name:          "already in target status changes nothing",
			initial:       wfBean{id: "b1", status: "completed", body: "Done."},
			id:            "b1",
			status:        "completed",
			section:       buildSection(summarySectionHeading, "Again"),
			wantUnchanged: true,
			wantStatus:    "completed",
			wantBody:      "Done.",
		},
		{
			name:    "not found",
			initial: wfBean{id: "b1"},
			id:      "nope",
			status:  "completed",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := setupWorkflowCore(t, []wfBean{tt.initial})
			res, err := transitionBean(context.Background(), resolver, tt.id, tt.status, tt.section, false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Unchanged != tt.wantUnchanged {
				t.Errorf("Unchanged = %v, want %v", res.Unchanged, tt.wantUnchanged)
			}

			// Verify persisted state by reloading from disk.
			reloaded := beancore.New(resolver.Core.Root(), config.Default())
			if err := reloaded.Load(); err != nil {
				t.Fatal(err)
			}
			b, err := reloaded.Get(tt.id)
			if err != nil {
				t.Fatal(err)
			}
			if b.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", b.Status, tt.wantStatus)
			}
			if tt.wantBody != "" && strings.TrimSpace(b.Body) != tt.wantBody {
				t.Errorf("body = %q, want %q", b.Body, tt.wantBody)
			}
		})
	}
}

func TestTransitionBeanUnarchivesOnStart(t *testing.T) {
	resolver := setupWorkflowCore(t, []wfBean{{id: "arch", status: "completed"}})
	if err := resolver.Core.Archive("arch"); err != nil {
		t.Fatal(err)
	}
	if !resolver.Core.IsArchived("arch") {
		t.Fatal("precondition: bean should be archived")
	}

	res, err := transitionBean(context.Background(), resolver, "arch", "in-progress", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Unarchived {
		t.Error("expected Unarchived")
	}
	if resolver.Core.IsArchived("arch") {
		t.Error("bean should no longer be archived")
	}
	if res.Bean.Status != "in-progress" {
		t.Errorf("status = %q", res.Bean.Status)
	}
}

func TestFindNextBean(t *testing.T) {
	tests := []struct {
		name  string
		beans []wfBean
		want  string // "" = none
	}{
		{
			name: "nothing at all",
			want: "",
		},
		{
			name: "only non-todo statuses",
			beans: []wfBean{
				{id: "d", status: "draft", priority: "critical"},
				{id: "ip", status: "in-progress", priority: "critical"},
				{id: "c", status: "completed", priority: "critical"},
				{id: "s", status: "scrapped", priority: "critical"},
			},
			want: "",
		},
		{
			name: "highest priority wins",
			beans: []wfBean{
				{id: "n", priority: "normal", age: 3 * time.Hour},
				{id: "h", priority: "high"},
				{id: "l", priority: "low", age: 5 * time.Hour},
			},
			want: "h",
		},
		{
			name: "explicitly blocked bean skipped",
			beans: []wfBean{
				{id: "blocker", priority: "low"},
				{id: "blocked", priority: "critical", blockedBy: []string{"blocker"}},
			},
			want: "blocker",
		},
		{
			name: "completed blocker no longer blocks",
			beans: []wfBean{
				{id: "blocker", status: "completed"},
				{id: "blocked", priority: "critical", blockedBy: []string{"blocker"}},
				{id: "other", priority: "high"},
			},
			want: "blocked",
		},
		{
			name: "implicitly blocked via ancestor skipped",
			beans: []wfBean{
				{id: "gate", priority: "deferred"},
				{id: "epic", typ: "epic", status: "in-progress", blockedBy: []string{"gate"}},
				{id: "child", priority: "critical", parent: "epic"},
			},
			want: "gate",
		},
		{
			name: "implicitly terminal via scrapped ancestor skipped",
			beans: []wfBean{
				{id: "epic", typ: "epic", status: "scrapped"},
				{id: "child", priority: "critical", parent: "epic"},
				{id: "free", priority: "low"},
			},
			want: "free",
		},
		{
			name: "leaf preferred over container at same priority",
			beans: []wfBean{
				{id: "epic", typ: "epic", age: 10 * time.Hour},
				{id: "milestone", typ: "milestone", age: 11 * time.Hour},
				{id: "task", age: time.Hour, parent: "epic"},
			},
			want: "task",
		},
		{
			name: "oldest created wins ties",
			beans: []wfBean{
				{id: "new", typ: "bug", age: time.Minute},
				{id: "old", typ: "feature", age: time.Hour},
			},
			want: "old",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := setupWorkflowCore(t, tt.beans)
			got, err := resolver.NextBean(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			gotID := ""
			if got != nil {
				gotID = got.ID
			}
			if gotID != tt.want {
				t.Errorf("NextBean() = %q, want %q", gotID, tt.want)
			}
		})
	}
}

// hierarchy: milestone m1 > epic e1 > tasks; milestone m2 (done); loose task.
var workflowHierarchy = []wfBean{
	{id: "m1", typ: "milestone", priority: "low", age: 10 * time.Hour},
	{id: "m2", typ: "milestone", status: "completed", age: 9 * time.Hour},
	{id: "m3", typ: "milestone", priority: "high", age: time.Hour},
	{id: "e1", typ: "epic", parent: "m1"},
	{id: "t1", parent: "e1", status: "completed"},
	{id: "t2", parent: "e1", status: "in-progress"},
	{id: "t3", parent: "e1", blockedBy: []string{"t2"}},
	{id: "t4", parent: "e1", status: "scrapped"},
	{id: "t5", parent: "m2", status: "completed"},
	{id: "loose"},
}

func TestBuildProgressReport(t *testing.T) {
	tests := []struct {
		name                                         string
		parent                                       string
		wantErr                                      bool
		total, completed, scrapped, percent, blocked int
		inProgress                                   int
	}{
		{name: "whole project", total: 10, completed: 3, scrapped: 1, percent: 33, blocked: 1, inProgress: 1},
		{name: "milestone subtree", parent: "m1", total: 5, completed: 1, scrapped: 1, percent: 25, blocked: 1, inProgress: 1},
		{name: "epic subtree", parent: "e1", total: 4, completed: 1, scrapped: 1, percent: 33, blocked: 1, inProgress: 1},
		{name: "leaf has no descendants", parent: "t1"},
		{name: "unknown parent", parent: "nope", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := setupWorkflowCore(t, workflowHierarchy)
			r, err := buildProgressReport(context.Background(), resolver, tt.parent)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if errorCode(err) != "NOT_FOUND" {
					t.Errorf("errorCode = %s, want NOT_FOUND", errorCode(err))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Total != tt.total || r.Completed != tt.completed || r.Scrapped != tt.scrapped ||
				r.Percent != tt.percent || r.Blocked != tt.blocked || r.ByStatus["in-progress"] != tt.inProgress {
				t.Errorf("got total=%d completed=%d scrapped=%d percent=%d blocked=%d in-progress=%d",
					r.Total, r.Completed, r.Scrapped, r.Percent, r.Blocked, r.ByStatus["in-progress"])
			}
			if (tt.parent == "") != (r.Scope == nil) {
				t.Errorf("scope = %v, want parent %q", r.Scope, tt.parent)
			}
			out := renderProgressReport(r, config.Default())
			if r.Total > 0 && !strings.Contains(out, "% complete") {
				t.Errorf("rendered output missing progress line:\n%s", out)
			}
		})
	}
}

func TestBuildMilestoneList(t *testing.T) {
	tests := []struct {
		name       string
		all        bool
		wantIDs    []string
		wantCounts map[string][2]int // id -> {completed, total}
	}{
		{
			name:       "active only, sorted by priority",
			wantIDs:    []string{"m3", "m1"},
			wantCounts: map[string][2]int{"m3": {0, 0}, "m1": {1, 5}},
		},
		{
			name:       "all includes completed",
			all:        true,
			wantIDs:    []string{"m3", "m2", "m1"},
			wantCounts: map[string][2]int{"m2": {1, 1}, "m1": {1, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := setupWorkflowCore(t, workflowHierarchy)
			list, err := buildMilestoneList(context.Background(), resolver, tt.all)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, mp := range list {
				got = append(got, mp.Milestone.ID)
				if want, ok := tt.wantCounts[mp.Milestone.ID]; ok {
					if mp.Progress.Completed != want[0] || mp.Progress.Total != want[1] {
						t.Errorf("%s: completed/total = %d/%d, want %d/%d", mp.Milestone.ID,
							mp.Progress.Completed, mp.Progress.Total, want[0], want[1])
					}
				}
			}
			if !slices.Equal(got, tt.wantIDs) {
				t.Errorf("milestones = %v, want %v", got, tt.wantIDs)
			}
			out := renderMilestoneList(list, config.Default())
			for _, id := range tt.wantIDs {
				if !strings.Contains(out, id) {
					t.Errorf("rendered output missing %s:\n%s", id, out)
				}
			}
		})
	}
}
