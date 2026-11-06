package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// nonPaletteColor matches SGR color parameters outside the 16-color ANSI
// palette: truecolor (38;2 / 48;2) and 256-color (38;5 / 48;5) sequences.
var nonPaletteColor = regexp.MustCompile(`[34]8;[25];`)

func withColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// newColorTestResolver creates a core with beans covering all default
// statuses, types and priorities, plus parent/blocking links.
func newColorTestResolver(t *testing.T) (*beangraph.CoreResolver, *config.Config, []*bean.Bean) {
	t.Helper()
	beansDir := filepath.Join(t.TempDir(), ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	core := beancore.New(beansDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })

	var beans []*bean.Bean
	for i, s := range config.DefaultStatuses {
		b := &bean.Bean{
			ID:       "color-" + s.Name,
			Title:    "Bean " + s.Name,
			Status:   s.Name,
			Type:     config.DefaultTypes[i%len(config.DefaultTypes)].Name,
			Priority: config.DefaultPriorities[i%len(config.DefaultPriorities)].Name,
			Tags:     []string{"tag-" + s.Name},
			Body:     "# Heading\n\nSome *body* with `code` and a [link](https://example.com).\n\n```go\nfunc main() { fmt.Println(\"hi\") // comment\n}\n```\n\n> quote\n\n---\n",
		}
		if len(beans) > 0 {
			b.Parent = beans[0].ID
			b.Blocking = []string{beans[len(beans)-1].ID}
		}
		if err := core.Create(b); err != nil {
			t.Fatal(err)
		}
		beans = append(beans, b)
	}
	return &beangraph.CoreResolver{Core: core}, cfg, beans
}

// TestTUIViewsUseANSIPalette renders the TUI views owned by the shared styles
// under truecolor and 256-color profiles and asserts that only 16-color ANSI
// palette escapes are emitted, so the UI follows the terminal theme.
func TestTUIViewsUseANSIPalette(t *testing.T) {
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256} {
		t.Run(profileName(profile), func(t *testing.T) {
			withColorProfile(t, profile)
			resolver, cfg, beans := newColorTestResolver(t)
			ids := []string{beans[1].ID}
			const w, h = 140, 40

			views := map[string]string{
				"help":     newHelpOverlayModel(w, h).ModalView("background", w, h),
				"status":   newStatusPickerModel(ids, "t", "todo", cfg, w, h).ModalView("bg", w, h),
				"type":     newTypePickerModel(ids, "t", "task", cfg, w, h).ModalView("bg", w, h),
				"priority": newPriorityPickerModel(ids, "t", "high", cfg, w, h).ModalView("bg", w, h),
				"parent":   newParentPickerModel(ids, "t", []string{"task"}, beans[0].ID, resolver, cfg, w, h).ModalView("bg", w, h),
				"blocking": newBlockingPickerModel(beans[1].ID, "t", beans[1].Blocking, resolver, cfg, w, h).ModalView("bg", w, h),
			}

			// The browse view: wide (tree and detail side by side, with the
			// filter bar), narrow, with the detail pane focused, filtered and
			// with a search.
			app := newBrowseTestApp(t, resolver, cfg)
			views["browse"] = sizedView(t, app, w, h)
			pressKey(t, app, "j")
			pressKey(t, app, "enter")
			views["browse-detail"] = sizedView(t, app, w, h)
			views["browse-narrow-detail"] = sizedView(t, app, 70, 24)
			pressKey(t, app, "esc")
			views["browse-narrow"] = sizedView(t, app, 70, 24)
			setQuery(app, "status:todo tag:tag-todo done:no priority:>=high -type:bug")
			pressKey(t, app, "/")
			pressKey(t, app, "b")
			views["browse-filtered"] = sizedView(t, app, w, h)
			pressKey(t, app, "enter")
			// The focused filter bar with an open dropdown, full and collapsed.
			pressKey(t, app, "F")
			pressKey(t, app, "enter")
			views["browse-dropdown"] = sizedView(t, app, w, h)
			views["browse-dropdown-short"] = sizedView(t, app, 70, 24)
			pressKey(t, app, "esc")
			pressKey(t, app, "esc")

			// The filter input is styled from the list theme; render it too.
			sp := newStatusPickerModel(ids, "t", "todo", cfg, w, h)
			sp.list.SetFilterText("to")
			sp.list.SetFilterState(list.Filtering)
			views["status-filtering"] = sp.View()

			for name, view := range views {
				if !strings.Contains(view, "\x1b[") {
					t.Errorf("%s: expected styled output, got none (test would be vacuous)", name)
				}
				if m := nonPaletteColor.FindString(view); m != "" {
					t.Errorf("%s: emits non-palette color escape %q:\n%q", name, m, view)
				}
			}
		})
	}
}

// newBrowseTestApp creates an app on the resolver's core and loads it.
func newBrowseTestApp(t *testing.T, resolver *beangraph.CoreResolver, cfg *config.Config) *App {
	t.Helper()
	app := New(resolver.Core, cfg)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	runCmd(t, app, app.Init())
	return app
}

// sizedView resizes the app and renders it.
func sizedView(t *testing.T, app *App, w, h int) string {
	t.Helper()
	app.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return app.View()
}

func profileName(p termenv.Profile) string {
	switch p {
	case termenv.TrueColor:
		return "truecolor"
	case termenv.ANSI256:
		return "ansi256"
	case termenv.ANSI:
		return "ansi"
	default:
		return "ascii"
	}
}

// kansoTheme is the kanso-mist palette as exported from Neovim.
func kansoTheme() *ui.Theme {
	return &ui.Theme{
		Name: "kanso", Background: "dark",
		Fg: "#c5c9c7", Bg: "#22262d", Muted: "#75797f", Border: "#393b44", Accent: "#8ba4b0",
		Selection: "#393b44", Success: "#98bb6c", Warning: "#dca561", Danger: "#c34043",
		Info: "#658594", Hint: "#6a9589",
		ANSI: [16]string{
			"#22262d", "#c4746e", "#8a9a7b", "#c4b28a", "#8ba4b0", "#a292a3", "#c5c9c7", "#a4a7a4",
			"#909398", "#e46876", "#87a987", "#e6c384", "#7fb4ca", "#938aa9", "#7aa89f", "#c5c9c7",
		},
	}
}

// TestTUIViewsUseTheme applies a hex theme and checks that the views (and
// the package-level styles rebuilt by the theme hook) use its colors, and
// that the app view is painted with the theme background.
func TestTUIViewsUseTheme(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	ui.ApplyTheme(kansoTheme(), true)
	t.Cleanup(func() { ui.ApplyTheme(nil, false) })

	accent := termenv.TrueColor.Color("#8ba4b0").Sequence(false)
	bg := termenv.TrueColor.Color("#22262d").Sequence(true)
	selection := termenv.TrueColor.Color("#393b44").Sequence(true)
	if got := listTitleStyle().Render("x"); !strings.Contains(got, accent) {
		t.Errorf("listTitleStyle doesn't use the theme accent: %q", got)
	}
	if got := helpKeyStyle().Render("x"); !strings.Contains(got, accent) {
		t.Errorf("helpKeyStyle doesn't use the theme accent: %q", got)
	}

	resolver, cfg, _ := newColorTestResolver(t)
	const w, h = 140, 40
	if view := newHelpOverlayModel(w, h).ModalView("background", w, h); !strings.Contains(view, "38;2;") {
		t.Errorf("help: no theme (truecolor) colors: %q", view)
	}

	app := New(resolver.Core, cfg)
	app.Update(tea.WindowSizeMsg{Width: w, Height: h})
	view := app.View()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("painted view has %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, "\x1b["+bg) || lipgloss.Width(l) != w {
			t.Fatalf("line %d not painted to full width: %q", i, l)
		}
	}
	// The browse view (tree + detail pane) uses theme colors, including the
	// selection background on the cursor row.
	view = newDemoApp(t, w, h).View()
	if !strings.Contains(view, accent) {
		t.Error("browse view doesn't use the theme accent")
	}
	if !strings.Contains(view, selection) {
		t.Error("browse view doesn't paint the theme selection background")
	}
}
