package beancore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hmans/beans/pkg/bean"
)

// AuditKind identifies a category of audit finding. Kinds are stable
// identifiers intended for machine consumption (e.g. `beans audit --json`).
type AuditKind string

const (
	// AuditLandedButOpen: an open bean is referenced by a commit that is
	// already reachable from the base branch.
	AuditLandedButOpen AuditKind = "landed-but-open"
	// AuditContainerDoneChildrenOpen: a completed/scrapped bean still has
	// open children (which only inherit the terminal status implicitly).
	AuditContainerDoneChildrenOpen AuditKind = "container-done-children-open"
	// AuditContainerReadyToClose: an open bean whose children are all
	// completed/scrapped.
	AuditContainerReadyToClose AuditKind = "container-ready-to-close"
	// AuditStaleInProgress: an in-progress bean that has not been updated
	// for longer than the stale threshold.
	AuditStaleInProgress AuditKind = "stale-in-progress"
	// AuditBlockedByDone: an open bean whose explicit blockers are all
	// completed/scrapped, so the remaining blocking links are stale.
	AuditBlockedByDone AuditKind = "blocked-by-done"
)

// AuditKinds lists all finding kinds in report order.
var AuditKinds = []AuditKind{
	AuditLandedButOpen,
	AuditContainerDoneChildrenOpen,
	AuditContainerReadyToClose,
	AuditStaleInProgress,
	AuditBlockedByDone,
}

// maxAuditCommits caps the number of commits recorded per landed-but-open
// finding; CommitCount always holds the full number.
const maxAuditCommits = 10

// AuditCommit is a commit fed into the landed-but-open check.
type AuditCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Subject   string `json:"subject"`
	// Message is the full commit message; it is scanned for bean IDs but not
	// included in findings.
	Message string `json:"-"`
}

// AuditCommitSource streams commits (newest first) to yield, stopping when
// yield returns false. It returns any error encountered while producing
// commits.
type AuditCommitSource func(yield func(AuditCommit) bool) error

// AuditOptions configures Core.Audit.
type AuditOptions struct {
	// Now is the reference time for staleness; zero means time.Now().
	Now time.Time
	// StaleAfter is the inactivity threshold for in-progress beans; values
	// <= 0 disable the stale-in-progress check.
	StaleAfter time.Duration
	// Commits, if non-nil, enables the landed-but-open check.
	Commits AuditCommitSource
	// IgnoreCommits, if non-nil, is called once with the hashes of all
	// commits that reference an open bean, and returns the hashes to
	// disregard (e.g. bookkeeping commits that only edit bean files and so
	// do not indicate that the work has landed).
	IgnoreCommits func(hashes []string) (map[string]bool, error)
}

// AuditBlocker describes a resolved blocker in a blocked-by-done finding.
type AuditBlocker struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Via is the field that records the link: "blocked_by" (on the blocked
	// bean) or "blocking" (on the blocker).
	Via string `json:"via"`
}

// AuditDetails holds kind-specific structured data for a finding.
type AuditDetails struct {
	Status string `json:"status,omitempty"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`

	// landed-but-open
	Commits     []AuditCommit `json:"commits,omitempty"`
	CommitCount int           `json:"commit_count,omitempty"`

	// container-done-children-open / container-ready-to-close
	OpenChildren   []string `json:"open_children,omitempty"`
	ClosedChildren []string `json:"closed_children,omitempty"`

	// stale-in-progress
	LastUpdated *time.Time `json:"last_updated,omitempty"`
	IdleDays    int        `json:"idle_days,omitempty"`

	// blocked-by-done
	Blockers []AuditBlocker `json:"blockers,omitempty"`
}

// AuditFinding is a single workflow-hygiene issue.
type AuditFinding struct {
	Kind AuditKind `json:"kind"`
	// BeanIDs lists the beans involved; the first is the bean the finding is
	// about (the one that most likely needs updating).
	BeanIDs []string     `json:"bean_ids"`
	Message string       `json:"message"`
	Details AuditDetails `json:"details"`
}

// Audit inspects workflow/state hygiene across all beans. It complements
// CheckAllLinks (structural integrity) by flagging beans whose state looks
// out of date. Findings are ordered by kind (see AuditKinds), then bean ID.
//
// Only errors from the git-related callbacks (Commits, IgnoreCommits) are
// returned; in that case no landed-but-open findings are reported, but
// findings from the other checks still are.
func (c *Core) Audit(opts AuditOptions) ([]AuditFinding, error) {
	beans := c.All()
	sort.Slice(beans, func(i, j int) bool { return beans[i].ID < beans[j].ID })

	byID := make(map[string]*bean.Bean, len(beans))
	children := make(map[string][]*bean.Bean)
	incomingBlocking := make(map[string][]*bean.Bean)
	for _, b := range beans {
		byID[b.ID] = b
		if b.Parent != "" {
			children[b.Parent] = append(children[b.Parent], b)
		}
		for _, target := range b.Blocking {
			incomingBlocking[target] = append(incomingBlocking[target], b)
		}
	}

	var findings []AuditFinding

	var commitErr error
	if opts.Commits != nil {
		var landed []AuditFinding
		landed, commitErr = auditLandedButOpen(beans, opts.Commits, opts.IgnoreCommits)
		findings = append(findings, landed...)
	}

	findings = append(findings, auditContainers(beans, children)...)

	if opts.StaleAfter > 0 {
		now := opts.Now
		if now.IsZero() {
			now = time.Now()
		}
		findings = append(findings, auditStaleInProgress(beans, now, opts.StaleAfter)...)
	}

	findings = append(findings, auditBlockedByDone(beans, byID, incomingBlocking)...)

	return findings, commitErr
}

// auditLandedButOpen matches bean IDs (as whole tokens) in commit messages.
func auditLandedButOpen(beans []*bean.Bean, source AuditCommitSource, ignore func([]string) (map[string]bool, error)) ([]AuditFinding, error) {
	open := make(map[string]*bean.Bean)
	for _, b := range beans {
		if !isResolvedStatus(b.Status) {
			open[strings.ToLower(b.ID)] = b
		}
	}
	if len(open) == 0 {
		return nil, nil
	}

	// Matching commits per (lowercased) bean ID, newest first.
	matches := make(map[string][]AuditCommit)
	var matchedHashes []string
	err := source(func(commit AuditCommit) bool {
		ids := ReferencedIDs(commit.Message, func(tok string) bool { _, ok := open[tok]; return ok })
		if len(ids) == 0 {
			return true
		}
		ref := AuditCommit{Hash: commit.Hash, ShortHash: commit.ShortHash, Subject: commit.Subject}
		matchedHashes = append(matchedHashes, commit.Hash)
		for _, id := range ids {
			matches[id] = append(matches[id], ref)
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	var ignored map[string]bool
	if ignore != nil && len(matchedHashes) > 0 {
		if ignored, err = ignore(matchedHashes); err != nil {
			return nil, err
		}
	}

	var findings []AuditFinding
	for _, b := range beans {
		var commits []AuditCommit
		for _, c := range matches[strings.ToLower(b.ID)] {
			if !ignored[c.Hash] {
				commits = append(commits, c)
			}
		}
		if len(commits) == 0 {
			continue
		}
		latest := commits[0]
		msg := fmt.Sprintf("%s is %s but referenced by %s %q on the base branch", b.ID, b.Status, latest.ShortHash, latest.Subject)
		if len(commits) > 1 {
			msg += fmt.Sprintf(" (+%d more)", len(commits)-1)
		}
		findings = append(findings, AuditFinding{
			Kind:    AuditLandedButOpen,
			BeanIDs: []string{b.ID},
			Message: msg,
			Details: AuditDetails{
				Status:      b.Status,
				Type:        b.Type,
				Title:       b.Title,
				Commits:     commits[:min(len(commits), maxAuditCommits)],
				CommitCount: len(commits),
			},
		})
	}
	return findings, nil
}

// ReferencedIDs returns the distinct tokens in text (lowercased, in order of
// first appearance) for which known returns true. A token is a maximal run of
// ID characters (letters, digits, '-', '_'), so IDs only match as whole words:
// "Refs: beans-abc1" and "(beans-abc1)" match beans-abc1, but "beans-abc12"
// and the bean filename "beans-abc1--slug.md" do not.
func ReferencedIDs(text string, known func(token string) bool) []string {
	var ids []string
	var seen map[string]bool
	isIDChar := func(r rune) bool {
		return r == '-' || r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
	}
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return !isIDChar(r) }) {
		tok = strings.ToLower(strings.TrimRight(tok, "-_"))
		if tok == "" || !known(tok) || seen[tok] {
			continue
		}
		if seen == nil {
			seen = make(map[string]bool)
		}
		seen[tok] = true
		ids = append(ids, tok)
	}
	return ids
}

// auditContainers reports parent/child status mismatches.
func auditContainers(beans []*bean.Bean, children map[string][]*bean.Bean) []AuditFinding {
	var doneWithOpen, readyToClose []AuditFinding
	for _, b := range beans {
		kids := children[b.ID]
		if len(kids) == 0 {
			continue
		}
		var openKids, closedKids []string
		for _, k := range kids {
			if isResolvedStatus(k.Status) {
				closedKids = append(closedKids, k.ID)
			} else {
				openKids = append(openKids, k.ID)
			}
		}
		details := AuditDetails{Status: b.Status, Type: b.Type, Title: b.Title, OpenChildren: openKids, ClosedChildren: closedKids}

		switch {
		case isResolvedStatus(b.Status) && len(openKids) > 0:
			doneWithOpen = append(doneWithOpen, AuditFinding{
				Kind:    AuditContainerDoneChildrenOpen,
				BeanIDs: append([]string{b.ID}, openKids...),
				Message: fmt.Sprintf("%s %s is %s but has %s still open: %s", typeLabel(b), b.ID, b.Status, pluralize(len(openKids), "child", "children"), strings.Join(openKids, ", ")),
				Details: details,
			})
		case !isResolvedStatus(b.Status) && len(openKids) == 0:
			readyToClose = append(readyToClose, AuditFinding{
				Kind:    AuditContainerReadyToClose,
				BeanIDs: append([]string{b.ID}, closedKids...),
				Message: fmt.Sprintf("%s %s is %s but %s completed/scrapped", typeLabel(b), b.ID, b.Status, allChildrenPhrase(len(closedKids))),
				Details: details,
			})
		}
	}
	return append(doneWithOpen, readyToClose...)
}

// auditStaleInProgress reports in-progress beans idle for longer than staleAfter.
func auditStaleInProgress(beans []*bean.Bean, now time.Time, staleAfter time.Duration) []AuditFinding {
	var findings []AuditFinding
	for _, b := range beans {
		if b.Status != "in-progress" {
			continue
		}
		last := b.UpdatedAt
		if last == nil {
			last = b.CreatedAt
		}
		if last == nil {
			continue
		}
		idle := now.Sub(*last)
		if idle <= staleAfter {
			continue
		}
		days := int(idle / (24 * time.Hour))
		lastCopy := *last
		findings = append(findings, AuditFinding{
			Kind:    AuditStaleInProgress,
			BeanIDs: []string{b.ID},
			Message: fmt.Sprintf("%s has been in-progress without updates for %s (last updated %s)", b.ID, pluralize(days, "day", "days"), last.Format("2006-01-02")),
			Details: AuditDetails{Status: b.Status, Type: b.Type, Title: b.Title, LastUpdated: &lastCopy, IdleDays: days},
		})
	}
	return findings
}

// auditBlockedByDone reports open beans whose explicit blockers are all
// resolved. Such beans are already treated as unblocked (see
// FindActiveBlockers), so the remaining links are stale bookkeeping. Beans
// that still have at least one active blocker are not reported, and links to
// missing beans are left to `beans check`.
func auditBlockedByDone(beans []*bean.Bean, byID map[string]*bean.Bean, incoming map[string][]*bean.Bean) []AuditFinding {
	var findings []AuditFinding
	for _, b := range beans {
		if isResolvedStatus(b.Status) {
			continue
		}
		var blockers []AuditBlocker
		active := false
		seen := make(map[string]bool)
		add := func(blocker *bean.Bean, via string) {
			if blocker.ID == b.ID || seen[blocker.ID+"\x00"+via] {
				return
			}
			seen[blocker.ID+"\x00"+via] = true
			if !isResolvedStatus(blocker.Status) {
				active = true
				return
			}
			blockers = append(blockers, AuditBlocker{ID: blocker.ID, Status: blocker.Status, Via: via})
		}
		for _, id := range b.BlockedBy {
			if blocker, ok := byID[id]; ok {
				add(blocker, "blocked_by")
			}
		}
		for _, blocker := range incoming[b.ID] {
			add(blocker, "blocking")
		}
		if active || len(blockers) == 0 {
			continue
		}

		ids := []string{b.ID}
		var parts []string
		listed := make(map[string]bool)
		for _, bl := range blockers {
			if !listed[bl.ID] {
				listed[bl.ID] = true
				ids = append(ids, bl.ID)
				parts = append(parts, fmt.Sprintf("%s (%s)", bl.ID, bl.Status))
			}
		}
		findings = append(findings, AuditFinding{
			Kind:    AuditBlockedByDone,
			BeanIDs: ids,
			Message: fmt.Sprintf("%s is still linked as blocked, but all its blockers are done: %s", b.ID, strings.Join(parts, ", ")),
			Details: AuditDetails{Status: b.Status, Type: b.Type, Title: b.Title, Blockers: blockers},
		})
	}
	return findings
}

func typeLabel(b *bean.Bean) string {
	if b.Type == "" {
		return "bean"
	}
	return b.Type
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func allChildrenPhrase(n int) string {
	if n == 1 {
		return "its only child is"
	}
	return fmt.Sprintf("all %d children are", n)
}
