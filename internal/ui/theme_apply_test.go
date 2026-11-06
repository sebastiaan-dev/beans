package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

// kansoTheme is the kanso-mist palette as exported from Neovim.
func kansoTheme() *Theme {
	return &Theme{
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

// withTheme applies t for the duration of the test.
func withTheme(t *testing.T, th *Theme, background bool) {
	t.Helper()
	ApplyTheme(th, background)
	t.Cleanup(func() { ApplyTheme(nil, false) })
}

// rgb returns the truecolor SGR parameters of a hex color as lipgloss
// renders them.
func rgb(hex string) string {
	return termenv.TrueColor.Color(hex).Sequence(false)
}

func TestApplyThemeRebuildsStyles(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	before := map[string]string{
		"Muted": Muted.Render("x"), "ID": ID.Render("x"), "Success": Success.Render("x"),
		"TreeLine": TreeLine.Render("x"), "StatusDone": StatusDone.Render("x"),
	}
	withTheme(t, kansoTheme(), true)
	after := map[string]string{
		"Muted": Muted.Render("x"), "ID": ID.Render("x"), "Success": Success.Render("x"),
		"TreeLine": TreeLine.Render("x"), "StatusDone": StatusDone.Render("x"),
	}
	want := map[string]string{
		"Muted": "#75797f", "ID": "#8ba4b0", "Success": "#98bb6c", "TreeLine": "#393b44", "StatusDone": "#75797f",
	}
	for name, hex := range want {
		if before[name] == after[name] {
			t.Errorf("%s not rebuilt: %q", name, after[name])
		}
		if !strings.Contains(after[name], rgb(hex)) {
			t.Errorf("%s = %q, want color %s (%s)", name, after[name], hex, rgb(hex))
		}
	}
	if got := Selected.Render("x"); !strings.Contains(got, termenv.TrueColor.Color("#393b44").Sequence(true)) {
		t.Errorf("Selected = %q, want the theme's selection background", got)
	}

	ApplyTheme(nil, false)
	for name, s := range before {
		got := map[string]string{
			"Muted": Muted.Render("x"), "ID": ID.Render("x"), "Success": Success.Render("x"),
			"TreeLine": TreeLine.Render("x"), "StatusDone": StatusDone.Render("x"),
		}[name]
		if got != s {
			t.Errorf("%s after restoring the terminal theme = %q, want %q", name, got, s)
		}
	}
	if got := Selected.Render("x"); !sgrParams(got)["7"] {
		t.Errorf("terminal Selected = %q, want reverse video", got)
	}
}

// TestApplyTerminalThemeIsIdentity checks that the terminal theme reproduces
// the built-in ANSI styles exactly, also after another theme was applied.
func TestApplyTerminalThemeIsIdentity(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	want := renderSharedStyles(t, termenv.TrueColor)
	namedBefore := map[string]lipgloss.Color{}
	for k, v := range NamedColors {
		namedBefore[k] = v
	}
	ApplyTheme(kansoTheme(), true)
	ApplyTheme(nil, false)
	got := renderSharedStyles(t, termenv.TrueColor)
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s differs after re-applying the terminal theme:\n got %q\nwant %q", name, got[name], w)
		}
	}
	for k, v := range namedBefore {
		if NamedColors[k] != v {
			t.Errorf("NamedColors[%q] = %q, want %q", k, NamedColors[k], v)
		}
	}
	if MarkdownChromaFormatter != "terminal16" || ColorFg != "" || ColorBg != "" || ColorSelection != "" || ColorBorder != "8" {
		t.Errorf("terminal roles: formatter=%q fg=%q bg=%q selection=%q border=%q",
			MarkdownChromaFormatter, ColorFg, ColorBg, ColorSelection, ColorBorder)
	}
}

func TestApplyThemeColorRoles(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	withTheme(t, kansoTheme(), true)
	for name, tc := range map[string]struct {
		got  lipgloss.Color
		want string
	}{
		"Primary": {ColorPrimary, "#8ba4b0"}, "Secondary": {ColorSecondary, "#75797f"},
		"Success": {ColorSuccess, "#98bb6c"}, "Warning": {ColorWarning, "#dca561"},
		"Danger": {ColorDanger, "#c34043"}, "Muted": {ColorMuted, "#75797f"},
		"Subtle": {ColorSubtle, "#393b44"}, "Blue": {ColorBlue, "#8ba4b0"}, "Cyan": {ColorCyan, "#c5c9c7"},
		"Fg": {ColorFg, "#c5c9c7"}, "Bg": {ColorBg, "#22262d"}, "Border": {ColorBorder, "#393b44"},
		"Selection": {ColorSelection, "#393b44"}, "Default": {ColorDefault, ""},
	} {
		if string(tc.got) != tc.want {
			t.Errorf("Color%s = %q, want %q", name, tc.got, tc.want)
		}
	}

	// Named config colors and palette indices use the theme's slots.
	for in, want := range map[string]string{
		"red": "#c4746e", "green": "#8a9a7b", "yellow": "#c4b28a", "blue": "#8ba4b0",
		"purple": "#a292a3", "cyan": "#c5c9c7", "gray": "#75797f", "black": "#22262d",
		"white": "#a4a7a4", "bright-blue": "#7fb4ca", "12": "#7fb4ca", "1": "#c4746e",
		"default": "", "#123456": "#123456", "unknown": "#75797f",
	} {
		if got := ResolveColor(in); string(got) != want {
			t.Errorf("ResolveColor(%q) = %q, want %q", in, got, want)
		}
	}
	if got := RenderStatusTextWithColor("todo", "green", false); !strings.Contains(got, rgb("#8a9a7b")) {
		t.Errorf("status text = %q, want the theme's green", got)
	}
	if MarkdownChromaFormatter != "terminal16m" {
		t.Errorf("MarkdownChromaFormatter = %q, want terminal16m", MarkdownChromaFormatter)
	}
}

// TestApplyThemeWithoutBackground keeps the terminal's foreground and
// background when the theme background is not painted.
func TestApplyThemeWithoutBackground(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	withTheme(t, kansoTheme(), false)
	if ColorFg != "" || ColorBg != "" {
		t.Errorf("fg/bg = %q/%q, want terminal defaults", ColorFg, ColorBg)
	}
	if ColorPrimary != "#8ba4b0" {
		t.Errorf("ColorPrimary = %q, want the theme accent", ColorPrimary)
	}
	if got := PaintBackground("x", 3, 1); got != "x" {
		t.Errorf("PaintBackground = %q, want the view unchanged", got)
	}
}

func TestApplyThemePartialPaletteFallsBackToTerminalSlots(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	withTheme(t, &Theme{Accent: "#ff00ff"}, true)
	if ColorPrimary != "#ff00ff" || ColorDanger != "1" || ResolveColor("cyan") != "6" || ColorBg != "" {
		t.Errorf("primary=%q danger=%q cyan=%q bg=%q", ColorPrimary, ColorDanger, ResolveColor("cyan"), ColorBg)
	}
}

func TestOnThemeApplied(t *testing.T) {
	themeHooksMu.Lock()
	saved := themeHooks
	themeHooksMu.Unlock()
	t.Cleanup(func() {
		themeHooksMu.Lock()
		themeHooks = saved
		themeHooksMu.Unlock()
	})
	var calls []string
	OnThemeApplied(func() { calls = append(calls, "a:"+string(ColorPrimary)) })
	OnThemeApplied(func() { calls = append(calls, "b") })
	withTheme(t, kansoTheme(), true)
	ApplyTheme(nil, false)
	// Hooks see the new palette and run in registration order.
	want := []string{"a:#8ba4b0", "b", "a:5", "b"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Errorf("hook calls = %v, want %v", calls, want)
	}
}

func TestThemedMarkdown(t *testing.T) {
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		t.Run(profile.Name(), func(t *testing.T) {
			withColorProfile(t, profile)
			withTheme(t, kansoTheme(), true)
			r, err := glamour.NewTermRenderer(
				glamour.WithStyles(MarkdownStyleConfig()),
				glamour.WithChromaFormatter(MarkdownChromaFormatterFor(profile)),
				glamour.WithColorProfile(profile),
			)
			if err != nil {
				t.Fatal(err)
			}
			md, err := r.Render("# H1\n\ntext `code` [link](https://x.y)\n\n```go\n// comment\nfunc main() { fmt.Println(\"s\") }\n```\n")
			if err != nil {
				t.Fatal(err)
			}
			switch profile {
			case termenv.TrueColor:
				for _, hex := range []string{
					"#c5c9c7", // text
					"#8ba4b0", // heading (accent), function names
					"#c4746e", // inline code (red slot)
					"#a292a3", // keyword (magenta slot)
					"#75797f", // comment (muted)
				} {
					if !strings.Contains(md, rgb(hex)) {
						t.Errorf("markdown lacks theme color %s: %q", hex, md)
					}
				}
			case termenv.ANSI256:
				if nonPaletteColor.FindString(md) == "" || strings.Contains(md, "38;2;") {
					t.Errorf("want 256-color escapes only: %q", md)
				}
			case termenv.ANSI:
				if strings.Contains(md, "38;2;") || strings.Contains(md, "38;5;") {
					t.Errorf("want 16-color escapes only: %q", md)
				}
			case termenv.Ascii:
				for p := range sgrParams(md) {
					if strings.HasPrefix(p, "3") && len(p) == 2 || p == "38" {
						t.Errorf("color parameter %s under Ascii: %q", p, md)
					}
				}
			}
		})
	}
}

// TestThemedStylesDegrade checks that hex theme colors degrade with the
// color profile and vanish without color support.
func TestThemedStylesDegrade(t *testing.T) {
	for _, profile := range []termenv.Profile{termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		t.Run(profile.Name(), func(t *testing.T) {
			withColorProfile(t, profile)
			withTheme(t, kansoTheme(), true)
			for name, s := range renderSharedStyles(t, profile) {
				if name == "markdown" {
					continue
				}
				if strings.Contains(s, "38;2;") || strings.Contains(s, "48;2;") {
					t.Errorf("%s emits truecolor under %s: %q", name, profile.Name(), s)
				}
				if profile == termenv.Ascii && strings.Contains(s, "\x1b[") {
					t.Errorf("%s emits escapes under Ascii: %q", name, s)
				}
			}
			if profile == termenv.Ascii {
				if got := PaintBackground("x", 4, 2); got != "x" {
					t.Errorf("PaintBackground under Ascii = %q", got)
				}
			}
		})
	}
}

func TestPaintBackground(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	if got := PaintBackground("a\nb", 3, 3); got != "a\nb" {
		t.Errorf("terminal theme: PaintBackground = %q, want unchanged", got)
	}
	withTheme(t, kansoTheme(), true)
	set := "\x1b[" + termenv.TrueColor.Color("#22262d").Sequence(true) + ";" + rgb("#c5c9c7") + "m"
	view := "ab\n" + Danger.Render("x") + "y"
	got := PaintBackground(view, 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (padded to height): %q", len(lines), got)
	}
	if lines[0] != set+"ab  \x1b[0m" {
		t.Errorf("line 0 = %q", lines[0])
	}
	// The colors are restored after the styled span's reset.
	if !strings.HasPrefix(lines[1], set) || !strings.Contains(lines[1], "\x1b[0m"+set+"y  \x1b[0m") {
		t.Errorf("line 1 = %q", lines[1])
	}
	if lines[2] != set+"    \x1b[0m" {
		t.Errorf("line 2 = %q", lines[2])
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 4 {
			t.Errorf("line %q has width %d, want 4", l, w)
		}
	}
}

func TestApplyConfiguredTheme(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	t.Cleanup(func() { ApplyTheme(nil, false) })
	dir := t.TempDir()
	path := filepath.Join(dir, "palette.yml")
	if err := os.WriteFile(path, []byte("fg: '#c5c9c7'\nbg: '#22262d'\naccent: '#8ba4b0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ThemeEnvVar, "")
	t.Setenv(config.ThemeBackgroundEnvVar, "")

	cfg := config.Default()
	cfg.SetConfigDir(dir)
	cfg.TUI.Theme = "file:palette.yml"
	if err := ApplyConfiguredTheme(cfg, true); err != nil {
		t.Fatal(err)
	}
	if ColorPrimary != "#8ba4b0" || ColorBg != "#22262d" || CurrentTheme() == nil {
		t.Errorf("file theme not applied: primary=%q bg=%q", ColorPrimary, ColorBg)
	}

	// The CLI never paints the background.
	if err := ApplyConfiguredTheme(cfg, false); err != nil {
		t.Fatal(err)
	}
	if ColorBg != "" || ColorPrimary != "#8ba4b0" {
		t.Errorf("CLI: bg=%q primary=%q", ColorBg, ColorPrimary)
	}

	// tui.theme_background: false
	off := false
	cfg.TUI.ThemeBackground = &off
	if err := ApplyConfiguredTheme(cfg, true); err != nil || ColorBg != "" {
		t.Errorf("theme_background false: err=%v bg=%q", err, ColorBg)
	}

	// Errors fall back to the terminal theme.
	cfg.TUI.Theme = "file:missing.yml"
	if err := ApplyConfiguredTheme(cfg, true); err == nil || !strings.Contains(err.Error(), "missing.yml") {
		t.Errorf("err = %v, want missing file error", err)
	}
	if CurrentTheme() != nil || ColorPrimary != "5" {
		t.Errorf("failed theme did not fall back to terminal: primary=%q", ColorPrimary)
	}

	// $BEANS_THEME overrides tui.theme.
	t.Setenv(config.ThemeEnvVar, "terminal")
	cfg.TUI.Theme = "file:palette.yml"
	if err := ApplyConfiguredTheme(cfg, true); err != nil || CurrentTheme() != nil {
		t.Errorf("BEANS_THEME=terminal: err=%v theme=%v", err, CurrentTheme())
	}

	// Without color support nothing is loaded.
	withColorProfile(t, termenv.Ascii)
	t.Setenv(config.ThemeEnvVar, "file:does-not-exist.yml")
	if err := ApplyConfiguredTheme(cfg, true); err != nil || CurrentTheme() != nil {
		t.Errorf("Ascii profile: err=%v theme=%v", err, CurrentTheme())
	}
}
