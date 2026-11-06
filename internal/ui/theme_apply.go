package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/ui/theme"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// Theme is a color theme (see package theme). A nil *Theme is the terminal
// theme: the terminal's own 16-color ANSI palette.
type Theme = theme.Theme

var (
	// currentTheme is the applied theme (nil: terminal theme).
	currentTheme *Theme
	// paintBackground reports whether PaintBackground paints ColorBg.
	paintBackground bool
	// ansiPalette holds the colors of the 16 palette slots.
	ansiPalette = terminalPalette()

	themeHooksMu sync.Mutex
	themeHooks   []func()
)

func terminalPalette() (p [theme.ANSISlots]lipgloss.Color) {
	for i := range p {
		p[i] = lipgloss.Color(strconv.Itoa(i))
	}
	return p
}

// ansiSlot returns the color of palette slot n (0-15) in the current theme.
func ansiSlot(n int) lipgloss.Color {
	return ansiPalette[n]
}

// CurrentTheme returns the applied theme, or nil for the terminal theme.
func CurrentTheme() *Theme {
	return currentTheme
}

// OnThemeApplied registers fn to run after every ApplyTheme, so packages can
// rebuild styles they derive from the palette. Hooks run in registration
// order on the goroutine calling ApplyTheme (before the TUI starts).
func OnThemeApplied(fn func()) {
	themeHooksMu.Lock()
	defer themeHooksMu.Unlock()
	themeHooks = append(themeHooks, fn)
}

// ApplyTheme sets the color palette (all Color* variables, NamedColors and
// the palette slots used by ResolveColor) from t, rebuilds every shared
// style and the markdown settings, and runs the OnThemeApplied hooks.
// A nil theme restores the terminal theme. It is ApplyThemeSettings without
// role overrides.
//
// background selects whether the theme's background is painted (ColorBg and
// ColorFg are set, see PaintBackground). It is ignored for the terminal
// theme. Without a painted background, text keeps the terminal's default
// foreground so it stays legible on the terminal's background.
//
// ApplyTheme is not safe for concurrent use with rendering: call it before
// starting the UI or, in the TUI, from the Bubbletea Update loop.
func ApplyTheme(t *Theme, background bool) {
	ApplyThemeSettings(ThemeSettings{Theme: t, Background: background})
}

// ThemeSettings is a theme together with how it is applied.
type ThemeSettings struct {
	// Theme is the theme (nil: terminal theme).
	Theme *Theme
	// Background paints the background (see ApplyTheme). With the terminal
	// theme it paints the "bg" override, if any.
	Background bool
	// Overrides maps theme roles (config.ThemeRoles) to colors, applied on
	// top of Theme (see ValidateThemeOverrides). Invalid entries are ignored.
	Overrides map[string]string
}

// Equal reports whether s and o apply the same colors.
func (s ThemeSettings) Equal(o ThemeSettings) bool {
	if s.Background != o.Background || len(s.Overrides) != len(o.Overrides) {
		return false
	}
	for k, v := range s.Overrides {
		if ov, ok := o.Overrides[k]; !ok || ov != v {
			return false
		}
	}
	if s.Theme == nil || o.Theme == nil {
		return s.Theme == o.Theme
	}
	return *s.Theme == *o.Theme
}

// ApplyThemeSettings applies a theme and its role overrides (see
// ApplyTheme). Overrides win over every theme color, including the border
// contrast guard of nvim and file themes. Override colors are resolved
// against the theme's palette: an ANSI index or color name means the
// theme's palette slot.
//
// Like ApplyTheme it must not run concurrently with rendering.
func ApplyThemeSettings(s ThemeSettings) {
	t, background := s.Theme, s.Background
	currentTheme = t

	if t == nil {
		ColorPrimary = lipgloss.Color("5")
		ColorSecondary = lipgloss.Color("8")
		ColorSuccess = lipgloss.Color("2")
		ColorWarning = lipgloss.Color("3")
		ColorDanger = lipgloss.Color("1")
		ColorMuted = lipgloss.Color("8")
		ColorSubtle = lipgloss.Color("8")
		ColorBlue = lipgloss.Color("4")
		ColorCyan = lipgloss.Color("6")
		ColorInfo = lipgloss.Color("4")
		ColorFg = ""
		ColorBg = ""
		ColorBorder = lipgloss.Color("8")
		ColorSelection = ""
		ansiPalette = terminalPalette()
	} else {
		for i := range ansiPalette {
			ansiPalette[i] = colorOr(t.ANSI[i], lipgloss.Color(strconv.Itoa(i)))
		}
		ColorPrimary = colorOr(t.Accent, ansiPalette[5])
		ColorMuted = colorOr(t.Muted, ansiPalette[8])
		ColorSecondary = ColorMuted
		ColorBorder = colorOr(t.Border, ColorMuted)
		ColorSubtle = ColorBorder
		ColorSuccess = colorOr(t.Success, ansiPalette[2])
		ColorWarning = colorOr(t.Warning, ansiPalette[3])
		ColorDanger = colorOr(t.Danger, ansiPalette[1])
		ColorBlue = ansiPalette[4]
		ColorCyan = ansiPalette[6]
		ColorInfo = colorOr(t.Info, ansiPalette[4])
		ColorSelection = lipgloss.Color(t.Selection)
		ColorFg = lipgloss.Color(t.Fg)
		ColorBg = lipgloss.Color(t.Bg)
	}
	updateNamedColors()

	// Resolve all overrides against the base palette before assigning any,
	// so e.g. "border=gray" means the theme's gray, not an overridden muted.
	o := make(map[string]lipgloss.Color, len(s.Overrides))
	for role, c := range s.Overrides {
		if col, ok := resolveOverrideColor(c); ok {
			o[role] = col
		}
	}
	set := func(role string, dst ...*lipgloss.Color) {
		if c, ok := o[role]; ok {
			for _, d := range dst {
				*d = c
			}
		}
	}
	set("accent", &ColorPrimary)
	set("muted", &ColorMuted, &ColorSecondary)
	set("border", &ColorBorder, &ColorSubtle)
	set("success", &ColorSuccess)
	set("warning", &ColorWarning)
	set("danger", &ColorDanger)
	set("info", &ColorInfo)
	set("selection", &ColorSelection)
	set("fg", &ColorFg)
	set("bg", &ColorBg)
	ColorFocusBorder = ColorPrimary
	if t != nil && t.FocusBorder != "" {
		ColorFocusBorder = lipgloss.Color(t.FocusBorder)
	}
	set("focus_border", &ColorFocusBorder)

	// The background is only painted with a theme (or a bg override) and
	// when the caller allows it. Without it, text keeps the terminal's
	// default foreground unless fg is overridden explicitly.
	paintBackground = background && ColorBg != "" && (t != nil || o["bg"] != "")
	if !paintBackground {
		ColorBg = ""
		if _, ok := o["fg"]; !ok {
			ColorFg = ""
		}
	}

	updateNamedColors()
	buildStyles()
	MarkdownChromaFormatter = MarkdownChromaFormatterFor(lipgloss.ColorProfile())

	themeHooksMu.Lock()
	hooks := append([]func(){}, themeHooks...)
	themeHooksMu.Unlock()
	for _, fn := range hooks {
		fn()
	}
}

// Colors of theme roles that have no counterpart among the shared colors
// above.
var (
	// ColorFocusBorder is the border of the focused pane (default:
	// ColorPrimary).
	ColorFocusBorder = lipgloss.Color("5")
	// ColorInfo is the informational status color.
	ColorInfo = lipgloss.Color("4")
)

// overrideColorNames are the color names accepted in role overrides (the
// keys of NamedColors). It is fixed at init, so validating overrides is
// safe while ApplyTheme updates NamedColors.
var overrideColorNames = func() map[string]bool {
	names := make(map[string]bool, len(NamedColors))
	for name := range NamedColors {
		names[name] = true
	}
	return names
}()

// ValidateThemeOverrides checks role overrides (as returned by
// config.Config.GetThemeOverrides): every color must be "#rgb"/"#rrggbb",
// an ANSI palette index "0".."15" or a color name (see NamedColors). It
// does not depend on the applied theme and is safe to call concurrently
// with ApplyTheme.
func ValidateThemeOverrides(overrides map[string]string) error {
	roles := make([]string, 0, len(overrides))
	for role := range overrides {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		if !config.IsThemeRole(role) {
			return fmt.Errorf("unknown theme role %q", role)
		}
		if !validOverrideColor(overrides[role]) {
			return fmt.Errorf("theme override %s: invalid color %q (use #rrggbb, 0-15 or a color name)", role, overrides[role])
		}
	}
	return nil
}

func validOverrideColor(c string) bool {
	c = strings.TrimSpace(c)
	if strings.HasPrefix(c, "#") {
		_, err := theme.NormalizeHex(c)
		return err == nil
	}
	return isANSIIndex(c) || overrideColorNames[strings.ToLower(c)]
}

// resolveOverrideColor resolves an override color against the current
// palette (call after updateNamedColors).
func resolveOverrideColor(c string) (lipgloss.Color, bool) {
	c = strings.TrimSpace(c)
	if !validOverrideColor(c) {
		return "", false
	}
	if strings.HasPrefix(c, "#") {
		hex, _ := theme.NormalizeHex(c)
		return lipgloss.Color(hex), true
	}
	if isANSIIndex(c) {
		n, _ := strconv.Atoi(c)
		return ansiSlot(n), true
	}
	return NamedColors[strings.ToLower(c)], true
}

func colorOr(c string, fallback lipgloss.Color) lipgloss.Color {
	if c == "" {
		return fallback
	}
	return lipgloss.Color(c)
}

// updateNamedColors points NamedColors at the current palette. With the
// terminal theme this yields exactly the ANSI indices of the original map.
func updateNamedColors() {
	NamedColors["default"] = ColorDefault
	NamedColors["black"] = ansiSlot(0)
	NamedColors["red"] = ansiSlot(1)
	NamedColors["green"] = ansiSlot(2)
	NamedColors["yellow"] = ansiSlot(3)
	NamedColors["blue"] = ansiSlot(4)
	NamedColors["purple"] = ansiSlot(5)
	NamedColors["magenta"] = ansiSlot(5)
	NamedColors["cyan"] = ansiSlot(6)
	NamedColors["white"] = ansiSlot(7)
	NamedColors["gray"] = ColorSecondary
	NamedColors["grey"] = ColorSecondary
	NamedColors["bright-black"] = ansiSlot(8)
	NamedColors["bright-red"] = ansiSlot(9)
	NamedColors["bright-green"] = ansiSlot(10)
	NamedColors["bright-yellow"] = ansiSlot(11)
	NamedColors["bright-blue"] = ansiSlot(12)
	NamedColors["bright-magenta"] = ansiSlot(13)
	NamedColors["bright-purple"] = ansiSlot(13)
	NamedColors["bright-cyan"] = ansiSlot(14)
	NamedColors["bright-white"] = ansiSlot(15)
}

// LoadTheme loads the theme configured by cfg ($BEANS_THEME, then
// tui.theme) and reports whether its background should be painted. The
// terminal theme is returned as nil. Neovim exports are cached in the user
// cache dir.
func LoadTheme(ctx context.Context, cfg *config.Config) (t *Theme, background bool, err error) {
	spec, err := cfg.GetTheme()
	if err != nil {
		return nil, false, err
	}
	background, err = cfg.GetThemeBackground(spec)
	if err != nil {
		return nil, false, err
	}
	t, err = theme.Load(ctx, spec, theme.NvimOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("theme %s: %w", spec, err)
	}
	return t, background, nil
}

// LoadThemeSettings loads the theme and role overrides configured by cfg
// (see LoadTheme and config.Config.GetThemeOverrides). It runs no UI code,
// so it can run in the background while the UI renders. On error it still
// returns usable settings: the terminal theme when the theme fails to load,
// no overrides when they are invalid.
func LoadThemeSettings(ctx context.Context, cfg *config.Config) (ThemeSettings, error) {
	var errs []error
	overrides, err := cfg.GetThemeOverrides()
	if err == nil {
		err = ValidateThemeOverrides(overrides)
	}
	if err != nil {
		errs = append(errs, err)
		overrides = nil
	}
	if len(overrides) == 0 {
		overrides = nil
	}
	t, bg, err := LoadTheme(ctx, cfg)
	if err != nil {
		errs = append(errs, err)
		t = nil
		bg, _ = cfg.GetThemeBackground(config.ThemeSpec{Kind: config.ThemeTerminal})
	}
	return ThemeSettings{Theme: t, Background: bg, Overrides: overrides}, errors.Join(errs...)
}

// ApplyConfiguredTheme loads and applies the theme and overrides configured
// by cfg. background enables painting the theme's background when the
// configuration allows it (the TUI passes true; line-oriented CLI output
// passes false). Without color output (NO_COLOR, non-TTY: lipgloss's Ascii
// profile) nothing is loaded. On error the terminal theme (or the theme
// without overrides) is applied and the error is returned for the caller to
// report as a warning.
func ApplyConfiguredTheme(cfg *config.Config, background bool) error {
	_, err := ApplyConfiguredThemeSettings(cfg, background)
	return err
}

// ApplyConfiguredThemeSettings is ApplyConfiguredTheme returning the
// settings it applied.
func ApplyConfiguredThemeSettings(cfg *config.Config, background bool) (ThemeSettings, error) {
	if lipgloss.ColorProfile() == termenv.Ascii {
		ApplyTheme(nil, false)
		return ThemeSettings{}, nil
	}
	s, err := LoadThemeSettings(context.Background(), cfg)
	s.Background = background && s.Background
	ApplyThemeSettings(s)
	return s, err
}

// WarnThemeError prints a theme loading error to stderr.
func WarnThemeError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "beans: warning: %v (using terminal colors)\n", err)
	}
}

// PaintBackground paints the theme's background and foreground (ColorBg,
// ColorFg) behind a rendered view of the given size: every line is padded to
// width, missing lines up to height are added, and the colors are restored
// after every SGR reset inside the view. Without a painted background (or
// colors) the view is returned unchanged.
func PaintBackground(view string, width, height int) string {
	if !paintBackground || ColorBg == "" {
		return view
	}
	profile := lipgloss.ColorProfile()
	if profile == termenv.Ascii {
		return view
	}
	params := []string{}
	if seq := profile.Color(string(ColorBg)).Sequence(true); seq != "" {
		params = append(params, seq)
	}
	if ColorFg != "" {
		if seq := profile.Color(string(ColorFg)).Sequence(false); seq != "" {
			params = append(params, seq)
		}
	}
	if len(params) == 0 {
		return view
	}
	set := "\x1b[" + strings.Join(params, ";") + "m"
	const reset = "\x1b[0m"
	restore := strings.NewReplacer(
		"\x1b[0m", reset+set,
		"\x1b[m", reset+set,
	)

	lines := strings.Split(view, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(set)
		b.WriteString(restore.Replace(line))
		if pad := width - lipgloss.Width(line); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(reset)
	}
	return b.String()
}
