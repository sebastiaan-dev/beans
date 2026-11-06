package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// scrolledHelp returns the text of all pages of the open help overlay,
// scrolling down with j until the end, checking the frame of every page.
func scrolledHelp(t *testing.T, app *App) string {
	t.Helper()
	var pages []string
	for i := 0; i < 200; i++ {
		view := stripAnsi(app.View())
		checkFrame(t, view, app.width, app.height)
		pages = append(pages, view)
		before := app.helpOverlay.offset
		pressKey(t, app, "j")
		if app.helpOverlay.offset == before {
			return strings.Join(pages, "\n")
		}
	}
	t.Fatal("help does not stop scrolling")
	return ""
}

func TestCommandRegistryIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, c := range commands {
		if c.id == "" || ids[c.id] {
			t.Errorf("command id %q is empty or duplicate", c.id)
		}
		ids[c.id] = true
		if !slices.Contains(helpSectionOrder, c.section) {
			t.Errorf("%s: unknown help section %q", c.id, c.section)
		}
		if len(c.keys) == 0 || c.help == "" {
			t.Errorf("%s: needs keys and a help text", c.id)
		}
		// Keys and descriptions fit the two help columns at 70 columns.
		if w := lipgloss.Width(c.keyText()); w >= helpKeyWidth {
			t.Errorf("%s: key label %q is too wide for the help (%d cells)", c.id, c.keyText(), w)
		}
		colW := (newHelpOverlayModel(70, 24).modalWidth() - 4 - 3) / 2
		if w := lipgloss.Width(c.help); w > colW-helpKeyWidth {
			t.Errorf("%s: help %q is cut at 70 columns", c.id, c.help)
		}
		for _, k := range c.keys {
			for _, part := range strings.Split(k, " ") {
				if k != " " && keyMsgFor(part).String() != part {
					t.Errorf("%s: key %q can't be pressed (got %q)", c.id, part, keyMsgFor(part).String())
				}
			}
		}
	}
}

// TestHelpMatchesRegistry checks that the help overlay shows exactly the
// registered commands: every registered binding with its description, and
// no key that is not registered.
func TestHelpMatchesRegistry(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{160, 40}, {70, 24}, {50, 20}} {
		app := newDemoApp(t, size[0], size[1])
		pressKey(t, app, "?")
		view := scrolledHelp(t, app)
		// Registry → help.
		shown := 0
		for _, c := range commands {
			if c.noHelp {
				continue
			}
			shown++
			if !strings.Contains(view, fmt.Sprintf("%-*s%s", helpKeyWidth, c.keyText(), c.help)) {
				t.Errorf("%dx%d: help lacks %q %q", size[0], size[1], c.keyText(), c.help)
			}
		}
		// Help → registry.
		n := 0
		for _, sec := range helpSections() {
			for _, k := range sec.keys {
				n++
				if !slices.ContainsFunc(commands, func(c command) bool {
					return c.section == sec.title && c.keyText() == k[0] && c.help == k[1]
				}) {
					t.Errorf("help entry %q %q is not a registered command", k[0], k[1])
				}
			}
		}
		if n != shown {
			t.Errorf("help lists %d entries, the registry %d", n, shown)
		}
	}
}

// registryKeyTokens returns every key label used by the registry.
func registryKeyTokens() map[string]bool {
	tokens := map[string]bool{}
	add := func(s string) {
		for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '/' }) {
			tokens[f] = true
		}
	}
	for _, c := range commands {
		add(c.keyText())
		add(c.hintKey)
		for _, k := range c.keys {
			tokens[keyLabel(k)] = true
			add(strings.ReplaceAll(k, " ", ""))
		}
	}
	return tokens
}

// TestFooterHintsMatchRegistry checks that every key the footers and the
// header advertise is a registered command key.
func TestFooterHintsMatchRegistry(t *testing.T) {
	tokens := registryKeyTokens()
	app := newDemoApp(t, 160, 40)
	var hints []keyHint
	collect := func(state string, hs []keyHint) {
		for _, h := range hs {
			for _, f := range strings.FieldsFunc(h.key, func(r rune) bool { return r == ' ' || r == '/' }) {
				if !tokens[f] && !tokens[h.key] {
					t.Errorf("%s: footer hint %q %q is not a registered key", state, h.key, h.desc)
				}
			}
		}
		hints = append(hints, hs...)
	}
	collect("tree", app.list.treeHints(true))
	setQuery(app, "login")
	collect("filtered", app.list.treeHints(false))
	app.list.selectedBeans["demo-t001"] = true
	collect("selection", app.list.treeHints(true))
	clear(app.list.selectedBeans)
	moveTo(t, app, "demo-t001")
	pressKey(t, app, "enter")
	collect("detail relations", app.detail.hints())
	pressKey(t, app, "tab")
	collect("detail body", app.detail.hints())
	pressKey(t, app, "F")
	collect("filter bar", app.filterBarHints())
	header := stripAnsi(app.headerLine(150))
	for _, want := range []string{": commands", "/ query", "? help", "q quit"} {
		if !strings.Contains(header, want) {
			t.Errorf("header lacks %q: %q", want, header)
		}
	}
	if len(hints) == 0 {
		t.Fatal("no hints collected")
	}
}

func TestFuzzyScoreRanking(t *testing.T) {
	texts := []string{"Set status…", "Set type…", "Show keyboard shortcuts", "Toggle selection of bean", "Start bean in a workspace", "Set parent…"}
	rank := func(q string) []string {
		type scored struct {
			text  string
			score int
		}
		var out []scored
		for _, s := range texts {
			if sc, ok := fuzzyScore(q, s); ok {
				out = append(out, scored{s, sc})
			}
		}
		slices.SortStableFunc(out, func(a, b scored) int { return b.score - a.score })
		names := make([]string, len(out))
		for i, s := range out {
			names[i] = s.text
		}
		return names
	}
	cases := []struct {
		query string
		want  []string
	}{
		// Prefix beats word start beats subsequence; scattered subsequences
		// ("s…t" in "Toggle selection") don't match.
		{"st", []string{"Start bean in a workspace", "Set status…", "Set type…", "Set parent…"}},
		{"stat", []string{"Set status…", "Start bean in a workspace"}},
		// Acronyms match.
		{"sbw", []string{"Start bean in a workspace"}},
		{"tsob", []string{"Toggle selection of bean"}},
		// Every word must match.
		{"set p", []string{"Set parent…", "Set type…"}},
		{"SHOW", []string{"Show keyboard shortcuts"}},
		{"xyz", []string{}},
	}
	for _, c := range cases {
		got := rank(c.query)
		if !slices.Equal(got, c.want) {
			t.Errorf("rank(%q) = %q, want %q", c.query, got, c.want)
		}
		// Deterministic.
		if again := rank(c.query); !slices.Equal(got, again) {
			t.Errorf("rank(%q) is not deterministic", c.query)
		}
	}
	if s, ok := fuzzyScore("", "anything"); !ok || s != 0 {
		t.Errorf("empty query: %d %v", s, ok)
	}
}

// typePalette opens the palette and types query.
func typePalette(t *testing.T, app *App, query string) {
	t.Helper()
	pressKey(t, app, ":")
	if app.state != viewCommandPalette {
		t.Fatalf(": should open the palette, state = %v", app.state)
	}
	if query != "" {
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
		runCmd(t, app, cmd)
	}
}

func paletteFirst(t *testing.T, app *App) paletteEntry {
	t.Helper()
	if len(app.palette.entries) == 0 {
		t.Fatalf("palette has no entries for %q", app.palette.input.Value())
	}
	return app.palette.entries[app.palette.cursor]
}

func TestPaletteRanksActionsAndBeans(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	typePalette(t, app, "set status")
	if e := paletteFirst(t, app); e.cmd == nil || e.cmd.id != "status" {
		t.Errorf("'set status' should rank the status command first: %+v", e)
	}
	pressKey(t, app, "esc")

	typePalette(t, app, "t002")
	if e := paletteFirst(t, app); e.bean == nil || e.bean.ID != "demo-t002" {
		t.Errorf("a short ID should rank its bean first: %+v", e)
	}
	if app.palette.entries[0].section != sectionBeans {
		t.Error("the beans section should come first when a bean matches best")
	}
	pressKey(t, app, "esc")

	typePalette(t, app, "export json")
	if e := paletteFirst(t, app); e.bean == nil || e.bean.ID != "demo-t006" {
		t.Errorf("'export json' should find the JSON export bean: %+v", e)
	}
	view := stripAnsi(app.View())
	checkFrame(t, view, 160, 40)
	for _, want := range []string{"Command palette", "Beans", "Export as JSON"} {
		if !strings.Contains(view, want) {
			t.Errorf("palette view lacks %q:\n%s", want, view)
		}
	}
}

func TestPaletteShowsUnavailableActionsWithReason(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	typePalette(t, app, "undo")
	e := paletteFirst(t, app)
	if e.cmd == nil || e.cmd.id != "undo" || e.reason != "Nothing to undo" {
		t.Fatalf("undo should be listed as unavailable: %+v", e)
	}
	if view := stripAnsi(app.View()); !strings.Contains(view, "Undo last change — Nothing to undo") {
		t.Errorf("the reason should be shown:\n%s", view)
	}
	pressKey(t, app, "enter")
	if app.state != viewCommandPalette || app.palette.message != "Nothing to undo" {
		t.Errorf("enter on an unavailable action should keep the palette open with the reason: state=%v msg=%q", app.state, app.palette.message)
	}
	// Unavailable actions sort after available ones.
	pressKey(t, app, "esc")
	typePalette(t, app, "")
	seen := false
	for _, e := range app.palette.entries {
		if e.cmd == nil {
			continue
		}
		if e.reason != "" {
			seen = true
		} else if seen {
			t.Fatalf("available %s listed after an unavailable action", e.cmd.id)
		}
	}
}

func TestPaletteRunsActionWithPicker(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	moveTo(t, app, "demo-t002")
	typePalette(t, app, "set status")
	pressKey(t, app, "enter")
	if app.state != viewStatusPicker || app.statusPicker.beanIDs[0] != "demo-t002" {
		t.Fatalf("Set status should open the status picker for the bean: state=%v", app.state)
	}
	pressKey(t, app, "esc")
	if app.state != viewList {
		t.Fatalf("esc should close the picker, state = %v", app.state)
	}

	// The command is remembered at the top.
	typePalette(t, app, "")
	if e := app.palette.entries[0]; e.section != sectionRecent || e.cmd.id != "status" {
		t.Errorf("the last command should come first as recent: %+v", e)
	}
	if view := stripAnsi(app.View()); !strings.Contains(view, "Recent") {
		t.Errorf("the palette should show the recent section:\n%s", view)
	}
}

// TestPaletteRunsTheKeyPath checks that running a command from the palette
// has the same effect as pressing its key.
func TestPaletteRunsTheKeyPath(t *testing.T) {
	skip := map[string]bool{"quit": true, "edit": true, "copy-id": true}
	type snapshot struct {
		state      viewState
		focus      focusArea
		cursor     string
		query      string
		searching  bool
		inline     bool
		selected   int
		rows       int
		status     string
		barOpen    bool
		detailBean string
	}
	snap := func(a *App) snapshot {
		s := snapshot{state: a.state, focus: a.focus, cursor: cursorID(a), query: a.list.search.Value(),
			searching: a.list.searching, inline: a.list.inline != nil, selected: len(a.list.selectedBeans),
			rows: len(a.list.rows), status: a.list.statusMessage, barOpen: a.bar.open}
		if a.detail.bean != nil {
			s.detailBean = a.detail.bean.ID
		}
		return s
	}
	setup := func(t *testing.T) *App {
		app := newDemoApp(t, 160, 40)
		moveTo(t, app, "demo-e001")
		return app
	}
	for i := range commands {
		c := &commands[i]
		if c.run == nil || skip[c.id] {
			continue
		}
		t.Run(c.id, func(t *testing.T) {
			byKey := setup(t)
			if c.unavailable(byKey) != "" {
				t.Skipf("unavailable in the demo: %s", c.unavailable(byKey))
			}
			for _, k := range commandKeySequence(c.keys[0]) {
				_, cmd := byKey.Update(keyMsgFor(k))
				runCmd(t, byKey, cmd)
			}

			byPalette := setup(t)
			typePalette(t, byPalette, c.paletteTitle())
			for j, e := range byPalette.palette.entries {
				if e.cmd == c {
					byPalette.palette.cursor = j
				}
			}
			if e := paletteFirst(t, byPalette); e.cmd != c {
				t.Fatalf("%q not found in the palette", c.paletteTitle())
			}
			_, cmd := byPalette.Update(keyType(tea.KeyEnter))
			runCmd(t, byPalette, cmd)

			if a, b := snap(byKey), snap(byPalette); a != b {
				t.Errorf("palette and key differ:\nkey:     %+v\npalette: %+v", a, b)
			}
		})
	}
}

func TestPaletteJumpsToBeanRevealingFolds(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	pressKeys(t, app, "z", "M")
	if app.list.rowIndex("demo-t005") >= 0 {
		t.Fatal("zM should fold demo-t005 away")
	}
	typePalette(t, app, "csv")
	if view := stripAnsi(app.View()); !strings.Contains(view, "folded") {
		t.Errorf("folded beans should be marked:\n%s", view)
	}
	pressKey(t, app, "esc")
	typePalette(t, app, "csv")
	pressKey(t, app, "enter")
	if app.state != viewList || cursorID(app) != "demo-t005" || app.focus != focusTree {
		t.Errorf("enter should jump to the bean: state=%v cursor=%s focus=%v", app.state, cursorID(app), app.focus)
	}

	// Beans hidden by the filters open in the detail pane.
	setQuery(app, "login")
	typePalette(t, app, "export as json")
	pressKey(t, app, "enter")
	if app.focus != focusDetail || app.detail.bean == nil || app.detail.bean.ID != "demo-t006" {
		t.Errorf("a filtered bean should be shown in the detail pane: focus=%v", app.focus)
	}
	if !strings.Contains(app.list.statusMessage, "hidden by the filters") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
}

func TestPaletteKeys(t *testing.T) {
	app := newDemoApp(t, 70, 24)
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	runCmd(t, app, cmd)
	if app.state != viewCommandPalette {
		t.Fatalf("ctrl+p should open the palette, state = %v", app.state)
	}
	checkFrame(t, stripAnsi(app.View()), 70, 24)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.palette.cursor != 2 {
		t.Errorf("ctrl+n and ↓ should move down: cursor = %d", app.palette.cursor)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if app.palette.cursor != 1 {
		t.Errorf("ctrl+p should move up in the palette: cursor = %d", app.palette.cursor)
	}
	// Typing q is text, not quit.
	if pressKeyQuits(t, app, keyRunes("q")) || app.palette.input.Value() != "q" {
		t.Errorf("q should be typed into the palette: %q", app.palette.input.Value())
	}
	// The cursor stays visible when moving past the shown rows.
	pressKey(t, app, "backspace")
	for i := 0; i < 40; i++ {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	e := paletteFirst(t, app)
	view := stripAnsi(app.View())
	checkFrame(t, view, 70, 24)
	if e.cmd != nil && !strings.Contains(view, "▌"+e.cmd.paletteTitle()) {
		t.Errorf("selected entry %q not visible:\n%s", e.cmd.paletteTitle(), view)
	}
	pressKey(t, app, "esc")
	if app.state != viewList {
		t.Errorf("esc should close the palette, state = %v", app.state)
	}

	// ctrl+p keeps its meaning in text inputs: up in the query input.
	pressKey(t, app, "/")
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	runCmd(t, app, cmd)
	if app.state != viewList || !app.list.searching {
		t.Errorf("ctrl+p while typing a query must not open the palette: state=%v", app.state)
	}
	pressKey(t, app, "esc")
	// ... and in open filter dropdowns.
	pressKeys(t, app, "F", "enter")
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	runCmd(t, app, cmd)
	if app.state != viewList || !app.bar.open {
		t.Errorf("ctrl+p in a dropdown must not open the palette: state=%v open=%v", app.state, app.bar.open)
	}
}

func TestHelpScrolling(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 70, 24)
	pressKey(t, app, "?")
	h := &app.helpOverlay
	view := stripAnsi(app.View())
	checkFrame(t, view, 70, 24)
	for _, want := range []string{": command palette", "j/k scroll", "1–18/"} {
		if !strings.Contains(view, want) {
			t.Errorf("70x24 help lacks %q:\n%s", want, view)
		}
	}
	pressKey(t, app, "j")
	if h.offset != 1 {
		t.Errorf("j: offset = %d", h.offset)
	}
	pressKey(t, app, "k")
	pressKey(t, app, "k")
	if h.offset != 0 {
		t.Errorf("k past the top: offset = %d", h.offset)
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	runCmd(t, app, cmd)
	last := len(h.bodyLines()) - h.bodyRows()
	if want := min(h.bodyRows()-1, last); h.offset != want {
		t.Errorf("pgdn: offset = %d, want %d", h.offset, want)
	}
	pressKey(t, app, "G")
	if h.offset != last {
		t.Errorf("G: offset = %d, want %d", h.offset, last)
	}
	pressKey(t, app, "j")
	if h.offset != last {
		t.Errorf("j past the end: offset = %d", h.offset)
	}
	checkFrame(t, stripAnsi(app.View()), 70, 24)
	pressKey(t, app, "g")
	if h.offset != 0 {
		t.Errorf("g: offset = %d", h.offset)
	}

	// Everything fits at 160x40: no scrolling.
	big := newDemoApp(t, 160, 40)
	pressKey(t, big, "?")
	view = stripAnsi(big.View())
	checkFrame(t, view, 160, 40)
	if strings.Contains(view, "j/k scroll") {
		t.Errorf("160x40 help should fit without scrolling:\n%s", view)
	}
	pressKey(t, big, "j")
	if big.helpOverlay.offset != 0 {
		t.Error("nothing to scroll at 160x40")
	}

	// : opens the palette from the help.
	pressKey(t, app, ":")
	if app.state != viewCommandPalette {
		t.Errorf(": in the help should open the palette, state = %v", app.state)
	}
}

func TestPaletteDirectCommands(t *testing.T) {
	app := newDemoApp(t, 160, 40)
	setQuery(app, "login")
	pressKeys(t, app, "enter")
	if app.focus != focusDetail {
		t.Fatal("enter should focus the details")
	}
	typePalette(t, app, "focus the tree")
	pressKey(t, app, "enter")
	if app.focus != focusTree {
		t.Errorf("Focus the tree: focus = %v", app.focus)
	}
	typePalette(t, app, "clear filters")
	pressKey(t, app, "enter")
	if app.list.hasActiveFilter() || app.state != viewList {
		t.Errorf("Clear filters: query = %q state = %v", app.list.search.Value(), app.state)
	}
}

func TestASCIICommandPalette(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	for _, size := range [][2]int{{160, 40}, {70, 24}} {
		app := newDemoApp(t, size[0], size[1])
		pressKey(t, app, ":")
		for _, q := range []string{"", "s", "undo", "t00"} {
			app.palette.input.SetValue(q)
			app.refreshPalette()
			if bad, found := nonASCII(stripAnsi(app.View())); found {
				t.Errorf("palette %q at %dx%d renders non-ASCII %q", q, size[0], size[1], bad)
			}
		}
	}
}
