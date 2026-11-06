package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// reloadTestEnv is a project with a config file and a fake Neovim config.
type reloadTestEnv struct {
	dir, configPath, nvimDir string
	msgs                     chan tea.Msg
}

func newReloadTestEnv(t *testing.T, beansYml string) *reloadTestEnv {
	t.Helper()
	for _, v := range []string{config.ThemeEnvVar, config.ThemeBackgroundEnvVar, config.ThemeOverridesEnvVar} {
		t.Setenv(v, "")
	}
	root := t.TempDir()
	e := &reloadTestEnv{
		dir:     filepath.Join(root, "project"),
		nvimDir: filepath.Join(root, "nvim"),
		msgs:    make(chan tea.Msg, 16),
	}
	e.configPath = filepath.Join(e.dir, config.ConfigFileName)
	writeTestFile(t, e.configPath, beansYml)
	writeTestFile(t, filepath.Join(e.nvimDir, "init.lua"), "vim.cmd.colorscheme('a')\n")
	writeTestFile(t, filepath.Join(e.nvimDir, "colors", "a.lua"), "-- a\n")
	return e
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// watcher starts a watcher on the env's config; load replaces the theme
// loading when not nil.
func (e *reloadTestEnv) watcher(t *testing.T, load func(context.Context, *config.Config) (ui.ThemeSettings, error)) *themeWatcher {
	t.Helper()
	cfg, err := config.Load(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := newThemeWatcher(cfg, ui.ThemeSettings{}, func(m tea.Msg) { e.msgs <- m })
	if err != nil {
		t.Fatal(err)
	}
	w.debounce = 50 * time.Millisecond
	w.nvimConfigDir = func() string { return e.nvimDir }
	if load != nil {
		w.load = load
	}
	w.start()
	t.Cleanup(w.stop)
	return w
}

func (e *reloadTestEnv) next(t *testing.T) themeReloadedMsg {
	t.Helper()
	select {
	case m := <-e.msgs:
		return m.(themeReloadedMsg)
	case <-time.After(5 * time.Second):
		t.Fatal("no theme reload message")
		return themeReloadedMsg{}
	}
}

func (e *reloadTestEnv) none(t *testing.T) {
	t.Helper()
	select {
	case m := <-e.msgs:
		t.Fatalf("unexpected message %+v", m)
	case <-time.After(300 * time.Millisecond):
	}
}

// countingLoad returns settings whose override "accent" is the number of
// loads, so every load yields different settings.
func countingLoad(n *atomic.Int32) func(context.Context, *config.Config) (ui.ThemeSettings, error) {
	return func(_ context.Context, cfg *config.Config) (ui.ThemeSettings, error) {
		spec, _ := cfg.GetTheme()
		return ui.ThemeSettings{
			Theme:     &ui.Theme{Name: spec.String()},
			Overrides: map[string]string{"accent": string(rune('0' + n.Add(1)))},
		}, nil
	}
}

func TestThemeWatcherNvimSourceChange(t *testing.T) {
	e := newReloadTestEnv(t, "tui:\n  theme: nvim\n")
	var loads atomic.Int32
	e.watcher(t, countingLoad(&loads))

	// A burst of changes to a colorscheme file reloads once.
	for i := 0; i < 5; i++ {
		writeTestFile(t, filepath.Join(e.nvimDir, "colors", "a.lua"), strings.Repeat("-", i)+"\n")
	}
	if m := e.next(t); m.err != nil || m.settings.Theme.Name != "nvim" {
		t.Fatalf("msg = %+v", m)
	}
	e.none(t)
	if n := loads.Load(); n != 1 {
		t.Errorf("%d loads for one burst, want 1", n)
	}

	// init.lua (switching the colorscheme) and files in new directories
	// below lua/ are sources too.
	writeTestFile(t, filepath.Join(e.nvimDir, "init.lua"), "vim.cmd.colorscheme('b')\n")
	e.next(t)
	writeTestFile(t, filepath.Join(e.nvimDir, "lua", "plugins", "theme.lua"), "return {}\n")
	e.next(t)
	writeTestFile(t, filepath.Join(e.nvimDir, "lua", "plugins", "theme.lua"), "return { 'x' }\n")
	e.next(t)

	// Files outside the cache key don't trigger a reload.
	writeTestFile(t, filepath.Join(e.nvimDir, "README.md"), "x\n")
	writeTestFile(t, filepath.Join(e.nvimDir, "lua", ".hidden", "x.lua"), "x\n")
	writeTestFile(t, filepath.Join(e.dir, "other.txt"), "x\n")
	e.none(t)
}

func TestThemeWatcherConfigChange(t *testing.T) {
	e := newReloadTestEnv(t, "tui:\n  theme: terminal\n")
	var loads atomic.Int32
	w := e.watcher(t, countingLoad(&loads))

	// The terminal theme has no other sources.
	writeTestFile(t, filepath.Join(e.nvimDir, "init.lua"), "x\n")
	e.none(t)

	// Switching to nvim in .beans.yml reloads and starts watching nvim.
	writeTestFile(t, e.configPath, "tui:\n  theme: nvim\n")
	if m := e.next(t); m.settings.Theme.Name != "nvim" {
		t.Fatalf("msg = %+v", m)
	}
	writeTestFile(t, filepath.Join(e.nvimDir, "colors", "a.lua"), "-- changed\n")
	e.next(t)

	// Switching to a file theme watches the palette file instead.
	writeTestFile(t, e.configPath, "tui:\n  theme: file:palette.yml\n")
	if m := e.next(t); !strings.HasPrefix(m.settings.Theme.Name, "file:") {
		t.Fatalf("msg = %+v", m)
	}
	writeTestFile(t, filepath.Join(e.dir, "palette.yml"), "fg: '#ffffff'\n")
	e.next(t)
	writeTestFile(t, filepath.Join(e.nvimDir, "colors", "a.lua"), "-- again\n")
	e.none(t)
	if w.sources.nvimDir != "" {
		t.Errorf("still watching nvim: %+v", w.sources)
	}

	// A broken config is reported.
	writeTestFile(t, e.configPath, "tui: [\n")
	if m := e.next(t); m.err == nil || !strings.Contains(m.err.Error(), config.ConfigFileName) {
		t.Fatalf("msg = %+v, want a config error", m)
	}
}

func TestThemeWatcherSkipsUnchangedSettings(t *testing.T) {
	e := newReloadTestEnv(t, "tui:\n  theme: nvim\n")
	var loads atomic.Int32
	e.watcher(t, func(context.Context, *config.Config) (ui.ThemeSettings, error) {
		if loads.Add(1) == 2 {
			return ui.ThemeSettings{}, errors.New("nvim colorscheme \"b\": E185\nmore")
		}
		return ui.ThemeSettings{}, nil
	})
	// Same settings as applied: nothing to do.
	writeTestFile(t, filepath.Join(e.nvimDir, "init.lua"), "a\n")
	e.none(t)
	// Errors are always reported.
	writeTestFile(t, filepath.Join(e.nvimDir, "init.lua"), "b\n")
	if m := e.next(t); m.err == nil {
		t.Fatalf("msg = %+v, want an error", m)
	}
}

// TestThemeReloadAppliesInUpdate drives the real flow: a palette file
// changes, the watcher loads the theme in the background while the app keeps
// rendering, and Update applies it. Run with -race: the background reload
// must not touch the ui state that View reads.
func TestThemeReloadAppliesInUpdate(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	t.Cleanup(func() { ui.ApplyTheme(nil, false) })
	e := newReloadTestEnv(t, "tui:\n  theme: file:palette.yml\n  theme_overrides:\n    accent: '4'\n")
	palette := filepath.Join(e.dir, "palette.yml")
	writeTestFile(t, palette, "fg: '#c5c9c7'\nbg: '#22262d'\nmuted: '#75797f'\nborder: '#5c6066'\nansi: ['#22262d', '', '', '', '#8ba4b0']\n")

	cfg, err := config.Load(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ui.ApplyConfiguredThemeSettings(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if ui.ColorBorder != "#5c6066" || ui.ColorPrimary != "#8ba4b0" {
		t.Fatalf("initial theme: border=%q accent=%q", ui.ColorBorder, ui.ColorPrimary)
	}
	resolver, _, _ := newColorTestResolver(t)
	app := newBrowseTestApp(t, resolver, cfg)

	w, err := newThemeWatcher(cfg, settings, func(m tea.Msg) { e.msgs <- m })
	if err != nil {
		t.Fatal(err)
	}
	w.debounce = 20 * time.Millisecond
	w.start()
	t.Cleanup(w.stop)

	// Keep rendering (and re-applying the theme, as Update would) on this
	// goroutine while the watcher reloads in the background.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// A faint border is replaced by the contrast guard; the accent
		// override now resolves against the new slot 4.
		_ = os.WriteFile(palette, []byte("fg: '#c5c9c7'\nbg: '#22262d'\nmuted: '#75797f'\nborder: '#393b44'\nansi: ['#22262d', '', '', '', '#7fb4ca']\n"), 0o644)
	}()
	var msg themeReloadedMsg
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case m := <-e.msgs:
			msg = m.(themeReloadedMsg)
			break loop
		case <-deadline:
			t.Fatal("no reload")
		default:
			_ = app.View()
			ui.ApplyThemeSettings(settings)
		}
	}
	wg.Wait()
	if msg.err != nil {
		t.Fatal(msg.err)
	}

	borderSeq := termenv.TrueColor.Color("#75797f").Sequence(false)
	app.Update(msg)
	if ui.ColorBorder != "#75797f" || ui.ColorPrimary != "#7fb4ca" || ui.ColorFocusBorder != "#7fb4ca" {
		t.Errorf("reloaded theme not applied: border=%q accent=%q focus=%q", ui.ColorBorder, ui.ColorPrimary, ui.ColorFocusBorder)
	}
	if view := app.View(); !strings.Contains(view, borderSeq) {
		t.Error("view not redrawn with the reloaded border color")
	}

	// A failed reload keeps the theme and shows a one-line status message.
	app.Update(themeReloadedMsg{err: errors.New("theme nvim: E185\ndetails")})
	if ui.ColorBorder != "#75797f" || app.list.statusMessage != "Theme not reloaded: theme nvim: E185" {
		t.Errorf("failed reload: border=%q status=%q", ui.ColorBorder, app.list.statusMessage)
	}
}
