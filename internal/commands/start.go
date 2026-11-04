package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/spf13/cobra"
)

var startJSON bool

var startCmd = &cobra.Command{
	Use:   "start <id>",
	Short: "Start working on a bean (set it to in-progress)",
	Long: `Sets a bean's status to "in-progress" and shows its details.

Warnings are printed (but the bean is still started) when:
  - other work items are already in-progress (in-progress ancestors of the
    bean and milestones/epics are ignored), or
  - the bean is blocked, explicitly or via an ancestor.

Starting an archived bean moves it out of the archive.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		resolver := &beangraph.CoreResolver{Core: core}

		res, err := transitionBean(ctx, resolver, args[0], "in-progress", "", startJSON)
		if err != nil {
			return err
		}
		b := res.Bean

		warnings, err := startWarnings(ctx, resolver, b)
		if err != nil {
			return cmdError(startJSON, output.ErrValidation, "querying beans: %v", err)
		}
		if res.Unchanged {
			warnings = append([]string{"bean is already in-progress"}, warnings...)
		}

		if startJSON {
			msg := "Bean started"
			switch {
			case res.Unchanged:
				msg = "Bean already in-progress"
			case res.Unarchived:
				msg = "Bean unarchived and started"
			}
			return output.SuccessWithWarnings(b, msg, warnings)
		}

		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, ui.Warning.Render("Warning: ")+w)
		}
		if !res.Unchanged {
			verb := "Started "
			if res.Unarchived {
				verb = "Unarchived and started "
			}
			fmt.Println(ui.Success.Render(verb) + ui.ID.Render(b.ID) + " " + ui.Muted.Render(b.Path))
			fmt.Println()
		}
		showStyledBean(b)
		return nil
	},
}

// startWarnings returns human-readable warnings about starting b: other
// work items already in progress, and active blockers.
func startWarnings(ctx context.Context, resolver *beangraph.CoreResolver, b *bean.Bean) ([]string, error) {
	inProgress, err := resolver.Beans(ctx, &model.BeanFilter{Status: []string{"in-progress"}})
	if err != nil {
		return nil, err
	}
	all, err := resolver.Beans(ctx, nil)
	if err != nil {
		return nil, err
	}

	var warnings []string
	if others := otherInProgress(b, inProgress, all); len(others) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d other bean(s) already in-progress: %s",
			len(others), formatBeanRefs(others)))
	}
	if blockers := resolver.Core.FindAllBlockers(b.ID); len(blockers) > 0 {
		warnings = append(warnings, fmt.Sprintf("bean is blocked by: %s", formatBeanRefs(blockers)))
	}
	return warnings, nil
}

// otherInProgress returns the in-progress beans that compete with b for
// attention: it excludes b itself, b's ancestors (an in-progress parent epic
// is expected), and milestones/epics in general.
func otherInProgress(b *bean.Bean, inProgress, all []*bean.Bean) []*bean.Bean {
	byID := make(map[string]*bean.Bean, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	ancestors := make(map[string]bool)
	for p := b.Parent; p != "" && !ancestors[p]; {
		ancestors[p] = true
		parent, ok := byID[p]
		if !ok {
			break
		}
		p = parent.Parent
	}

	var others []*bean.Bean
	for _, x := range inProgress {
		if x.ID == b.ID || ancestors[x.ID] || isContainerType(x.Type) {
			continue
		}
		others = append(others, x)
	}
	return others
}

// formatBeanRefs formats beans as "id (title), id (title)".
func formatBeanRefs(beans []*bean.Bean) string {
	s := ""
	for i, b := range beans {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s (%s)", b.ID, b.Title)
	}
	return s
}

func RegisterStartCmd(root *cobra.Command) {
	startCmd.Flags().BoolVar(&startJSON, "json", false, "Output as JSON")
	root.AddCommand(startCmd)
}
