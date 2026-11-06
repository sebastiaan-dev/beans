package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/filterquery"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	listJSON        bool
	listSearch      string
	listStatus      []string
	listNoStatus    []string
	listType        []string
	listNoType      []string
	listPriority    []string
	listNoPriority  []string
	listTag         []string
	listNoTag       []string
	listHasParent   bool
	listNoParent    bool
	listParentID    string
	listHasBlocking bool
	listNoBlocking  bool
	listIsBlocked   bool
	listReady       bool
	listQuiet       bool
	listSort        string
	listFull        bool
	listLimit       int
	listArchived    bool
	listNoArchived  bool
	listQuery       string
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all beans",
	Long: `Lists all beans in the .beans directory.

Search Syntax (--search/-S):
  The search flag supports Bleve query string syntax:

  login          Exact term match
  login~         Fuzzy match (1 edit distance, finds "loggin", "logins")
  login~2        Fuzzy match (2 edit distance)
  log*           Wildcard prefix match
  "user login"   Exact phrase match
  user AND login Both terms required
  user OR login  Either term matches
  slug:auth      Search only in slug field
  title:login    Search only in title field
  body:auth      Search only in body field

Archived Beans:
  Beans moved to .beans/archive/ (see "beans archive") are listed by default
  and marked "(archived)" in the tree output; JSON output includes an
  "archived" field. Use --no-archived to hide them or --archived to show only
  archived beans.

Filter Queries (--query/-Q):
  --query takes the structured filter syntax of the Beans UI and the TUI's
  "/" input. Terms are ANDed; comma-separated values within a term are ORed.

  status:todo,draft      Status is one of the values
  type:bug,feature       Type is one of the values
  priority:high          Priority is one of the values (empty = normal)
  priority:>=high        Priority comparison (>=, <=, >, <); higher = more urgent
  tag:ui,backend         Has at least one of the tags
  parent:<id|title>      Descendant (any depth) of the referenced bean
  milestone:<id|title>   Like parent:, but the ancestor must be a milestone
  epic:<id|title>        Like parent:, but the ancestor must be an epic
  blocked:yes|no         Blocked, directly or via an ancestor
  ready:yes|no           Ready to start (same as --ready)
  done:yes|no            Completed/scrapped, itself or via an ancestor
  archived:yes|no        Stored in .beans/archive/
  -status:completed      A leading "-" negates a term
  "some words"           Quotes group values or text containing spaces
  anything else          Text in the title, ID, type, status or tags

  The query is combined (AND) with the other filter flags.

Limiting Results (--limit):
  --limit N keeps the first N beans after all filters and sorting (--sort)
  have been applied; 0 (the default) means no limit. It applies to the beans
  matching the filters, so in the tree view unmatched ancestors may still be
  shown (dimmed) for context but do not count toward the limit.

Examples:
  beans list --sort created --limit 5     Five most recently created beans
  beans list --no-archived                Hide archived beans
  beans list --archived --json            Archived beans as JSON
  beans list -Q 'type:bug priority:>=high -status:completed'
                                          Open urgent bugs
  beans list -Q 'milestone:"Version 1.0" ready:yes'
                                          Ready work below a milestone`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateListOptions(listLimit, listArchived, listNoArchived); err != nil {
			return err
		}

		// Build GraphQL filter from CLI flags
		filter := &model.BeanFilter{
			Status:          listStatus,
			ExcludeStatus:   listNoStatus,
			Type:            listType,
			ExcludeType:     listNoType,
			Priority:        listPriority,
			ExcludePriority: listNoPriority,
			Tags:            listTag,
			ExcludeTags:     listNoTag,
		}

		// Add search filter if provided
		if listSearch != "" {
			filter.Search = &listSearch
		}

		// Add parent/blocks filters
		if listHasParent {
			filter.HasParent = &listHasParent
		}
		if listNoParent {
			filter.NoParent = &listNoParent
		}
		if listParentID != "" {
			filter.ParentID = &listParentID
		}
		if listHasBlocking {
			filter.HasBlocking = &listHasBlocking
		}
		if listNoBlocking {
			filter.NoBlocking = &listNoBlocking
		}
		// --ready and --is-blocked are mutually exclusive
		if listReady && listIsBlocked {
			return fmt.Errorf("--ready and --is-blocked are mutually exclusive")
		}

		if listIsBlocked {
			filter.IsBlocked = &listIsBlocked
		}

		// --ready: beans available to start (not blocked, excludes in-progress/completed/scrapped/draft,
		// and excludes beans with implicit terminal status from a scrapped/completed ancestor)
		if listReady {
			isBlocked := false
			excludeImplicitTerminal := true
			filter.IsBlocked = &isBlocked
			filter.ExcludeStatus = append(filter.ExcludeStatus, "in-progress", "completed", "scrapped", "draft")
			filter.ExcludeImplicitTerminal = &excludeImplicitTerminal
		}

		// Execute query via core resolver
		resolver := &beangraph.CoreResolver{Core: core}
		beans, err := resolver.Beans(context.Background(), filter)
		if err != nil {
			return fmt.Errorf("querying beans: %w", err)
		}

		// All beans: context for the query's relational terms and ancestors
		// for the tree view.
		allBeans, err := resolver.Beans(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("querying all beans: %w", err)
		}

		// Apply the structured filter query
		beans, err = applyListQuery(beans, allBeans, listQuery)
		if err != nil {
			return err
		}

		// Filter by archive location (not part of the GraphQL filter)
		beans = filterArchived(beans, archiveFilterMode(listArchived, listNoArchived))

		// Sort beans, then apply limit to the sorted, filtered result
		sortBeans(beans, listSort, cfg)
		beans = limitBeans(beans, listLimit)

		// JSON output (flat list)
		if listJSON {
			if !listFull {
				for _, b := range beans {
					b.Body = ""
				}
			}
			return output.SuccessMultiple(beans)
		}

		// Quiet mode: just IDs (flat)
		if listQuiet {
			for _, b := range beans {
				fmt.Println(b.ID)
			}
			return nil
		}

		// Default: tree view, with ancestors from all beans for context
		// Pre-compute implicit statuses for all beans
		implicitStatuses := make(map[string]string, len(allBeans))
		for _, b := range allBeans {
			if status, _ := core.ImplicitStatus(b.ID); status != "" {
				implicitStatuses[b.ID] = status
			}
		}

		// Create sort function for tree building
		sortFn := func(b []*bean.Bean) {
			sortBeans(b, listSort, cfg)
		}

		// Build tree
		tree := ui.BuildTree(beans, allBeans, sortFn, implicitStatuses)

		if len(tree) == 0 {
			fmt.Println(ui.Muted.Render("No beans found. Create one with: beans new <title>"))
			return nil
		}

		// Calculate max ID width from all beans in tree
		maxIDWidth := 2
		for _, b := range allBeans {
			if len(b.ID) > maxIDWidth {
				maxIDWidth = len(b.ID)
			}
		}
		maxIDWidth += 2

		// Check if any beans have tags
		hasTags := false
		for _, b := range beans {
			if len(b.Tags) > 0 {
				hasTags = true
				break
			}
		}

		// Detect terminal width (default to 80 if not a terminal)
		termWidth := 80
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termWidth = w
		}

		fmt.Print(ui.RenderTree(tree, cfg, maxIDWidth, hasTags, termWidth))
		return nil
	},
}

func sortBeans(beans []*bean.Bean, sortBy string, cfg *config.Config) {
	statusNames := cfg.StatusNames()
	priorityNames := cfg.PriorityNames()
	typeNames := cfg.TypeNames()

	switch sortBy {
	case "created":
		sort.Slice(beans, func(i, j int) bool {
			if beans[i].CreatedAt == nil && beans[j].CreatedAt == nil {
				return beans[i].ID < beans[j].ID
			}
			if beans[i].CreatedAt == nil {
				return false
			}
			if beans[j].CreatedAt == nil {
				return true
			}
			return beans[i].CreatedAt.After(*beans[j].CreatedAt)
		})
	case "updated":
		sort.Slice(beans, func(i, j int) bool {
			if beans[i].UpdatedAt == nil && beans[j].UpdatedAt == nil {
				return beans[i].ID < beans[j].ID
			}
			if beans[i].UpdatedAt == nil {
				return false
			}
			if beans[j].UpdatedAt == nil {
				return true
			}
			return beans[i].UpdatedAt.After(*beans[j].UpdatedAt)
		})
	case "status":
		// Build status order from configured statuses
		statusOrder := make(map[string]int)
		for i, s := range statusNames {
			statusOrder[s] = i
		}
		sort.Slice(beans, func(i, j int) bool {
			oi, oj := statusOrder[beans[i].Status], statusOrder[beans[j].Status]
			if oi != oj {
				return oi < oj
			}
			return beans[i].ID < beans[j].ID
		})
	case "priority":
		// Build priority order from configured priorities
		priorityOrder := make(map[string]int)
		for i, p := range priorityNames {
			priorityOrder[p] = i
		}
		// Find normal priority index for beans without priority
		normalIdx := len(priorityNames)
		for i, p := range priorityNames {
			if p == "normal" {
				normalIdx = i
				break
			}
		}
		sort.Slice(beans, func(i, j int) bool {
			pi := normalIdx
			if beans[i].Priority != "" {
				if order, ok := priorityOrder[beans[i].Priority]; ok {
					pi = order
				}
			}
			pj := normalIdx
			if beans[j].Priority != "" {
				if order, ok := priorityOrder[beans[j].Priority]; ok {
					pj = order
				}
			}
			if pi != pj {
				return pi < pj
			}
			return beans[i].ID < beans[j].ID
		})
	case "id":
		sort.Slice(beans, func(i, j int) bool {
			return beans[i].ID < beans[j].ID
		})
	default:
		// Default: sort by status order, then priority, then type order, then title (same as TUI)
		bean.SortByStatusPriorityAndType(beans, statusNames, priorityNames, typeNames)
	}
}

// archiveFilter selects beans by whether they live in the archive directory.
type archiveFilter int

const (
	archiveInclude archiveFilter = iota // include all beans (default)
	archiveExclude                      // exclude archived beans
	archiveOnly                         // only archived beans
)

// archiveFilterMode maps the --archived/--no-archived flags to a filter mode.
func archiveFilterMode(onlyArchived, noArchived bool) archiveFilter {
	switch {
	case onlyArchived:
		return archiveOnly
	case noArchived:
		return archiveExclude
	default:
		return archiveInclude
	}
}

// validateListOptions validates list flags that cannot be expressed as cobra
// constraints alone.
func validateListOptions(limit int, onlyArchived, noArchived bool) error {
	if limit < 0 {
		return fmt.Errorf("--limit must be a non-negative integer, got %d", limit)
	}
	if onlyArchived && noArchived {
		return fmt.Errorf("--archived and --no-archived are mutually exclusive")
	}
	return nil
}

// filterArchived returns the beans matching the archive filter mode.
func filterArchived(beans []*bean.Bean, mode archiveFilter) []*bean.Bean {
	if mode == archiveInclude {
		return beans
	}
	result := make([]*bean.Bean, 0, len(beans))
	for _, b := range beans {
		if b.IsArchived() == (mode == archiveOnly) {
			result = append(result, b)
		}
	}
	return result
}

// applyListQuery returns the beans matching the --query filter query. all
// holds every bean, for the query's relational terms (parent:, blocked:,
// ready:, …). Values that can never match (e.g. "status:todoo") are reported
// as an error rather than silently matching nothing.
func applyListQuery(beans, all []*bean.Bean, query string) ([]*bean.Bean, error) {
	if strings.TrimSpace(query) == "" {
		return beans, nil
	}
	q := filterquery.Parse(query)
	for _, t := range q.ActiveClauses() {
		if bad := filterquery.InvalidValues(t); len(bad) > 0 {
			msg := fmt.Sprintf("invalid --query term %q: unknown value(s) %s", t.String(), strings.Join(bad, ", "))
			if known := filterquery.KnownValues(t.Field); known != nil {
				msg += fmt.Sprintf(" (known: %s)", strings.Join(known, ", "))
			}
			return nil, fmt.Errorf("%s", msg)
		}
	}
	return filterquery.Filter(beans, q, filterquery.NewContext(all)), nil
}

// limitBeans returns at most limit beans from the front of the slice.
// A limit of 0 means no limit.
func limitBeans(beans []*bean.Bean, limit int) []*bean.Bean {
	if limit > 0 && len(beans) > limit {
		return beans[:limit]
	}
	return beans
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func RegisterListCmd(root *cobra.Command) {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
	listCmd.Flags().StringVarP(&listSearch, "search", "S", "", "Full-text search in title and body")
	listCmd.Flags().StringArrayVarP(&listStatus, "status", "s", nil, "Filter by status (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoStatus, "no-status", nil, "Exclude by status (can be repeated)")
	listCmd.Flags().StringArrayVarP(&listType, "type", "t", nil, "Filter by type (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoType, "no-type", nil, "Exclude by type (can be repeated)")
	listCmd.Flags().StringArrayVarP(&listPriority, "priority", "p", nil, "Filter by priority (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoPriority, "no-priority", nil, "Exclude by priority (can be repeated)")
	listCmd.Flags().StringArrayVar(&listTag, "tag", nil, "Filter by tag (can be repeated, OR logic)")
	listCmd.Flags().StringArrayVar(&listNoTag, "no-tag", nil, "Exclude beans with tag (can be repeated)")
	listCmd.Flags().BoolVar(&listHasParent, "has-parent", false, "Filter beans with a parent")
	listCmd.Flags().BoolVar(&listNoParent, "no-parent", false, "Filter beans without a parent")
	listCmd.Flags().StringVar(&listParentID, "parent", "", "Filter by parent ID")
	listCmd.Flags().BoolVar(&listHasBlocking, "has-blocking", false, "Filter beans that are blocking others")
	listCmd.Flags().BoolVar(&listNoBlocking, "no-blocking", false, "Filter beans that aren't blocking others")
	listCmd.Flags().BoolVar(&listIsBlocked, "is-blocked", false, "Filter beans that are blocked by others")
	listCmd.Flags().BoolVar(&listReady, "ready", false, "Filter beans available to start (not blocked, excludes in-progress/completed/scrapped/draft)")
	listCmd.Flags().BoolVarP(&listQuiet, "quiet", "q", false, "Only output IDs (one per line)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort by: created, updated, status, priority, id (default: status, priority, type, title)")
	listCmd.Flags().BoolVar(&listFull, "full", false, "Include bean body in JSON output")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Maximum number of beans to show after filtering and sorting (0 = no limit)")
	listCmd.Flags().BoolVar(&listArchived, "archived", false, "Only show archived beans (in .beans/archive/)")
	listCmd.Flags().BoolVar(&listNoArchived, "no-archived", false, "Exclude archived beans (in .beans/archive/)")
	listCmd.Flags().StringVarP(&listQuery, "query", "Q", "", `Structured filter query, e.g. 'type:bug priority:>=high -status:completed' (see "Filter Queries")`)
	root.AddCommand(listCmd)
}
