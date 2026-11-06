package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	nonIDChars      = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	repeatedUnderSc = regexp.MustCompile(`_{2,}`)
)

// fallbackID is used when nothing usable remains of a branch or directory name.
const fallbackID = "worktree"

// SanitizeID turns a branch or directory name into a workspace ID that is safe
// in URLs, file names (<id>.meta.json) and session IDs. Like worktrunk's
// `sanitize` filter it replaces '/' and '\' with '-' ("feat/login" ->
// "feat-login"); other characters outside [A-Za-z0-9._-] also become '-'.
// Runs of '_' collapse to one so IDs can't contain the "__" used by the
// central session ID ("__central__") and run session suffix ("__run"); leading
// '.', '-' and '_' and trailing '.' and '-' are trimmed. IDs longer than the
// name limit are shortened with a hash suffix so they stay unique.
func SanitizeID(s string) string {
	id := nonIDChars.ReplaceAllString(s, "-")
	id = repeatedUnderSc.ReplaceAllString(id, "_")
	id = strings.TrimLeft(id, ".-_")
	id = strings.TrimRight(id, ".-")
	if id == "" {
		return fallbackID
	}
	if len(id) > maxNameLength {
		id = strings.TrimRight(id[:maxNameLength-7], ".-") + "-" + shortHash(s)
	}
	return id
}

// shortHash returns a short, stable hex digest of s.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:6]
}

// idCandidate is a worktree whose ID is being assigned.
type idCandidate struct {
	index    int    // position in the listing
	key      string // branch, or path for detached worktrees (unique per worktree)
	ids      []string
	priority int // lower claims its preferred ID first
}

const (
	priorityExact   = iota // the branch is already a valid ID ("feat-x")
	priorityKnown          // legacy beans/<id> branch or a detached worktree's known ID
	priorityDerived        // derived from a sanitized branch or directory name
)

// assignIDs gives every worktree a stable, unique workspace ID:
//
//   - A branch that is already a valid ID uses it unchanged ("feat-x").
//   - A legacy beans/<name> branch (created by the git backend) keeps its git
//     backend ID <name>, so switching backends keeps conversations, metadata
//     and ports.
//   - Other branches use SanitizeID(branch) ("feat/x" -> "feat-x").
//   - A detached worktree (e.g. during a rebase) keeps the ID it had while on
//     a branch if known (knownID), otherwise uses its sanitized directory name.
//
// When several worktrees want the same ID, exact matches win, then known IDs,
// then derived ones; ties are broken by branch/path order. A losing worktree
// gets "<id>-<hash>" with a hash of its branch (or path), which doesn't depend
// on the other worktrees, so IDs only change when a better claimant appears.
func assignIDs(entries []Entry, knownID func(path string) string) []string {
	cands := make([]idCandidate, len(entries))
	for i, e := range entries {
		c := idCandidate{index: i, key: e.Branch, priority: priorityDerived}
		switch {
		case e.Branch != "":
			sanitized := SanitizeID(e.Branch)
			if rest, ok := strings.CutPrefix(e.Branch, branchPrefix); ok && ValidateName(rest) == nil && SanitizeID(rest) == rest {
				c.ids = append(c.ids, rest)
				c.priority = priorityKnown
			} else if sanitized == e.Branch {
				c.priority = priorityExact
			}
			c.ids = append(c.ids, sanitized)
		default:
			c.key = e.Path
			if knownID != nil {
				if id := knownID(e.Path); id != "" {
					c.ids = append(c.ids, id)
					c.priority = priorityKnown
				}
			}
			c.ids = append(c.ids, SanitizeID(filepath.Base(e.Path)))
		}
		cands[i] = c
	}

	order := make([]*idCandidate, len(cands))
	for i := range cands {
		order[i] = &cands[i]
	}
	sort.SliceStable(order, func(a, b int) bool {
		if order[a].priority != order[b].priority {
			return order[a].priority < order[b].priority
		}
		return order[a].key < order[b].key
	})

	taken := make(map[string]bool, len(entries))
	ids := make([]string, len(entries))
	for _, c := range order {
		id := ""
		for _, want := range c.ids {
			if !taken[want] {
				id = want
				break
			}
		}
		if id == "" {
			base := c.ids[len(c.ids)-1]
			id = base + "-" + shortHash(c.key)
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("%s-%s-%d", base, shortHash(c.key), n)
			}
		}
		taken[id] = true
		ids[c.index] = id
	}
	return ids
}
