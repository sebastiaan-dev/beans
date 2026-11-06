package graph

import (
	"os"
	"strings"
	"testing"

	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/forge"
)

func TestPrPrompt_NoPR(t *testing.T) {
	ctx := actionContext{ForgeCLI: "gh", PullRequest: nil}
	prompt := prPrompt(ctx)

	if !strings.Contains(prompt, "Create a pull request") {
		t.Error("expected 'Create a pull request' in prompt for no-PR state")
	}
	if strings.Contains(prompt, "Merge") {
		t.Error("prompt should not mention merging when no PR exists")
	}
}

func TestPrPrompt_HasLocalChanges(t *testing.T) {
	ctx := actionContext{
		ForgeCLI:    "gh",
		PullRequest: &forge.PullRequest{Checks: forge.CheckStatusPass},
		HasChanges:  true,
	}
	prompt := prPrompt(ctx)

	if !strings.Contains(prompt, "Update the existing pull request") {
		t.Error("expected 'Update the existing pull request' in prompt")
	}
	if !strings.Contains(prompt, "Do NOT merge the PR") {
		t.Error("expected 'Do NOT merge' guardrail in update prompt")
	}
}

func TestPrPrompt_HasUnpushedCommits(t *testing.T) {
	ctx := actionContext{
		ForgeCLI:           "gh",
		PullRequest:        &forge.PullRequest{Checks: forge.CheckStatusPass},
		HasUnpushedCommits: true,
	}
	prompt := prPrompt(ctx)

	if !strings.Contains(prompt, "Update the existing pull request") {
		t.Error("expected 'Update the existing pull request' in prompt")
	}
	if !strings.Contains(prompt, "Do NOT merge the PR") {
		t.Error("expected 'Do NOT merge' guardrail in update prompt")
	}
}

func TestPrPrompt_ChecksFailing(t *testing.T) {
	ctx := actionContext{
		ForgeCLI:    "gh",
		PullRequest: &forge.PullRequest{Checks: forge.CheckStatusFail},
	}
	prompt := prPrompt(ctx)

	if !strings.Contains(prompt, "CI checks on this PR are failing") {
		t.Error("expected failing checks prompt")
	}
	if !strings.Contains(prompt, "Do NOT merge the PR") {
		t.Error("expected 'Do NOT merge' guardrail in fix-tests prompt")
	}
}

func TestPrPrompt_Mergeable(t *testing.T) {
	ctx := actionContext{
		ForgeCLI:    "gh",
		PullRequest: &forge.PullRequest{Checks: forge.CheckStatusPass, Mergeable: true},
	}
	prompt := prPrompt(ctx)

	if !strings.Contains(prompt, "Merge this pull request") {
		t.Error("expected 'Merge this pull request' in prompt")
	}
}

func TestPrPrompt_UsesForgeCLI(t *testing.T) {
	for _, cli := range []string{"gh", "glab"} {
		ctx := actionContext{ForgeCLI: cli, PullRequest: nil}
		prompt := prPrompt(ctx)
		if !strings.Contains(prompt, cli+" pr create") {
			t.Errorf("expected prompt to use %s CLI", cli)
		}
	}
}

func TestIntegratePrompt_GitBackend(t *testing.T) {
	for _, kind := range []worktree.BackendKind{"", worktree.BackendGit} {
		ctx := actionContext{WorktreeBackend: kind, WorkDir: "/wt/feat", MainRepoPath: "/repo", BaseBranch: "develop"}
		prompt := integratePrompt(ctx)
		if prompt != gitIntegratePrompt(ctx) {
			t.Errorf("backend %q: expected the git integrate prompt", kind)
		}
		for _, want := range []string{
			"git rebase main",
			"git reset --soft main",
			"git -C '/repo' merge --ff-only $SQUASH_SHA",
			integratePrepareMainScript("/repo"),
			"git reset --hard main",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("backend %q: git prompt is missing %q", kind, want)
			}
		}
		if strings.Contains(prompt, "wt ") {
			t.Errorf("backend %q: git prompt must not use worktrunk", kind)
		}
	}
}

func TestIntegratePrompt_WorktrunkBackend(t *testing.T) {
	ctx := actionContext{
		WorktreeBackend: worktree.BackendWorktrunk,
		WorkDir:         "/wt/repo.feat",
		MainRepoPath:    "/the repo",
		BaseBranch:      "develop",
	}
	prompt := integratePrompt(ctx)
	if prompt != worktrunkIntegratePrompt(ctx) {
		t.Fatal("expected the worktrunk integrate prompt")
	}

	for _, want := range []string{
		"into develop with worktrunk",
		"git rebase 'develop'",
		"git rebase --abort",
		`git reset --soft 'develop' && git commit -m "<your message>"`,
		"SQUASH_SHA=$(git rev-parse HEAD)",
		integratePrepareMainScript("/the repo"),
		"If it prints CONFLICT, STOP",
		"wt -C '/wt/repo.feat' merge 'develop' --no-commit --no-rebase --no-remove --format json </dev/null",
		// Approvals: detect wt's messages, never approve or skip hooks.
		`"needs approval" or "Cannot prompt for approval"`,
		"NEVER pass --yes, -y or --no-hooks to wt",
		"running `wt config approvals add` in '/the repo', then run Integrate again",
		// Failures.
		`"pre-merge command failed"`,
		"go back to step (c)",
		`"not rebased onto": develop moved since you squashed. Go back to step (a).`,
		`"conflicting uncommitted changes"`,
		`"has uncommitted changes"`,
		"NEVER remove this worktree",
		"git rev-parse HEAD 'develop'",
		"Do NOT push anything to any remote",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("worktrunk prompt is missing %q", want)
		}
	}
	for _, unwanted := range []string{"merge --ff-only", "git reset --hard", "--yes "} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("worktrunk prompt must not contain %q", unwanted)
		}
	}

	// Without a base branch the prompt targets main.
	ctx.BaseBranch = ""
	if prompt := integratePrompt(ctx); !strings.Contains(prompt, "merge 'main' --no-commit") || !strings.Contains(prompt, "git rebase 'main'") {
		t.Error("expected the worktrunk prompt to default to main")
	}
}

func TestIntegrateActionUsesBackendPrompt(t *testing.T) {
	action := findAgentAction("integrate")
	if action == nil {
		t.Fatal("integrate action not found")
	}
	git := action.PromptFunc(actionContext{MainRepoPath: "/repo", WorkDir: "/wt"})
	wt := action.PromptFunc(actionContext{MainRepoPath: "/repo", WorkDir: "/wt", WorktreeBackend: worktree.BackendWorktrunk})
	if !strings.Contains(git, "merge --ff-only") || strings.Contains(git, "wt -C") {
		t.Error("git backend: expected the git merge prompt")
	}
	if !strings.Contains(wt, "wt -C '/wt' merge") || strings.Contains(wt, "merge --ff-only") {
		t.Error("worktrunk backend: expected the wt merge prompt")
	}
}

func TestLocalBaseBranch(t *testing.T) {
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-q", "-b", "main")
	gitCmd(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	gitCmd(t, repo, "branch", "feature/x")
	gitCmd(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitCmd(t, repo, "update-ref", "refs/remotes/upstream/dev", "HEAD")

	tests := []struct{ baseRef, want string }{
		{"", ""},
		{"main", "main"},
		{"feature/x", "feature/x"},     // local branch with a slash
		{"origin/main", "main"},        // remote-tracking ref → local branch
		{"upstream/dev", "dev"},        // even when the local branch doesn't exist (wt reports it)
		{"no/such-ref", "no/such-ref"}, // unknown refs are passed through
	}
	for _, tt := range tests {
		if got := localBaseBranch(repo, tt.baseRef); got != tt.want {
			t.Errorf("localBaseBranch(%q) = %q, want %q", tt.baseRef, got, tt.want)
		}
	}
}

func TestAddWorktreeBackend(t *testing.T) {
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-q", "-b", "main")
	gitCmd(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	gitCmd(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	meta := t.TempDir()

	var ctx actionContext
	(&Resolver{}).addWorktreeBackend(&ctx)
	if ctx.WorktreeBackend != "" || ctx.BaseBranch != "" {
		t.Errorf("without a worktree manager: got %+v", ctx)
	}

	ctx = actionContext{}
	r := &Resolver{WorktreeMgr: worktree.NewManager(repo, meta, "origin/main", "")}
	r.addWorktreeBackend(&ctx)
	if ctx.WorktreeBackend != worktree.BackendGit || ctx.BaseBranch != "" {
		t.Errorf("git backend: got backend %q, base branch %q", ctx.WorktreeBackend, ctx.BaseBranch)
	}

	ctx = actionContext{}
	r = &Resolver{WorktreeMgr: worktree.NewManager(repo, meta, "origin/main", "",
		worktree.WithBackend(worktree.NewWorktrunkBackend(os.DevNull)))}
	r.addWorktreeBackend(&ctx)
	if ctx.WorktreeBackend != worktree.BackendWorktrunk || ctx.BaseBranch != "main" {
		t.Errorf("worktrunk backend: got backend %q, base branch %q", ctx.WorktreeBackend, ctx.BaseBranch)
	}
}
