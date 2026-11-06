package theme

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ErrNvimNotFound is returned when the nvim executable is not in $PATH.
var ErrNvimNotFound = errors.New("nvim is not installed (not found in $PATH)")

// DefaultNvimTimeout bounds a Neovim export, so a broken config cannot hang
// the TUI's startup.
const DefaultNvimTimeout = 3 * time.Second

// nvimCacheVersion is part of every cache key; bump it when the export
// script or the NvimExport format changes.
const nvimCacheVersion = "2"

// NvimHighlightGroups are the highlight groups exported from Neovim.
var NvimHighlightGroups = []string{
	"Normal", "NormalFloat", "Comment", "Function", "Statement", "String", "Type",
	"Constant", "Identifier", "Special", "Title", "Visual", "CursorLine", "Search",
	"FloatBorder", "WinSeparator", "LineNr", "NonText", "DiagnosticError", "DiagnosticWarn",
	"DiagnosticOk", "DiagnosticInfo", "DiagnosticHint", "DiffAdd", "DiffDelete",
}

// NvimHighlight is a resolved highlight group (links followed).
type NvimHighlight struct {
	Fg      string `json:"fg,omitempty"`
	Bg      string `json:"bg,omitempty"`
	Reverse bool   `json:"reverse,omitempty"`
}

// NvimExport is the raw data exported from a Neovim colorscheme.
type NvimExport struct {
	ColorsName string                   `json:"colors_name,omitempty"`
	Background string                   `json:"background,omitempty"`
	Highlights map[string]NvimHighlight `json:"highlights,omitempty"`
	// TerminalColors maps "0".."15" to g:terminal_color_N.
	TerminalColors map[string]string `json:"terminal_colors,omitempty"`
	// Error is set when the requested colorscheme could not be applied.
	Error string `json:"error,omitempty"`
}

// Output markers around the JSON, so stray output from plugins on stdout
// cannot corrupt the export.
const (
	nvimBeginMarker = "<<<BEANS-THEME-BEGIN>>>"
	nvimEndMarker   = "<<<BEANS-THEME-END>>>"
	nvimSchemeEnv   = "BEANS_NVIM_COLORSCHEME"
)

// nvimScript collects the export. It is passed as a single `:lua` command,
// so it must not contain line comments. The colorscheme name comes from
// the environment to avoid any quoting issues.
var nvimScript = strings.Join([]string{
	`local out = {highlights = {}, terminal_colors = {}}`,
	`local cs = vim.env.` + nvimSchemeEnv,
	`if cs and cs ~= "" then`,
	`  for i = 0, 15 do vim.g["terminal_color_" .. i] = nil end`,
	`  local ok, err = pcall(vim.cmd.colorscheme, cs)`,
	`  if not ok then out.error = tostring(err) end`,
	`end`,
	`out.colors_name = vim.g.colors_name`,
	`out.background = vim.o.background`,
	`local function hex(v) if type(v) == "number" then return string.format("#%06x", v) end return nil end`,
	`for _, g in ipairs({"` + strings.Join(NvimHighlightGroups, `","`) + `"}) do`,
	`  local ok, hl = pcall(vim.api.nvim_get_hl, 0, {name = g, link = false})`,
	`  if ok and type(hl) == "table" and (hl.fg or hl.bg or hl.reverse) then out.highlights[g] = {fg = hex(hl.fg), bg = hex(hl.bg), reverse = hl.reverse or nil} end`,
	`end`,
	`for i = 0, 15 do`,
	`  local c = vim.g["terminal_color_" .. i]`,
	`  if type(c) == "string" then out.terminal_colors[tostring(i)] = c end`,
	`end`,
	`if next(out.highlights) == nil then out.highlights = vim.empty_dict() end`,
	`if next(out.terminal_colors) == nil then out.terminal_colors = vim.empty_dict() end`,
	`io.stdout:write("` + nvimBeginMarker + `" .. vim.json.encode(out) .. "` + nvimEndMarker + `")`,
	`io.stdout:flush()`,
}, " ")

// NvimOptions configures a Neovim theme export.
type NvimOptions struct {
	// Binary is the nvim executable (default: "nvim" from $PATH).
	Binary string
	// Timeout bounds the export (default: DefaultNvimTimeout).
	Timeout time.Duration
	// CacheDir caches exports ("" disables caching).
	CacheDir string
	// ConfigDir is Neovim's config directory, used for cache invalidation
	// (default: NvimConfigDir()).
	ConfigDir string
}

// NvimConfigDir returns the directory Neovim reads the user's config from
// (stdpath("config")), honoring $XDG_CONFIG_HOME and $NVIM_APPNAME.
func NvimConfigDir() string {
	app := os.Getenv("NVIM_APPNAME")
	if app == "" {
		app = "nvim"
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, app)
	}
	if runtime.GOOS == "windows" {
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			return filepath.Join(l, app)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", app)
}

// NvimAvailable reports whether nvim is installed.
func NvimAvailable() bool {
	_, err := exec.LookPath("nvim")
	return err == nil
}

func (o NvimOptions) withDefaults() NvimOptions {
	if o.Binary == "" {
		o.Binary = "nvim"
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultNvimTimeout
	}
	if o.ConfigDir == "" {
		o.ConfigDir = NvimConfigDir()
	}
	return o
}

// LoadNvim exports the colors of a Neovim colorscheme using the user's
// normal Neovim config (so plugin colorschemes load) and maps them to a
// Theme. An empty colorscheme exports the one Neovim loads by default.
// Results are cached in opts.CacheDir (see NvimCacheKey).
func LoadNvim(ctx context.Context, colorscheme string, opts NvimOptions) (*Theme, error) {
	exp, err := ExportNvim(ctx, colorscheme, opts)
	if err != nil {
		return nil, err
	}
	return FromNvim(exp, colorscheme)
}

// ExportNvim returns the raw export of a Neovim colorscheme, from the cache
// when it is still valid.
func ExportNvim(ctx context.Context, colorscheme string, opts NvimOptions) (*NvimExport, error) {
	opts = opts.withDefaults()
	bin, err := exec.LookPath(opts.Binary)
	if err != nil {
		if opts.Binary == "nvim" {
			return nil, ErrNvimNotFound
		}
		return nil, fmt.Errorf("nvim executable %q not found: %w", opts.Binary, err)
	}

	var cachePath string
	if opts.CacheDir != "" {
		if key, err := NvimCacheKey(bin, colorscheme, opts.ConfigDir); err == nil {
			cachePath = filepath.Join(opts.CacheDir, "nvim-"+key+".json")
			if exp, ok := readNvimCache(cachePath); ok {
				return exp, nil
			}
		}
	}

	exp, err := runNvimExport(ctx, bin, colorscheme, opts.Timeout)
	if err != nil {
		return nil, err
	}
	if cachePath != "" {
		// Caching is best effort: a read-only cache dir only costs speed.
		_ = writeNvimCache(cachePath, exp)
	}
	return exp, nil
}

func runNvimExport(ctx context.Context, bin, colorscheme string, timeout time.Duration) (*NvimExport, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// -n: no swap file, -i NONE: don't read or write the shada file, so the
	// export leaves no trace in the user's Neovim state.
	cmd := exec.CommandContext(ctx, bin, "--headless", "-n", "-i", "NONE",
		"-c", "lua "+nvimScript, "-c", "qa!")
	cmd.Env = append(os.Environ(), nvimSchemeEnv+"="+colorscheme)
	cmd.Stdin = nil  // /dev/null: never wait for input
	cmd.Stderr = nil // /dev/null: config warnings are irrelevant here
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Plugins may spawn children that inherit stdout; don't wait for them
	// after nvim itself exits or is killed.
	cmd.WaitDelay = 500 * time.Millisecond
	isolateProcessGroup(cmd)

	runErr := cmd.Run()
	out := stdout.String()
	begin := strings.Index(out, nvimBeginMarker)
	end := strings.LastIndex(out, nvimEndMarker)
	if begin < 0 || end < begin {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("nvim did not finish exporting the colorscheme within %s", timeout)
		}
		if runErr != nil {
			return nil, fmt.Errorf("running nvim: %w", runErr)
		}
		return nil, errors.New("nvim produced no theme export")
	}

	var exp NvimExport
	if err := json.Unmarshal([]byte(out[begin+len(nvimBeginMarker):end]), &exp); err != nil {
		return nil, fmt.Errorf("parsing nvim theme export: %w", err)
	}
	if exp.Error != "" {
		return nil, fmt.Errorf("nvim colorscheme %q: %s", colorscheme, exp.Error)
	}
	return &exp, nil
}

// NvimCacheKey identifies an export: it changes when the colorscheme
// argument, the nvim binary (path, size and modification time, which change
// with every version), $NVIM_APPNAME, or any file of the Neovim config that
// can select or define a colorscheme changes (init.lua/init.vim,
// lazy-lock.json, lazyvim.json, lua/, colors/, plugin/, after/).
func NvimCacheKey(bin, colorscheme, configDir string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v%s\x00%s\x00%s\x00%s\x00", nvimCacheVersion, colorscheme, os.Getenv("NVIM_APPNAME"), configDir)

	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	info, err := os.Stat(bin)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(h, "bin\x00%s\x00%d\x00%d\x00", bin, info.Size(), info.ModTime().UnixNano())

	if configDir != "" {
		if err := fingerprintNvimConfig(h, configDir); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

// nvimConfigFiles and nvimConfigDirs are the parts of a Neovim config that
// can influence colors.
var (
	nvimConfigFiles = []string{"init.lua", "init.vim", "lazy-lock.json", "lazyvim.json"}
	nvimConfigDirs  = []string{"lua", "colors", "plugin", "after"}
)

// NvimThemeSources returns the parts of the Neovim config in configDir that
// are part of the cache key (see NvimCacheKey): the top-level files and the
// directories whose contents are fingerprinted recursively. Watching them
// detects every change that can invalidate a cached export.
func NvimThemeSources(configDir string) (files, dirs []string) {
	for _, name := range nvimConfigFiles {
		files = append(files, filepath.Join(configDir, name))
	}
	for _, name := range nvimConfigDirs {
		dirs = append(dirs, filepath.Join(configDir, name))
	}
	return files, dirs
}

type hashWriter interface{ Write([]byte) (int, error) }

func fingerprintNvimConfig(h hashWriter, dir string) error {
	var entries []string
	add := func(path string, info fs.FileInfo) {
		rel, _ := filepath.Rel(dir, path)
		entries = append(entries, fmt.Sprintf("%s\x00%d\x00%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
	}
	for _, name := range nvimConfigFiles {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			add(p, info)
		}
	}
	for _, name := range nvimConfigDirs {
		root := filepath.Join(dir, name)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			add(path, info)
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(entries)
	for _, e := range entries {
		fmt.Fprintf(h, "%s\n", e)
	}
	return nil
}

func readNvimCache(path string) (*NvimExport, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var exp NvimExport
	if err := json.Unmarshal(data, &exp); err != nil || exp.Error != "" {
		return nil, false
	}
	return &exp, true
}

func writeNvimCache(path string, exp *NvimExport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(exp)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nvim-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// FromNvim maps a Neovim export to theme roles:
//
//	Fg        Normal.fg
//	Bg        Normal.bg
//	Muted     Comment.fg, LineNr.fg
//	Border    FloatBorder.fg, WinSeparator.fg, LineNr.fg, NonText.fg,
//	          Comment.fg: the first whose contrast against Bg is at least
//	          MinBorderContrast and differs from Fg (else Fg, else the one
//	          with the most contrast)
//	Accent    Function.fg, Title.fg, Statement.fg
//	Selection Visual.bg, CursorLine.bg
//	Success   DiagnosticOk.fg, String.fg
//	Warning   DiagnosticWarn.fg
//	Danger    DiagnosticError.fg, DiffDelete.fg
//	Info      DiagnosticInfo.fg
//	Hint      DiagnosticHint.fg
//	ANSI      g:terminal_color_0..15; missing slots are derived from the
//	          highlight groups (see deriveNvimANSI)
//
// colorscheme is the requested name, used as theme name when Neovim reports
// none.
func FromNvim(exp *NvimExport, colorscheme string) (*Theme, error) {
	hl := func(group string) NvimHighlight {
		h := exp.Highlights[group]
		h.Fg, _ = NormalizeHex(h.Fg)
		h.Bg, _ = NormalizeHex(h.Bg)
		return h
	}
	fg := func(group string) string { return hl(group).Fg }
	bg := func(group string) string { return hl(group).Bg }

	t := &Theme{
		Name:       exp.ColorsName,
		Background: exp.Background,
		Fg:         fg("Normal"),
		Bg:         bg("Normal"),
	}
	if t.Name == "" {
		t.Name = colorscheme
	}
	if t.Background != "light" {
		t.Background = "dark"
	}
	// first returns the first candidate that is set and visible on Bg.
	first := func(cands ...string) string {
		for _, c := range cands {
			if c != "" && c != t.Bg {
				return c
			}
		}
		return ""
	}
	t.Muted = first(fg("Comment"), fg("LineNr"))
	t.Border = pickBorder(t.Bg, t.Fg, first(fg("FloatBorder")), first(fg("WinSeparator")),
		first(fg("LineNr")), first(fg("NonText")), first(fg("Comment")), t.Muted)
	t.Accent = first(fg("Function"), fg("Title"), fg("Statement"))
	t.Selection = first(bg("Visual"), bg("CursorLine"))
	t.Success = first(fg("DiagnosticOk"), fg("String"))
	t.Warning = first(fg("DiagnosticWarn"))
	t.Danger = first(fg("DiagnosticError"), fg("DiffDelete"))
	t.Info = first(fg("DiagnosticInfo"))
	t.Hint = first(fg("DiagnosticHint"))

	for i := range t.ANSI {
		c, err := NormalizeHex(exp.TerminalColors[fmt.Sprint(i)])
		if err == nil {
			t.ANSI[i] = c
		}
	}
	deriveNvimANSI(t, fg, bg)
	t.complete()

	if t.isEmpty() {
		return nil, errors.New("nvim colorscheme defines no colors")
	}
	return t, nil
}

// deriveNvimANSI fills ANSI slots that the colorscheme left unset (no
// g:terminal_color_N) from the highlight groups closest in meaning.
func deriveNvimANSI(t *Theme, fg, bg func(string) string) {
	first := func(cands ...string) string {
		for _, c := range cands {
			if c != "" {
				return c
			}
		}
		return ""
	}
	derived := [ANSISlots]string{
		0: first(bg("CursorLine"), t.Bg),
		1: first(fg("DiagnosticError"), fg("DiffDelete")),
		2: first(fg("String"), fg("DiagnosticOk")),
		3: first(fg("DiagnosticWarn"), fg("Type")),
		4: first(fg("Function"), fg("DiagnosticInfo")),
		5: first(fg("Statement"), fg("Constant")),
		6: first(fg("Special"), fg("DiagnosticHint")),
		7: t.Fg,
		8: first(fg("Comment"), fg("LineNr")),
	}
	for i := 9; i <= 14; i++ {
		derived[i] = derived[i-8]
	}
	derived[15] = t.Fg
	for i := range t.ANSI {
		if t.ANSI[i] == "" {
			t.ANSI[i] = derived[i]
		}
	}
}
