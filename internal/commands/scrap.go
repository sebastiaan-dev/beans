package commands

import (
	"context"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/spf13/cobra"
)

var (
	scrapReason string
	scrapJSON   bool
)

var scrapCmd = &cobra.Command{
	Use:   "scrap <id> --reason <text>",
	Short: "Mark a bean as scrapped, recording why",
	Long: `Sets a bean's status to "scrapped".

The --reason flag is required: a "## Reasons for Scrapping" section containing
the given text is appended to the bean body, preserving project memory.
Use '-' to read the reason from stdin.

If the bean is already scrapped, nothing is changed (no reason is appended).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Checked here rather than via MarkFlagRequired so that --json
		// callers get a structured error.
		if !cmd.Flags().Changed("reason") {
			return cmdError(scrapJSON, output.ErrValidation, "--reason is required")
		}
		text, err := resolveAppendContent(scrapReason)
		if err != nil {
			return cmdError(scrapJSON, output.ErrValidation, "%s", err)
		}
		section := buildSection(scrapSectionHeading, text)
		if section == "" {
			return cmdError(scrapJSON, output.ErrValidation, "--reason must not be empty")
		}

		resolver := &beangraph.CoreResolver{Core: core}
		res, err := transitionBean(context.Background(), resolver, args[0], "scrapped", section, scrapJSON)
		if err != nil {
			return err
		}
		return reportTransition(res, scrapJSON, "scrapped", "Scrapped")
	},
}

func RegisterScrapCmd(root *cobra.Command) {
	scrapCmd.Flags().StringVarP(&scrapReason, "reason", "m", "", "Reason for scrapping, appended to the body (required; use '-' for stdin)")
	scrapCmd.Flags().BoolVar(&scrapJSON, "json", false, "Output as JSON")
	root.AddCommand(scrapCmd)
}
