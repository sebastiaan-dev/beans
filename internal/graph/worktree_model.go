package graph

import (
	"context"
	"runtime"
	"sync"

	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// worktreeToModel converts an internal worktree to a GraphQL model.
// It takes an optional beancore.Core to resolve BeanIDs into full Bean objects.
//
// The git status fields are not computed here: the model gets a lazy
// worktreeGitStatus, which the Worktree field resolvers only consult when a
// query selects those fields.
func worktreeToModel(wt *worktree.Worktree, core *beancore.Core, baseRef string) *model.Worktree {
	m := &model.Worktree{
		ID:        wt.ID,
		Branch:    wt.Branch,
		Path:      wt.Path,
		Beans:     []*bean.Bean{},
		GitStatus: newWorktreeGitStatus(wt.Path, baseRef),
	}
	if wt.Name != "" {
		m.Name = &wt.Name
	}
	if wt.Description != "" {
		m.Description = &wt.Description
	}
	if core != nil {
		for _, id := range wt.BeanIDs {
			if b, err := core.Get(id); err == nil {
				m.Beans = append(m.Beans, b)
			}
		}
	}
	// Map setup status
	switch wt.Setup {
	case worktree.SetupRunning:
		s := model.WorktreeSetupStatusRunning
		m.SetupStatus = &s
	case worktree.SetupDone:
		s := model.WorktreeSetupStatusDone
		m.SetupStatus = &s
	case worktree.SetupFailed:
		s := model.WorktreeSetupStatusFailed
		m.SetupStatus = &s
	}
	if wt.SetupError != "" {
		m.SetupError = &wt.SetupError
	}
	return m
}

// gitStatusOps are the git commands behind worktree status. Tests replace
// worktreeGitOps to count or fake them.
type gitStatusOps struct {
	hasChanges     func(dir string) bool
	defaultBase    func(dir string) (string, bool)
	resolveCommits func(dir string, revs ...string) ([]string, error)
	aheadBehind    func(dir, baseRef string) (ahead, behind int, err error)
	mergeConflicts func(dir, ours, theirs string) (bool, error)
}

var worktreeGitOps = gitStatusOps{
	hasChanges: gitutil.HasChanges,
	defaultBase: func(dir string) (string, bool) {
		return gitutil.DefaultRemoteBranch(dir, "origin")
	},
	resolveCommits: gitutil.ResolveCommits,
	aheadBehind:    gitutil.AheadBehind,
	mergeConflicts: gitutil.MergeConflicts,
}

// gitStatusSlots bounds the git processes run concurrently for status
// queries, which resolve every worktree (and every status field) in parallel.
var gitStatusSlots = make(chan struct{}, max(4, runtime.NumCPU()))

// withGitSlot runs f while holding one of the gitStatusSlots.
func withGitSlot(f func()) {
	gitStatusSlots <- struct{}{}
	defer func() { <-gitStatusSlots }()
	f()
}

// runConcurrently runs fns in parallel and waits for all of them.
func runConcurrently(fns ...func()) {
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	wg.Wait()
}

// worktreeGitStatus implements model.WorktreeGitStatus for a worktree
// directory. Every value is computed at most once, on first use, so the
// fields of one GraphQL object share the work:
//
//   - hasChanges: one `git status --porcelain`
//   - hasUnmergedCommits and commitsBehind: one `git rev-list --left-right --count`
//   - hasConflicts: one `git merge-tree`
//
// The commit-derived values only depend on the commits HEAD and the base
// branch point to, so they are computed against the resolved SHAs and shared
// via commitStatusCache.
type worktreeGitStatus struct {
	dir     string
	baseRef string
	ops     gitStatusOps

	changesOnce sync.Once
	changes     bool

	pairOnce sync.Once
	pair     *commitPairStatus // nil if HEAD or the base branch can't be resolved
}

var _ model.WorktreeGitStatus = (*worktreeGitStatus)(nil)

func newWorktreeGitStatus(dir, baseRef string) *worktreeGitStatus {
	return &worktreeGitStatus{dir: dir, baseRef: baseRef, ops: worktreeGitOps}
}

func (s *worktreeGitStatus) HasChanges(context.Context) bool {
	s.changesOnce.Do(func() {
		withGitSlot(func() { s.changes = s.ops.hasChanges(s.dir) })
	})
	return s.changes
}

func (s *worktreeGitStatus) HasUnmergedCommits(context.Context) bool {
	// Without a configured base ref there is nothing to be ahead of (this
	// matches gitutil.HasUnmergedCommits; only behind/conflicts fall back to
	// the remote's default branch).
	if s.baseRef == "" {
		return false
	}
	ahead, _ := s.commitPair().aheadBehind(s.dir, s.ops)
	return ahead > 0
}

func (s *worktreeGitStatus) CommitsBehind(context.Context) int {
	_, behind := s.commitPair().aheadBehind(s.dir, s.ops)
	return behind
}

func (s *worktreeGitStatus) HasConflicts(context.Context) bool {
	return s.commitPair().hasConflicts(s.dir, s.ops)
}

// commitPair resolves HEAD and the base branch (the configured base ref, or
// the remote's default branch) to commit SHAs and returns their shared status.
func (s *worktreeGitStatus) commitPair() *commitPairStatus {
	s.pairOnce.Do(func() {
		base := s.baseRef
		if base == "" {
			var ok bool
			withGitSlot(func() { base, ok = s.ops.defaultBase(s.dir) })
			if !ok {
				return
			}
		}
		var shas []string
		var err error
		withGitSlot(func() { shas, err = s.ops.resolveCommits(s.dir, "HEAD", base) })
		if err != nil {
			return
		}
		s.pair = commitStatusCache.get(commitPairKey{head: shas[0], base: shas[1]})
	})
	return s.pair
}

// commitPairKey identifies a (HEAD, base) pair of commit SHAs.
type commitPairKey struct{ head, base string }

// commitPairStatus holds the ahead/behind counts and conflict state of a
// (HEAD, base) commit pair. Commits are immutable, so once computed these
// values never go stale; only successful results are kept, so a failing git
// command is retried next time.
type commitPairStatus struct {
	key commitPairKey

	countsMu      sync.Mutex
	countsDone    bool
	ahead, behind int

	conflictsMu   sync.Mutex
	conflictsDone bool
	conflicts     bool
}

// aheadBehind returns the commits HEAD is ahead of / behind the base. A nil
// receiver (unresolvable commits) yields 0, 0.
func (p *commitPairStatus) aheadBehind(dir string, ops gitStatusOps) (int, int) {
	if p == nil {
		return 0, 0
	}
	p.countsMu.Lock()
	defer p.countsMu.Unlock()
	if !p.countsDone {
		var ahead, behind int
		var err error
		withGitSlot(func() { ahead, behind, err = ops.aheadBehind(dir, p.key.base) })
		if err != nil {
			return 0, 0
		}
		p.ahead, p.behind, p.countsDone = ahead, behind, true
	}
	return p.ahead, p.behind
}

// hasConflicts reports whether rebasing HEAD onto the base would conflict. A
// nil receiver (unresolvable commits) yields false.
func (p *commitPairStatus) hasConflicts(dir string, ops gitStatusOps) bool {
	if p == nil {
		return false
	}
	p.conflictsMu.Lock()
	defer p.conflictsMu.Unlock()
	if !p.conflictsDone {
		var conflicts bool
		var err error
		withGitSlot(func() { conflicts, err = ops.mergeConflicts(dir, p.key.base, p.key.head) })
		if err != nil {
			return false
		}
		p.conflicts, p.conflictsDone = conflicts, true
	}
	return p.conflicts
}

// commitPairCacheLimit caps commitStatusCache; when exceeded the cache is
// emptied (entries are cheap to recompute and the working set — one pair per
// worktree and base commit — is tiny).
const commitPairCacheLimit = 1024

// commitStatusCache shares commit-pair status across requests, worktrees and
// subscription refreshes.
var commitStatusCache = &commitPairCache{entries: map[commitPairKey]*commitPairStatus{}}

type commitPairCache struct {
	mu      sync.Mutex
	entries map[commitPairKey]*commitPairStatus
}

func (c *commitPairCache) get(key commitPairKey) *commitPairStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.entries[key]; ok {
		return p
	}
	if len(c.entries) >= commitPairCacheLimit {
		c.entries = map[commitPairKey]*commitPairStatus{}
	}
	p := &commitPairStatus{key: key}
	c.entries[key] = p
	return p
}

func (c *commitPairCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[commitPairKey]*commitPairStatus{}
}
