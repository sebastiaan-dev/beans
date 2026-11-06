package worktree

import (
	"bufio"
	"strings"
)

// Entry is one worktree as reported by `git worktree list --porcelain`.
type Entry struct {
	Path     string
	Head     string
	Branch   string // short branch name ("feat/x"); empty when detached or bare
	Detached bool
	Bare     bool
	Locked   bool
	Prunable bool // the worktree's directory is missing (stale admin entry)
}

// parseEntries parses `git worktree list --porcelain` output. The first entry
// is always the main worktree (git lists it first).
func parseEntries(output string) []Entry {
	var entries []Entry
	var cur *Entry

	flush := func() {
		if cur != nil && cur.Path != "" {
			entries = append(entries, *cur)
		}
		cur = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Entry{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			// Attribute line without a worktree line; ignore.
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		case line == "":
			flush()
		}
	}
	// Porcelain output may not end with a blank line.
	flush()
	return entries
}

// parsePorcelain parses `git worktree list --porcelain` output and returns
// the workspaces of the git backend: worktrees whose branch starts with the
// beans prefix, plus beans-managed worktrees that are temporarily detached
// (e.g. during a rebase), identified by their location in worktreesDir.
// Entries marked as "prunable" (stale/missing directory) are skipped.
func parsePorcelain(output string, worktreesDir string) []Worktree {
	return gitBackend{}.workspaces(parseEntries(output), worktreesDir, nil)
}
