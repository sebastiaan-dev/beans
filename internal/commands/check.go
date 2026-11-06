package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

var (
	checkJSON bool
	checkFix  bool
)

type checkResult struct {
	Success      bool                      `json:"success"`
	ConfigErrors []string                  `json:"config_errors"`
	BeanIssues   *beancore.LinkCheckResult `json:"bean_issues,omitempty"`
	DuplicateIDs []beancore.DuplicateID    `json:"duplicate_ids"`
	Fixed        int                       `json:"fixed,omitempty"`
	// Notices are informational hints that never fail the check.
	Notices []string `json:"notices,omitempty"`
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate configuration and bean integrity",
	Long: `Checks configuration and bean integrity, including:
- Configuration settings (colors, default type)
- Duplicate bean IDs (multiple files claiming the same ID)
- Broken links (links to non-existent beans)
- Self-references (beans linking to themselves)
- Circular dependencies (cycles in blocks/parent relationships)

Use --fix to automatically remove broken links and self-references.
Note: Cycles cannot be auto-fixed and require manual intervention.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var configErrors []string
		var fixed int

		// === Configuration checks ===
		if !checkJSON {
			fmt.Println(ui.Bold.Render("Configuration"))
		}

		// 1. Check statuses are defined (always true since hardcoded)
		if !checkJSON {
			fmt.Printf("  %s Statuses defined (%d hardcoded)\n", ui.Success.Render("✓"), len(config.DefaultStatuses))
		}

		// 2. Check default_status exists in statuses (always true since hardcoded)
		if !checkJSON {
			fmt.Printf("  %s Default status '%s' exists\n", ui.Success.Render("✓"), cfg.GetDefaultStatus())
		}

		// 2b. Check default_type is a valid hardcoded type
		if cfg.GetDefaultType() != "" && !cfg.IsValidType(cfg.GetDefaultType()) {
			configErrors = append(configErrors, fmt.Sprintf("default_type '%s' is not a valid type", cfg.GetDefaultType()))
		} else if cfg.GetDefaultType() != "" {
			if !checkJSON {
				fmt.Printf("  %s Default type '%s' is valid\n", ui.Success.Render("✓"), cfg.GetDefaultType())
			}
		}

		// 2c. Check agent.default_effort is a valid effort level
		if effort := cfg.GetDefaultEffort(); effort != "" && !config.IsValidEffortLevel(effort) {
			configErrors = append(configErrors, fmt.Sprintf("agent.default_effort '%s' is not valid (use low, medium, high, or max)", effort))
		} else if effort != "" {
			if !checkJSON {
				fmt.Printf("  %s Default effort '%s' is valid\n", ui.Success.Render("✓"), effort)
			}
		}

		// 2d. Check worktree.backend and suggest worktrunk when it is installed.
		// Suggestions and a missing wt binary are informational only.
		backend := cfg.GetWorktreeBackend()
		if !config.IsValidWorktreeBackend(backend) {
			configErrors = append(configErrors, fmt.Sprintf("worktree.backend '%s' is not valid (use git or worktrunk)", backend))
		} else if !checkJSON {
			fmt.Printf("  %s Worktree backend '%s' is valid\n", ui.Success.Render("✓"), backend)
		}
		var notices []string
		if notice := worktreeBackendNotice(backend, worktree.WorktrunkAvailable()); notice != "" {
			notices = append(notices, notice)
			if !checkJSON {
				fmt.Printf("  %s %s\n", ui.Muted.Render("i"), notice)
			}
		}

		// 3. Check all status colors are valid (hardcoded statuses)
		for _, s := range config.DefaultStatuses {
			if !ui.IsValidColor(s.Color) {
				configErrors = append(configErrors, fmt.Sprintf("invalid color '%s' for status '%s'", s.Color, s.Name))
			}
		}
		if !checkJSON {
			colorErrors := 0
			for _, e := range configErrors {
				if len(e) > 13 && e[:13] == "invalid color" {
					colorErrors++
				}
			}
			if colorErrors == 0 {
				fmt.Printf("  %s All status colors valid\n", ui.Success.Render("✓"))
			}
		}

		// 4. Check all type colors are valid (hardcoded types)
		for _, t := range config.DefaultTypes {
			if !ui.IsValidColor(t.Color) {
				configErrors = append(configErrors, fmt.Sprintf("invalid color '%s' for type '%s'", t.Color, t.Name))
			}
		}
		if !checkJSON {
			typeColorErrors := 0
			for _, e := range configErrors {
				if len(e) > 13 && e[:13] == "invalid color" {
					typeColorErrors++
				}
			}
			if typeColorErrors == 0 {
				fmt.Printf("  %s All type colors valid\n", ui.Success.Render("✓"))
			}
		}

		// Print config errors in human-readable mode
		if !checkJSON {
			for _, e := range configErrors {
				fmt.Printf("  %s %s\n", ui.Danger.Render("✗"), e)
			}
		}

		// === Bean file checks ===
		if !checkJSON {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Bean Files"))
		}

		duplicateIDs, err := core.FindDuplicateIDs()
		if err != nil {
			return err
		}
		if duplicateIDs == nil {
			duplicateIDs = []beancore.DuplicateID{}
		}
		if !checkJSON {
			for _, d := range duplicateIDs {
				fmt.Printf("  %s Duplicate ID %s used by %d files: %s\n", ui.Danger.Render("✗"), d.ID, len(d.Paths), strings.Join(d.Paths, ", "))
			}
			if len(duplicateIDs) == 0 {
				fmt.Printf("  %s No duplicate IDs found\n", ui.Success.Render("✓"))
			}
		}

		// === Bean link checks ===
		if !checkJSON {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Bean Links"))
		}

		linkResult := core.CheckAllLinks()

		// Handle --fix mode
		if checkFix && (len(linkResult.BrokenLinks) > 0 || len(linkResult.SelfLinks) > 0) {
			fixedCount, err := core.FixBrokenLinks()
			if err != nil {
				return fmt.Errorf("fixing broken links: %w", err)
			}
			fixed = fixedCount

			if !checkJSON {
				for _, bl := range linkResult.BrokenLinks {
					fmt.Printf("  %s %s: removed broken link %s:%s\n", ui.Success.Render("✓"), bl.BeanID, bl.LinkType, bl.Target)
				}
				for _, sl := range linkResult.SelfLinks {
					fmt.Printf("  %s %s: removed self-reference in %s link\n", ui.Success.Render("✓"), sl.BeanID, sl.LinkType)
				}
			}

			// Clear the fixed issues from the result
			linkResult.BrokenLinks = []beancore.BrokenLink{}
			linkResult.SelfLinks = []beancore.SelfLink{}
		} else if !checkJSON {
			// Report issues without fixing
			for _, bl := range linkResult.BrokenLinks {
				fmt.Printf("  %s %s: broken link %s:%s\n", ui.Danger.Render("✗"), bl.BeanID, bl.LinkType, bl.Target)
			}
			for _, sl := range linkResult.SelfLinks {
				fmt.Printf("  %s %s: self-reference in %s link\n", ui.Danger.Render("✗"), sl.BeanID, sl.LinkType)
			}
		}

		// Cycles cannot be auto-fixed
		if !checkJSON {
			for _, c := range linkResult.Cycles {
				if checkFix {
					fmt.Printf("  %s Cannot auto-fix cycle: %s (via %s)\n", ui.Warning.Render("!"), formatCycle(c.Path), c.LinkType)
				} else {
					fmt.Printf("  %s Circular dependency: %s (via %s)\n", ui.Danger.Render("✗"), formatCycle(c.Path), c.LinkType)
				}
			}
		}

		// Show success if no issues
		if !checkJSON && !linkResult.HasIssues() && fixed == 0 {
			fmt.Printf("  %s No link issues found\n", ui.Success.Render("✓"))
		}

		// === Summary ===
		totalIssues := len(configErrors) + len(duplicateIDs) + linkResult.TotalIssues()

		if checkJSON {
			result := checkResult{
				Success:      totalIssues == 0,
				ConfigErrors: configErrors,
				BeanIssues:   linkResult,
				DuplicateIDs: duplicateIDs,
				Fixed:        fixed,
				Notices:      notices,
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Println()
			if totalIssues == 0 && fixed == 0 {
				fmt.Println(ui.Success.Render("All checks passed"))
			} else if totalIssues == 0 && fixed > 0 {
				fmt.Println(ui.Success.Render(fmt.Sprintf("Fixed %d issue(s)", fixed)))
			} else if fixed > 0 {
				// Some issues fixed, some remain (cycles)
				fmt.Println(ui.Warning.Render(fmt.Sprintf("Fixed %d issue(s), %d require manual intervention", fixed, totalIssues)))
			} else if totalIssues == 1 {
				fmt.Println(ui.Danger.Render("1 issue found"))
			} else {
				fmt.Println(ui.Danger.Render(fmt.Sprintf("%d issues found", totalIssues)))
			}
		}

		// Exit with error code if validation failed
		if totalIssues > 0 {
			os.Exit(1)
		}

		return nil
	},
}

// worktreeBackendNotice returns an informational hint about the worktree
// backend: suggest worktrunk when wt is installed but not used, and point out
// a missing wt when the worktrunk backend is configured (beans-serve won't
// start then). Returns "" when there is nothing to say.
func worktreeBackendNotice(backend config.WorktreeBackend, wtAvailable bool) string {
	switch {
	case backend == config.WorktreeBackendGit && wtAvailable:
		return "worktrunk (wt) found on PATH; set worktree.backend: worktrunk to manage workspaces with it"
	case backend == config.WorktreeBackendWorktrunk && !wtAvailable:
		return "worktree.backend is worktrunk but wt was not found on PATH; beans-serve needs it to manage workspaces"
	}
	return ""
}

func RegisterCheckCmd(root *cobra.Command) {
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "Output as JSON")
	checkCmd.Flags().BoolVar(&checkFix, "fix", false, "Automatically fix broken links and self-references")
	root.AddCommand(checkCmd)
}
