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

// newStartCmd returns the `beans start` command.
func newStartCmd() *cobra.Command {
	var startJSON bool
	var startWT startWorktreeOptions

	cmd := &cobra.Command{
		Use:   "start <id> [--worktree [--name <name>] [--exec <program> [-- <args>...]]]",
		Short: "Start working on a bean (set it to in-progress)",
		Long: `Sets a bean's status to "in-progress" and shows its details.

Warnings are printed (but the bean is still started) when:
  - other work items are already in-progress (in-progress ancestors of the
    bean and milestones/epics are ignored), or
  - the bean is blocked, explicitly or via an ancestor.

Starting an archived bean moves it out of the archive.

With --worktree (-w), the bean is started in its own git worktree instead,
like the Beans UI's Start Work: beans creates the worktree (or reuses it when
it already exists), copies the bean into the worktree's .beans/ and sets it to
in-progress there. The bean in the main worktree is left unchanged; the status
change travels with the branch. The worktree/branch name is derived from the
bean ("<id>-<slug>", shortened) unless --name is given.

  - With worktree.backend: worktrunk, beans runs your usual
    "wt switch [--create] <name> --base <base_ref> --no-cd" in this terminal,
    so all worktrunk hooks (including switch hooks) run and wt can ask you to
    approve project hooks. A child process can't change your shell's
    directory, so cd into the printed path (or use --exec).
  - With the git backend, the worktree is created like beans-serve does
    (branch beans/<name> under worktree.path) and worktree.setup runs.

--exec (-x) runs a program in the worktree once the bean is prepared, with
arguments after "--", e.g.:

  beans start abc1 -w -x claude -- "Work on bean abc1"`,
		Args: func(cmd *cobra.Command, args []string) error {
			_, _, err := parseStartArgs(args, cmd.ArgsLenAtDash())
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			id, execArgs, err := parseStartArgs(args, cmd.ArgsLenAtDash())
			if err != nil {
				return err
			}
			opts := startWT
			opts.execArgs, opts.json = execArgs, startJSON
			if err := opts.validate(); err != nil {
				return err
			}
			if opts.worktree {
				cmd.SilenceUsage = true
				return runStartWorktree(ctx, id, &opts)
			}

			resolver := &beangraph.CoreResolver{Core: core}
			res, err := transitionBean(ctx, resolver, id, "in-progress", "", startJSON)
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

	cmd.Flags().BoolVar(&startJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVarP(&startWT.worktree, "worktree", "w", false, "Start the bean in its own git worktree (created or reused)")
	cmd.Flags().StringVar(&startWT.name, "name", "", "Worktree/branch name for --worktree (default: derived from the bean)")
	cmd.Flags().StringVarP(&startWT.exec, "exec", "x", "", "Program to run in the worktree after starting (arguments after --)")
	return cmd
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
	root.AddCommand(newStartCmd())
}
