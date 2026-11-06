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
	applyMarkdownGlyphs(&style)
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
	// BeanBlocking and BeanBlockedBy are symmetric: each combines the bean's
	// own list with the other beans' inverse links.
	if blocking, _ := resolver.BeanBlocking(ctx, b, nil); blocking != nil {
		for _, c := range blocking {
			links = append(links, resolvedLink{linkType: "blocking", bean: c})
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
	ws       *workspaces // workspace badges (see workspaces.go)

	bean  *bean.Bean
	links []resolvedLink

	width  int
	height int

	focused    bool
	section    detailSection
	relCursor  int
	bodyOffset int

	// Scrolling of the whole pane when its boxes do not fit (see
	// detail_scroll.go): the first line shown and the section the offset
	// was last adjusted for.
	paneOffset    int
	scrollSection detailSection
	scrollFocused bool

	// Rendered body, cached for bodyKey (width and body text).
	body    []string
	bodyKey string

	// Checkboxes of the body, the checkbox cursor, the rendered line of the
	// current checkbox (-1 = not shown) and whether to scroll to it.
	checks      []checkboxItem
	checkCursor int
	checkLine   int
	checkScroll bool

	// IDs of previously shown beans, for backspace.
	history []string

	// Records the clickable regions while rendering (layout.go).
	hits hitRecorder
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
		m.relCursor, m.bodyOffset, m.checkCursor, m.paneOffset = 0, 0, 0, 0
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
		m.body, m.bodyKey, m.checks = nil, "", nil
		return
	}
	m.checks = findCheckboxes(m.bean.Body)
	m.checkCursor = max(0, min(m.checkCursor, len(m.checks)-1))
	key := fmt.Sprintf("%d\x00%d\x00%s\x00%s", m.bodyWidth(), lipgloss.ColorProfile(), m.checkboxKey(), m.bean.Body)
	if key != m.bodyKey {
		m.bodyKey = key
		m.renderBody()
	}
	m.relCursor = max(0, min(m.relCursor, len(m.links)-1))
	l := m.layout()
	checkScroll := m.checkScroll
	if m.checkScroll {
		m.checkScroll = false
		m.scrollToCheckbox(l.bodyRows)
	}
	m.bodyOffset = max(0, min(m.bodyOffset, len(m.body)-l.bodyRows))
	m.syncPaneOffset(l, checkScroll)
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
	// scroll is set when the boxes do not fit even when shrunk: then all
	// of them are shown in full and the whole pane scrolls.
	scroll bool
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
			n += m.historyHeight()
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
	if need() > m.height {
		return m.scrollLayout()
	}
	room := m.height - need() + 1
	if l.history && len(m.body) > room {
		// The body matters more than the history.
		l.history = false
		room += m.historyHeight()
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
	scroll := func(delta int) { m.scrollBy(delta) }
	page := max(l.bodyRows-1, 1)
	if l.scroll {
		page = max(m.height-1, 1)
	}
	if handled, cmd := m.handleCheckboxKey(key.String()); handled {
		return m, cmd
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
		scroll(-page)
	case "pgdown", "ctrl+d", "ctrl+f", " ":
		scroll(page)
	case "home":
		m.scrollHome()
	case "end", "G":
		m.scrollEnd()
	case "s", "t", "P", "p", "b":
		return m, openPickerCmd(pickerKeys[key.String()], []string{b.ID}, b)
	case "e":
		return m, func() tea.Msg { return openEditorMsg{beanID: b.ID, beanPath: b.Path} }
	case "y":
		return m, func() tea.Msg { return copyBeanIDMsg{ids: []string{b.ID}} }
	}
	return m, nil
}

// hints returns the footer hints for the focused pane in order of importance.
func (m detailModel) hints() []keyHint {
	// The hints refer to the command registry (commands.go).
	var hints []keyHint
	hints = append(hints, cmdHint("back"))
	if m.section == sectionRelations {
		hints = append(hints, cmdHint("open-relation"), cmdHint("detail-move", "move"))
	} else {
		hints = append(hints, cmdHint("detail-move"))
		if len(m.checks) > 0 {
			hints = append(hints, cmdHint("checkbox-select"), cmdHint("checkbox-toggle"))
		}
	}
	if m.section == sectionRelations {
		hints = append(hints, cmdHint("detail-sections"))
	} else {
		hints = append(hints, cmdHint("detail-sections", "filters"))
	}
	if len(m.history) > 0 {
		hints = append(hints, cmdHint("previous-bean"))
	}
	return append(hints,
		cmdHint("edit"),
		cmdHint("status"),
		cmdHint("type"),
		cmdHint("priority"),
		cmdHint("parent"),
		cmdHint("blocking"),
		cmdHint("copy-id"),
		cmdHint("detail-page"),
	)
}

// Rendering --------------------------------------------------------------------

// relationLines renders all relation entries.
func (m detailModel) relationLines(width int) []string {
	if len(m.links) == 0 {
		return []string{mutedStyle().Render("(none)")}
	}
	cursor := -1
	if m.focused && m.section == sectionRelations {
		cursor = m.relCursor
	}
	return relationLinesFor(m.config, m.links, cursor, width)
}

// relationLinesFor renders relations as "label  S T id title" lines of at
// most width cells, the label shown once per group, with a cursor on the
// cursor-th one (-1 = none).
func relationLinesFor(cfg *config.Config, links []resolvedLink, cursor, width int) []string {
	labelW := 0
	for _, l := range links {
		labelW = max(labelW, lipgloss.Width(formatLinkLabel(l.linkType, l.incoming)))
	}
	lines := make([]string, len(links))
	prev := ""
	for i, l := range links {
		label := formatLinkLabel(l.linkType, l.incoming)
		shown := label
		if label == prev {
			shown = ""
		}
		prev = label
		selected := i == cursor
		cursorMark := "  "
		if selected {
			cursorMark = accentStyle().Render(glyphs().Cursor + " ")
		}
		// Highlighted without a background inside the box.
		ref := beanRef(cfg, l.bean, rowStyler{selected: selected})
		if l.linkType == "implicit" {
			ref = mutedStyle().Render(l.bean.Status+" "+glyphs().Sep) + " " + ref
		}
		line := cursorMark + mutedStyle().Render(fmt.Sprintf("%-*s", labelW, shown)) + "  " + ref
		lines[i] = truncateText(line, width)
	}
	return lines
}

// fieldBox renders a small single-value box.
func fieldBox(label, hotkey, value string, width int) []string {
	return box{label: label, hotkey: hotkey, lines: []string{value}, width: width, padding: 1}.render()
}

// recordGrid lays out field boxes in rows that fit width (boxGridCells) at
// line top of the pane and records each box as a field invoking its key (none for "").
func (m detailModel) recordGrid(top, width int, keys []string, boxes []func(w int) []string) []string {
	lines, cells := boxGridCells(width, boxes)
	for i, c := range cells {
		if keys[i] != "" {
			m.hits.add(c.x, top+c.y, c.w, c.h, hitTarget{kind: hitDetailField, key: keys[i]})
		}
	}
	return lines
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
		m.hits = m.hits.within(w, h)
		if m.layout().scroll {
			// The pane scrolls as a whole: boxes are recorded at their
			// position in the scrolled pane (clipped to it above).
			m.hits = m.hits.at(0, -m.paneOffset)
		}
		lines = m.renderBoxes(w)
	}
	if m.bean != nil && m.layout().scroll {
		lines = lines[min(m.paneOffset, len(lines)):]
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
	out = append(out, box{label: "Title" + glyphs().sep() + b.ID, topRight: workspaceBadge(m.resolver.Core, m.ws, b.ID, ""), hotkey: "e", lines: l.titleLines, width: w, padding: 1}.render()...)
	m.hits.add(0, 0, w, len(out), hitTarget{kind: hitDetailField, key: "e"})

	status := fg(colNamed(colors.StatusColor)).Bold(!colors.IsArchive).Render(b.Status)
	if implicit, _ := m.resolver.Core.ImplicitStatus(b.ID); implicit != "" {
		status += mutedStyle().Render(" " + glyphs().Implicit + implicit)
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
		sep := mutedStyle().Render(glyphs().sep())
		m.hits.add(0, len(out), w, 3, hitTarget{kind: hitDetailField, key: "s"})
		out = append(out, fieldBox(strings.Join([]string{"Status", "Type", "Priority"}, glyphs().sep()), "s t P", status+sep+typ+sep+prio, w)...)
	} else {
		out = append(out, m.recordGrid(len(out), w, []string{"s", "t", "P"}, []func(int) []string{
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
		out = append(out, m.recordGrid(len(out), w, []string{"p", ""}, []func(int) []string{
			func(bw int) []string { return fieldBox("Parent", "p", parent, bw) },
			func(bw int) []string { return fieldBox("Tags", "", tags, bw) },
		})...)
	}

	// Body
	bodyLabel := "Body"
	if len(m.body) > l.bodyRows {
		end := min(m.bodyOffset+l.bodyRows, len(m.body))
		bodyLabel = fmt.Sprintf("Body%s%d%s%d/%d", glyphs().sep(), m.bodyOffset+1, glyphs().Range, end, len(m.body))
	}
	bodyLines := m.body[min(m.bodyOffset, len(m.body)):]
	m.recordScrollBox(len(out), w, l.bodyRows, "e", hitBody, hitBodyLine, m.bodyOffset, min(l.bodyRows, len(bodyLines)))
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
	shownRels := 0
	if len(m.links) > 0 {
		shownRels = min(l.relRows, len(rel)-relOffset)
	}
	m.recordScrollBox(len(out), w, l.relRows, "b", 0, hitRelation, relOffset, shownRels)
	relLabel := "Relations"
	if len(rel) > l.relRows {
		relLabel = fmt.Sprintf("Relations%s%d/%d", glyphs().sep(), m.relCursor+1, len(m.links))
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
		created, updated := glyphs().Range, glyphs().Range
		if b.CreatedAt != nil {
			created = b.CreatedAt.Local().Format("2006-01-02 15:04")
		}
		if b.UpdatedAt != nil {
			updated = b.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		historyRows := []string{
			row("Created", created),
			row("Updated", updated),
			row("ETag", b.ETag()),
		}
		if info, ok := workspaceFor(m.resolver.Core, m.ws, b.ID); ok {
			badge := workspaceBadge(m.resolver.Core, m.ws, b.ID, "")
			path := truncatePathLeft(info.Path, w-4-9-lipgloss.Width(badge)-1)
			historyRows = append(historyRows, row("Changed", badge+mutedStyle().Render(" "+path)))
		}
		out = append(out, box{label: "History", lines: historyRows, width: w, padding: 1}.render()...)
	}
	return out
}
