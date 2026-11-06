package graph

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/internal/worktree"
)

// gitOpCounts counts the git status operations run via worktreeGitOps.
type gitOpCounts struct {
	detectBeanIDs, hasChanges, defaultBase, resolveCommits, aheadBehind, mergeConflicts atomic.Int32
}

func (c *gitOpCounts) total() int32 {
	return c.detectBeanIDs.Load() + c.hasChanges.Load() + c.defaultBase.Load() +
		c.resolveCommits.Load() + c.aheadBehind.Load() + c.mergeConflicts.Load()
}

// countGitOps wraps worktreeGitOps with counters for the duration of the test
// and starts with an empty commit-pair cache.
func countGitOps(t *testing.T) *gitOpCounts {
	t.Helper()
	orig := worktreeGitOps
	c := &gitOpCounts{}
	worktreeGitOps = gitStatusOps{
		detectBeanIDs: func(mgr *worktree.Manager, dir string) []string {
			c.detectBeanIDs.Add(1)
			return orig.detectBeanIDs(mgr, dir)
		},
		hasChanges: func(dir string) bool { c.hasChanges.Add(1); return orig.hasChanges(dir) },
		defaultBase: func(dir string) (string, bool) {
			c.defaultBase.Add(1)
			return orig.defaultBase(dir)
		},
		resolveCommits: func(dir string, revs ...string) ([]string, error) {
			c.resolveCommits.Add(1)
			return orig.resolveCommits(dir, revs...)
		},
		aheadBehind: func(dir, base string) (int, int, error) {
			c.aheadBehind.Add(1)
			return orig.aheadBehind(dir, base)
		},
		mergeConflicts: func(dir, ours, theirs string) (bool, error) {
			c.mergeConflicts.Add(1)
			return orig.mergeConflicts(dir, ours, theirs)
		},
	}
	commitStatusCache.reset()
	t.Cleanup(func() {
		worktreeGitOps = orig
		commitStatusCache.reset()
	})
	return c
}

func newGraphQLClient(r *Resolver) *client.Client {
	h := handler.New(NewExecutableSchema(Config{Resolvers: r}))
	h.AddTransport(transport.POST{})
	return client.New(h)
}

type worktreeStatusResult struct {
	ID                 string
	Branch             string
	Path               string
	HasChanges         bool
	HasUnmergedCommits bool
	CommitsBehind      int
	HasConflicts       bool
}

const allStatusQuery = `{ worktrees { id branch path hasChanges hasUnmergedCommits commitsBehind hasConflicts } }`

func queryWorktrees(t *testing.T, c *client.Client, query string) map[string]worktreeStatusResult {
	t.Helper()
	var resp struct{ Worktrees []worktreeStatusResult }
	c.MustPost(query, &resp)
	out := make(map[string]worktreeStatusResult, len(resp.Worktrees))
	for _, wt := range resp.Worktrees {
		out[wt.ID] = wt
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupStatusWorktrees creates worktrees in a variety of git states:
//
//	clean     — nothing changed (behind main by 1)
//	dirty     — unstaged change to a tracked file
//	untracked — an untracked file
//	staged    — a staged new file
//	ahead     — one commit not on main, no conflict
//	conflict  — one commit conflicting with main
func setupStatusWorktrees(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	r, _, repo := setupWorktreeResolver(t)
	r.ProjectRoot = repo
	writeFile(t, filepath.Join(repo, "shared.txt"), "base\n")
	gitCmd(t, repo, "add", "shared.txt")
	gitCmd(t, repo, "commit", "-m", "shared")

	paths := map[string]string{}
	for _, id := range []string{"clean", "dirty", "untracked", "staged", "ahead", "conflict"} {
		wt, err := r.Mutation().CreateWorktree(context.Background(), id, nil)
		if err != nil {
			t.Fatalf("CreateWorktree(%s): %v", id, err)
		}
		// The worktree list reports symlink-resolved paths (macOS /var → /private/var).
		if paths[id], err = filepath.EvalSymlinks(wt.Path); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(paths["dirty"], "shared.txt"), "dirty\n")
	writeFile(t, filepath.Join(paths["untracked"], "new.txt"), "new\n")
	writeFile(t, filepath.Join(paths["staged"], "staged.txt"), "staged\n")
	gitCmd(t, paths["staged"], "add", "staged.txt")
	writeFile(t, filepath.Join(paths["ahead"], "ahead.txt"), "ahead\n")
	gitCmd(t, paths["ahead"], "add", "ahead.txt")
	gitCmd(t, paths["ahead"], "commit", "-m", "ahead")
	writeFile(t, filepath.Join(paths["conflict"], "shared.txt"), "branch\n")
	gitCmd(t, paths["conflict"], "commit", "-am", "branch change")

	// Main moves on: every worktree is one commit behind, "conflict" conflicts.
	writeFile(t, filepath.Join(repo, "shared.txt"), "main\n")
	gitCmd(t, repo, "commit", "-am", "main change")
	return r, paths
}

func TestWorktreesQueryStatusFields(t *testing.T) {
	r, paths := setupStatusWorktrees(t)
	got := queryWorktrees(t, newGraphQLClient(r), allStatusQuery)

	want := map[string]worktreeStatusResult{
		"clean":     {CommitsBehind: 1},
		"dirty":     {HasChanges: true, CommitsBehind: 1},
		"untracked": {HasChanges: true, CommitsBehind: 1},
		"staged":    {HasChanges: true, CommitsBehind: 1},
		"ahead":     {HasUnmergedCommits: true, CommitsBehind: 1},
		"conflict":  {HasUnmergedCommits: true, CommitsBehind: 1, HasConflicts: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d worktrees, want %d: %+v", len(got), len(want), got)
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Errorf("worktree %s missing", id)
			continue
		}
		if g.HasChanges != w.HasChanges || g.HasUnmergedCommits != w.HasUnmergedCommits ||
			g.CommitsBehind != w.CommitsBehind || g.HasConflicts != w.HasConflicts {
			t.Errorf("%s: got %+v, want %+v", id, g, w)
		}
		// The status must match the original per-field gitutil checks.
		legacy := worktreeStatusResult{
			HasChanges:         gitutil.HasChanges(paths[id]),
			HasUnmergedCommits: gitutil.HasUnmergedCommits(paths[id], "main"),
			CommitsBehind:      gitutil.CommitsBehind(paths[id], "main"),
			HasConflicts:       gitutil.HasConflicts(paths[id], "main"),
		}
		if g.HasChanges != legacy.HasChanges || g.HasUnmergedCommits != legacy.HasUnmergedCommits ||
			g.CommitsBehind != legacy.CommitsBehind || g.HasConflicts != legacy.HasConflicts {
			t.Errorf("%s: got %+v, legacy gitutil checks give %+v", id, g, legacy)
		}
	}
}

func TestWorktreesQueryGitWorkOnlyForSelectedFields(t *testing.T) {
	r, _ := setupStatusWorktrees(t)
	c := newGraphQLClient(r)
	counts := countGitOps(t)
	n := int32(6)

	t.Run("id-only query runs no git commands beyond the worktree list", func(t *testing.T) {
		var resp struct{ Worktrees []map[string]any }
		c.MustPost(`{ worktrees { id name description branch path setupStatus setupError pullRequest { number } } }`, &resp)
		if len(resp.Worktrees) != int(n) {
			t.Fatalf("got %d worktrees, want %d", len(resp.Worktrees), n)
		}
		if total := counts.total(); total != 0 {
			t.Errorf("expected no git status commands or bean detection, got %d", total)
		}
	})

	t.Run("beans runs only bean detection", func(t *testing.T) {
		var resp struct{ Worktrees []map[string]any }
		c.MustPost(`{ worktrees { id beans { id } } }`, &resp)
		if got := counts.detectBeanIDs.Load(); got != n {
			t.Errorf("bean detection ran %d times, want %d", got, n)
		}
		if other := counts.total() - counts.detectBeanIDs.Load(); other != 0 {
			t.Errorf("expected only bean detection, got %d other commands", other)
		}
		counts.detectBeanIDs.Store(0)
	})

	t.Run("hasChanges runs only git status", func(t *testing.T) {
		queryWorktrees(t, c, `{ worktrees { id hasChanges } }`)
		if got := counts.hasChanges.Load(); got != n {
			t.Errorf("hasChanges ran %d times, want %d", got, n)
		}
		if other := counts.total() - counts.hasChanges.Load(); other != 0 {
			t.Errorf("expected only hasChanges, got %d other commands", other)
		}
	})

	t.Run("all fields share one rev-list and one merge-tree per commit pair", func(t *testing.T) {
		before := counts.hasChanges.Load()
		queryWorktrees(t, c, allStatusQuery)
		if got := counts.hasChanges.Load() - before; got != n {
			t.Errorf("hasChanges ran %d times, want %d", got, n)
		}
		if got := counts.resolveCommits.Load(); got != n {
			t.Errorf("rev-parse ran %d times, want %d", got, n)
		}
		// clean/dirty/untracked/staged all sit on the same commit as when
		// they were created, so they share one (HEAD, main) pair: 3 pairs.
		if got := counts.aheadBehind.Load(); got != 3 {
			t.Errorf("rev-list ran %d times, want 3", got)
		}
		if got := counts.mergeConflicts.Load(); got != 3 {
			t.Errorf("merge-tree ran %d times, want 3", got)
		}
	})

	t.Run("repeat query reuses commit-pair results but rechecks working trees", func(t *testing.T) {
		hc, ab, mc := counts.hasChanges.Load(), counts.aheadBehind.Load(), counts.mergeConflicts.Load()
		queryWorktrees(t, c, allStatusQuery)
		if got := counts.hasChanges.Load() - hc; got != n {
			t.Errorf("hasChanges ran %d times, want %d (never cached)", got, n)
		}
		if counts.aheadBehind.Load() != ab || counts.mergeConflicts.Load() != mc {
			t.Error("expected cached rev-list/merge-tree results for unchanged commits")
		}
	})
}

// TestWorktreeQuery checks the single-worktree query: it computes the status
// of that worktree only.
func TestWorktreeQuery(t *testing.T) {
	r, _ := setupStatusWorktrees(t)
	c := newGraphQLClient(r)
	counts := countGitOps(t)

	var resp struct {
		Worktree *worktreeStatusResult
	}
	c.MustPost(`{ worktree(id: "dirty") { id hasChanges hasUnmergedCommits } }`, &resp)
	if resp.Worktree == nil || resp.Worktree.ID != "dirty" || !resp.Worktree.HasChanges || resp.Worktree.HasUnmergedCommits {
		t.Fatalf("worktree(dirty) = %+v", resp.Worktree)
	}
	if got := counts.hasChanges.Load(); got != 1 {
		t.Errorf("hasChanges ran %d times, want 1", got)
	}
	if got := counts.detectBeanIDs.Load(); got != 0 {
		t.Errorf("bean detection ran %d times, want 0", got)
	}

	c.MustPost(`{ worktree(id: "ahead") { id hasChanges hasUnmergedCommits } }`, &resp)
	if resp.Worktree == nil || resp.Worktree.HasChanges || !resp.Worktree.HasUnmergedCommits {
		t.Errorf("worktree(ahead) = %+v", resp.Worktree)
	}

	var missing struct{ Worktree *worktreeStatusResult }
	c.MustPost(`{ worktree(id: "nope") { id } }`, &missing)
	if missing.Worktree != nil {
		t.Errorf("worktree(nope) = %+v, want null", missing.Worktree)
	}
}

func TestWorktreesQueryStatusFollowsNewCommits(t *testing.T) {
	r, paths := setupStatusWorktrees(t)
	c := newGraphQLClient(r)
	countGitOps(t)

	if got := queryWorktrees(t, c, allStatusQuery)["clean"]; got.HasUnmergedCommits || got.HasChanges {
		t.Fatalf("clean worktree: %+v", got)
	}

	// Edit, then commit, a conflicting change in "clean": the cached status
	// of its old commit pair must not be reused.
	writeFile(t, filepath.Join(paths["clean"], "shared.txt"), "other\n")
	if got := queryWorktrees(t, c, allStatusQuery)["clean"]; !got.HasChanges {
		t.Errorf("expected uncommitted changes to show up: %+v", got)
	}
	gitCmd(t, paths["clean"], "commit", "-am", "conflicting")
	got := queryWorktrees(t, c, allStatusQuery)["clean"]
	want := worktreeStatusResult{ID: "clean", Branch: got.Branch, Path: got.Path,
		HasUnmergedCommits: true, CommitsBehind: 1, HasConflicts: true}
	if got != want {
		t.Errorf("after commit: got %+v, want %+v", got, want)
	}
}

func TestWorktreeGitStatusWithoutBaseRef(t *testing.T) {
	_, paths := setupStatusWorktrees(t)
	countGitOps(t)

	// No base ref and no origin remote: nothing to compare against.
	s := newWorktreeGitStatus(paths["conflict"], "")
	ctx := context.Background()
	if s.HasUnmergedCommits(ctx) || s.CommitsBehind(ctx) != 0 || s.HasConflicts(ctx) {
		t.Error("expected zero commit status without a base branch")
	}
	if !newWorktreeGitStatus(paths["dirty"], "").HasChanges(ctx) {
		t.Error("expected hasChanges without a base branch")
	}

	// Matches the legacy helpers.
	if gitutil.CommitsBehind(paths["conflict"], "") != 0 || gitutil.HasConflicts(paths["conflict"], "") {
		t.Error("legacy helpers disagree")
	}
}

func TestWorkspaceStatusesAndBranchStatus(t *testing.T) {
	r, paths := setupStatusWorktrees(t)
	ctx := context.Background()

	conflictPath := paths["conflict"]
	bs, err := r.Query().BranchStatus(ctx, &conflictPath)
	if err != nil {
		t.Fatal(err)
	}
	if bs.CommitsBehind != 1 || !bs.HasConflicts {
		t.Errorf("branchStatus: got %+v", bs)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := r.Subscription().WorkspaceStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statuses := <-ch
	got := map[string][2]bool{}
	for _, s := range statuses {
		got[s.ID] = [2]bool{s.HasChanges, s.HasUnmergedCommits}
	}
	want := map[string][2]bool{
		CentralSessionID: {false, false},
		"clean":          {false, false},
		"dirty":          {true, false},
		"untracked":      {true, false},
		"staged":         {true, false},
		"ahead":          {false, true},
		"conflict":       {false, true},
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("workspaceStatuses[%s] = %v, want %v", id, got[id], w)
		}
	}
}
