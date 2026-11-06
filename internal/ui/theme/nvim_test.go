package theme

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// touch moves a file's modification time forward (file systems may have a
// coarse timestamp resolution, so rewriting alone is not enough).
func touch(t *testing.T, path string, d time.Duration) {
	t.Helper()
	ts := time.Now().Add(d)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestNvimCacheKey(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "nvim")
	writeFile(t, bin, "#!/bin/sh\n")
	cfgDir := filepath.Join(dir, "nvim")
	writeFile(t, filepath.Join(cfgDir, "init.lua"), "vim.cmd.colorscheme('x')")
	writeFile(t, filepath.Join(cfgDir, "lua", "plugins", "colors.lua"), "return {}")

	key := func() string {
		t.Helper()
		k, err := NvimCacheKey(bin, "", cfgDir)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	base := key()
	if base != key() {
		t.Fatal("cache key is not stable")
	}

	if k, _ := NvimCacheKey(bin, "kanso-mist", cfgDir); k == base {
		t.Error("colorscheme argument does not change the key")
	}

	// Files outside the tracked set don't matter.
	writeFile(t, filepath.Join(cfgDir, "README.md"), "notes")
	writeFile(t, filepath.Join(cfgDir, "lua", ".git", "HEAD"), "ref")
	if key() != base {
		t.Error("untracked files changed the key")
	}

	steps := []struct {
		name   string
		change func()
	}{
		{"init.lua modified", func() { touch(t, filepath.Join(cfgDir, "init.lua"), time.Hour) }},
		{"lua/** modified", func() { writeFile(t, filepath.Join(cfgDir, "lua", "plugins", "colors.lua"), "return {1}") }},
		{"lua/** added", func() { writeFile(t, filepath.Join(cfgDir, "lua", "config", "options.lua"), "") }},
		{"lazy-lock.json added", func() { writeFile(t, filepath.Join(cfgDir, "lazy-lock.json"), "{}") }},
		{"lazy-lock.json modified", func() { touch(t, filepath.Join(cfgDir, "lazy-lock.json"), 2*time.Hour) }},
		{"colors/** added", func() { writeFile(t, filepath.Join(cfgDir, "colors", "mine.lua"), "") }},
		{"colors/** removed", func() { os.Remove(filepath.Join(cfgDir, "colors", "mine.lua")) }},
		{"nvim binary upgraded", func() { writeFile(t, bin, "#!/bin/sh\n# v2\n") }},
		{"NVIM_APPNAME", func() { t.Setenv("NVIM_APPNAME", "other") }},
	}
	prev := base
	for _, s := range steps {
		s.change()
		k := key()
		if k == prev {
			t.Errorf("%s: key unchanged", s.name)
		}
		prev = k
	}

	if _, err := NvimCacheKey(filepath.Join(dir, "missing"), "", cfgDir); err == nil {
		t.Error("expected an error for a missing binary")
	}
	if _, err := NvimCacheKey(bin, "", filepath.Join(dir, "no-config")); err != nil {
		t.Errorf("a missing config dir should be fine: %v", err)
	}
}

// fakeNvim writes a shell script standing in for nvim. It appends a line to
// the returned counter file on every run and then runs body.
func fakeNvim(t *testing.T, body string) (bin, counter string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in for nvim")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "nvim")
	counter = filepath.Join(dir, "runs")
	writeFile(t, bin, "#!/bin/sh\necho run >> '"+counter+"'\n"+body+"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, counter
}

func runs(t *testing.T, counter string) int {
	data, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run")
}

const fakeExport = `printf 'noise<<<BEANS-THEME-BEGIN>>>{"colors_name":"fake","background":"dark","highlights":{"Normal":{"fg":"#c5c9c7","bg":"#22262d"}},"terminal_colors":{"1":"#ff0000"}}<<<BEANS-THEME-END>>>'`

func TestExportNvimCaching(t *testing.T) {
	bin, counter := fakeNvim(t, fakeExport)
	cfgDir := t.TempDir()
	writeFile(t, filepath.Join(cfgDir, "init.lua"), "")
	opts := NvimOptions{Binary: bin, CacheDir: filepath.Join(t.TempDir(), "themes"), ConfigDir: cfgDir}

	load := func() *Theme {
		t.Helper()
		th, err := LoadNvim(context.Background(), "", opts)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	if th := load(); th.Name != "fake" || th.Fg != "#c5c9c7" || th.ANSI[1] != "#ff0000" {
		t.Errorf("unexpected theme %+v", th)
	}
	load()
	if n := runs(t, counter); n != 1 {
		t.Errorf("nvim ran %d times, want 1 (second load from cache)", n)
	}

	touch(t, filepath.Join(cfgDir, "init.lua"), time.Hour)
	load()
	if n := runs(t, counter); n != 2 {
		t.Errorf("nvim ran %d times after a config change, want 2", n)
	}

	if _, err := LoadNvim(context.Background(), "other", opts); err != nil {
		t.Fatal(err)
	}
	if n := runs(t, counter); n != 3 {
		t.Errorf("nvim ran %d times for another colorscheme, want 3", n)
	}

	// A corrupt cache entry is ignored and replaced.
	entries, _ := filepath.Glob(filepath.Join(opts.CacheDir, "nvim-*.json"))
	if len(entries) != 3 {
		t.Fatalf("cache has %d entries, want 3", len(entries))
	}
	for _, e := range entries {
		writeFile(t, e, "{corrupt")
	}
	load()
	if n := runs(t, counter); n != 4 {
		t.Errorf("nvim ran %d times after cache corruption, want 4", n)
	}
	load()
	if n := runs(t, counter); n != 4 {
		t.Errorf("nvim ran %d times, want the repaired cache entry to be used", n)
	}
}

func TestExportNvimErrors(t *testing.T) {
	t.Run("unknown colorscheme is not cached", func(t *testing.T) {
		bin, counter := fakeNvim(t, `printf '<<<BEANS-THEME-BEGIN>>>{"error":"Vim:E185: Cannot find color scheme"}<<<BEANS-THEME-END>>>'`)
		opts := NvimOptions{Binary: bin, CacheDir: t.TempDir(), ConfigDir: t.TempDir()}
		for i := 0; i < 2; i++ {
			_, err := LoadNvim(context.Background(), "nope", opts)
			if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "E185") {
				t.Fatalf("err = %v, want unknown colorscheme error", err)
			}
		}
		if n := runs(t, counter); n != 2 {
			t.Errorf("nvim ran %d times, want errors not to be cached", n)
		}
	})
	t.Run("no export", func(t *testing.T) {
		bin, _ := fakeNvim(t, "echo hello")
		_, err := LoadNvim(context.Background(), "", NvimOptions{Binary: bin, ConfigDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "no theme export") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("nvim fails", func(t *testing.T) {
		bin, _ := fakeNvim(t, "exit 3")
		_, err := LoadNvim(context.Background(), "", NvimOptions{Binary: bin, ConfigDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "exit status 3") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("timeout, also with a child holding stdout", func(t *testing.T) {
		bin, _ := fakeNvim(t, "sleep 30 &\nsleep 30")
		start := time.Now()
		_, err := LoadNvim(context.Background(), "", NvimOptions{Binary: bin, ConfigDir: t.TempDir(), Timeout: 200 * time.Millisecond})
		if err == nil || !strings.Contains(err.Error(), "within") {
			t.Errorf("err = %v, want timeout", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("timeout took %s", d)
		}
	})
	t.Run("stdin is not inherited", func(t *testing.T) {
		// A config waiting for input must not block: stdin is /dev/null.
		bin, _ := fakeNvim(t, "read line\n"+fakeExport)
		if _, err := LoadNvim(context.Background(), "", NvimOptions{Binary: bin, ConfigDir: t.TempDir(), Timeout: 2 * time.Second}); err != nil {
			t.Error(err)
		}
	})
	t.Run("missing binary", func(t *testing.T) {
		if _, err := LoadNvim(context.Background(), "", NvimOptions{Binary: filepath.Join(t.TempDir(), "nvim")}); err == nil {
			t.Error("expected an error")
		}
		t.Setenv("PATH", t.TempDir())
		if _, err := LoadNvim(context.Background(), "", NvimOptions{}); err != ErrNvimNotFound {
			t.Errorf("err = %v, want ErrNvimNotFound", err)
		}
	})
}

func TestNvimConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	t.Setenv("NVIM_APPNAME", "")
	if got := NvimConfigDir(); got != filepath.Join("/x", "nvim") {
		t.Errorf("NvimConfigDir() = %q", got)
	}
	t.Setenv("NVIM_APPNAME", "lazy")
	if got := NvimConfigDir(); got != filepath.Join("/x", "lazy") {
		t.Errorf("NvimConfigDir() with NVIM_APPNAME = %q", got)
	}
}

// isolatedNvim points Neovim at a temporary config with two colorschemes
// (the default one selected by init.lua, and a second one without terminal
// colors), so the test does not depend on the user's config.
func isolatedNvim(t *testing.T) (cfgDir string) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	root := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(root, strings.ToLower(v)))
	}
	t.Setenv("NVIM_APPNAME", "")
	cfgDir = filepath.Join(root, "xdg_config_home", "nvim")
	writeFile(t, filepath.Join(cfgDir, "init.lua"), `vim.cmd.colorscheme("beanstest")`+"\n")
	writeFile(t, filepath.Join(cfgDir, "colors", "beanstest.lua"), `
vim.cmd("highlight clear")
vim.g.colors_name = "beanstest"
vim.o.background = "dark"
local hl = function(name, val) vim.api.nvim_set_hl(0, name, val) end
hl("Normal", { fg = "#c5c9c7", bg = "#22262d" })
hl("Comment", { fg = "#75797f" })
hl("Function", { fg = "#8ba4b0" })
hl("Visual", { bg = "#393b44" })
hl("FloatBorder", { fg = "#393b44" })
hl("DiagnosticError", { fg = "#c34043" })
hl("MyLink", { fg = "#dca561" })
hl("DiagnosticWarn", { link = "MyLink" })
for i = 0, 15 do vim.g["terminal_color_" .. i] = string.format("#%02x0000", i * 16) end
`)
	writeFile(t, filepath.Join(cfgDir, "colors", "beansplain.lua"), `
vim.cmd("highlight clear")
vim.g.colors_name = "beansplain"
vim.o.background = "light"
vim.api.nvim_set_hl(0, "Normal", { fg = "#111111", bg = "#eeeeee" })
vim.api.nvim_set_hl(0, "String", { fg = "#228822" })
`)
	return cfgDir
}

// TestLoadNvimHeadless runs the real nvim against an isolated config.
func TestLoadNvimHeadless(t *testing.T) {
	cfgDir := isolatedNvim(t)
	opts := NvimOptions{CacheDir: filepath.Join(t.TempDir(), "cache"), Timeout: 10 * time.Second}

	th, err := LoadNvim(context.Background(), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "beanstest" || th.Fg != "#c5c9c7" || th.Bg != "#22262d" || th.Muted != "#75797f" ||
		th.Accent != "#8ba4b0" || th.Selection != "#393b44" || th.Border != "#393b44" ||
		th.Danger != "#c34043" || th.Warning != "#dca561" {
		t.Errorf("default colorscheme mapped to %+v", th)
	}
	if th.ANSI[1] != "#100000" || th.ANSI[15] != "#f00000" {
		t.Errorf("terminal colors = %v", th.ANSI)
	}

	// A specific colorscheme: terminal colors of the default one must not
	// leak into it.
	plain, err := LoadNvim(context.Background(), "beansplain", opts)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Name != "beansplain" || plain.Background != "light" || plain.Fg != "#111111" ||
		plain.ANSI[2] != "#228822" || plain.ANSI[7] != "#111111" || plain.ANSI[1] == "#100000" {
		t.Errorf("beansplain mapped to %+v", plain)
	}

	if _, err := LoadNvim(context.Background(), "does-not-exist", opts); err == nil ||
		!strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("err = %v, want unknown colorscheme error", err)
	}

	// colors/** is part of the cache key, so editing the colorscheme
	// refreshes the cached export.
	writeFile(t, filepath.Join(cfgDir, "colors", "beanstest.lua"),
		`vim.g.colors_name = "beanstest"`+"\n"+`vim.api.nvim_set_hl(0, "Normal", { fg = "#000001", bg = "#ffffff" })`+"\n")
	touch(t, filepath.Join(cfgDir, "colors", "beanstest.lua"), time.Hour)
	th, err = LoadNvim(context.Background(), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if th.Fg != "#000001" {
		t.Errorf("changed colorscheme not picked up: fg = %q", th.Fg)
	}
}
