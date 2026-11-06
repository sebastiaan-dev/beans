package commands

// Shared helpers for the workflow commands (start, complete, scrap, next,
// progress, milestones). These commands are thin, opinionated shortcuts on
// top of the GraphQL core resolver; all mutations go through
// beangraph.CoreResolver.UpdateBean.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
)

// Section headings appended to a bean's body by the workflow commands.
// These match the conventions documented in prompt.tmpl.
const (
	summarySectionHeading = "## Summary of Changes"
	scrapSectionHeading   = "## Reasons for Scrapping"
)

// transitionResult describes the outcome of a workflow status transition.
type transitionResult struct {
	Bean *bean.Bean
	// Unchanged is true when the bean already had the target status and
	// nothing was modified.
	Unchanged bool
	// Unarchived is true when the bean was moved out of the archive.
	Unarchived bool
}

// transitionBean sets a bean's status via the UpdateBean mutation and
// optionally appends a markdown section to its body.
//
// If the bean already has the target status, nothing is changed (no section
// is appended either, to avoid duplicate sections) and Unchanged is set.
// If the bean is archived and the target status is not an archive status,
// the bean is unarchived first so active work never lives in the archive.
func transitionBean(ctx context.Context, resolver *beangraph.CoreResolver, id, status, section string, jsonMode bool) (*transitionResult, error) {
	b, err := resolver.Bean(ctx, id)
	if err != nil {
		return nil, cmdError(jsonMode, output.ErrNotFound, "failed to find bean: %v", err)
	}
	if b == nil {
		return nil, cmdError(jsonMode, output.ErrNotFound, "bean not found: %s", id)
	}

	if b.Status == status {
		return &transitionResult{Bean: b, Unchanged: true}, nil
	}

	res := &transitionResult{}
	if !resolver.Core.Config().IsArchiveStatus(status) && resolver.Core.IsArchived(b.ID) {
		if _, err := resolver.Core.LoadAndUnarchive(b.ID); err != nil {
			return nil, cmdError(jsonMode, output.ErrFileError, "failed to unarchive bean: %v", err)
		}
		res.Unarchived = true
	}

	input := model.UpdateBeanInput{Status: &status}
	if section != "" {
		input.BodyMod = &model.BodyModification{Append: &section}
	}

	updated, err := resolver.UpdateBean(ctx, b.ID, input)
	if err != nil {
		return nil, mutationError(jsonMode, err)
	}
	res.Bean = updated
	return res, nil
}

// buildSection returns a markdown section with the given heading and text.
// Returns "" for blank text. If the text already starts with the heading,
// it is returned as-is to avoid a duplicated heading.
func buildSection(heading, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, heading+"\n") || text == heading {
		return text
	}
	return heading + "\n\n" + text
}

// isContainerType reports whether a bean type is a grouping container
// (milestone or epic) rather than a directly workable item.
func isContainerType(t string) bool {
	return t == "milestone" || t == "epic"
}

// isTerminalStatus reports whether a status means the bean is done.
func isTerminalStatus(status string) bool {
	return status == "completed" || status == "scrapped"
}

// progressStats summarizes the status distribution of a set of beans.
type progressStats struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
	Completed int            `json:"completed"`
	Scrapped  int            `json:"scrapped"`
	// Percent is completed / (total - scrapped) * 100, rounded down to an
	// integer. Scrapped beans are excluded because they will never be done.
	// It is 0 when there is nothing left to count.
	Percent int `json:"percent_complete"`
}

// computeProgress counts beans by (explicit) status. Every configured
// status is present in ByStatus, even with a zero count.
func computeProgress(beans []*bean.Bean, statusNames []string) progressStats {
	stats := progressStats{ByStatus: make(map[string]int, len(statusNames))}
	for _, s := range statusNames {
		stats.ByStatus[s] = 0
	}
	for _, b := range beans {
		stats.Total++
		stats.ByStatus[b.Status]++
	}
	stats.Completed = stats.ByStatus["completed"]
	stats.Scrapped = stats.ByStatus["scrapped"]
	if denom := stats.Total - stats.Scrapped; denom > 0 {
		stats.Percent = int(math.Floor(float64(stats.Completed) * 100 / float64(denom)))
	}
	return stats
}

// descendantsOf returns all beans below rootID in the parent hierarchy
// (excluding the root itself). Cycles are tolerated.
func descendantsOf(all []*bean.Bean, rootID string) []*bean.Bean {
	children := make(map[string][]*bean.Bean)
	for _, b := range all {
		if b.Parent != "" {
			children[b.Parent] = append(children[b.Parent], b)
		}
	}

	var result []*bean.Bean
	seen := map[string]bool{rootID: true}
	queue := []string{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, c := range children[id] {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			result = append(result, c)
			queue = append(queue, c.ID)
		}
	}
	return result
}

// renderProgressBar renders a fixed-width textual progress bar.
func renderProgressBar(percent, width int) string {
	if width <= 0 {
		return ""
	}
	percent = max(0, min(100, percent))
	filled := percent * width / 100
	return strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
}

// statusNamesOrdered returns configured status names followed by any
// unknown statuses present in stats (sorted), so nothing is silently hidden.
func statusNamesOrdered(stats progressStats, c *config.Config) []string {
	names := c.StatusNames()
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	var extra []string
	for s := range stats.ByStatus {
		if !known[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	return append(names, extra...)
}

// statusLabel returns a human-friendly label for a status, e.g. "In Progress".
func statusLabel(status string) string {
	words := strings.Split(status, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// formatCountLine renders a "N completed of M (+S scrapped)" style summary.
func formatCountLine(stats progressStats) string {
	line := fmt.Sprintf("%d/%d completed", stats.Completed, stats.Total-stats.Scrapped)
	if stats.Scrapped > 0 {
		line += fmt.Sprintf(" (+%d scrapped)", stats.Scrapped)
	}
	return line
}

// notFoundError reports a missing bean.
type notFoundError struct{ id string }

func (e *notFoundError) Error() string { return "bean not found: " + e.id }

// errorCode maps an error to the JSON error code used in --json mode.
func errorCode(err error) string {
	var nf *notFoundError
	if errors.As(err, &nf) {
		return output.ErrNotFound
	}
	return output.ErrValidation
}

// writeJSON writes v as indented JSON to stdout, like the other --json outputs.
func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
