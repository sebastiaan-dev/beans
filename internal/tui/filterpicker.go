package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

// listFilters holds the filters applied to the bean list.
//
// Within a category the selected values are OR'ed (e.g. status todo OR
// in-progress); categories are AND'ed together. An empty category does not
// constrain the list. The slices are treated as immutable: update them by
// replacing them, never by mutating them in place, because listFilters is
// copied by value through the Bubbletea model.
type listFilters struct {
	statuses []string
	types    []string
	tags     []string

	// hideResolved hides completed and scrapped beans, including beans that
	// inherit such a status from an ancestor.
	hideResolved bool
}

// isEmpty reports whether no filter is active.
func (f listFilters) isEmpty() bool {
	return !f.hideResolved && len(f.statuses) == 0 && len(f.types) == 0 && len(f.tags) == 0
}

// isResolvedStatus reports whether a status is terminal (completed or scrapped).
func isResolvedStatus(status string) bool {
	for _, s := range config.DefaultStatuses {
		if s.Name == status {
			return s.Archive
		}
	}
	return false
}

// matches reports whether a bean passes the filters. implicitStatus is the
// terminal status the bean inherits from an ancestor, if any.
func (f listFilters) matches(b *bean.Bean, implicitStatus string) bool {
	if f.hideResolved && (isResolvedStatus(b.Status) || implicitStatus != "") {
		return false
	}
	if len(f.statuses) > 0 && !slices.Contains(f.statuses, b.Status) {
		return false
	}
	if len(f.types) > 0 && !slices.Contains(f.types, b.Type) {
		return false
	}
	if len(f.tags) > 0 && !slices.ContainsFunc(f.tags, b.HasTag) {
		return false
	}
	return true
}

// apply returns the beans that pass the filters, preserving order.
func (f listFilters) apply(beans []*bean.Bean, implicitStatuses map[string]string) []*bean.Bean {
	if f.isEmpty() {
		return beans
	}
	result := make([]*bean.Bean, 0, len(beans))
	for _, b := range beans {
		if f.matches(b, implicitStatuses[b.ID]) {
			result = append(result, b)
		}
	}
	return result
}

// summary returns human-readable descriptions of the active filters.
func (f listFilters) summary() []string {
	var parts []string
	if f.hideResolved {
		parts = append(parts, "done hidden")
	}
	if len(f.statuses) > 0 {
		parts = append(parts, "status:"+strings.Join(f.statuses, ","))
	}
	if len(f.types) > 0 {
		parts = append(parts, "type:"+strings.Join(f.types, ","))
	}
	if len(f.tags) > 0 {
		label := "tag:"
		if len(f.tags) > 1 {
			label = "tags:"
		}
		parts = append(parts, label+strings.Join(f.tags, ","))
	}
	return parts
}

// openFilterPickerMsg requests opening the filter modal
type openFilterPickerMsg struct{}

// closeFilterPickerMsg is sent when the filter modal is cancelled
type closeFilterPickerMsg struct{}

// filtersAppliedMsg is sent when the user applies filters from the modal
type filtersAppliedMsg struct {
	filters listFilters
}

// filterSection identifies a section of the filter modal
type filterSection int

const (
	filterSectionStatus filterSection = iota
	filterSectionType
	filterSectionTags
	filterSectionCount
)

// filterOption is a selectable value in the filter modal
type filterOption struct {
	name      string
	color     string
	isArchive bool
	count     int // usage count (tags only)
}

// Filter modal sizing
const (
	filterModalMinTagRows = 3
	filterModalMaxTagRows = 8
)

// filterPickerModel is a modal for filtering the list by status, type and tags.
type filterPickerModel struct {
	statuses []filterOption
	types    []filterOption
	tags     []filterOption // all known tags, sorted by count desc then name

	selected     [filterSectionCount]map[string]bool
	hideResolved bool // preserved from the list; toggled with H in the list

	section  filterSection
	cursors  [filterSectionCount]int
	tagQuery string

	width  int
	height int
}

func newFilterPickerModel(current listFilters, tags []tagWithCount, width, height int) filterPickerModel {
	m := filterPickerModel{
		hideResolved: current.hideResolved,
		width:        width,
		height:       height,
	}

	for _, s := range config.DefaultStatuses {
		m.statuses = append(m.statuses, filterOption{name: s.Name, color: s.Color, isArchive: s.Archive})
	}
	for _, t := range config.DefaultTypes {
		m.types = append(m.types, filterOption{name: t.Name, color: t.Color})
	}

	// Keep currently selected tags selectable even if no bean uses them anymore.
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		m.tags = append(m.tags, filterOption{name: t.tag, count: t.count})
		seen[t.tag] = true
	}
	for _, t := range current.tags {
		if !seen[t] {
			m.tags = append(m.tags, filterOption{name: t})
		}
	}
	sort.SliceStable(m.tags, func(i, j int) bool {
		if m.tags[i].count != m.tags[j].count {
			return m.tags[i].count > m.tags[j].count
		}
		return m.tags[i].name < m.tags[j].name
	})

	for i, values := range [][]string{current.statuses, current.types, current.tags} {
		m.selected[i] = make(map[string]bool, len(values))
		for _, v := range values {
			m.selected[i][v] = true
		}
	}

	return m
}

func (m filterPickerModel) Init() tea.Cmd {
	return nil
}

// visibleTags returns the tags matching the type-ahead query. Prefix matches
// come before substring matches; the count-based order is kept otherwise.
func (m filterPickerModel) visibleTags() []filterOption {
	query := strings.ToLower(strings.TrimSpace(m.tagQuery))
	if query == "" {
		return m.tags
	}
	var prefix, contains []filterOption
	for _, t := range m.tags {
		name := strings.ToLower(t.name)
		switch {
		case strings.HasPrefix(name, query):
			prefix = append(prefix, t)
		case strings.Contains(name, query):
			contains = append(contains, t)
		}
	}
	return append(prefix, contains...)
}

// options returns the options shown in a section.
func (m filterPickerModel) options(s filterSection) []filterOption {
	switch s {
	case filterSectionStatus:
		return m.statuses
	case filterSectionType:
		return m.types
	default:
		return m.visibleTags()
	}
}

// filters returns the filters represented by the modal's current state.
// Values are ordered by the option order (config order for statuses/types,
// alphabetical for tags) so the result is deterministic.
func (m filterPickerModel) filters() listFilters {
	pick := func(opts []filterOption, sel map[string]bool) []string {
		var out []string
		for _, o := range opts {
			if sel[o.name] {
				out = append(out, o.name)
			}
		}
		return out
	}
	tags := pick(m.tags, m.selected[filterSectionTags])
	sort.Strings(tags)
	return listFilters{
		statuses:     pick(m.statuses, m.selected[filterSectionStatus]),
		types:        pick(m.types, m.selected[filterSectionType]),
		tags:         tags,
		hideResolved: m.hideResolved,
	}
}

func (m *filterPickerModel) moveCursor(delta int) {
	n := len(m.options(m.section))
	if n == 0 {
		m.cursors[m.section] = 0
		return
	}
	c := m.cursors[m.section] + delta
	m.cursors[m.section] = (c%n + n) % n
}

func (m *filterPickerModel) toggleCurrent() {
	opts := m.options(m.section)
	c := m.cursors[m.section]
	if c < 0 || c >= len(opts) {
		return
	}
	sel := m.selected[m.section]
	name := opts[c].name
	if sel[name] {
		delete(sel, name)
	} else {
		sel[name] = true
	}
}

func (m *filterPickerModel) clearSelections() {
	for i := range m.selected {
		m.selected[i] = make(map[string]bool)
	}
	m.tagQuery = ""
	m.cursors[filterSectionTags] = 0
}

func (m filterPickerModel) Update(msg tea.Msg) (filterPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		inTags := m.section == filterSectionTags
		// Fast typing can deliver several runes in one message. Outside the
		// tag search each rune is a shortcut, so handle them one by one.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !inTags {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				var cmd tea.Cmd
				m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "enter":
			filters := m.filters()
			return m, func() tea.Msg { return filtersAppliedMsg{filters: filters} }
		case "esc":
			return m, func() tea.Msg { return closeFilterPickerMsg{} }
		case "tab", "right":
			m.section = (m.section + 1) % filterSectionCount
			return m, nil
		case "shift+tab", "left":
			m.section = (m.section + filterSectionCount - 1) % filterSectionCount
			return m, nil
		case "up", "ctrl+p":
			m.moveCursor(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveCursor(1)
			return m, nil
		case " ":
			m.toggleCurrent()
			return m, nil
		case "ctrl+x":
			m.clearSelections()
			return m, nil
		}

		if inTags {
			switch msg.Type {
			case tea.KeyBackspace:
				if r := []rune(m.tagQuery); len(r) > 0 {
					m.tagQuery = string(r[:len(r)-1])
					m.cursors[filterSectionTags] = 0
				}
			case tea.KeyRunes:
				m.tagQuery += string(msg.Runes)
				m.cursors[filterSectionTags] = 0
			}
			return m, nil
		}

		switch msg.String() {
		case "k":
			m.moveCursor(-1)
		case "j":
			m.moveCursor(1)
		case "/":
			m.section = filterSectionTags
		}
	}

	return m, nil
}

// renderOptionRow renders a single checkbox row.
func renderOptionRow(label string, checked, isCursor bool) string {
	cursor := "  "
	if isCursor {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	}
	box := ui.Muted.Render("[ ]")
	if checked {
		box = helpKeyStyle.Render("[x]")
	}
	return cursor + box + " " + label
}

func (m filterPickerModel) sectionHeader(s filterSection, label string) string {
	if m.section == s {
		return listTitleStyle.Render(label)
	}
	return ui.Muted.Render(label)
}

func (m filterPickerModel) renderChoiceSection(s filterSection, label string, render func(filterOption) string) string {
	var b strings.Builder
	b.WriteString(m.sectionHeader(s, label))
	for i, o := range m.options(s) {
		b.WriteString("\n")
		b.WriteString(renderOptionRow(render(o), m.selected[s][o.name], m.section == s && m.cursors[s] == i))
	}
	return b.String()
}

// tagRows returns how many tag rows fit on screen.
func (m filterPickerModel) tagRows() int {
	// Rough fixed overhead: borders, title, status/type columns, headers, footer.
	return max(filterModalMinTagRows, min(filterModalMaxTagRows, m.height-24))
}

func (m filterPickerModel) renderTagSection() string {
	var b strings.Builder
	focused := m.section == filterSectionTags

	query := m.tagQuery
	if focused {
		query += "█"
	}
	b.WriteString(m.sectionHeader(filterSectionTags, "Tags") + "  " + ui.Muted.Render("search: ") + query)

	tags := m.visibleTags()
	rows := m.tagRows()
	cursor := m.cursors[filterSectionTags]
	start := 0
	if cursor >= rows {
		start = cursor - rows + 1
	}
	end := min(len(tags), start+rows)

	switch {
	case len(m.tags) == 0:
		b.WriteString("\n" + ui.Muted.Render("  no tags in use"))
	case len(tags) == 0:
		b.WriteString("\n" + ui.Muted.Render("  no matching tags"))
	}
	for i := start; i < end; i++ {
		t := tags[i]
		label := ui.RenderTag(t.name) + ui.Muted.Render(fmt.Sprintf(" (%d)", t.count))
		b.WriteString("\n" + renderOptionRow(label, m.selected[filterSectionTags][t.name], focused && i == cursor))
	}
	if hidden := len(tags) - (end - start); hidden > 0 {
		b.WriteString("\n" + ui.Muted.Render(fmt.Sprintf("  … %d more (type to narrow)", hidden)))
	}

	if selected := m.filters().tags; len(selected) > 0 {
		b.WriteString("\n" + ui.Muted.Render("selected: ") + strings.Join(selected, ", "))
	}
	return b.String()
}

func (m filterPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Never wider than the terminal (the border adds 2 columns).
	modalWidth := max(1, min(max(50, min(70, m.width*60/100)), m.width-2))

	title := listTitleStyle.Render("Filter Beans")

	statusCol := m.renderChoiceSection(filterSectionStatus, "Status", func(o filterOption) string {
		return ui.RenderStatusTextWithColor(o.name, o.color, o.isArchive)
	})
	typeCol := m.renderChoiceSection(filterSectionType, "Type", func(o filterOption) string {
		return ui.RenderTypeText(o.name, o.color)
	})
	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(22).Render(statusCol),
		typeCol,
	)

	hide := "shown"
	if m.hideResolved {
		hide = "hidden"
	}
	hideLine := ui.Muted.Render("completed/scrapped: " + hide + " (toggle with H in the list)")

	help := helpKeyStyle.Render("tab") + " " + helpStyle.Render("section") + "  " +
		helpKeyStyle.Render("↑↓") + " " + helpStyle.Render("move") + "  " +
		helpKeyStyle.Render("space") + " " + helpStyle.Render("toggle") + "\n" +
		helpKeyStyle.Render("ctrl+x") + " " + helpStyle.Render("clear") + "  " +
		helpKeyStyle.Render("enter") + " " + helpStyle.Render("apply") + "  " +
		helpKeyStyle.Render("esc") + " " + helpStyle.Render("cancel")

	content := title + "\n\n" + columns + "\n\n" + m.renderTagSection() + "\n\n" + hideLine + "\n\n" + help

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(modalWidth)

	return border.Render(content)
}

// ModalView returns the filter modal as a centered overlay on top of the background
func (m filterPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	return overlayModal(bgView, m.View(), fullWidth, fullHeight)
}
