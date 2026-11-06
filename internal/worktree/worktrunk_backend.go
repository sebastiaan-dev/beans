package worktree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
)

// worktrunkBackend manages worktrees with worktrunk (`wt`). Worktrunk
// worktrees are plain git worktrees, so listing reads `git worktree list
// --porcelain` (much faster than `wt list`, which computes statuses); creation
// and removal go through wt so its path template and hooks apply.
//
// Every linked worktree of the repository is a workspace, whoever created it.
type worktrunkBackend struct {
	bin string // path to the wt executable
}

// NewWorktrunkBackend returns a worktrunk backend that runs the given wt
// executable.
func NewWorktrunkBackend(bin string) Backend {
	return &worktrunkBackend{bin: bin}
}

func (*worktrunkBackend) Kind() BackendKind { return BackendWorktrunk }

func (*worktrunkBackend) workspaces(entries []Entry, _ string, idForPath func(string) string) []Worktree {
	// The main worktree (always listed first), bare entries and stale
	// (prunable) entries are never workspaces.
	var linked []Entry
	for i, e := range entries {
		if i == 0 || e.Prunable || e.Bare {
			continue
		}
		linked = append(linked, e)
	}

	ids := assignIDs(linked, idForPath)
	worktrees := make([]Worktree, len(linked))
	for i, e := range linked {
		worktrees[i] = Worktree{
			ID:       ids[i],
			Branch:   e.Branch,
			Path:     e.Path,
			Detached: e.Branch == "",
		}
	}
	return worktrees
}

// wtSwitchResult is the JSON output of `wt switch --format json`.
type wtSwitchResult struct {
	Action string `json:"action"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

func (b *worktrunkBackend) create(req createRequest) (string, string, error) {
	// --no-hooks skips all hooks of the switch; beans runs the creation hooks
	// (pre-start, post-start) itself afterwards but not the switch hooks
	// (pre-switch, post-switch), which are about moving the user's shell.
	args := []string{"-C", req.repoRoot, "switch", "--create", req.name, "--no-cd", "--no-hooks", "--format", "json"}
	if req.baseRef != "" {
		args = append(args, "--base", req.baseRef)
	}
	stdout, stderr, err := b.run(args...)
	if err != nil {
		return "", "", fmt.Errorf("wt switch --create %s: %s: %w", req.name, cmdOutput(stdout, stderr), err)
	}
	var res wtSwitchResult
	if err := json.Unmarshal(jsonDocument(stdout), &res); err != nil || res.Path == "" {
		return "", "", fmt.Errorf("wt switch --create %s: unexpected output %q", req.name, strings.TrimSpace(string(stdout)))
	}
	branch := res.Branch
	if branch == "" {
		branch = req.name
	}
	return branch, res.Path, nil
}

// wtHook is one configured hook command as reported by `wt hook show --format json`.
type wtHook struct {
	Name          *string `json:"name"`
	NeedsApproval bool    `json:"needs_approval"`
	Source        string  `json:"source"`
	Template      string  `json:"template"`
	Type          string  `json:"type"`
}

// ApprovalError reports worktrunk project hooks that need the user's
// approval. Beans never approves project hooks on the user's behalf (no
// `--yes`), and wt can't prompt without a terminal.
type ApprovalError struct {
	RepoRoot     string
	WorktreePath string
	Operation    string   // e.g. "creation", "removal"
	Commands     []string // "<type>: <template>"
	Output       string   // wt's own message, if any
}

func (e *ApprovalError) Error() string {
	var sb strings.Builder
	if e.Operation == "removal" {
		sb.WriteString("worktrunk project hooks need your approval, so the worktree was not removed")
	} else {
		fmt.Fprintf(&sb, "worktrunk project hooks need your approval, so beans did not run the %s hooks", e.Operation)
	}
	if len(e.Commands) > 0 {
		sb.WriteString(":\n")
		for _, c := range e.Commands {
			sb.WriteString("  " + c + "\n")
		}
	} else {
		sb.WriteString(".\n")
	}
	if e.Output != "" {
		sb.WriteString(e.Output + "\n")
	}
	fmt.Fprintf(&sb, "Beans never approves project hooks for you. Review and approve them by running `wt config approvals add` in %s", e.RepoRoot)
	if e.Operation == "creation" && e.WorktreePath != "" {
		fmt.Fprintf(&sb, ", then run `wt hook pre-start` and `wt hook post-start` in %s (beans' worktree.setup command, if any, was not run either)", e.WorktreePath)
	} else {
		sb.WriteString(", then try again")
	}
	sb.WriteString(".")
	return sb.String()
}

var (
	baseVar             = regexp.MustCompile(`\bbase\b`)
	baseWorktreePathVar = regexp.MustCompile(`\bbase_worktree_path\b`)
)

func (b *worktrunkBackend) creationHooks(repoRoot, baseRef string, wt *Worktree) (*CreationHooks, error) {
	stdout, stderr, err := b.run("-C", wt.Path, "hook", "show", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("wt hook show: %s: %w", cmdOutput(stdout, stderr), err)
	}
	var hooks []wtHook
	if err := json.Unmarshal(jsonDocument(stdout), &hooks); err != nil {
		return nil, fmt.Errorf("wt hook show: unexpected output %q", strings.TrimSpace(string(stdout)))
	}

	byType := map[string][]wtHook{}
	var unapproved []string
	for _, h := range hooks {
		if h.Type != "pre-start" && h.Type != "post-start" {
			continue
		}
		byType[h.Type] = append(byType[h.Type], h)
		if h.NeedsApproval {
			unapproved = append(unapproved, h.Type+": "+h.Template)
		}
	}
	if len(unapproved) > 0 {
		return nil, &ApprovalError{RepoRoot: repoRoot, WorktreePath: wt.Path, Operation: "creation", Commands: unapproved}
	}

	hookRunner := func(hookType string, hs []wtHook) func() (string, error) {
		if len(hs) == 0 {
			return nil
		}
		return func() (string, error) {
			args := []string{"-C", wt.Path, "hook", hookType}
			args = append(args, operationVars(hs, baseRef, repoRoot)...)
			stdout, stderr, err := b.run(args...)
			out := cmdOutput(stdout, stderr)
			if err != nil {
				if isApprovalFailure(out) {
					return out, &ApprovalError{RepoRoot: repoRoot, WorktreePath: wt.Path, Operation: "creation", Output: out}
				}
				return out, fmt.Errorf("wt hook %s: %w", hookType, err)
			}
			return out, nil
		}
	}

	pre := hookRunner("pre-start", byType["pre-start"])
	post := hookRunner("post-start", byType["post-start"])
	if pre == nil && post == nil {
		return nil, nil
	}
	return &CreationHooks{Before: pre, After: post}, nil
}

// operationVars returns the --KEY=VALUE arguments that give manually run
// creation hooks the operation variables `wt switch --create` would set:
// {{ base }} is the base ref and {{ base_worktree_path }} the main worktree.
// wt binds such an argument only when a hook template references the key and
// otherwise forwards it into {{ args }}, so they are passed only when used.
func operationVars(hooks []wtHook, baseRef, repoRoot string) []string {
	var usesBase, usesBasePath bool
	for _, h := range hooks {
		usesBase = usesBase || baseVar.MatchString(h.Template)
		usesBasePath = usesBasePath || baseWorktreePathVar.MatchString(h.Template)
	}
	var args []string
	if usesBase && baseRef != "" {
		args = append(args, "--base="+baseRef)
	}
	if usesBasePath {
		args = append(args, "--base-worktree-path="+repoRoot)
	}
	return args
}

// wtRemoveResult is one entry of `wt remove --format json` output.
type wtRemoveResult struct {
	Branch        string `json:"branch"`
	BranchOutcome string `json:"branch_outcome"`
	Path          string `json:"path"`
}

func (b *worktrunkBackend) remove(repoRoot string, wt *Worktree) error {
	// Address the worktree by branch (wt's convention), or by path when it is
	// detached. --force discards uncommitted changes like the git backend's
	// `git worktree remove --force`; without -D, wt deletes the branch only
	// when it is merged (adds no changes to the default branch). --foreground
	// waits for the removal so the worktree is gone when this returns; the
	// pre-remove hooks run (blocking) in the worktree, post-remove hooks in
	// the background.
	target := wt.Branch
	if target == "" {
		target = wt.Path
	}
	stdout, stderr, err := b.run("-C", repoRoot, "remove", target, "--foreground", "--force", "--format", "json")
	if err != nil {
		out := cmdOutput(stdout, stderr)
		if isApprovalFailure(out) {
			return &ApprovalError{RepoRoot: repoRoot, WorktreePath: wt.Path, Operation: "removal", Output: out}
		}
		return fmt.Errorf("wt remove %s: %s: %w", target, out, err)
	}
	var results []wtRemoveResult
	if json.Unmarshal(jsonDocument(stdout), &results) == nil {
		for _, r := range results {
			if r.Branch != "" && r.BranchOutcome != "" {
				log.Printf("[worktree] removed worktree %s via worktrunk (branch %s: %s)", r.Path, r.Branch, r.BranchOutcome)
			}
		}
	}
	return nil
}

// run runs wt with the given arguments and returns stdout and stderr.
// Stdin is empty, so wt never waits for an interactive approval prompt.
func (b *worktrunkBackend) run(args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(b.bin, args...)
	cmd.Env = commandEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// isApprovalFailure reports whether wt failed because project hooks need approval.
func isApprovalFailure(output string) bool {
	return strings.Contains(output, "needs approval") || strings.Contains(output, "Cannot prompt for approval")
}

// cmdOutput combines a command's stdout and stderr for error messages.
func cmdOutput(stdout, stderr []byte) string {
	parts := []string{}
	if s := strings.TrimSpace(string(stderr)); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(string(stdout)); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// jsonDocument returns the JSON document in wt's stdout: the output from the
// first line that starts a JSON value, so stray text before it is ignored.
func jsonDocument(stdout []byte) []byte {
	lines := bytes.Split(stdout, []byte("\n"))
	for i, line := range lines {
		t := bytes.TrimSpace(line)
		if len(t) > 0 && (t[0] == '{' || t[0] == '[') {
			return bytes.Join(lines[i:], []byte("\n"))
		}
	}
	return stdout
}
