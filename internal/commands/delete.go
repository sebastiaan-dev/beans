package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/spf13/cobra"
)

var (
	forceDelete bool
	deleteJSON  bool
)

// beanWithLinks holds a bean and its incoming links for batch processing
type beanWithLinks struct {
	bean  *bean.Bean
	links []beancore.IncomingLink
}

var deleteCmd = &cobra.Command{
	Use:     "delete <id> [id...]",
	Aliases: []string{"rm"},
	Short:   "Delete one or more beans",
	Long: `Deletes one or more beans after confirmation (use -f to skip confirmation).

If other beans reference the target bean(s) (as parent or via blocking), you will be
warned and those references will be removed after confirmation. Use -f to skip all warnings.

The confirmation prompt is only shown when stdin is a terminal. When stdin is not
a terminal (pipes, /dev/null, scripts, coding agents), beans never prompts and
never deletes silently: it exits with an error asking you to re-run with -f/--force.
--json implies --force.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Runtime errors (not found, non-interactive stdin) are not usage errors.
		cmd.SilenceUsage = true
		ctx := context.Background()
		resolver := &beangraph.CoreResolver{Core: core}

		// Collect all beans and their incoming links upfront (validate before deleting)
		var targets []beanWithLinks
		for _, id := range args {
			b, err := resolver.Bean(ctx, id)
			if err != nil {
				return cmdError(deleteJSON, output.ErrNotFound, "failed to find bean: %v", err)
			}
			if b == nil {
				return cmdError(deleteJSON, output.ErrNotFound, "bean not found: %s", id)
			}
			targets = append(targets, beanWithLinks{
				bean:  b,
				links: core.FindIncomingLinks(b.ID),
			})
		}

		// Prompt for confirmation (JSON implies force)
		if !forceDelete && !deleteJSON {
			ok, err := confirmDeleteMultiple(targets)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}

		// Delete all beans via GraphQL mutation
		var deleted []*bean.Bean
		var totalLinksRemoved int
		for _, target := range targets {
			_, err := resolver.DeleteBean(ctx, target.bean.ID)
			if err != nil {
				return cmdError(deleteJSON, output.ErrFileError, "failed to delete bean %s: %v", target.bean.ID, err)
			}
			deleted = append(deleted, target.bean)
			totalLinksRemoved += len(target.links)
		}

		// Output results
		if deleteJSON {
			if len(deleted) == 1 {
				return output.Success(deleted[0], "Bean deleted")
			}
			return output.JSON(output.Response{
				Success: true,
				Beans:   deleted,
				Count:   len(deleted),
				Message: fmt.Sprintf("%d beans deleted", len(deleted)),
			})
		}

		if totalLinksRemoved > 0 {
			fmt.Printf("Removed %d reference(s)\n", totalLinksRemoved)
		}
		for _, b := range deleted {
			fmt.Printf("Deleted %s\n", b.Path)
		}
		return nil
	},
}

// confirmDeleteMultiple prompts the user to confirm deletion of one or more beans.
// It returns an error without prompting when stdin is not a terminal.
func confirmDeleteMultiple(targets []beanWithLinks) (bool, error) {
	return confirm(func(w io.Writer) { writeDeletePrompt(w, targets) }, "-f/--force")
}

// writeDeletePrompt renders the deletion confirmation prompt for targets to w.
func writeDeletePrompt(w io.Writer, targets []beanWithLinks) {
	beansWithLinks := 0
	totalLinks := 0
	for _, t := range targets {
		if len(t.links) > 0 {
			beansWithLinks++
			totalLinks += len(t.links)
		}
	}

	// Single bean: use simpler format
	if len(targets) == 1 {
		t := targets[0]
		if len(t.links) > 0 {
			fmt.Fprintf(w, "Warning: %d bean(s) link to '%s':\n", len(t.links), t.bean.Title)
			for _, link := range t.links {
				fmt.Fprintf(w, "  - %s (%s) via %s\n", link.FromBean.ID, link.FromBean.Title, link.LinkType)
			}
			fmt.Fprint(w, "Delete anyway and remove references? [y/N] ")
		} else {
			fmt.Fprintf(w, "Delete '%s' (%s)? [y/N] ", t.bean.Title, t.bean.Path)
		}
	} else {
		// Multiple beans: show batch summary
		fmt.Fprintf(w, "About to delete %d bean(s):\n", len(targets))
		for _, t := range targets {
			if len(t.links) > 0 {
				fmt.Fprintf(w, "  - %s (%s) ← %d incoming link(s)\n", t.bean.ID, t.bean.Title, len(t.links))
			} else {
				fmt.Fprintf(w, "  - %s (%s)\n", t.bean.ID, t.bean.Title)
			}
		}
		if beansWithLinks > 0 {
			fmt.Fprintf(w, "\nWarning: %d bean(s) have incoming references (%d total) that will be removed.\n", beansWithLinks, totalLinks)
		}
		fmt.Fprint(w, "\nProceed with deletion? [y/N] ")
	}
}

func RegisterDeleteCmd(root *cobra.Command) {
	deleteCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "Skip confirmation and warnings")
	deleteCmd.Flags().BoolVar(&deleteJSON, "json", false, "Output as JSON (implies --force)")
	root.AddCommand(deleteCmd)
}
