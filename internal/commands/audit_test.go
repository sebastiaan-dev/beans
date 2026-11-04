package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

// setupAuditCore creates a project directory (optionally a git repo with the
// given commit messages on main) containing a .beans dir with the given beans.
func setupAuditCore(t *testing.T, gitRepo bool, commits []string, beans ...*bean.Bean) *beancore.Core {
	t.Helper()
	dir := t.TempDir()
	if gitRepo {
		run := func(args ...string) {
			t.Helper()
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		run("init", "-q", "-b", "main")
		run("config", "user.email", "test@test.com")
		run("config", "user.name", "Test")
		for _, msg := range commits {
			run("commit", "-q", "--allow-empty", "-m", msg)
		}
	}

	beansDir := filepath.Join(dir, ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := beancore.New(beansDir, config.Default())
	c.SetWarnWriter(nil)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	for _, b := range beans {
		if b.Title == "" {
			b.Title = "Title " + b.ID
		}
		if err := c.Create(b); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestRunAudit(t *testing.T) {
	newBeans := func() []*bean.Bean {
		return []*bean.Bean{
			{ID: "ab01", Status: "todo"},
			{ID: "ep01", Type: "epic", Status: "completed"},
			{ID: "ab02", Status: "todo", Parent: "ep01"},
		}
	}
	commits := []string{"initial", "feat: done (Refs: ab01)"}

	tests := []struct {
		name      string
		gitRepo   bool
		params    auditParams
		wantKinds []beancore.AuditKind
		wantNote  string
		wantGit   bool
	}{
		{
			name:      "git repo with base ref",
			gitRepo:   true,
			params:    auditParams{BaseRef: "main", StaleDays: 14},
			wantKinds: []beancore.AuditKind{beancore.AuditLandedButOpen, beancore.AuditContainerDoneChildrenOpen},
			wantGit:   true,
		},
		{
			name:      "missing base ref",
			gitRepo:   true,
			params:    auditParams{BaseRef: "origin/main", StaleDays: 14},
			wantKinds: []beancore.AuditKind{beancore.AuditContainerDoneChildrenOpen},
			wantNote:  `base ref "origin/main" not found`,
		},
		{
			name:      "not a git repo",
			gitRepo:   false,
			params:    auditParams{BaseRef: "main", StaleDays: 14},
			wantKinds: []beancore.AuditKind{beancore.AuditContainerDoneChildrenOpen},
			wantNote:  "not a git repository",
		},
		{
			name:      "no-git flag",
			gitRepo:   true,
			params:    auditParams{BaseRef: "main", StaleDays: 14, NoGit: true},
			wantKinds: []beancore.AuditKind{beancore.AuditContainerDoneChildrenOpen},
			wantNote:  "--no-git",
		},
		{
			name:      "since excludes old commits",
			gitRepo:   true,
			params:    auditParams{BaseRef: "main", Since: "2099-12-31T00:00:00", StaleDays: 14},
			wantKinds: []beancore.AuditKind{beancore.AuditContainerDoneChildrenOpen},
			wantGit:   true,
		},
		{
			name:      "stale check disabled note",
			gitRepo:   false,
			params:    auditParams{BaseRef: "main", StaleDays: 0},
			wantKinds: []beancore.AuditKind{beancore.AuditContainerDoneChildrenOpen},
			wantNote:  "stale-in-progress skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupAuditCore(t, tt.gitRepo, commits, newBeans()...)
			tt.params.Now = time.Now()
			result, err := runAudit(c, tt.params)
			if err != nil {
				t.Fatal(err)
			}

			var kinds []beancore.AuditKind
			for _, f := range result.Findings {
				kinds = append(kinds, f.Kind)
			}
			if !reflect.DeepEqual(kinds, tt.wantKinds) {
				t.Errorf("kinds = %v, want %v", kinds, tt.wantKinds)
			}
			if result.Count != len(tt.wantKinds) || result.Clean != (len(tt.wantKinds) == 0) {
				t.Errorf("count/clean = %d/%v", result.Count, result.Clean)
			}
			if tt.wantNote != "" && !strings.Contains(strings.Join(result.Notes, "\n"), tt.wantNote) {
				t.Errorf("notes = %v, want one containing %q", result.Notes, tt.wantNote)
			}
			if (result.Git != nil) != tt.wantGit {
				t.Errorf("git info = %+v, want present=%v", result.Git, tt.wantGit)
			}
		})
	}
}

func TestRunAuditLandedDetails(t *testing.T) {
	c := setupAuditCore(t, true, []string{"initial", "feat: done (Refs: ab01)"}, &bean.Bean{ID: "ab01", Status: "in-progress"})
	result, err := runAudit(c, auditParams{BaseRef: "main", StaleDays: 14, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Git == nil || result.Git.CommitsScanned != 2 {
		t.Fatalf("git info = %+v, want 2 commits scanned", result.Git)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %+v", result.Findings)
	}
	f := result.Findings[0]
	if len(f.Details.Commits) != 1 || f.Details.Commits[0].Subject != "feat: done (Refs: ab01)" || f.Details.Commits[0].ShortHash == "" {
		t.Errorf("commits = %+v", f.Details.Commits)
	}

	// JSON shape: stable keys for machine consumers.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"clean", "count", "findings", "notes", "git"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("JSON missing key %q: %s", key, data)
		}
	}
	finding := decoded["findings"].([]any)[0].(map[string]any)
	if finding["kind"] != "landed-but-open" || !reflect.DeepEqual(finding["bean_ids"], []any{"ab01"}) || finding["message"] == "" {
		t.Errorf("finding JSON = %v", finding)
	}
}

func TestPrintAuditReport(t *testing.T) {
	tests := []struct {
		name   string
		result auditResult
		want   []string
	}{
		{
			name:   "clean",
			result: auditResult{Clean: true, Findings: []beancore.AuditFinding{}, Notes: []string{"landed-but-open skipped: not a git repository"}},
			want:   []string{"not a git repository", "No audit findings"},
		},
		{
			name: "grouped findings",
			result: auditResult{
				Count: 2,
				Findings: []beancore.AuditFinding{
					{Kind: beancore.AuditStaleInProgress, BeanIDs: []string{"a"}, Message: "a is stale", Details: beancore.AuditDetails{Title: "Alpha"}},
					{Kind: beancore.AuditBlockedByDone, BeanIDs: []string{"b", "c"}, Message: "b is unblocked"},
				},
				Git: &auditGitInfo{BaseRef: "main", CommitsScanned: 7},
			},
			want: []string{"[stale-in-progress]", "a is stale", "Alpha", "[blocked-by-done]", "b is unblocked", "--remove-blocked-by", "Scanned 7 commit(s) on main", "2 findings"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printAuditReport(&buf, tt.result)
			out := buf.String()
			last := -1
			for _, w := range tt.want {
				idx := strings.Index(out, w)
				if idx < 0 {
					t.Errorf("output missing %q:\n%s", w, out)
					continue
				}
				if idx < last {
					t.Errorf("%q out of order:\n%s", w, out)
				}
				last = idx
			}
		})
	}
}

func TestAuditKindsHaveTitlesAndHints(t *testing.T) {
	for _, k := range beancore.AuditKinds {
		if auditKindTitles[k] == "" || auditKindHints[k] == "" {
			t.Errorf("kind %s missing title or hint", k)
		}
	}
}

func TestBookkeepingCommits(t *testing.T) {
	paths := map[string][]string{
		"beans":  {".beans/a.md", ".beans/archive/b.md"},
		"mixed":  {".beans/a.md", "main.go"},
		"code":   {"main.go"},
		"empty":  {},
		"prefix": {".beansx/a.md"},
	}
	tests := []struct {
		rules []string
		want  map[string]bool
	}{
		{[]string{".beans"}, map[string]bool{"beans": true}},
		{[]string{".beans/"}, map[string]bool{"beans": true}},
		{[]string{".beans", "main.go"}, map[string]bool{"beans": true, "mixed": true, "code": true}},
		{[]string{"docs/.beans"}, map[string]bool{}},
		{nil, map[string]bool{}},
	}
	for _, tt := range tests {
		if got := bookkeepingCommits(paths, tt.rules); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("bookkeepingCommits(%q) = %v, want %v", tt.rules, got, tt.want)
		}
	}
}

func TestRunAuditIgnoresBookkeepingCommits(t *testing.T) {
	c := setupAuditCore(t, true, []string{"initial"},
		&bean.Bean{ID: "ab01", Status: "todo"},
		&bean.Bean{ID: "ab02", Status: "todo"},
	)
	dir := filepath.Dir(c.Root())
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("add", ".beans")
	git("commit", "-q", "-m", "chore: create beans ab01 and ab02")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "main.go")
	git("commit", "-q", "-m", "feat: implement it (Refs: ab02)")

	cfgFile := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(cfgFile, []byte("beans:\n  path: .beans\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", config.ConfigFileName)
	if err := os.WriteFile(filepath.Join(c.Root(), "ab01--x.md"), []byte("---\ntitle: x\nstatus: todo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".beans")
	git("commit", "-q", "-m", "chore: tweak config and ab01")

	result, err := runAudit(c, auditParams{BaseRef: "main", StaleDays: 14, Now: time.Now(), BookkeepingPaths: []string{c.Root(), cfgFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].BeanIDs[0] != "ab02" || result.Findings[0].Details.CommitCount != 1 {
		t.Fatalf("findings = %+v, want only ab02 with 1 commit", result.Findings)
	}
	if result.Git.BookkeepingCommits != 2 {
		t.Errorf("bookkeeping commits = %d, want 2", result.Git.BookkeepingCommits)
	}

	// Without bookkeeping paths every referencing commit counts.
	result, err = runAudit(c, auditParams{BaseRef: "main", StaleDays: 14, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 2 {
		t.Errorf("findings without bookkeeping paths = %+v, want 2", result.Findings)
	}
}

func TestRepoRelativePaths(t *testing.T) {
	c := setupAuditCore(t, true, []string{"initial"})
	dir := filepath.Dir(c.Root())
	t.Chdir(dir)
	got := repoRelativePaths(".beans", []string{".beans", filepath.Join(dir, ".beans.yml"), dir, t.TempDir()})
	if want := []string{".beans", ".beans.yml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("repoRelativePaths = %v, want %v", got, want)
	}
	if got := repoRelativePaths(t.TempDir(), []string{dir}); got != nil {
		t.Errorf("outside a repo: %v, want nil", got)
	}
}
