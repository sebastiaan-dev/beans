package graph

import (
	"context"

	"github.com/hmans/beans/internal/agent"
	"github.com/hmans/beans/internal/portalloc"
	"github.com/hmans/beans/internal/terminal"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/forge"
)

//go:generate go tool gqlgen generate

// CentralSessionID is the special session identifier for the central agent chat
// that runs in the project root (not a worktree).
const CentralSessionID = "__central__"

// RunSessionSuffix is appended to workspace IDs to form the terminal session ID
// for run command sessions (e.g., "worktree-abc__run").
const RunSessionSuffix = "__run"

// Resolver is the root resolver for the GraphQL schema.
// It embeds CoreResolver for bean CRUD operations and adds UI-specific
// concerns (agents, worktrees, terminals, git operations).
type Resolver struct {
	*beangraph.CoreResolver
	WorktreeMgr *worktree.Manager
	AgentMgr    *agent.Manager
	TerminalMgr *terminal.Manager
	PortAlloc   *portalloc.Allocator
	Forge       forge.Provider // git forge provider (GitHub, GitLab, etc.) — nil if not detected
	ProjectRoot string         // absolute path to the project root (parent of .beans)
}

// populatePRsBatch fetches PR data for multiple worktrees in a single batch query
// and sets the results on the corresponding models.
func populatePRsBatch(ctx context.Context, worktrees []*model.Worktree, forgeProvider forge.Provider, repoDir string) {
	if forgeProvider == nil || len(worktrees) == 0 {
		return
	}

	branches := make([]string, len(worktrees))
	for i, wt := range worktrees {
		branches[i] = wt.Branch
	}

	prs, err := forgeProvider.FindPRs(ctx, repoDir, branches)
	if err != nil {
		return
	}

	for _, wt := range worktrees {
		if pr, ok := prs[wt.Branch]; ok {
			wt.PullRequest = forgePRToModel(pr)
		}
	}
}

// forgePRToModel converts a forge PullRequest to a GraphQL model PullRequest.
func forgePRToModel(pr *forge.PullRequest) *model.PullRequest {
	return &model.PullRequest{
		Number:         pr.Number,
		Title:          pr.Title,
		State:          pr.State,
		URL:            pr.URL,
		IsDraft:        pr.IsDraft,
		CheckStatus:    string(pr.Checks),
		ReviewApproved: pr.ReviewApproved,
		Mergeable:      pr.Mergeable,
	}
}
