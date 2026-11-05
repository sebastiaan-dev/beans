package beancore

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/pkg/bean"
)

// worktreeChangedBeanIDs returns the IDs of the beans a worktree changed
// relative to its base: the merge-base of HEAD and baseRef. It covers changes
// committed on the branch as well as uncommitted (staged, unstaged and
// untracked) changes in the worktree, including moves into archive/ and
// deletions. ok is false if the changes can't be determined, e.g. because the
// directory isn't a git worktree or the base ref doesn't exist.
//
// It runs three cheap git commands (merge-base, a name-only diff and an
// untracked-file listing, all restricted to .beans/); the listing runs
// concurrently with the other two.
func worktreeChangedBeanIDs(worktreePath, baseRef string) (ids map[string]bool, ok bool) {
	type result struct {
		out []byte
		err error
	}
	untrackedCh := make(chan result, 1)
	go func() {
		out, err := exec.Command("git", "-C", worktreePath, "ls-files", "-z", "--others", "--exclude-standard", "--", BeansDir).Output()
		untrackedCh <- result{out, err}
	}()

	base, found := gitutil.MergeBase(worktreePath, baseRef)
	if !found {
		<-untrackedCh
		return nil, false
	}
	// Compares the merge-base with the working tree, so it includes committed,
	// staged and unstaged changes. --no-renames reports both sides of a move;
	// --relative makes paths relative to the worktree root like the pathspec.
	diff, err := exec.Command("git", "-C", worktreePath, "diff", "--name-only", "-z", "--no-renames", "--relative", base, "--", BeansDir).Output()
	untracked := <-untrackedCh
	if err != nil || untracked.err != nil {
		return nil, false
	}

	ids = make(map[string]bool)
	for _, out := range [][]byte{diff, untracked.out} {
		for _, p := range bytes.Split(out, []byte{0}) {
			if id := beanIDFromRepoPath(string(p)); id != "" {
				ids[id] = true
			}
		}
	}
	return ids, true
}

// beanIDFromRepoPath returns the bean ID for a repository-relative path of a
// bean file (.beans/<file>.md or .beans/archive/<file>.md), or "" if the path
// isn't one.
func beanIDFromRepoPath(p string) string {
	rest, ok := strings.CutPrefix(filepath.ToSlash(p), BeansDir+"/")
	if !ok || !strings.HasSuffix(rest, ".md") {
		return ""
	}
	if dir, file, nested := strings.Cut(rest, "/"); nested {
		if dir != ArchiveDir || strings.Contains(file, "/") {
			return ""
		}
		rest = file
	}
	id, _ := bean.ParseFilename(rest)
	return id
}
