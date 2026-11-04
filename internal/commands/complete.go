package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/spf13/cobra"
)

var (
	completeSummary string
	completeJSON    bool
)

var completeCmd = &cobra.Command{
	Use:   "complete <id>",
	Short: "Mark a bean as completed",
	Long: `Sets a bean's status to "completed".

With --summary, a "## Summary of Changes" section containing the given text is
appended to the bean body. Use '-' to read the summary from stdin.

If the bean is already completed, nothing is changed (no summary is appended).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		section := ""
		if cmd.Flags().Changed("summary") {
			text, err := resolveAppendContent(completeSummary)
			if err != nil {
				return cmdError(completeJSON, output.ErrValidation, "%s", err)
			}
			section = buildSection(summarySectionHeading, text)
			if section == "" {
				return cmdError(completeJSON, output.ErrValidation, "--summary must not be empty")
			}
		}

		resolver := &beangraph.CoreResolver{Core: core}
		res, err := transitionBean(context.Background(), resolver, args[0], "completed", section, completeJSON)
		if err != nil {
			return err
		}
		return reportTransition(res, completeJSON, "completed", "Completed")
	},
}

// reportTransition prints the result of a complete/scrap transition.
func reportTransition(res *transitionResult, jsonMode bool, status, verb string) error {
	b := res.Bean
	if res.Unchanged {
		warning := fmt.Sprintf("bean is already %s; nothing changed", status)
		if jsonMode {
			return output.SuccessWithWarnings(b, "Bean already "+status, []string{warning})
		}
		fmt.Fprintln(os.Stderr, ui.Warning.Render("Warning: ")+warning)
		fmt.Println(ui.Muted.Render("Already "+status+" ") + ui.ID.Render(b.ID) + " " + b.Title)
		return nil
	}
	if jsonMode {
		return output.Success(b, "Bean "+status)
	}
	fmt.Println(ui.Success.Render(verb+" ") + ui.ID.Render(b.ID) + " " + b.Title)
	return nil
}

func RegisterCompleteCmd(root *cobra.Command) {
	completeCmd.Flags().StringVarP(&completeSummary, "summary", "m", "", "Summary of changes to append to the body (use '-' for stdin)")
	completeCmd.Flags().BoolVar(&completeJSON, "json", false, "Output as JSON")
	root.AddCommand(completeCmd)
}
