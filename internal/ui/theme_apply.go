package ui

import (
	"context"
	"fmt"
	"os"
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
// A nil theme restores the terminal theme.
//
// background selects whether the theme's background is painted (ColorBg and
// ColorFg are set, see PaintBackground). It is ignored for the terminal
// theme. Without a painted background, text keeps the terminal's default
// foreground so it stays legible on the terminal's background.
//
// ApplyTheme is not safe for concurrent use with rendering; call it before
// starting the UI.
func ApplyTheme(t *Theme, background bool) {
	currentTheme = t
	paintBackground = t != nil && background && t.Bg != ""

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
		ColorSelection = lipgloss.Color(t.Selection)
		ColorFg, ColorBg = "", ""
		if paintBackground {
			ColorFg = lipgloss.Color(t.Fg)
			ColorBg = lipgloss.Color(t.Bg)
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

// ApplyConfiguredTheme loads and applies the theme configured by cfg.
// background enables painting the theme's background when the
// configuration allows it (the TUI passes true; line-oriented CLI output
// passes false). Without color output (NO_COLOR, non-TTY: lipgloss's Ascii
// profile) nothing is loaded. On error the terminal theme is applied and the
// error is returned for the caller to report as a warning.
func ApplyConfiguredTheme(cfg *config.Config, background bool) error {
	if lipgloss.ColorProfile() == termenv.Ascii {
		ApplyTheme(nil, false)
		return nil
	}
	t, bg, err := LoadTheme(context.Background(), cfg)
	if err != nil {
		ApplyTheme(nil, false)
		return err
	}
	ApplyTheme(t, background && bg)
	return nil
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
