package tui

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

var updateGolden = flag.Bool("update", false, "update the golden files in testdata/")

// demoBeans are the bean files of the browse view fixture: a milestone with
// epics and tasks, tags, blocking, completed, scrapped and archived beans.
var demoBeans = map[string]string{
	"demo-m001--release-10.md": `---
title: Release 1.0
status: todo
type: milestone
---
`,
	"demo-e001--login-overhaul.md": `---
title: Login overhaul
status: in-progress
type: epic
tags: [auth]
parent: demo-m001
---
`,
	"demo-t001--two-factor.md": `---
title: Two-factor authentication for the login form
status: in-progress
type: feature
priority: high
tags: [auth, security]
parent: demo-e001
---
Collection of improvements to the login flow based on the security review and the user research analysis. Covers 2FA, error messages and lockout handling.

## Tasks

- [ ] TOTP support
- [ ] Recovery codes
`,
	"demo-t002--error-texts.md": `---
title: Check the error texts of the login mask
status: todo
type: task
parent: demo-e001
---
`,
	"demo-t003--password-reset.md": `---
title: Password reset via e-mail with a very long title that will need to wrap across several lines in the tree pane
status: todo
type: task
priority: low
parent: demo-e001
blocked_by: [demo-t001]
---
`,
	"archive/demo-t004--session-timeout.md": `---
title: Session timeout bug
status: completed
type: bug
parent: demo-e001
---
`,
	"demo-e002--export.md": `---
title: Export
status: todo
type: epic
tags: [export]
parent: demo-m001
---
`,
	"demo-t005--csv.md": `---
title: Export as CSV
status: todo
type: task
priority: critical
parent: demo-e002
---
`,
	"demo-t006--json.md": `---
title: Export as JSON
status: draft
type: task
priority: deferred
parent: demo-e002
---
`,
	"demo-m002--scrapped.md": `---
title: Scrapped milestone
status: scrapped
type: milestone
---
`,
	"demo-t007--child-of-scrapped.md": `---
title: Child of scrapped
status: todo
type: task
parent: demo-m002
---
`,
	"demo-t008--loose.md": `---
title: Loose task without parent
status: todo
type: task
tags: [docs]
---
`,
}

// newDemoApp creates the fixture project and a loaded app of the given size.
func newDemoApp(t *testing.T, width, height int) *App {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	beansDir := filepath.Join(root, ".beans")
	const stamp = "created_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-03T04:05:06Z\n"
	for name, content := range demoBeans {
		path := filepath.Join(beansDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content = strings.Replace(content, "---\n", "---\n"+stamp, 1)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultWithPrefix("demo-")
	cfg.Project.Name = "demo"
	core := beancore.New(beansDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })

	app := New(core, cfg)
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	runCmd(t, app, app.Init())
	return app
}

// withStableRendering renders without colors and in UTC.
func withStableRendering(t *testing.T) {
	t.Helper()
	withColorProfile(t, termenv.Ascii)
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })
}

// checkFrame asserts that a rendered screen is exactly width x height with
// an intact outer frame.
func checkFrame(t *testing.T, screen string, width, height int) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != height {
		t.Errorf("screen has %d lines, want %d", len(lines), height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != width {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, w, width, l)
		}
		var first, last string
		if r := []rune(l); len(r) > 0 {
			first, last = string(r[0]), string(r[len(r)-1])
		}
		want := [2]string{"│", "│"}
		switch i {
		case 0:
			want = [2]string{"╭", "╮"}
		case len(lines) - 1:
			want = [2]string{"╰", "╯"}
		}
		if first != want[0] || last != want[1] {
			t.Errorf("line %d: frame broken (%q…%q): %q", i, first, last, l)
		}
	}
}

func TestBrowseGolden(t *testing.T) {
	withStableRendering(t)
	cases := []struct {
		name          string
		width, height int
		keys          []string
	}{
		{"browse-160x40", 160, 40, []string{"j", "j"}},
		{"browse-100x30", 100, 30, []string{"j", "j"}},
		{"browse-70x24", 70, 24, []string{"j", "j"}},
		{"detail-160x40", 160, 40, []string{"j", "j", "enter"}},
		{"detail-70x24", 70, 24, []string{"j", "j", "enter"}},
		{"folded-filtered-100x30", 100, 30, []string{"z", "M", "/", "e", "x", "p", "enter"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newDemoApp(t, tc.width, tc.height)
			for _, k := range tc.keys {
				pressKey(t, app, k)
			}
			screen := stripAnsi(app.View())
			checkFrame(t, screen, tc.width, tc.height)

			path := filepath.Join("testdata", tc.name+".golden")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(screen+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/tui -run TestBrowseGolden -update)", err)
			}
			if got := screen + "\n"; got != string(want) {
				t.Errorf("screen differs from %s (run with -update to accept):\n%s", path, got)
			}
		})
	}
}

// TestBrowseFramesAtManySizes renders the browse view in its states at many
// terminal sizes and checks that it always fills the terminal exactly.
func TestBrowseFramesAtManySizes(t *testing.T) {
	withStableRendering(t)
	states := map[string][]string{
		"tree":   nil,
		"detail": {"j", "j", "enter"},
		"search": {"/", "x"},
	}
	for name, keys := range states {
		for _, size := range [][2]int{{200, 60}, {160, 40}, {120, 35}, {100, 30}, {99, 27}, {80, 24}, {70, 24}, {50, 16}, {40, 12}, {30, 10}} {
			app := newDemoApp(t, size[0], size[1])
			for _, k := range keys {
				pressKey(t, app, k)
			}
			screen := stripAnsi(app.View())
			t.Run(name, func(t *testing.T) { checkFrame(t, screen, size[0], size[1]) })
		}
	}
}

func TestBrowseTooSmall(t *testing.T) {
	app := newDemoApp(t, 20, 8)
	if got := stripAnsi(app.View()); !strings.Contains(got, "Terminal too small") {
		t.Errorf("tiny terminal: %q", got)
	}
}

func TestBrowseHeader(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	header := stripAnsi(app.headerLine(96))
	if !strings.HasPrefix(header, "beans: demo") || !strings.HasSuffix(header, "/ search · F filters · ? help · q quit") {
		t.Errorf("header = %q", header)
	}
	// Narrow terminals drop the less important hints first.
	header = stripAnsi(app.headerLine(40))
	if !strings.HasPrefix(header, "beans: demo") || !strings.HasSuffix(header, "? help · q quit") || strings.Contains(header, "filters") {
		t.Errorf("narrow header = %q", header)
	}
}

func TestBrowseFilterBar(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 120, 40)
	bar := stripAnsi(strings.Join(app.filterBar(116), "\n"))
	for _, want := range []string{"Status", "Type", "Tags", "Completed", "(any)", "shown", "(F)", "(g t)", "(H)"} {
		if !strings.Contains(bar, want) {
			t.Errorf("filter bar lacks %q:\n%s", want, bar)
		}
	}

	app.list.setFilters(listFilters{statuses: []string{"todo", "draft"}, tags: []string{"auth"}, hideResolved: true})
	runCmd(t, app, app.list.loadBeans)
	pressKey(t, app, "/")
	pressKey(t, app, "x")
	bar = stripAnsi(strings.Join(app.filterBar(116), "\n"))
	for _, want := range []string{"todo, draft", "auth", "hidden", "Search", "x"} {
		if !strings.Contains(bar, want) {
			t.Errorf("filter bar lacks %q:\n%s", want, bar)
		}
	}

	// Short terminals have no filter bar; the tree's label summarizes the
	// filters instead.
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	screen := stripAnsi(app.View())
	if strings.Contains(screen, "─ Status ─") {
		t.Error("short terminals should not show the filter bar")
	}
	if !strings.Contains(screen, "done hidden") {
		t.Errorf("tree label should summarize the filters:\n%s", screen)
	}
}

func TestShortID(t *testing.T) {
	tests := []struct{ id, prefix, want string }{
		{"beans-abc1", "beans-", "abc1"},
		{"bt-pl5p", "bt", "pl5p"},
		{"btpl5p", "bt", "pl5p"},
		{"other-abc1", "beans-", "other-abc1"},
		{"abc1", "", "abc1"},
		{"beans-", "beans-", "beans-"},
	}
	for _, tt := range tests {
		if got := shortID(tt.id, tt.prefix); got != tt.want {
			t.Errorf("shortID(%q, %q) = %q, want %q", tt.id, tt.prefix, got, tt.want)
		}
	}

	// Rows show the short ID, y copies the full one.
	app := newDemoApp(t, 100, 30)
	row := stripAnsi(app.list.renderRow(0, 60)[0])
	if !strings.Contains(row, " m001 ") || strings.Contains(row, "demo-m001") {
		t.Errorf("row should show the short ID: %q", row)
	}
	ids, _ := app.list.selectionOrCurrent()
	if !reflect.DeepEqual(ids, []string{"demo-m001"}) {
		t.Errorf("y should act on the full ID, got %v", ids)
	}
}

func TestWrapTokens(t *testing.T) {
	plain := lipgloss.NewStyle()
	wrap := func(text string, width, maxLines int) []string {
		return wrapTokens(wordTokens(text, plain), width, maxLines, plain)
	}
	tests := []struct {
		text     string
		width    int
		maxLines int
		want     []string
	}{
		{"short title", 20, 3, []string{"short title"}},
		{"a title that wraps onto lines", 12, 3, []string{"a title that", "wraps onto", "lines"}},
		{"one two three four five six seven", 9, 2, []string{"one two", "three…"}},
		{"one two three four", 13, 1, []string{"one two thre…"}},
		{"supercalifragilistic word", 8, 3, []string{"supercal", "ifragili", "stic…"}},
		{"exact fit", 9, 1, []string{"exact fit"}},
		{"", 10, 3, []string{}},
	}
	for _, tt := range tests {
		got := wrap(tt.text, tt.width, tt.maxLines)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("wrap(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.maxLines, got, tt.want)
		}
		for _, l := range got {
			if w := lipgloss.Width(l); w > tt.width {
				t.Errorf("wrap(%q): line %q is %d wide", tt.text, l, w)
			}
		}
	}
}

// TestTreeRowWrapping checks that long titles wrap under the title column
// and are cut with an ellipsis after treeRowMaxLines lines.
func TestTreeRowWrapping(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	i := app.list.rowIndex("demo-t003")
	lines := app.list.renderRow(i, 40)
	if len(lines) != treeRowMaxLines {
		t.Fatalf("row has %d lines, want %d: %q", len(lines), treeRowMaxLines, lines)
	}
	first := stripAnsi(lines[0])
	titleCol := strings.Index(first, "↓") // the priority symbol starts the title
	for _, l := range lines[1:] {
		l = stripAnsi(l)
		if indent := len(l) - len(strings.TrimLeft(l, " ")); indent != lipgloss.Width(first[:titleCol]) {
			t.Errorf("continuation %q not aligned under the title of %q", l, first)
		}
	}
	if last := strings.TrimRight(stripAnsi(lines[len(lines)-1]), " "); !strings.HasSuffix(last, "…") {
		t.Errorf("truncated title should end with an ellipsis: %q", last)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 40 {
			t.Errorf("line %q is %d wide, want 40", stripAnsi(l), w)
		}
	}
}

// TestMarkdownBodyNotIndented guards against glamour's document margin
// indenting wrapped lines after the first.
func TestMarkdownBodyNotIndented(t *testing.T) {
	withColorProfile(t, termenv.Ascii)
	body := "Collection of improvements to the login flow based on the security review and the user research analysis. Covers 2FA, error messages and lockout handling."
	lines := renderMarkdown(body, 40)
	if len(lines) < 3 {
		t.Fatalf("expected the paragraph to wrap, got %q", lines)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, " ") {
			t.Errorf("wrapped line is indented: %q (all: %q)", l, lines)
		}
		if w := lipgloss.Width(l); w > 40 {
			t.Errorf("line %q is %d wide", l, w)
		}
	}

	// The same through the detail pane.
	app := newDemoApp(t, 100, 40)
	pressKey(t, app, "j")
	pressKey(t, app, "j")
	screen := stripAnsi(app.View())
	if !strings.Contains(screen, "│ Collection of improvements") {
		t.Fatalf("body not shown:\n%s", screen)
	}
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, "│   analysis") || strings.Contains(l, "│   user research") {
			t.Errorf("wrapped body line is indented: %q", l)
		}
	}
}

func TestMarkdownRendererCache(t *testing.T) {
	a := markdownRenderer(40)
	if a == nil || markdownRenderer(40) != a {
		t.Fatal("renderers should be cached per width")
	}
	if markdownRenderer(41) == a {
		t.Error("different widths need different renderers")
	}
	// After a theme change the renderers are rebuilt.
	resetMarkdownRenderers()
	if markdownRenderer(40) == a {
		t.Error("reset should drop the cached renderers")
	}
}
