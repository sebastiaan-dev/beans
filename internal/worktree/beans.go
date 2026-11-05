package worktree

import (
	"errors"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/pkg/bean"
)

// beansDir is the directory (relative to the worktree root) whose changes
// identify the beans a worktree works on.
const beansDir = ".beans/"

// beanDetectSlots bounds the worktrees whose beans are detected concurrently
// (each detection runs up to two git processes at once).
var beanDetectSlots = make(chan struct{}, max(4, runtime.NumCPU()))

// detectBeanIDsAll sets BeanIDs of every worktree, detecting them
// concurrently. It does not touch Manager state, so callers must not hold
// m.mu (git can be slow and other operations shouldn't wait for it).
func (m *Manager) detectBeanIDsAll(worktrees []Worktree) {
	var wg sync.WaitGroup
	for i := range worktrees {
		wg.Add(1)
		go func(wt *Worktree) {
			defer wg.Done()
			beanDetectSlots <- struct{}{}
			defer func() { <-beanDetectSlots }()
			wt.BeanIDs = m.DetectBeanIDs(wt.Path)
		}(&worktrees[i])
	}
	wg.Wait()
}

// DetectBeanIDs returns the IDs of the beans changed in the worktree compared
// to its merge-base with the base branch: bean files directly in .beans/ (not
// in subdirectories such as archive/ or dot-prefixed ones) that were added or
// modified since the merge-base — committed, staged, unstaged or untracked —
// and still exist. Returns nil if git fails.
func (m *Manager) DetectBeanIDs(worktreePath string) []string {
	paths, err := changedBeanPaths(worktreePath, m.baseRef)
	if err != nil {
		return nil
	}
	return beanIDsFromPaths(paths)
}

// changedBeanPaths lists the paths under .beans/ that exist in the working
// tree and differ from the merge-base of HEAD and baseRef (or from HEAD when
// there is no merge-base), including untracked ones. An empty baseRef means
// the remote's default branch. `git diff --merge-base` finds the merge-base
// itself, so with the untracked listing running concurrently this takes one
// round of git processes; both are restricted to .beans/, so their cost
// doesn't grow with the rest of the repository.
func changedBeanPaths(dir, baseRef string) ([]string, error) {
	var untracked []string
	var untrackedErr error
	untrackedDone := make(chan struct{})
	go func() {
		defer close(untrackedDone)
		untracked, untrackedErr = gitLines(dir, "ls-files", "-z", "--others", "--exclude-standard", "--", beansDir)
	}()

	// --diff-filter=d drops files deleted in the working tree; --no-renames
	// reports a renamed bean file under its new name.
	diff := []string{"diff", "-z", "--name-only", "--no-renames", "--diff-filter=d"}
	var changed []string
	diffErr := errNoBase
	if baseRef == "" {
		if remote, ok := gitutil.DefaultRemoteBranch(dir, "origin"); ok {
			baseRef = remote
		}
	}
	if baseRef != "" && !strings.HasPrefix(baseRef, "-") {
		changed, diffErr = gitLines(dir, append(diff, "--merge-base", baseRef, "--", beansDir)...)
	}
	if diffErr != nil {
		// No merge-base (unknown base ref, unrelated histories): compare
		// with HEAD, i.e. only uncommitted changes count.
		changed, diffErr = gitLines(dir, append(diff, "HEAD", "--", beansDir)...)
	}
	<-untrackedDone
	if diffErr != nil {
		return nil, diffErr
	}
	if untrackedErr != nil {
		return nil, untrackedErr
	}
	return append(changed, untracked...), nil
}

// errNoBase means there is no base ref to compare against.
var errNoBase = errors.New("no base ref")

// gitLines runs a read-only git command whose output is NUL-terminated (-z)
// in dir and returns its non-empty entries. --no-optional-locks keeps it from writing the index (and
// racing with git commands run in the worktree).
func gitLines(dir string, args ...string) ([]string, error) {
	cmd := exec.Command("git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\x00") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// beanIDsFromPaths returns the sorted, unique IDs of the bean files directly
// in .beans/ among paths.
func beanIDsFromPaths(paths []string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, path := range paths {
		rest, ok := strings.CutPrefix(path, beansDir)
		if !ok || strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".md") {
			continue
		}
		id, _ := bean.ParseFilename(rest)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
