package worktree

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// branchPrefix is the branch prefix of workspaces created by the git backend.
const branchPrefix = "beans/"

// gitBackend manages worktrees with plain git. Workspaces are the worktrees
// on beans/<id> branches, created under the beans worktree root.
type gitBackend struct{}

func (gitBackend) Kind() BackendKind { return BackendGit }

func (gitBackend) workspaces(entries []Entry, worktreeRoot string, _ func(string) string) []Worktree {
	var worktrees []Worktree
	for i, e := range entries {
		// The main worktree (always listed first) is never a workspace.
		if i == 0 || e.Prunable || e.Bare {
			continue
		}
		switch {
		case strings.HasPrefix(e.Branch, branchPrefix):
			// Normal case: the worktree is on a beans/ branch.
			worktrees = append(worktrees, Worktree{
				ID:     strings.TrimPrefix(e.Branch, branchPrefix),
				Branch: e.Branch,
				Path:   e.Path,
			})
		case e.Detached && worktreeRoot != "" && strings.HasPrefix(e.Path, worktreeRoot):
			// Detached HEAD (e.g. during rebase): identify by path.
			id := filepath.Base(e.Path)
			worktrees = append(worktrees, Worktree{
				ID:       id,
				Branch:   branchPrefix + id,
				Path:     e.Path,
				Detached: true,
			})
		}
	}
	return worktrees
}

func (gitBackend) create(req createRequest) (string, string, error) {
	branch := branchPrefix + req.name
	worktreePath := filepath.Join(req.worktreeRoot, req.name)

	if _, err := os.Stat(worktreePath); err == nil {
		return "", "", fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	args := []string{"worktree", "add", worktreePath, "-b", branch}
	if req.baseRef != "" {
		args = append(args, req.baseRef)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = req.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[worktree] failed to create worktree %s at %s: %s: %v", req.name, worktreePath, strings.TrimSpace(string(out)), err)
		return "", "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return branch, worktreePath, nil
}

func (gitBackend) creationHooks(string, string, *Worktree) (*CreationHooks, error) {
	return nil, nil
}

func (gitBackend) remove(repoRoot string, wt *Worktree) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
