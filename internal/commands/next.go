package commands

import (
	"context"
	"fmt"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/spf13/cobra"
)

var nextJSON bool

const nextNoneMessage = "No beans ready to work on"

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show the single most important bean to work on next",
	Long: `Shows the single most important bean that is ready to be worked on.

Candidates are beans with status "todo" that are not blocked (explicitly or via
an ancestor) and do not inherit a completed/scrapped status from an ancestor;
draft, in-progress, completed and scrapped beans are never suggested.

Candidates are ranked by:
  1. priority (critical > high > normal > low > deferred; unset = normal)
  2. leaf work items (task, bug, feature) before containers (milestones,
     epics, or any bean that still has open children)
  3. creation date (oldest first)

With --json, a response envelope is printed: {"success": true, "bean": {...}}.
When nothing is ready, the "bean" field is omitted and "message" explains why.

Exit status is 0 both when a bean is found and when nothing is ready; it is
non-zero only on errors.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		resolver := &beangraph.CoreResolver{Core: core}

		b, err := findNextBean(ctx, resolver)
		if err != nil {
			return cmdError(nextJSON, output.ErrValidation, "querying beans: %v", err)
		}

		if b == nil {
			if nextJSON {
				return output.SuccessMessage(nextNoneMessage)
			}
			fmt.Println(ui.Muted.Render(nextNoneMessage + "."))
			fmt.Println(ui.Muted.Render("Check blocked beans with `beans list --is-blocked`, or everything with `beans list`."))
			return nil
		}

		if nextJSON {
			return output.Success(b, "Next bean to work on")
		}
		showStyledBean(b)
		return nil
	},
}

// findNextBean returns the highest-ranked ready bean, or nil if none.
func findNextBean(ctx context.Context, resolver *beangraph.CoreResolver) (*bean.Bean, error) {
	candidates, err := resolver.Beans(ctx, readyFilter())
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	all, err := resolver.Beans(ctx, nil)
	if err != nil {
		return nil, err
	}
	rankNextCandidates(candidates, resolver.Core.Config().PriorityNames(), openChildCounts(all))
	return candidates[0], nil
}

func RegisterNextCmd(root *cobra.Command) {
	nextCmd.Flags().BoolVar(&nextJSON, "json", false, "Output as JSON")
	root.AddCommand(nextCmd)
}
