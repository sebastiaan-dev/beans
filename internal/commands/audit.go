package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// The audit itself runs in beancore (Core.RunAudit, shared with the TUI's
// audit view); these names are the command's view of it.
type (
	auditParams  = beancore.AuditRunParams
	auditResult  = beancore.AuditReport
	auditGitInfo = beancore.AuditGitInfo
)

var (
	auditKindTitles = beancore.AuditKindTitles
	auditKindHints  = beancore.AuditKindHints

	repoRelativePaths  = gitutil.RepoRelativePaths
	bookkeepingCommits = gitutil.CommitsChangingOnly
)

// runAudit audits c (see beancore.Core.RunAudit).
func runAudit(c *beancore.Core, p auditParams) (auditResult, error) {
	return c.RunAudit(p)
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
	auditCmd.Flags().IntVar(&auditStaleDays, "stale-days", beancore.DefaultAuditStaleDays, "Flag in-progress beans not updated for more than this many days (0 disables)")
	auditCmd.Flags().StringVar(&auditSince, "since", "", "Only scan commits newer than this date (passed to git log --since, e.g. 2024-01-01 or \"3 months ago\")")
	auditCmd.Flags().StringVar(&auditBaseRef, "base-ref", "", "Git ref whose history is scanned (default: worktree.base_ref, or main)")
	auditCmd.Flags().BoolVar(&auditNoGit, "no-git", false, "Skip the git history check (landed-but-open)")
	auditCmd.Flags().BoolVar(&auditExitZero, "exit-zero", false, "Exit with status 0 even when there are findings")
	root.AddCommand(auditCmd)
}
