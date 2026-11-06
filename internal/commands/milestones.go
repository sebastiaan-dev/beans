package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

var (
	milestonesAll  bool
	milestonesJSON bool
)

const milestoneBarWidth = 20

// milestoneProgress is one entry of the `beans milestones --json` output.
type milestoneProgress struct {
	Milestone *bean.Bean `json:"milestone"`
	// Progress counts all descendants of the milestone (not just direct children).
	Progress progressStats `json:"progress"`
}

var milestonesCmd = &cobra.Command{
	Use:   "milestones",
	Short: "List milestones with their progress",
	Long: `Lists beans of type "milestone" with the progress of all their descendants
(children, grandchildren, ...).

Completed and scrapped milestones are hidden unless --all is given.
Milestones are sorted by priority, then by creation date (oldest first).

Progress is shown as completed/(total - scrapped), matching ` + "`beans progress`" + `.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		resolver := &beangraph.CoreResolver{Core: core}

		list, err := buildMilestoneList(ctx, resolver, milestonesAll)
		if err != nil {
			return cmdError(milestonesJSON, output.ErrValidation, "querying beans: %v", err)
		}

		if milestonesJSON {
			return writeJSON(list)
		}

		fmt.Print(renderMilestoneList(list, resolver.Core.Config()))
		return nil
	},
}

// buildMilestoneList returns milestones with descendant progress, sorted by
// priority then creation date. Completed/scrapped milestones are omitted
// unless includeAll is set.
func buildMilestoneList(ctx context.Context, resolver *beangraph.CoreResolver, includeAll bool) ([]milestoneProgress, error) {
	filter := &model.BeanFilter{Type: []string{"milestone"}}
	if !includeAll {
		filter.ExcludeStatus = []string{"completed", "scrapped"}
	}
	milestones, err := resolver.Beans(ctx, filter)
	if err != nil {
		return nil, err
	}
	all, err := resolver.Beans(ctx, nil)
	if err != nil {
		return nil, err
	}

	c := resolver.Core.Config()
	sortMilestones(milestones, c.PriorityNames())

	list := make([]milestoneProgress, 0, len(milestones))
	for _, m := range milestones {
		list = append(list, milestoneProgress{
			Milestone: m,
			Progress:  computeProgress(descendantsOf(all, m.ID), c.StatusNames()),
		})
	}
	return list, nil
}

// sortMilestones orders milestones by priority (unset = normal), then
// oldest created first, then ID.
func sortMilestones(milestones []*bean.Bean, priorityNames []string) {
	// All milestones are containers, so the container tie-breaker of the
	// `next` ranking never applies: this is priority, then created, then ID.
	beangraph.RankNextCandidates(milestones, priorityNames, nil)
}

// renderMilestoneList renders the human-readable milestone list.
func renderMilestoneList(list []milestoneProgress, c *config.Config) string {
	if len(list) == 0 {
		return ui.Muted.Render("No milestones found.") + "\n"
	}

	maxID := 0
	for _, mp := range list {
		maxID = max(maxID, len(mp.Milestone.ID))
	}

	var sb strings.Builder
	for _, mp := range list {
		m := mp.Milestone
		statusColor := "gray"
		if s := c.GetStatus(m.Status); s != nil {
			statusColor = s.Color
		}
		sb.WriteString(ui.ID.Render(fmt.Sprintf("%-*s", maxID, m.ID)) + " ")
		sb.WriteString(ui.RenderStatusTextWithColor(fmt.Sprintf("%-11s", m.Status), statusColor, c.IsArchiveStatus(m.Status)) + " ")
		sb.WriteString(ui.Title.Render(m.Title) + "\n")

		indent := strings.Repeat(" ", maxID+1)
		if mp.Progress.Total == 0 {
			sb.WriteString(indent + ui.Muted.Render("no child beans") + "\n")
			continue
		}
		sb.WriteString(indent + ui.Success.Render(renderProgressBar(mp.Progress.Percent, milestoneBarWidth)) +
			fmt.Sprintf(" %3d%%  %s\n", mp.Progress.Percent, formatCountLine(mp.Progress)))
	}
	return sb.String()
}

func RegisterMilestonesCmd(root *cobra.Command) {
	milestonesCmd.Flags().BoolVar(&milestonesAll, "all", false, "Include completed and scrapped milestones")
	milestonesCmd.Flags().BoolVar(&milestonesJSON, "json", false, "Output as JSON")
	root.AddCommand(milestonesCmd)
}
