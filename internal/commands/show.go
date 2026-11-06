package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	showJSON     bool
	showRaw      bool
	showBodyOnly bool
	showETagOnly bool
)

var showCmd = &cobra.Command{
	Use:   "show <id> [id...]",
	Short: "Show a bean's contents",
	Long:  `Displays the full contents of one or more beans, including front matter and body.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resolver := &beangraph.CoreResolver{Core: core}

		// Collect all beans
		var beans []*bean.Bean
		for _, id := range args {
			b, err := resolver.Bean(context.Background(), id)
			if err != nil {
				if showJSON {
					return output.Error(output.ErrNotFound, err.Error())
				}
				return fmt.Errorf("failed to find bean: %w", err)
			}
			if b == nil {
				if showJSON {
					return output.Error(output.ErrNotFound, fmt.Sprintf("bean not found: %s", id))
				}
				return fmt.Errorf("bean not found: %s", id)
			}
			beans = append(beans, b)
		}

		// JSON output
		if showJSON {
			if len(beans) == 1 {
				return output.SuccessSingle(beans[0])
			}
			return output.SuccessMultiple(beans)
		}

		// Raw markdown output (frontmatter + body)
		if showRaw {
			for i, b := range beans {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				content, err := b.Render()
				if err != nil {
					return fmt.Errorf("failed to render bean: %w", err)
				}
				fmt.Print(string(content))
			}
			return nil
		}

		// Body only (no header, no styling)
		if showBodyOnly {
			for i, b := range beans {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				fmt.Print(b.Body)
			}
			return nil
		}

		// ETag only (for easy extraction in scripts)
		if showETagOnly {
			for i, b := range beans {
				if i > 0 {
					fmt.Println()
				}
				fmt.Print(b.ETag())
			}
			return nil
		}

		// Default: styled human-friendly output
		for i, b := range beans {
			if i > 0 {
				fmt.Println()
				fmt.Println(ui.Muted.Render(strings.Repeat("═", 60)))
				fmt.Println()
			}
			showStyledBean(b)
		}

		return nil
	},
}

// showStyledBean displays a single bean with styled output.
func showStyledBean(b *bean.Bean) {
	statusCfg := cfg.GetStatus(b.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isArchive := cfg.IsArchiveStatus(b.Status)

	var header strings.Builder
	header.WriteString(ui.ID.Render(b.ID))
	header.WriteString(" ")
	header.WriteString(ui.RenderStatusWithColor(b.Status, statusColor, isArchive))
	if b.IsArchived() {
		header.WriteString(" ")
		header.WriteString(ui.Muted.Render(ui.ArchivedMarker))
	}
	if implicitStatus, implicitStatusFrom := core.ImplicitStatus(b.ID); implicitStatus != "" {
		header.WriteString(" ")
		header.WriteString(ui.Muted.Render("↑" + implicitStatus + " (from " + implicitStatusFrom + ")"))
	}

	// Display type
	if b.Type != "" {
		typeCfg := cfg.GetType(b.Type)
		typeColor := "gray"
		if typeCfg != nil {
			typeColor = typeCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderTypeWithColor(b.Type, typeColor))
	}

	if b.Priority != "" {
		priorityCfg := cfg.GetPriority(b.Priority)
		priorityColor := "gray"
		if priorityCfg != nil {
			priorityColor = priorityCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderPriorityWithColor(b.Priority, priorityColor))
	}
	if len(b.Tags) > 0 {
		header.WriteString("  ")
		header.WriteString(ui.Muted.Render(strings.Join(b.Tags, ", ")))
	}
	if b.CreatedAt != nil {
		header.WriteString("  ")
		header.WriteString(ui.Muted.Render("created " + b.CreatedAt.Format("2006-01-02 15:04 UTC")))
	}
	if b.UpdatedAt != nil {
		header.WriteString("  ")
		header.WriteString(ui.Muted.Render("updated " + b.UpdatedAt.Format("2006-01-02 15:04 UTC")))
	}
	header.WriteString("\n")
	header.WriteString(ui.Title.Render(b.Title))

	// Display relationships
	if b.Parent != "" || len(b.Blocking) > 0 {
		header.WriteString("\n")
		header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))
		header.WriteString("\n")
		header.WriteString(formatRelationships(b))
	}

	header.WriteString("\n")
	header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))

	headerBox := lipgloss.NewStyle().
		MarginBottom(1).
		Render(header.String())

	fmt.Println(headerBox)

	// Render the body with Glamour
	if b.Body != "" {
		isTTY := term.IsTerminal(int(os.Stdout.Fd()))
		rendered, err := renderMarkdownBody(b.Body, isTTY, lipgloss.ColorProfile())
		if err != nil {
			fmt.Printf("failed to render markdown: %v\n", err)
			return
		}

		fmt.Print(rendered)
	}
}

// showBodyWordWrap is the column at which rendered bean bodies are wrapped.
const showBodyWordWrap = 80

// newBodyRenderer returns the glamour renderer used for bean bodies.
//
// On a terminal it uses the shared markdown style of the current theme (the
// same one as the TUI detail view; with the default theme, the 16-color ANSI
// palette) with the given color profile, so the output follows the theme. It deliberately avoids glamour's auto style, which emits
// 256-color codes and performs slow terminal background detection. When stdout
// is not a terminal, or colors are disabled (an Ascii profile, e.g. NO_COLOR or
// TERM=dumb), it uses glamour's plain "notty" style without escape sequences.
func newBodyRenderer(isTTY bool, profile termenv.Profile) (*glamour.TermRenderer, error) {
	if !isTTY || profile == termenv.Ascii {
		return glamour.NewTermRenderer(
			glamour.WithStandardStyle(glamourstyles.NoTTYStyle),
			glamour.WithColorProfile(termenv.Ascii),
			glamour.WithWordWrap(showBodyWordWrap),
		)
	}
	return glamour.NewTermRenderer(
		glamour.WithStyles(ui.MarkdownStyleConfig()),
		glamour.WithChromaFormatter(ui.MarkdownChromaFormatterFor(profile)),
		glamour.WithColorProfile(profile),
		glamour.WithWordWrap(showBodyWordWrap),
	)
}

// renderMarkdownBody renders a bean body as styled markdown.
func renderMarkdownBody(body string, isTTY bool, profile termenv.Profile) (string, error) {
	renderer, err := newBodyRenderer(isTTY, profile)
	if err != nil {
		return "", fmt.Errorf("failed to create renderer: %w", err)
	}
	return renderer.Render(body)
}

// formatRelationships formats parent and blocks for display.
func formatRelationships(b *bean.Bean) string {
	var parts []string

	// Display parent
	if b.Parent != "" {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("parent:"),
			ui.ID.Render(b.Parent)))
	}

	// Display blocking
	for _, target := range b.Blocking {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("blocking:"),
			ui.ID.Render(target)))
	}
	return strings.Join(parts, "\n")
}

func RegisterShowCmd(root *cobra.Command) {
	showCmd.Flags().BoolVar(&showJSON, "json", false, "Output as JSON")
	showCmd.Flags().BoolVar(&showRaw, "raw", false, "Output raw markdown without styling")
	showCmd.Flags().BoolVar(&showBodyOnly, "body-only", false, "Output only the body content")
	showCmd.Flags().BoolVar(&showETagOnly, "etag-only", false, "Output only the etag")
	showCmd.MarkFlagsMutuallyExclusive("json", "raw", "body-only", "etag-only")
	root.AddCommand(showCmd)
}
