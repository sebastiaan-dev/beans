package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/spf13/cobra"
)

var (
	auditJSON      bool
	auditStaleDays int
	auditSince     string
	auditBaseRef   string
	auditNoGit     bool
	auditExitZero  bool
)

// auditGitInfo describes the git history scan behind landed-but-open.
type auditGitInfo struct {
	BaseRef        string `json:"base_ref"`
	Since          string `json:"since,omitempty"`
	CommitsScanned int    `json:"commits_scanned"`
	// BookkeepingCommits counts commits that mention an open bean but only
	// change bean files (e.g. "chore: create bean ..."); they are ignored.
	BookkeepingCommits int `json:"bookkeeping_commits_ignored"`
}

type auditResult struct {
	Clean    bool                    `json:"clean"`
	Count    int                     `json:"count"`
	Findings []beancore.AuditFinding `json:"findings"`
	// Notes explains checks that were skipped (e.g. not a git repository).
	Notes []string      `json:"notes"`
	Git   *auditGitInfo `json:"git,omitempty"`
}

var auditKindTitles = map[beancore.AuditKind]string{
	beancore.AuditLandedButOpen:             "Open beans already referenced on the base branch",
	beancore.AuditContainerDoneChildrenOpen: "Completed/scrapped beans with open children",
	beancore.AuditContainerReadyToClose:     "Open beans whose children are all done",
	beancore.AuditStaleInProgress:           "Stale in-progress beans",
	beancore.AuditBlockedByDone:             "Stale blocking links (all blockers done)",
}

var auditKindHints = map[beancore.AuditKind]string{
	beancore.AuditLandedButOpen:             "If the work is done: beans update <id> -s completed",
	beancore.AuditContainerDoneChildrenOpen: "Complete/scrap the children, or reopen the parent",
	beancore.AuditContainerReadyToClose:     "If nothing is left: beans update <id> -s completed",
	beancore.AuditStaleInProgress:           "Update, finish, or move back to todo",
	beancore.AuditBlockedByDone:             "beans update <id> --remove-blocked-by <blocker> (or <blocker> --remove-blocking <id>)",
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Report beans whose workflow state looks out of date",
	Long: `Audits workflow and state hygiene of your beans. Unlike 'beans check'
(configuration, broken links, cycles, duplicate IDs), audit looks for beans
whose status has likely drifted from reality:

  landed-but-open               open (todo/in-progress/draft) beans whose ID is
                                referenced in a commit message reachable from
                                the base branch (worktree.base_ref, default main)
  container-done-children-open  completed/scrapped beans with open children
  container-ready-to-close      open beans with children that are all
                                completed/scrapped
  stale-in-progress             in-progress beans not updated for more than
                                --stale-days days
  blocked-by-done               open beans whose explicit blockers are all
                                completed/scrapped (stale blocking links)

Bean IDs are matched in commit messages as whole tokens ("Refs: beans-abc1",
"(beans-abc1)", "Fixes beans-abc1"); the base branch history is read with a
single 'git log' pass, optionally limited with --since. Commits that only
change bean files or the beans config (e.g. "chore: create bean beans-abc1")
are bookkeeping, not landed work, and are ignored. When the project is
not in a git repository, or the base ref does not exist, the git-based check
is skipped with a note and the remaining checks still run.

Exit status: 0 when there are no findings, 1 when there are findings (or on
error). Use --exit-zero to always exit 0 on success, e.g. in hooks.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if auditStaleDays < 0 {
			return cmdError(auditJSON, output.ErrValidation, "--stale-days must be >= 0")
		}

		baseRef := auditBaseRef
		if baseRef == "" {
			baseRef = cfg.GetWorktreeBaseRef()
		}
		bookkeeping := []string{core.Root()}
		if cwd, err := os.Getwd(); err == nil {
			if path, err := resolveConfigFile(configPath, cwd); err == nil && path != "" {
				bookkeeping = append(bookkeeping, path)
			}
		}
		result, err := runAudit(core, auditParams{
			BaseRef:          baseRef,
			BookkeepingPaths: bookkeeping,
			Since:            auditSince,
			StaleDays:        auditStaleDays,
			NoGit:            auditNoGit,
			Now:              time.Now(),
		})
		if err != nil {
			return cmdError(auditJSON, "GIT_ERROR", "%v", err)
		}

		if auditJSON {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			printAuditReport(os.Stdout, result)
		}

		if !result.Clean && !auditExitZero {
			os.Exit(1)
		}
		return nil
	},
}

// auditParams are the resolved inputs of runAudit.
type auditParams struct {
	BaseRef string
	// BookkeepingPaths are files/directories (absolute paths) whose
	// exclusive modification marks a commit as bean bookkeeping: typically
	// the beans directory and the config file.
	BookkeepingPaths []string
	Since            string
	StaleDays        int
	NoGit            bool
	Now              time.Time
}

// runAudit audits c, scanning the git history of the repository containing
// c's beans directory when possible.
func runAudit(c *beancore.Core, p auditParams) (auditResult, error) {
	result := auditResult{Notes: []string{}}
	opts := beancore.AuditOptions{
		Now:        p.Now,
		StaleAfter: time.Duration(p.StaleDays) * 24 * time.Hour,
	}

	dir := c.Root()
	switch {
	case p.NoGit:
		result.Notes = append(result.Notes, "landed-but-open skipped: --no-git")
	case !gitutil.IsRepo(dir):
		result.Notes = append(result.Notes, "landed-but-open skipped: not a git repository")
	case !gitutil.RefExists(dir, p.BaseRef):
		result.Notes = append(result.Notes, fmt.Sprintf("landed-but-open skipped: base ref %q not found (set worktree.base_ref or use --base-ref)", p.BaseRef))
	default:
		info := &auditGitInfo{BaseRef: p.BaseRef, Since: p.Since}
		result.Git = info
		opts.Commits = func(yield func(beancore.AuditCommit) bool) error {
			return gitutil.ScanCommits(dir, p.BaseRef, p.Since, func(gc gitutil.Commit) bool {
				info.CommitsScanned++
				return yield(beancore.AuditCommit{Hash: gc.Hash, ShortHash: gc.ShortHash, Subject: gc.Subject, Message: gc.Message})
			})
		}
		if rules := repoRelativePaths(dir, p.BookkeepingPaths); len(rules) > 0 {
			opts.IgnoreCommits = func(hashes []string) (map[string]bool, error) {
				paths, err := gitutil.ChangedPaths(dir, hashes)
				if err != nil {
					return nil, err
				}
				ignored := bookkeepingCommits(paths, rules)
				info.BookkeepingCommits = len(ignored)
				return ignored, nil
			}
		}
	}
	if p.StaleDays <= 0 {
		result.Notes = append(result.Notes, "stale-in-progress skipped: --stale-days 0")
	}

	findings, err := c.Audit(opts)
	if err != nil {
		return result, fmt.Errorf("scanning git history: %w", err)
	}
	if findings == nil {
		findings = []beancore.AuditFinding{}
	}
	result.Findings = findings
	result.Count = len(findings)
	result.Clean = len(findings) == 0
	return result, nil
}

// repoRelativePaths converts absolute paths to slash-separated paths
// relative to the root of the git work tree containing dir, dropping paths
// outside of it.
func repoRelativePaths(dir string, paths []string) []string {
	top, err := gitutil.TopLevel(dir)
	if err != nil {
		return nil
	}
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		// The path may not exist (yet); resolve its parent instead.
		if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
			return filepath.Join(r, filepath.Base(p))
		}
		return p
	}
	top = resolve(top)
	var rels []string
	for _, p := range paths {
		rel, err := filepath.Rel(top, resolve(p))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	return rels
}

// bookkeepingCommits returns the commits whose changed files all match one
// of rules (repo-relative paths: a file, or a directory matching everything
// below it). Commits with no reported paths (empty or merge commits) are not
// considered bookkeeping.
func bookkeepingCommits(paths map[string][]string, rules []string) map[string]bool {
	matches := func(f string) bool {
		for _, r := range rules {
			r = strings.TrimSuffix(r, "/")
			if f == r || strings.HasPrefix(f, r+"/") {
				return true
			}
		}
		return false
	}
	ignored := make(map[string]bool)
	for hash, files := range paths {
		if len(files) == 0 {
			continue
		}
		all := true
		for _, f := range files {
			if !matches(f) {
				all = false
				break
			}
		}
		if all {
			ignored[hash] = true
		}
	}
	return ignored
}

// printAuditReport prints findings grouped by kind.
func printAuditReport(w io.Writer, result auditResult) {
	byKind := make(map[beancore.AuditKind][]beancore.AuditFinding)
	for _, f := range result.Findings {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}

	for _, kind := range beancore.AuditKinds {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s %s\n", ui.Bold.Render(fmt.Sprintf("%s (%d)", auditKindTitles[kind], len(group))), ui.Muted.Render("["+string(kind)+"]"))
		for _, f := range group {
			fmt.Fprintf(w, "  %s %s\n", ui.Danger.Render("✗"), f.Message)
			if f.Details.Title != "" {
				fmt.Fprintf(w, "    %s\n", ui.Muted.Render(f.Details.Title))
			}
		}
		fmt.Fprintf(w, "  %s\n\n", ui.Muted.Render("→ "+auditKindHints[kind]))
	}

	for _, n := range result.Notes {
		fmt.Fprintf(w, "%s %s\n", ui.Warning.Render("!"), n)
	}
	if result.Git != nil {
		scope := ""
		if result.Git.Since != "" {
			scope = " since " + result.Git.Since
		}
		ignored := ""
		if result.Git.BookkeepingCommits > 0 {
			ignored = fmt.Sprintf(" (ignored %d bean-only commit(s))", result.Git.BookkeepingCommits)
		}
		fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("Scanned %d commit(s) on %s%s%s", result.Git.CommitsScanned, result.Git.BaseRef, scope, ignored)))
	}

	switch result.Count {
	case 0:
		fmt.Fprintln(w, ui.Success.Render("No audit findings"))
	case 1:
		fmt.Fprintln(w, ui.Danger.Render("1 finding"))
	default:
		fmt.Fprintln(w, ui.Danger.Render(fmt.Sprintf("%d findings", result.Count)))
	}
}

func RegisterAuditCmd(root *cobra.Command) {
	auditCmd.Flags().BoolVar(&auditJSON, "json", false, "Output as JSON")
	auditCmd.Flags().IntVar(&auditStaleDays, "stale-days", 14, "Flag in-progress beans not updated for more than this many days (0 disables)")
	auditCmd.Flags().StringVar(&auditSince, "since", "", "Only scan commits newer than this date (passed to git log --since, e.g. 2024-01-01 or \"3 months ago\")")
	auditCmd.Flags().StringVar(&auditBaseRef, "base-ref", "", "Git ref whose history is scanned (default: worktree.base_ref, or main)")
	auditCmd.Flags().BoolVar(&auditNoGit, "no-git", false, "Skip the git history check (landed-but-open)")
	auditCmd.Flags().BoolVar(&auditExitZero, "exit-zero", false, "Exit with status 0 even when there are findings")
	root.AddCommand(auditCmd)
}
