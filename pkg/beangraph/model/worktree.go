package model

import (
	"context"

	"github.com/hmans/beans/pkg/bean"
)

// Worktree is a git worktree (workspace) as exposed via GraphQL.
//
// It is hand-written (bound in gqlgen.yml) rather than generated so that the
// git status fields (hasChanges, hasUnmergedCommits, commitsBehind,
// hasConflicts) can be served by field resolvers backed by GitStatus: they
// shell out to git, so they are only computed when a query selects them.
type Worktree struct {
	// Unique worktree identifier
	ID string `json:"id"`
	// Human-readable name
	Name *string `json:"name,omitempty"`
	// Auto-generated summary of what this workspace is doing
	Description *string `json:"description,omitempty"`
	// Git branch name
	Branch string `json:"branch"`
	// Filesystem path to the worktree
	Path string `json:"path"`
	// Beans detected from changes in this worktree vs the base branch
	Beans []*bean.Bean `json:"beans"`
	// Post-creation setup status (null if no setup configured)
	SetupStatus *WorktreeSetupStatus `json:"setupStatus,omitempty"`
	// Error message if setup failed
	SetupError *string `json:"setupError,omitempty"`
	// Open pull/merge request for this worktree's branch (null if none)
	PullRequest *PullRequest `json:"pullRequest,omitempty"`

	// GitStatus lazily computes the git status fields. If nil, they resolve
	// to false/0.
	GitStatus WorktreeGitStatus `json:"-"`
}

// WorktreeGitStatus computes a worktree's git status on demand.
// Implementations compute each value at most once and must be safe for
// concurrent use, since GraphQL resolves fields concurrently.
type WorktreeGitStatus interface {
	// HasChanges reports uncommitted changes or untracked files.
	HasChanges(ctx context.Context) bool
	// HasUnmergedCommits reports commits not yet in the base branch.
	HasUnmergedCommits(ctx context.Context) bool
	// CommitsBehind counts base-branch commits missing from the worktree branch.
	CommitsBehind(ctx context.Context) int
	// HasConflicts reports whether rebasing onto the base branch would conflict.
	HasConflicts(ctx context.Context) bool
}
