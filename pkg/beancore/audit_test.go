package beancore

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
)

// auditSummary is a compact, comparable form of a finding.
type auditSummary struct {
	Kind AuditKind
	IDs  string
}

func summarize(findings []AuditFinding) []auditSummary {
	out := []auditSummary{}
	for _, f := range findings {
		out = append(out, auditSummary{f.Kind, strings.Join(f.BeanIDs, ",")})
	}
	return out
}

func commitSource(commits ...AuditCommit) AuditCommitSource {
	return func(yield func(AuditCommit) bool) error {
		for _, c := range commits {
			if !yield(c) {
				return nil
			}
		}
		return nil
	}
}

func commit(short, msg string) AuditCommit {
	subject, _, _ := strings.Cut(msg, "\n")
	return AuditCommit{Hash: short + strings.Repeat("0", 40-len(short)), ShortHash: short, Subject: subject, Message: msg}
}

func TestAudit(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	daysAgo := func(n int) *time.Time {
		ts := now.Add(-time.Duration(n) * 24 * time.Hour)
		return &ts
	}

	tests := []struct {
		name    string
		beans   []*bean.Bean
		commits []AuditCommit
		opts    AuditOptions
		want    []auditSummary
	}{
		{
			name: "clean project",
			beans: []*bean.Bean{
				{ID: "aa01", Status: "todo"},
				{ID: "aa02", Status: "completed"},
				{ID: "ep01", Type: "epic", Status: "in-progress"},
				{ID: "aa03", Status: "todo", Parent: "ep01"},
			},
			commits: []AuditCommit{commit("c1", "fix: something (Refs: aa02)")},
			want:    []auditSummary{},
		},
		{
			name: "landed-but-open matches whole tokens only",
			beans: []*bean.Bean{
				{ID: "beans-ab01", Status: "todo"},
				{ID: "beans-ab02", Status: "in-progress"},
				{ID: "beans-ab03", Status: "draft"},
				{ID: "beans-ab04", Status: "todo"},
				{ID: "beans-ab05", Status: "completed"},
				{ID: "beans-ab06", Status: "todo"},
			},
			commits: []AuditCommit{
				commit("c4", "feat: thing\n\nRefs: beans-ab01"),
				commit("c3", "fix: other (beans-ab02)"),
				commit("c2", "Fixes BEANS-AB03."),
				commit("c1", "chore: touch .beans/beans-ab04--slug.md and beans-ab066 and xbeans-ab06 (beans-ab05)"),
			},
			want: []auditSummary{
				{AuditLandedButOpen, "beans-ab01"},
				{AuditLandedButOpen, "beans-ab02"},
				{AuditLandedButOpen, "beans-ab03"},
			},
		},
		{
			name:    "landed-but-open disabled without commit source",
			beans:   []*bean.Bean{{ID: "aa01", Status: "todo"}},
			commits: nil,
			want:    []auditSummary{},
		},
		{
			name: "container done with open children",
			beans: []*bean.Bean{
				{ID: "ep01", Type: "epic", Status: "completed"},
				{ID: "aa01", Status: "todo", Parent: "ep01"},
				{ID: "aa02", Status: "completed", Parent: "ep01"},
				{ID: "aa03", Status: "draft", Parent: "ep01"},
				{ID: "ms01", Type: "milestone", Status: "scrapped"},
				{ID: "aa04", Status: "scrapped", Parent: "ms01"},
			},
			want: []auditSummary{
				{AuditContainerDoneChildrenOpen, "ep01,aa01,aa03"},
			},
		},
		{
			name: "container ready to close",
			beans: []*bean.Bean{
				{ID: "ep01", Type: "epic", Status: "todo"},
				{ID: "aa01", Status: "completed", Parent: "ep01"},
				{ID: "aa02", Status: "scrapped", Parent: "ep01"},
				{ID: "ft01", Type: "feature", Status: "in-progress"},
				{ID: "aa03", Status: "completed", Parent: "ft01"},
				{ID: "aa04", Status: "todo", Parent: "ft01"},
				{ID: "ep02", Type: "epic", Status: "todo"}, // no children
			},
			want: []auditSummary{
				{AuditContainerReadyToClose, "ep01,aa01,aa02"},
			},
		},
		{
			name: "stale in-progress",
			beans: []*bean.Bean{
				{ID: "aa01", Status: "in-progress", UpdatedAt: daysAgo(20)},
				{ID: "aa02", Status: "in-progress", UpdatedAt: daysAgo(3)},
				{ID: "aa03", Status: "todo", UpdatedAt: daysAgo(100)},
				{ID: "aa04", Status: "in-progress", CreatedAt: daysAgo(30)},
				{ID: "aa05", Status: "in-progress"},
			},
			opts: AuditOptions{StaleAfter: 14 * 24 * time.Hour},
			want: []auditSummary{
				{AuditStaleInProgress, "aa01"},
				{AuditStaleInProgress, "aa04"},
			},
		},
		{
			name: "stale check disabled",
			beans: []*bean.Bean{
				{ID: "aa01", Status: "in-progress", UpdatedAt: daysAgo(200)},
			},
			opts: AuditOptions{StaleAfter: 0},
			want: []auditSummary{},
		},
		{
			name: "blocked by done",
			beans: []*bean.Bean{
				{ID: "bl01", Status: "completed"},
				{ID: "bl02", Status: "scrapped", Blocking: []string{"aa01", "aa03"}},
				{ID: "bl03", Status: "todo"},
				{ID: "aa01", Status: "todo", BlockedBy: []string{"bl01"}},         // both blockers done
				{ID: "aa02", Status: "todo", BlockedBy: []string{"bl01", "bl03"}}, // still actively blocked
				{ID: "aa03", Status: "completed", BlockedBy: []string{"bl01"}},    // itself done: irrelevant
				{ID: "aa04", Status: "todo", BlockedBy: []string{"missing"}},      // broken link: beans check
				{ID: "aa05", Status: "in-progress", BlockedBy: []string{"bl03"}},  // active blocker
			},
			want: []auditSummary{
				{AuditBlockedByDone, "aa01,bl01,bl02"},
			},
		},
		{
			name: "findings ordered by kind then id",
			beans: []*bean.Bean{
				{ID: "zz01", Status: "in-progress", BlockedBy: []string{"aa09"}, UpdatedAt: daysAgo(30)},
				{ID: "aa09", Status: "completed"},
				{ID: "mm01", Type: "epic", Status: "todo"},
				{ID: "mm02", Status: "completed", Parent: "mm01"},
			},
			commits: []AuditCommit{commit("c1", "feat: zz01 and mm01")},
			opts:    AuditOptions{StaleAfter: 14 * 24 * time.Hour},
			want: []auditSummary{
				{AuditLandedButOpen, "mm01"},
				{AuditLandedButOpen, "zz01"},
				{AuditContainerReadyToClose, "mm01,mm02"},
				{AuditStaleInProgress, "zz01"},
				{AuditBlockedByDone, "zz01,aa09"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, _ := setupTestCore(t)
			for _, b := range tt.beans {
				createdAt, updatedAt := b.CreatedAt, b.UpdatedAt
				if b.Title == "" {
					b.Title = b.ID
				}
				if err := core.Create(b); err != nil {
					t.Fatalf("Create(%s): %v", b.ID, err)
				}
				// Create stamps timestamps; restore the test's values.
				setTimestampsForTest(core, b.ID, createdAt, updatedAt)
			}

			opts := tt.opts
			opts.Now = now
			if tt.commits != nil {
				opts.Commits = commitSource(tt.commits...)
			}
			findings, err := core.Audit(opts)
			if err != nil {
				t.Fatalf("Audit: %v", err)
			}
			if got := summarize(findings); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// setTimestampsForTest replaces the stored bean with a copy carrying the
// given timestamps (stored beans are immutable).
func setTimestampsForTest(core *Core, id string, createdAt, updatedAt *time.Time) {
	core.mu.Lock()
	defer core.mu.Unlock()
	b := core.beans[id].Clone()
	b.CreatedAt, b.UpdatedAt = createdAt, updatedAt
	core.beans[id] = b
}

func TestAuditDetails(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	core, _ := setupTestCore(t)
	for _, b := range []*bean.Bean{
		{ID: "aa01", Title: "Landed", Status: "todo"},
		{ID: "aa02", Title: "Stale", Status: "in-progress"},
	} {
		if err := core.Create(b); err != nil {
			t.Fatal(err)
		}
	}
	stale, _ := core.Get("aa02")
	updated := now.Add(-20*24*time.Hour - time.Hour)
	setTimestampsForTest(core, "aa02", stale.CreatedAt, &updated)

	commits := []AuditCommit{commit("c3", "feat: newest (aa01)"), commit("c2", "unrelated")}
	for i := 0; i < maxAuditCommits+2; i++ {
		commits = append(commits, commit("c1", "Refs: aa01"))
	}

	findings, err := core.Audit(AuditOptions{Now: now, StaleAfter: 14 * 24 * time.Hour, Commits: commitSource(commits...)})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %+v", findings)
	}

	landed := findings[0]
	if landed.Kind != AuditLandedButOpen {
		t.Fatalf("first finding kind = %s", landed.Kind)
	}
	if landed.Details.CommitCount != maxAuditCommits+3 {
		t.Errorf("CommitCount = %d, want %d", landed.Details.CommitCount, maxAuditCommits+3)
	}
	if len(landed.Details.Commits) != maxAuditCommits {
		t.Errorf("len(Commits) = %d, want %d", len(landed.Details.Commits), maxAuditCommits)
	}
	if c := landed.Details.Commits[0]; c.ShortHash != "c3" || c.Subject != "feat: newest (aa01)" {
		t.Errorf("latest commit = %+v", c)
	}
	if !strings.Contains(landed.Message, `c3 "feat: newest (aa01)"`) || !strings.Contains(landed.Message, "+12 more") {
		t.Errorf("message = %q", landed.Message)
	}
	if landed.Details.Title != "Landed" || landed.Details.Status != "todo" {
		t.Errorf("details = %+v", landed.Details)
	}

	st := findings[1]
	if st.Kind != AuditStaleInProgress || st.Details.IdleDays != 20 {
		t.Errorf("stale finding = %+v", st)
	}
}

func TestAuditCommitSourceError(t *testing.T) {
	core, _ := setupTestCore(t)
	if err := core.Create(&bean.Bean{ID: "aa01", Title: "x", Status: "todo"}); err != nil {
		t.Fatal(err)
	}
	if err := core.Create(&bean.Bean{ID: "ep01", Title: "x", Type: "epic", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := core.Create(&bean.Bean{ID: "aa02", Title: "x", Status: "todo", Parent: "ep01"}); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	findings, err := core.Audit(AuditOptions{Commits: func(yield func(AuditCommit) bool) error {
		yield(commit("c1", "aa01"))
		return boom
	}})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	want := []auditSummary{
		{AuditContainerDoneChildrenOpen, "ep01,aa02"},
	}
	if got := summarize(findings); !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %v, want %v", got, want)
	}

	// Errors from IgnoreCommits are reported the same way.
	findings, err = core.Audit(AuditOptions{
		Commits:       commitSource(commit("c1", "aa01")),
		IgnoreCommits: func([]string) (map[string]bool, error) { return nil, boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := summarize(findings); !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %v, want %v", got, want)
	}
}

func TestAuditIgnoreCommits(t *testing.T) {
	tests := []struct {
		name      string
		ignore    []string
		wantIDs   []string
		wantCount map[string]int
	}{
		{name: "nothing ignored", ignore: nil, wantIDs: []string{"aa01", "aa02"}, wantCount: map[string]int{"aa01": 2, "aa02": 1}},
		{name: "bookkeeping commit ignored", ignore: []string{"c2"}, wantIDs: []string{"aa01"}, wantCount: map[string]int{"aa01": 1}},
		{name: "all ignored", ignore: []string{"c1", "c2"}, wantIDs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, _ := setupTestCore(t)
			for _, id := range []string{"aa01", "aa02"} {
				if err := core.Create(&bean.Bean{ID: id, Title: id, Status: "todo"}); err != nil {
					t.Fatal(err)
				}
			}
			c1, c2 := commit("c1", "feat: real work (aa01)"), commit("c2", "chore: create beans aa01 aa02")
			var gotHashes []string
			findings, err := core.Audit(AuditOptions{
				Commits: commitSource(c1, commit("c0", "unrelated"), c2),
				IgnoreCommits: func(hashes []string) (map[string]bool, error) {
					gotHashes = hashes
					ignored := map[string]bool{}
					for _, short := range tt.ignore {
						ignored[commit(short, "").Hash] = true
					}
					return ignored, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{c1.Hash, c2.Hash}; !reflect.DeepEqual(gotHashes, want) {
				t.Errorf("IgnoreCommits got %v, want only matching commits %v", gotHashes, want)
			}
			var ids []string
			for _, f := range findings {
				ids = append(ids, f.BeanIDs[0])
				if f.Details.CommitCount != tt.wantCount[f.BeanIDs[0]] || len(f.Details.Commits) != f.Details.CommitCount {
					t.Errorf("%s: commit count %d (%d listed), want %d", f.BeanIDs[0], f.Details.CommitCount, len(f.Details.Commits), tt.wantCount[f.BeanIDs[0]])
				}
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Errorf("findings for %v, want %v", ids, tt.wantIDs)
			}
		})
	}
}

func TestReferencedIDs(t *testing.T) {
	known := map[string]bool{"beans-abc1": true, "abc2": true}
	isKnown := func(s string) bool { return known[s] }

	tests := []struct {
		text string
		want []string
	}{
		{"Refs: beans-abc1", []string{"beans-abc1"}},
		{"feat: thing (beans-abc1)", []string{"beans-abc1"}},
		{"Fixes beans-abc1, closes abc2.", []string{"beans-abc1", "abc2"}},
		{"BEANS-ABC1", []string{"beans-abc1"}},
		{"beans-abc1 beans-abc1", []string{"beans-abc1"}},
		{"[beans-abc1]: trailing-hyphen beans-abc1-", []string{"beans-abc1"}},
		{"beans-abc12 xbeans-abc1 beans-abc1--slug.md abc2_x", nil},
		{"path/beans-abc1/file", []string{"beans-abc1"}},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := ReferencedIDs(tt.text, isKnown); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReferencedIDs(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
