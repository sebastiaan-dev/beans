package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

var (
	progressParent string
	progressJSON   bool
)

const progressBarWidth = 30

// progressReport is the JSON output of `beans progress`.
type progressReport struct {
	// Scope is the bean whose subtree was counted; nil for the whole project.
	Scope *bean.Bean `json:"scope,omitempty"`
	progressStats
	// Blocked counts beans that are not completed/scrapped and are blocked
	// explicitly or via an ancestor.
	Blocked int `json:"blocked"`
}

var progressCmd = &cobra.Command{
	Use:   "progress",
	Short: "Show a summary of work progress by status",
	Long: `Shows how many beans have each status, plus a progress bar.

Percent complete is completed / (total - scrapped): scrapped beans are never
going to be done, so they don't count against progress. Counts use each bean's
own status.

With --parent, only the descendants of the given bean (e.g. a milestone or
epic) are counted, not the bean itself.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		resolver := &beangraph.CoreResolver{Core: core}

		report, err := buildProgressReport(ctx, resolver, progressParent)
		if err != nil {
			return cmdError(progressJSON, errorCode(err), "%v", err)
		}

		if progressJSON {
			return writeJSON(report)
		}

		fmt.Print(renderProgressReport(report, resolver.Core.Config()))
		return nil
	},
}

// buildProgressReport computes progress for the whole project, or for the
// subtree below parentID when it is non-empty.
func buildProgressReport(ctx context.Context, resolver *beangraph.CoreResolver, parentID string) (*progressReport, error) {
	all, err := resolver.Beans(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("querying beans: %w", err)
	}

	report := &progressReport{}
	scoped := all
	if parentID != "" {
		root, err := resolver.Bean(ctx, parentID)
		if err != nil {
			return nil, fmt.Errorf("failed to find bean: %w", err)
		}
		if root == nil {
			return nil, &notFoundError{id: parentID}
		}
		report.Scope = root
		scoped = descendantsOf(all, root.ID)
	}

	report.progressStats = computeProgress(scoped, resolver.Core.Config().StatusNames())
	for _, b := range scoped {
		if !isTerminalStatus(b.Status) && resolver.Core.IsBlocked(b.ID) {
			report.Blocked++
		}
	}
	return report, nil
}

// renderProgressReport renders the human-readable progress summary.
func renderProgressReport(r *progressReport, c *config.Config) string {
	var sb strings.Builder
	if r.Scope != nil {
		sb.WriteString(ui.Bold.Render("Progress of ") + ui.ID.Render(r.Scope.ID) + " " + ui.Title.Render(r.Scope.Title) + "\n")
	} else {
		sb.WriteString(ui.Bold.Render("Project progress") + "\n")
	}
	if r.Total == 0 {
		sb.WriteString(ui.Muted.Render("No beans found.") + "\n")
		return sb.String()
	}

	names := statusNamesOrdered(r.progressStats, c)
	width := 0
	for _, s := range names {
		width = max(width, len(statusLabel(s)))
	}
	for _, s := range names {
		label := fmt.Sprintf("%-*s", width+1, statusLabel(s)+":")
		count := fmt.Sprintf("%d", r.ByStatus[s])
		if r.ByStatus[s] == 0 {
			sb.WriteString(ui.Muted.Render(label+" "+count) + "\n")
			continue
		}
		sb.WriteString(label + " " + ui.Bold.Render(count) + "\n")
	}
	sb.WriteString(fmt.Sprintf("%-*s %d\n", width+1, "Total:", r.Total))
	if r.Blocked > 0 {
		sb.WriteString(ui.Warning.Render(fmt.Sprintf("Blocked: %d open bean(s)", r.Blocked)) + "\n")
	}
	sb.WriteString(ui.Success.Render(renderProgressBar(r.Percent, progressBarWidth)) +
		fmt.Sprintf(" %d%% complete (%s)\n", r.Percent, formatCountLine(r.progressStats)))
	return sb.String()
}

func RegisterProgressCmd(root *cobra.Command) {
	progressCmd.Flags().StringVar(&progressParent, "parent", "", "Only count descendants of this bean (e.g. a milestone or epic)")
	progressCmd.Flags().BoolVar(&progressJSON, "json", false, "Output as JSON")
	root.AddCommand(progressCmd)
}
