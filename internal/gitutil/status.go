package gitutil

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FileChange represents a single changed file with diff stats.
type FileChange struct {
	Path      string
	Status    string // "modified", "added", "deleted", "untracked", "renamed"
	Additions int
	Deletions int
	Staged    bool
}

// FileChanges returns the list of changed files in the given directory,
// combining staged changes, unstaged changes, and untracked files.
func FileChanges(dir string) ([]FileChange, error) {
	var changes []FileChange

	// Staged changes
	staged, err := diffNumstat(dir, true)
	if err != nil {
		return nil, err
	}
	changes = append(changes, staged...)

	// Unstaged changes (only files not already covered by staged)
	unstaged, err := diffNumstat(dir, false)
	if err != nil {
		return nil, err
	}
	changes = append(changes, unstaged...)

	// Untracked files
	untracked, err := untrackedFiles(dir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, untracked...)

	return changes, nil
}

// diffNumstat runs git diff --numstat (with or without --cached) and parses
// the output into FileChange structs.
func diffNumstat(dir string, cached bool) ([]FileChange, error) {
	args := []string{"-C", dir, "diff", "--numstat"}
	if cached {
		args = append(args, "--cached")
	}

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return parseNumstat(string(out), cached)
}

// parseNumstat parses git diff --numstat output. Each line is:
//
//	<additions>\t<deletions>\t<path>
//
// Binary files show "-" for additions/deletions.
func parseNumstat(output string, staged bool) ([]FileChange, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var changes []FileChange
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}

		adds, _ := strconv.Atoi(parts[0]) // "-" for binary → 0
		dels, _ := strconv.Atoi(parts[1])
		path := parts[2]

		// Detect renames: git uses "old => new" or "{old => new}/rest" syntax
		status := "modified"
		if strings.Contains(path, " => ") {
			status = "renamed"
		}

		changes = append(changes, FileChange{
			Path:      path,
			Status:    status,
			Additions: adds,
			Deletions: dels,
			Staged:    staged,
		})
	}

	return changes, nil
}

// MergeBase returns the merge-base commit between HEAD and the given base ref.
// If baseRef is empty, falls back to the default remote branch (e.g. origin/main).
// Returns ("", false) if it can't be determined.
func MergeBase(dir, baseRef string) (string, bool) {
	if baseRef == "" {
		remote, ok := DefaultRemoteBranch(dir, "origin")
		if !ok {
			return "", false
		}
		baseRef = remote
	}
	cmd := exec.Command("git", "-C", dir, "merge-base", "HEAD", baseRef)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// AllChangesVsUpstream returns all file changes compared to the
// merge-base: committed + staged + unstaged + untracked, deduplicated into
// a single entry per file showing the total diff from merge-base to working tree.
// baseRef is the branch to compare against (e.g. "main"); if empty, falls back
// to the default remote branch.
//
// It runs the merge-base lookup and the untracked-file listing concurrently,
// then a single `git diff --raw --numstat` that yields both the line counts and
// which files were added since the merge-base.
func AllChangesVsUpstream(dir, baseRef string) ([]FileChange, error) {
	var untracked []FileChange
	var untrackedErr error
	untrackedDone := make(chan struct{})
	go func() {
		defer close(untrackedDone)
		untracked, untrackedErr = untrackedFiles(dir)
	}()

	base, ok := MergeBase(dir, baseRef)
	if !ok {
		<-untrackedDone
		// Fallback: if no merge-base, just return regular working tree changes
		return FileChanges(dir)
	}

	// Diff from merge-base to working tree (includes committed + staged + unstaged)
	cmd := exec.Command("git", "-C", dir, "diff", "--raw", "--numstat", base)
	out, diffErr := cmd.Output()
	<-untrackedDone
	if diffErr != nil {
		return nil, diffErr
	}
	changes, err := parseRawNumstat(string(out))
	if err != nil {
		return nil, err
	}

	// Also include untracked files (not covered by git diff)
	if untrackedErr != nil {
		return nil, untrackedErr
	}
	return append(changes, untracked...), nil
}

// parseRawNumstat parses `git diff --raw --numstat` output: one raw line
// (":<modes> <shas> <status>\t<paths>") per file, followed by one numstat line
// per file in the same order. Files that the raw status marks as added ("A")
// get status "added"; otherwise statuses are as in parseNumstat.
func parseRawNumstat(output string) ([]FileChange, error) {
	var statuses []string
	var numstat strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if raw, ok := strings.CutPrefix(line, ":"); ok {
			meta, _, _ := strings.Cut(raw, "\t")
			fields := strings.Fields(meta)
			status := ""
			if len(fields) > 0 {
				status = fields[len(fields)-1]
			}
			statuses = append(statuses, status)
			continue
		}
		numstat.WriteString(line)
		numstat.WriteByte('\n')
	}

	changes, err := parseNumstat(numstat.String(), false)
	if err != nil {
		return nil, err
	}
	if len(statuses) == len(changes) {
		for i := range changes {
			if statuses[i] == "A" && changes[i].Status == "modified" {
				changes[i].Status = "added"
			}
		}
	}
	return changes, nil
}

// AllFileDiff returns the unified diff for a file compared to the
// merge-base. This shows the complete change from merge-base to working tree.
// baseRef is the branch to compare against; if empty, falls back to the default
// remote branch.
func AllFileDiff(dir, filePath, baseRef string) (string, error) {
	base, ok := MergeBase(dir, baseRef)
	if !ok {
		// Fallback to regular unstaged diff
		return FileDiff(dir, filePath, false)
	}

	// Check if the file is untracked
	cmd := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(out)) != "" {
		// Untracked file — show full content as a diff
		cmd = exec.Command("git", "-C", dir, "diff", "--no-index", "/dev/null", filePath)
		out, err = cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return string(out), nil
			}
			return "", err
		}
		return string(out), nil
	}

	// Diff from merge-base to working tree for this file
	cmd = exec.Command("git", "-C", dir, "diff", base, "--", filePath)
	out, err = cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// HasUnmergedCommits returns true if the current branch in dir has commits
// that are not in the given base branch (i.e., commits ahead).
func HasUnmergedCommits(dir, baseBranch string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-list", "--count", baseBranch+"..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count > 0
}

// CommitsBehind returns the number of commits on baseBranch that are not
// reachable from HEAD (i.e., how far behind the worktree branch is).
func CommitsBehind(dir, baseBranch string) int {
	if baseBranch == "" {
		remote, ok := DefaultRemoteBranch(dir, "origin")
		if !ok {
			return 0
		}
		baseBranch = remote
	}
	cmd := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD.."+baseBranch)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count
}

// HasConflicts checks whether rebasing the current branch onto baseBranch
// would produce merge conflicts, without actually modifying the working tree.
// It performs a tree-level merge in memory using git merge-tree.
func HasConflicts(dir, baseBranch string) bool {
	if baseBranch == "" {
		remote, ok := DefaultRemoteBranch(dir, "origin")
		if !ok {
			return false
		}
		baseBranch = remote
	}
	// Errors (e.g., git too old) — assume no conflicts
	conflicts, _ := MergeConflicts(dir, baseBranch, "HEAD")
	return conflicts
}

// MergeConflicts reports whether merging ours and theirs would produce
// conflicts, using an in-memory tree-level merge (git merge-tree) that
// doesn't touch the working tree. The result depends only on the two commits.
func MergeConflicts(dir, ours, theirs string) (bool, error) {
	// git merge-tree --write-tree exits 0 if clean, 1 if conflicts; either way
	// it prints the resulting tree. It also exits 1 (with no output) when a
	// rev can't be resolved, which is an error rather than a conflict.
	cmd := exec.Command("git", "-C", dir, "merge-tree", "--write-tree", "--no-messages", ours, theirs)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(strings.TrimSpace(string(out))) > 0 {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// HasUnpushedCommits returns true if the current branch has commits
// that have not been pushed to its remote tracking branch.
// Returns false if there is no tracking branch (e.g., branch was never pushed).
func HasUnpushedCommits(dir string) bool {
	// Check if upstream tracking branch exists
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err := cmd.Run(); err != nil {
		// No tracking branch — treat as "has unpushed" since nothing was pushed yet
		return true
	}
	// Count commits ahead of upstream
	cmd = exec.Command("git", "-C", dir, "rev-list", "--count", "@{upstream}..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count > 0
}

// HasChanges returns true if there are any uncommitted changes (staged or
// unstaged) or untracked files. Returns false if git fails.
//
// It runs a single `git status --porcelain` instead of the three commands
// FileChanges needs. --no-optional-locks keeps this read-only check from
// taking the index lock (and racing with git commands run in the worktree),
// and --untracked-files=normal ignores a status.showUntrackedFiles=no config
// so untracked files always count, like in FileChanges.
func HasChanges(dir string) bool {
	cmd := exec.Command("git", "--no-optional-locks", "-C", dir, "status", "--porcelain", "--untracked-files=normal")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// AheadBehind counts the commits reachable from HEAD but not from baseRef
// (ahead) and those reachable from baseRef but not from HEAD (behind), with a
// single `git rev-list --left-right --count`. baseRef must be non-empty.
func AheadBehind(dir, baseRef string) (ahead, behind int, err error) {
	cmd := exec.Command("git", "-C", dir, "rev-list", "--left-right", "--count", baseRef+"...HEAD")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	if behind, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, err
	}
	if ahead, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// ResolveCommits resolves each rev to the full SHA of the commit it points to,
// with a single `git rev-parse`. It fails if any rev doesn't name a commit.
func ResolveCommits(dir string, revs ...string) ([]string, error) {
	args := []string{"-C", dir, "rev-parse"}
	for _, rev := range revs {
		if rev == "" || strings.HasPrefix(rev, "-") {
			return nil, fmt.Errorf("invalid revision %q", rev)
		}
		args = append(args, rev+"^{commit}")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(string(out))
	if len(shas) != len(revs) {
		return nil, fmt.Errorf("unexpected rev-parse output %q", out)
	}
	return shas, nil
}

// FileDiff returns the unified diff for a specific file.
// If staged is true, shows the staged diff (--cached); otherwise shows the working tree diff.
// For untracked files, it shows the full file content as an added diff.
func FileDiff(dir, filePath string, staged bool) (string, error) {
	if staged {
		// Staged diff
		cmd := exec.Command("git", "-C", dir, "diff", "--cached", "--", filePath)
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}

	// Check if the file is untracked
	cmd := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(out)) != "" {
		// Untracked file — show full content as a diff-like output
		cmd = exec.Command("git", "-C", dir, "diff", "--no-index", "/dev/null", filePath)
		out, err = cmd.Output()
		// git diff --no-index exits with 1 when files differ (which they always will)
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return string(out), nil
			}
			return "", err
		}
		return string(out), nil
	}

	// Unstaged diff for tracked file
	cmd = exec.Command("git", "-C", dir, "diff", "--", filePath)
	out, err = cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// DiscardFileChange discards a change to a single file.
// If staged is true, the file is unstaged first. Untracked files are removed;
// tracked files are restored to their committed state.
func DiscardFileChange(dir, filePath string, staged bool) error {
	if staged {
		// Unstage the file first
		cmd := exec.Command("git", "-C", dir, "reset", "HEAD", "--", filePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("unstage file: %s", strings.TrimSpace(string(out)))
		}
	}

	// Check if the file is untracked
	cmd := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("check untracked status: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		// Untracked file — remove it
		cmd = exec.Command("git", "-C", dir, "clean", "-f", "--", filePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("clean untracked file: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}

	// Tracked file — restore working tree copy
	cmd = exec.Command("git", "-C", dir, "checkout", "--", filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restore file: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// untrackedFiles returns untracked files via git ls-files.
func untrackedFiles(dir string) ([]FileChange, error) {
	cmd := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}

	var changes []FileChange
	for _, path := range strings.Split(output, "\n") {
		if path == "" {
			continue
		}
		changes = append(changes, FileChange{
			Path:   path,
			Status: "untracked",
			Staged: false,
		})
	}

	return changes, nil
}
