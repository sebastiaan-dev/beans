package tui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/internal/ui/theme"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// Live theme reload.
//
// While the TUI runs, a themeWatcher watches the sources of the theme in
// effect: the config file (tui.theme, tui.theme_background,
// tui.theme_overrides), the palette file of a file: theme, and the parts of
// the Neovim config that select or define the colorscheme of an nvim theme
// (the files of the export's cache key, see theme.NvimThemeSources). After a
// change (debounced) it reloads the theme in the background, re-exporting
// the Neovim colorscheme when needed, and sends the new settings to the
// program as a themeReloadedMsg. Update applies them with ui.ApplyThemeSettings:
// the shared ui colors and styles are only ever changed on the Bubbletea
// goroutine, never concurrently with View.
//
// Environment variables ($BEANS_THEME, ...) are read when reloading but not
// watched: they cannot change for a running process.

// themeReloadDebounce is how long the watcher waits for further changes
// before reloading (editors and plugin managers write several files).
const themeReloadDebounce = 300 * time.Millisecond

// themeReloadTimeout bounds a reload (mainly the Neovim export).
const themeReloadTimeout = 10 * time.Second

// themeReloadedMsg carries reloaded theme settings to Update. With err set
// the reload failed and the current theme is kept.
type themeReloadedMsg struct {
	settings ui.ThemeSettings
	err      error
}

// applyThemeReload applies reloaded theme settings (on the Bubbletea
// goroutine) and drops renderings that captured the previous colors.
func (a *App) applyThemeReload(msg themeReloadedMsg) tea.Cmd {
	if msg.err != nil {
		first, _, _ := strings.Cut(msg.err.Error(), "\n")
		a.list.statusMessage = "Theme not reloaded: " + first
		return nil
	}
	ui.ApplyThemeSettings(msg.settings)
	a.detail.bodyKey = "" // re-render the markdown body with the new colors
	a.detailStale = true
	return nil
}

// startThemeReload starts watching the theme sources of cfg; current are
// the settings applied at startup. It returns a function that stops the
// watcher. Without color output, or when the watcher cannot be created,
// nothing is watched.
func startThemeReload(cfg *config.Config, current ui.ThemeSettings, send func(tea.Msg)) (stop func()) {
	if lipgloss.ColorProfile() == termenv.Ascii {
		return func() {}
	}
	w, err := newThemeWatcher(cfg, current, send)
	if err != nil {
		return func() {}
	}
	w.start()
	return w.stop
}

// themeWatcher reloads the theme when its sources change (see above).
type themeWatcher struct {
	// configPath is the config file to reload; cfg is used as is when it
	// has none and no file has been created since.
	configPath string
	cfg        *config.Config
	send       func(tea.Msg)
	debounce   time.Duration
	// load loads theme settings (ui.LoadThemeSettings); replaceable in tests.
	load func(context.Context, *config.Config) (ui.ThemeSettings, error)
	// nvimConfigDir returns Neovim's config dir (theme.NvimConfigDir).
	nvimConfigDir func() string

	fsw    *fsnotify.Watcher
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Owned by the run goroutine after start.
	current ui.ThemeSettings
	sources themeSources
	watched map[string]bool
}

// themeSources are the resolved paths whose changes trigger a reload.
type themeSources struct {
	configFile  string
	paletteFile string
	nvimDir     string // "" unless the theme is an nvim theme
}

func newThemeWatcher(cfg *config.Config, current ui.ThemeSettings, send func(tea.Msg)) (*themeWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	path := cfg.ConfigPath()
	if path == "" && cfg.ConfigDir() != "" {
		path = filepath.Join(cfg.ConfigDir(), config.ConfigFileName)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &themeWatcher{
		configPath:    path,
		cfg:           cfg,
		send:          send,
		debounce:      themeReloadDebounce,
		load:          ui.LoadThemeSettings,
		nvimConfigDir: theme.NvimConfigDir,
		fsw:           fsw,
		ctx:           ctx,
		cancel:        cancel,
		current:       current,
		watched:       map[string]bool{},
	}
	return w, nil
}

// start watches the sources of the initial config and starts the loop.
func (w *themeWatcher) start() {
	w.updateWatches(w.cfg)
	w.wg.Add(1)
	go w.run()
}

// stop stops the watcher and waits for a running reload to finish (it is
// canceled).
func (w *themeWatcher) stop() {
	w.cancel()
	w.fsw.Close()
	w.wg.Wait()
}

func (w *themeWatcher) run() {
	defer w.wg.Done()
	var timer *time.Timer
	var fire <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-w.ctx.Done():
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod || !w.relevant(ev.Name) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(w.debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
			fire = timer.C
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// Overflows and similar errors only cost a missed reload.
		case <-fire:
			fire = nil
			w.reload()
		}
	}
}

// reload reloads the config and the theme and sends the result if it
// differs from the applied settings or failed.
func (w *themeWatcher) reload() {
	cfg := w.cfg
	if w.configPath != "" {
		c, err := config.Load(w.configPath)
		if err != nil {
			w.sendMsg(themeReloadedMsg{err: fmt.Errorf("reading %s: %w", w.configPath, err)})
			return
		}
		if c.ConfigDir() == "" { // no file: defaults anchored at its dir
			c.SetConfigDir(filepath.Dir(w.configPath))
		}
		cfg = c
	}
	ctx, cancel := context.WithTimeout(w.ctx, themeReloadTimeout)
	defer cancel()
	s, err := w.load(ctx, cfg)
	if w.ctx.Err() != nil {
		return
	}
	// The theme setting may have changed (e.g. file: -> nvim).
	w.updateWatches(cfg)
	if err != nil {
		w.sendMsg(themeReloadedMsg{err: err})
		return
	}
	if s.Equal(w.current) {
		return
	}
	w.current = s
	w.sendMsg(themeReloadedMsg{settings: s})
}

func (w *themeWatcher) sendMsg(msg themeReloadedMsg) {
	if w.ctx.Err() == nil {
		w.send(msg)
	}
}

// resolvePath resolves symlinks in the directory of p (fsnotify reports
// events under the watched, resolved path).
func resolvePath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(dir, filepath.Base(abs))
	}
	return abs
}

func resolveDir(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		if abs, err := filepath.Abs(r); err == nil {
			return abs
		}
		return r
	}
	abs, _ := filepath.Abs(p)
	return abs
}

// sourcesFor returns the theme sources of cfg.
func (w *themeWatcher) sourcesFor(cfg *config.Config) themeSources {
	s := themeSources{configFile: resolvePath(w.configPath)}
	spec, err := cfg.GetTheme()
	if err != nil {
		return s
	}
	switch spec.Kind {
	case config.ThemeFile:
		s.paletteFile = resolvePath(spec.Path)
	case config.ThemeNvim:
		if dir := w.nvimConfigDir(); dir != "" {
			s.nvimDir = resolveDir(dir)
		}
	}
	return s
}

// updateWatches watches the directories containing the sources of cfg and
// stops watching the ones no longer needed. Directories are watched rather
// than files because editors replace files when saving.
func (w *themeWatcher) updateWatches(cfg *config.Config) {
	w.sources = w.sourcesFor(cfg)
	want := map[string]bool{}
	for _, f := range []string{w.sources.configFile, w.sources.paletteFile} {
		if f != "" {
			want[filepath.Dir(f)] = true
		}
	}
	if d := w.sources.nvimDir; d != "" {
		want[d] = true
		_, dirs := theme.NvimThemeSources(d)
		for _, root := range dirs {
			_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
				if err != nil || !e.IsDir() {
					return nil
				}
				if path != root && strings.HasPrefix(e.Name(), ".") {
					return filepath.SkipDir
				}
				want[path] = true
				return nil
			})
		}
	}
	for dir := range w.watched {
		if !want[dir] {
			_ = w.fsw.Remove(dir)
			delete(w.watched, dir)
		}
	}
	for dir := range want {
		if w.watched[dir] {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		if err := w.fsw.Add(dir); err == nil {
			w.watched[dir] = true
		}
	}
}

// relevant reports whether a change of path can change the theme.
func (w *themeWatcher) relevant(path string) bool {
	path = filepath.Clean(path)
	if path == w.sources.configFile || (w.sources.paletteFile != "" && path == w.sources.paletteFile) {
		return true
	}
	d := w.sources.nvimDir
	if d == "" {
		return false
	}
	rel, err := filepath.Rel(d, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	files, dirs := theme.NvimThemeSources(d)
	for _, f := range files {
		if path == f {
			return true
		}
	}
	for _, dir := range dirs {
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			// Hidden entries (e.g. .git) are not part of the cache key.
			return !strings.Contains(string(filepath.Separator)+path[len(dir):], string(filepath.Separator)+".")
		}
	}
	return false
}
