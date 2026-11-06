package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// Markdown rendering -----------------------------------------------------------

// markdownRenderers caches glamour renderers by wrap width and color profile;
// creating one is comparatively expensive.
var (
	markdownRenderersMu sync.Mutex
	markdownRenderers   = map[markdownRendererKey]*glamour.TermRenderer{}
)

type markdownRendererKey struct {
	width   int
	profile termenv.Profile
}

// resetMarkdownRenderers drops the cached renderers, e.g. after the theme
// (and with it ui.MarkdownStyleConfig) changed.
func resetMarkdownRenderers() {
	markdownRenderersMu.Lock()
	defer markdownRenderersMu.Unlock()
	clear(markdownRenderers)
}

// markdownRenderer returns a renderer that wraps at width.
func markdownRenderer(width int) *glamour.TermRenderer {
	key := markdownRendererKey{width: width, profile: lipgloss.ColorProfile()}
	markdownRenderersMu.Lock()
	defer markdownRenderersMu.Unlock()
	if r, ok := markdownRenderers[key]; ok {
		return r
	}
	style := ui.MarkdownStyleConfig()
	// The box around the body provides the margin. glamour's default document
	// margin indents every line, and used to show as a 2-space indent on all
	// but the first line once the output was trimmed.
	zero := uint(0)
	style.Document.Margin = &zero
	// Use a fixed ANSI-palette style instead of WithAutoStyle() to avoid slow
	// terminal background detection; the palette follows the terminal theme
	// anyway. Use lipgloss's detected color profile so NO_COLOR and non-TTY
	// output are respected like in the rest of the UI.
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithChromaFormatter(ui.MarkdownChromaFormatter),
		glamour.WithColorProfile(key.profile),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		r = nil
	}
	markdownRenderers[key] = r
	return r
}

// renderMarkdown renders a bean body as lines wrapped to width.
func renderMarkdown(body string, width int) []string {
	width = max(width, 10)
	out := body
	if r := markdownRenderer(width); r != nil {
		if rendered, err := r.Render(body); err == nil {
			out = rendered
		}
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	blank := func(s string) bool { return strings.TrimSpace(stripAnsi(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = clampWidth(l, width)
	}
	return lines
}

// Links ------------------------------------------------------------------------

// resolvedLink represents a link with the target bean resolved
type resolvedLink struct {
	linkType string
	bean     *bean.Bean
	incoming bool // true if another bean links TO this one
}

// formatLinkLabel returns a human-readable label for the link type
func formatLinkLabel(linkType string, incoming bool) string {
	switch {
	case linkType == "implicit":
		return "status from"
	case linkType == "blocking" && incoming:
		return "blocked by"
	case linkType == "blocking":
		return "blocking"
	case linkType == "parent" && incoming:
		return "child"
	case linkType == "parent":
		return "parent"
	case incoming:
		return linkType + " (in)"
	default:
		return linkType
	}
}

// linkGroupOrder orders the relation groups in the detail pane.
func linkGroupOrder(l resolvedLink) int {
	switch formatLinkLabel(l.linkType, l.incoming) {
	case "status from":
		return 0
	case "parent":
		return 1
	case "child":
		return 2
	case "blocking":
		return 3
	case "blocked by":
		return 4
	default:
		return 5
	}
}

func resolveAllLinks(resolver *beangraph.CoreResolver, cfg *config.Config, b *bean.Bean) []resolvedLink {
	var links []resolvedLink
	ctx := context.Background()
	if _, fromID := resolver.Core.ImplicitStatus(b.ID); fromID != "" {
		if from, _ := resolver.Bean(ctx, fromID); from != nil {
			links = append(links, resolvedLink{linkType: "implicit", bean: from})
		}
	}
	if parent, _ := resolver.BeanParent(ctx, b); parent != nil {
		links = append(links, resolvedLink{linkType: "parent", bean: parent})
	}
	if children, _ := resolver.BeanChildren(ctx, b, nil); children != nil {
		for _, c := range children {
			links = append(links, resolvedLink{linkType: "parent", bean: c, incoming: true})
		}
	}
	// Beans this one blocks: its own blocking list plus beans listing it in
	// their blocked_by (BeanBlocking only resolves the former).
	blocks := map[string]bool{}
	if blocking, _ := resolver.BeanBlocking(ctx, b, nil); blocking != nil {
		for _, c := range blocking {
			blocks[c.ID] = true
			links = append(links, resolvedLink{linkType: "blocking", bean: c})
		}
	}
	for _, in := range resolver.Core.FindIncomingLinks(b.ID) {
		if in.LinkType == "blocked_by" && !blocks[in.FromBean.ID] {
			blocks[in.FromBean.ID] = true
			links = append(links, resolvedLink{linkType: "blocking", bean: in.FromBean})
		}
	}
	if blockedBy, _ := resolver.BeanBlockedBy(ctx, b, nil); blockedBy != nil {
		for _, c := range blockedBy {
			links = append(links, resolvedLink{linkType: "blocking", bean: c, incoming: true})
		}
	}

	// Keep link groups together; order beans within a group like the tree.
	statusNames, priorityNames, typeNames := cfg.StatusNames(), cfg.PriorityNames(), cfg.TypeNames()
	sort.SliceStable(links, func(i, j int) bool {
		gi, gj := linkGroupOrder(links[i]), linkGroupOrder(links[j])
		if gi != gj {
			return gi < gj
		}
		return compareBeansByStatusPriorityAndType(links[i].bean, links[j].bean, statusNames, priorityNames, typeNames)
	})
	return links
}

// compareBeansByStatusPriorityAndType compares two beans using the same ordering as bean.SortByStatusPriorityAndType.
func compareBeansByStatusPriorityAndType(a, b *bean.Bean, statusNames, priorityNames, typeNames []string) bool {
	index := func(names []string, v string, missing int) int {
		for i, n := range names {
			if n == v {
				return i
			}
		}
		return missing
	}
	normal := index(priorityNames, "normal", len(priorityNames))
	prio := func(p string) int {
		if p == "" {
			return normal
		}
		return index(priorityNames, p, len(priorityNames))
	}

	if oi, oj := index(statusNames, a.Status, len(statusNames)), index(statusNames, b.Status, len(statusNames)); oi != oj {
		return oi < oj
	}
	if pi, pj := prio(a.Priority), prio(b.Priority); pi != pj {
		return pi < pj
	}
	if ti, tj := index(typeNames, a.Type, len(typeNames)), index(typeNames, b.Type, len(typeNames)); ti != tj {
		return ti < tj
	}
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

// Detail pane ------------------------------------------------------------------

// detailSection is the focusable part of the detail pane.
type detailSection int

const (
	sectionRelations detailSection = iota
	sectionBody
)

// detailModel shows the selected bean's fields as labelled boxes, beside the
// tree (wide terminals) or instead of it (narrow terminals).
type detailModel struct {
	resolver *beangraph.CoreResolver
	config   *config.Config

	bean  *bean.Bean
	links []resolvedLink

	width  int
	height int

	focused    bool
	section    detailSection
	relCursor  int
	bodyOffset int

	// Rendered body, cached for bodyKey (width and body text).
	body    []string
	bodyKey string

	// IDs of previously shown beans, for backspace.
	history []string
}

func newDetailModel(resolver *beangraph.CoreResolver, cfg *config.Config) detailModel {
	return detailModel{resolver: resolver, config: cfg}
}

// setBean shows b (which may be nil). Scroll and cursor positions are kept
// while the same bean is refreshed. Unless force is set, showing the bean
// that is already shown does nothing; force refreshes its relations after
// beans were reloaded.
func (m *detailModel) setBean(b *bean.Bean, force bool) {
	if b == m.bean && !force {
		return
	}
	if b == nil || m.bean == nil || b.ID != m.bean.ID {
		m.relCursor, m.bodyOffset = 0, 0
	}
	m.bean = b
	m.links = nil
	if b != nil {
		m.links = resolveAllLinks(m.resolver, m.config, b)
	}
	if m.section == sectionRelations && len(m.links) == 0 {
		m.section = sectionBody
	}
	m.prepare()
}

// setSize sets the pane's size.
func (m *detailModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.prepare()
}

// focus gives the pane the focus; the relations are focused first if there
// are any, like the linked beans list used to be.
func (m *detailModel) focus() {
	m.focused = true
	m.section = sectionBody
	if len(m.links) > 0 {
		m.section = sectionRelations
	}
}

func (m *detailModel) blur() {
	m.focused = false
	m.history = nil
}

// bodyWidth is the width the body is rendered at.
func (m detailModel) bodyWidth() int {
	return max(m.width-4, 1)
}

// prepare renders the body for the current bean and width if needed and
// clamps the positions.
func (m *detailModel) prepare() {
	if m.bean == nil {
		m.body, m.bodyKey = nil, ""
		return
	}
	key := fmt.Sprintf("%d\x00%d\x00%s", m.bodyWidth(), lipgloss.ColorProfile(), m.bean.Body)
	if key != m.bodyKey {
		m.bodyKey = key
		if strings.TrimSpace(m.bean.Body) == "" {
			m.body = []string{mutedStyle().Render("(no body)")}
		} else {
			m.body = renderMarkdown(m.bean.Body, m.bodyWidth())
		}
	}
	m.relCursor = max(0, min(m.relCursor, len(m.links)-1))
	l := m.layout()
	m.bodyOffset = max(0, min(m.bodyOffset, len(m.body)-l.bodyRows))
}

// detailLayout is the vertical arrangement of the detail boxes.
type detailLayout struct {
	titleLines []string
	metaRows   int // rows of status/type/priority boxes
	extraRows  int // rows of parent/tags boxes (0 = hidden)
	relRows    int // visible relation lines
	bodyRows   int // visible body lines
	history    bool
	// compactMeta shows status, type and priority in one box (very short
	// terminals).
	compactMeta bool
}

// boxesPerRow returns how many small field boxes fit side by side.
func boxesPerRow(width, n int) int {
	return max(1, min(n, width/16))
}

func rowsFor(width, n int) int {
	per := boxesPerRow(width, n)
	return (n + per - 1) / per
}

// layout distributes the height between the boxes. When space runs out,
// the history goes first (also when the body would not fit otherwise), then
// the relations shrink (they scroll), then the parent/tags boxes go (the
// parent is listed in the relations too). On very short terminals the title
// is cut to one line and status, type and priority share one box.
func (m detailModel) layout() detailLayout {
	var l detailLayout
	if m.bean == nil {
		return l
	}
	l.titleLines = wrapTokens(wordTokens(m.bean.Title, lipgloss.NewStyle().Bold(true)), max(m.width-4, 1), 2, lipgloss.NewStyle())
	if len(l.titleLines) == 0 {
		l.titleLines = []string{mutedStyle().Render("(untitled)")}
	}
	l.metaRows = rowsFor(m.width, 3)
	l.extraRows = rowsFor(m.width, 2)
	l.relRows = max(len(m.links), 1)
	l.history = true

	need := func() int {
		n := 2 + len(l.titleLines) + 3*l.metaRows + 3*l.extraRows + 2 + l.relRows + 3 // body: 1 line
		if l.history {
			n += 5
		}
		return n
	}
	if need() > m.height {
		l.history = false
	}
	for need() > m.height && l.relRows > 3 {
		l.relRows--
	}
	if need() > m.height {
		l.extraRows = 0
	}
	for need() > m.height && l.relRows > 1 {
		l.relRows--
	}
	if need() > m.height && len(l.titleLines) > 1 {
		l.titleLines = wrapTokens(wordTokens(m.bean.Title, lipgloss.NewStyle().Bold(true)), max(m.width-4, 1), 1, lipgloss.NewStyle())
	}
	if need() > m.height && l.metaRows > 1 {
		l.metaRows, l.compactMeta = 1, true
	}
	room := m.height - need() + 1
	if l.history && len(m.body) > room {
		// The body matters more than the history.
		l.history = false
		room += 5
	}
	l.bodyRows = max(1, min(len(m.body), room))
	return l
}

// selectedRelation returns the ID of the bean under the relations cursor if
// the relations are focused.
func (m detailModel) selectedRelation() string {
	if m.section == sectionRelations && m.relCursor < len(m.links) {
		return m.links[m.relCursor].bean.ID
	}
	return ""
}

// popHistory removes and returns the previously shown bean, if any.
func (m *detailModel) popHistory() (string, bool) {
	n := len(m.history)
	if n == 0 {
		return "", false
	}
	id := m.history[n-1]
	m.history = m.history[:n-1]
	return id, true
}

// Update handles keys while the pane has the focus. Leaving the pane,
// moving between its sections (tab, shift+tab) and opening relations (esc,
// enter, backspace) are handled by the App, because they change what the
// tree shows or where the focus goes.
func (m detailModel) Update(msg tea.Msg) (detailModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || m.bean == nil {
		return m, nil
	}
	b := m.bean
	l := m.layout()
	scroll := func(delta int) {
		m.bodyOffset = max(0, min(m.bodyOffset+delta, len(m.body)-l.bodyRows))
	}
	switch key.String() {
	case "up", "k":
		if m.section == sectionRelations {
			m.relCursor = max(0, m.relCursor-1)
		} else {
			scroll(-1)
		}
	case "down", "j":
		if m.section == sectionRelations {
			m.relCursor = min(len(m.links)-1, m.relCursor+1)
		} else {
			scroll(1)
		}
	case "pgup", "ctrl+u", "ctrl+b":
		scroll(-max(l.bodyRows-1, 1))
	case "pgdown", "ctrl+d", "ctrl+f", " ":
		scroll(max(l.bodyRows-1, 1))
	case "home":
		scroll(-len(m.body))
	case "end", "G":
		scroll(len(m.body))
	case "p":
		return m, func() tea.Msg {
			return openParentPickerMsg{beanIDs: []string{b.ID}, beanTitle: b.Title, beanTypes: []string{b.Type}, currentParent: b.Parent}
		}
	case "s":
		return m, func() tea.Msg {
			return openStatusPickerMsg{beanIDs: []string{b.ID}, beanTitle: b.Title, currentStatus: b.Status}
		}
	case "t":
		return m, func() tea.Msg {
			return openTypePickerMsg{beanIDs: []string{b.ID}, beanTitle: b.Title, currentType: b.Type}
		}
	case "P":
		return m, func() tea.Msg {
			return openPriorityPickerMsg{beanIDs: []string{b.ID}, beanTitle: b.Title, currentPriority: b.Priority}
		}
	case "b":
		return m, func() tea.Msg {
			return openBlockingPickerMsg{beanID: b.ID, beanTitle: b.Title, currentBlocking: b.Blocking}
		}
	case "e":
		return m, func() tea.Msg { return openEditorMsg{beanID: b.ID, beanPath: b.Path} }
	case "y":
		return m, func() tea.Msg { return copyBeanIDMsg{ids: []string{b.ID}} }
	}
	return m, nil
}

// hints returns the footer hints for the focused pane in order of importance.
func (m detailModel) hints() []keyHint {
	var hints []keyHint
	hints = append(hints, keyHint{"esc", "back"})
	if m.section == sectionRelations {
		hints = append(hints, keyHint{"enter", "open"}, keyHint{"↑↓", "move"})
	} else {
		hints = append(hints, keyHint{"↑↓", "scroll"})
	}
	if m.section == sectionRelations {
		hints = append(hints, keyHint{"tab", "body"})
	} else {
		hints = append(hints, keyHint{"tab", "filters"})
	}
	if len(m.history) > 0 {
		hints = append(hints, keyHint{"bksp", "previous"})
	}
	return append(hints,
		keyHint{"e", "edit"},
		keyHint{"s", "status"},
		keyHint{"t", "type"},
		keyHint{"P", "priority"},
		keyHint{"p", "parent"},
		keyHint{"b", "blocking"},
		keyHint{"y", "copy id"},
		keyHint{"pgup/pgdn", "scroll body"},
	)
}

// Rendering --------------------------------------------------------------------

// beanLine renders a compact one-line reference to a bean: status and type
// glyphs, short ID and title.
func (m detailModel) beanLine(b *bean.Bean, titleStyle lipgloss.Style) string {
	colors := m.config.GetBeanColors(b.Status, b.Type, b.Priority)
	return fg(colNamed(colors.StatusColor)).Render(glyph(b.Status, ui.ShortStatus)) + " " +
		fg(colNamed(colors.TypeColor)).Render(glyph(b.Type, ui.ShortType)) + " " +
		mutedStyle().Render(shortID(b.ID, m.config.Beans.Prefix)) + " " +
		titleStyle.Render(b.Title)
}

// relationLines renders all relation entries.
func (m detailModel) relationLines(width int) []string {
	if len(m.links) == 0 {
		return []string{mutedStyle().Render("(none)")}
	}
	labelW := 0
	for _, l := range m.links {
		labelW = max(labelW, lipgloss.Width(formatLinkLabel(l.linkType, l.incoming)))
	}
	lines := make([]string, len(m.links))
	prev := ""
	for i, l := range m.links {
		label := formatLinkLabel(l.linkType, l.incoming)
		shown := label
		if label == prev {
			shown = ""
		}
		prev = label
		selected := m.focused && m.section == sectionRelations && i == m.relCursor
		cursor := "  "
		titleStyle := lipgloss.NewStyle()
		if selected {
			cursor = accentStyle().Render("▌ ")
			titleStyle = accentStyle().Bold(true)
		}
		ref := m.beanLine(l.bean, titleStyle)
		if l.linkType == "implicit" {
			ref = mutedStyle().Render(l.bean.Status+" ·") + " " + ref
		}
		line := cursor + mutedStyle().Render(fmt.Sprintf("%-*s", labelW, shown)) + "  " + ref
		lines[i] = truncateText(line, width)
	}
	return lines
}

// fieldBox renders a small single-value box.
func fieldBox(label, hotkey, value string, width int) []string {
	return box{label: label, hotkey: hotkey, lines: []string{value}, width: width, padding: 1}.render()
}

// boxGrid lays out field boxes in rows that fit width.
func boxGrid(width int, boxes []func(w int) []string) []string {
	per := boxesPerRow(width, len(boxes))
	var out []string
	for start := 0; start < len(boxes); start += per {
		row := boxes[start:min(start+per, len(boxes))]
		ws := splitWidths(width, len(row), 1)
		blocks := make([][]string, len(row))
		for i, f := range row {
			blocks[i] = f(ws[i])
		}
		out = append(out, joinColumns(1, blocks...)...)
	}
	return out
}

// View renders the pane at its size.
func (m detailModel) View() string {
	return strings.Join(m.viewLines(), "\n")
}

// viewLines renders exactly m.height lines of m.width cells.
func (m detailModel) viewLines() []string {
	w, h := max(m.width, 8), max(m.height, 0)
	var lines []string
	if m.bean == nil {
		lines = append(lines, mutedStyle().Render(" No bean selected"))
	} else {
		lines = m.renderBoxes(w)
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = fitWidth(l, w, lipgloss.NewStyle())
	}
	return lines
}

func (m detailModel) renderBoxes(w int) []string {
	b := m.bean
	l := m.layout()
	colors := m.config.GetBeanColors(b.Status, b.Type, b.Priority)
	none := mutedStyle().Render("(none)")

	var out []string
	out = append(out, box{label: "Title · " + b.ID, hotkey: "e", lines: l.titleLines, width: w, padding: 1}.render()...)

	status := fg(colNamed(colors.StatusColor)).Bold(!colors.IsArchive).Render(b.Status)
	if implicit, _ := m.resolver.Core.ImplicitStatus(b.ID); implicit != "" {
		status += mutedStyle().Render(" ↑" + implicit)
	}
	typ := fg(colNamed(colors.TypeColor)).Render(b.Type)
	if b.Type == "" {
		typ = none
	}
	prio := mutedStyle().Render("normal")
	if b.Priority != "" && b.Priority != "normal" {
		prio = fg(colNamed(colors.PriorityColor)).Bold(b.Priority == "critical" || b.Priority == "high").Render(b.Priority)
	}
	if l.compactMeta {
		sep := mutedStyle().Render(" · ")
		out = append(out, fieldBox("Status · Type · Priority", "s t P", status+sep+typ+sep+prio, w)...)
	} else {
		out = append(out, boxGrid(w, []func(int) []string{
			func(bw int) []string { return fieldBox("Status", "s", status, bw) },
			func(bw int) []string { return fieldBox("Type", "t", typ, bw) },
			func(bw int) []string { return fieldBox("Priority", "P", prio, bw) },
		})...)
	}

	if l.extraRows > 0 {
		parent := none
		if b.Parent != "" {
			if p, _ := m.resolver.Bean(context.Background(), b.Parent); p != nil {
				parent = mutedStyle().Render(shortID(p.ID, m.config.Beans.Prefix)) + " " + p.Title
			} else {
				parent = b.Parent + mutedStyle().Render(" (missing)")
			}
		}
		tags := none
		if len(b.Tags) > 0 {
			parts := make([]string, len(b.Tags))
			for i, t := range b.Tags {
				parts[i] = mutedStyle().Render("#") + t
			}
			tags = strings.Join(parts, " ")
		}
		out = append(out, boxGrid(w, []func(int) []string{
			func(bw int) []string { return fieldBox("Parent", "p", parent, bw) },
			func(bw int) []string { return fieldBox("Tags", "", tags, bw) },
		})...)
	}

	// Body
	bodyLabel := "Body"
	if len(m.body) > l.bodyRows {
		end := min(m.bodyOffset+l.bodyRows, len(m.body))
		bodyLabel = fmt.Sprintf("Body · %d–%d/%d", m.bodyOffset+1, end, len(m.body))
	}
	bodyLines := m.body[min(m.bodyOffset, len(m.body)):]
	out = append(out, box{
		label:   bodyLabel,
		hotkey:  "e",
		lines:   bodyLines,
		width:   w,
		height:  l.bodyRows + 2,
		focused: m.focused && m.section == sectionBody,
		padding: 1,
	}.render()...)

	// Relations, scrolled to keep the cursor visible.
	rel := m.relationLines(w - 4)
	relOffset := 0
	if m.relCursor >= l.relRows {
		relOffset = m.relCursor - l.relRows + 1
	}
	relLabel := "Relations"
	if len(rel) > l.relRows {
		relLabel = fmt.Sprintf("Relations · %d/%d", m.relCursor+1, len(m.links))
	}
	out = append(out, box{
		label:   relLabel,
		hotkey:  "b",
		lines:   rel[relOffset:],
		width:   w,
		height:  l.relRows + 2,
		focused: m.focused && m.section == sectionRelations,
		padding: 1,
	}.render()...)

	if l.history {
		row := func(label, value string) string {
			return mutedStyle().Render(fmt.Sprintf("%-9s", label)) + value
		}
		created, updated := "–", "–"
		if b.CreatedAt != nil {
			created = b.CreatedAt.Local().Format("2006-01-02 15:04")
		}
		if b.UpdatedAt != nil {
			updated = b.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		out = append(out, box{label: "History", lines: []string{
			row("Created", created),
			row("Updated", updated),
			row("ETag", b.ETag()),
		}, width: w, padding: 1}.render()...)
	}
	return out
}
