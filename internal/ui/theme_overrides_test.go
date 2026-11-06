package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

func TestValidateThemeOverrides(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantErr   string
	}{
		{"none", nil, ""},
		{"hex", map[string]string{"border": "#5c6066", "bg": "#abc"}, ""},
		{"index", map[string]string{"accent": "4", "muted": "15", "fg": "0"}, ""},
		{"names", map[string]string{"danger": "bright-red", "info": "Cyan", "selection": "default"}, ""},
		{"bad hex", map[string]string{"border": "#5c606"}, `border: invalid color "#5c606"`},
		{"hex digits", map[string]string{"border": "#zzzzzz"}, "invalid color"},
		{"index range", map[string]string{"accent": "16"}, `invalid color "16"`},
		{"non-canonical index", map[string]string{"accent": "04"}, "invalid color"},
		{"unknown name", map[string]string{"accent": "chartreuse"}, "chartreuse"},
		{"unknown role", map[string]string{"frame": "red"}, `unknown theme role "frame"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateThemeOverrides(tt.overrides)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// withSettings applies s for the duration of the test.
func withSettings(t *testing.T, s ThemeSettings) {
	t.Helper()
	ApplyThemeSettings(s)
	t.Cleanup(func() { ApplyTheme(nil, false) })
}

func TestApplyThemeOverrides(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	withSettings(t, ThemeSettings{Theme: kansoTheme(), Background: true, Overrides: map[string]string{
		"border":       "#5C6066",
		"accent":       "4",      // the theme's palette slot 4
		"danger":       "yellow", // the theme's yellow (slot 3)
		"muted":        "#888",
		"selection":    "#2d4f67",
		"success":      "bright-green",
		"warning":      "13",
		"info":         "#123456",
		"fg":           "#eeeeee",
		"bg":           "#101010",
		"focus_border": "gray", // resolved against the base palette: theme's muted
		"bogus-role":   "#ffffff",
		"border-x":     "not a color",
	}})
	want := map[string]lipgloss.Color{
		"ColorBorder":      "#5c6066",
		"ColorSubtle":      "#5c6066",
		"ColorPrimary":     "#8ba4b0",
		"ColorDanger":      "#c4b28a",
		"ColorMuted":       "#888888",
		"ColorSecondary":   "#888888",
		"ColorSelection":   "#2d4f67",
		"ColorSuccess":     "#87a987",
		"ColorWarning":     "#938aa9",
		"ColorInfo":        "#123456",
		"ColorFg":          "#eeeeee",
		"ColorBg":          "#101010",
		"ColorFocusBorder": "#75797f",
	}
	got := map[string]lipgloss.Color{
		"ColorBorder": ColorBorder, "ColorSubtle": ColorSubtle, "ColorPrimary": ColorPrimary,
		"ColorDanger": ColorDanger, "ColorMuted": ColorMuted, "ColorSecondary": ColorSecondary,
		"ColorSelection": ColorSelection, "ColorSuccess": ColorSuccess, "ColorWarning": ColorWarning,
		"ColorInfo": ColorInfo, "ColorFg": ColorFg, "ColorBg": ColorBg, "ColorFocusBorder": ColorFocusBorder,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	// Shared styles and named colors follow the overrides.
	if r := TreeLine.Render("x"); !strings.Contains(r, rgb("#5c6066")) {
		t.Errorf("TreeLine = %q, want the overridden border", r)
	}
	if NamedColors["gray"] != "#888888" {
		t.Errorf("gray = %q, want the overridden muted color", NamedColors["gray"])
	}
	if out := PaintBackground("x", 1, 1); !strings.Contains(out, "48;2;16;16;16") {
		t.Errorf("PaintBackground = %q, want the overridden bg", out)
	}
}

func TestApplyThemeOverridesFocusBorder(t *testing.T) {
	// focus_border follows the (overridden) accent by default, then the
	// theme's own focus_border role, then the override.
	withSettings(t, ThemeSettings{Theme: kansoTheme(), Overrides: map[string]string{"accent": "#abcdef"}})
	if ColorFocusBorder != "#abcdef" {
		t.Errorf("focus border = %q, want the overridden accent", ColorFocusBorder)
	}
	th := kansoTheme()
	th.FocusBorder = "#c4b28a"
	ApplyThemeSettings(ThemeSettings{Theme: th, Overrides: map[string]string{"accent": "#abcdef"}})
	if ColorFocusBorder != "#c4b28a" {
		t.Errorf("focus border = %q, want the theme's focus_border", ColorFocusBorder)
	}
	ApplyThemeSettings(ThemeSettings{Theme: th, Overrides: map[string]string{"focus_border": "1"}})
	if ColorFocusBorder != "#c4746e" {
		t.Errorf("focus border = %q, want palette slot 1", ColorFocusBorder)
	}
}

func TestApplyThemeOverridesTerminal(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	// ANSI indices and names stay terminal palette references.
	withSettings(t, ThemeSettings{Overrides: map[string]string{"border": "7", "accent": "blue", "focus_border": "#ff0000"}})
	if ColorBorder != "7" || ColorPrimary != "4" || ColorFocusBorder != "#ff0000" || CurrentTheme() != nil {
		t.Errorf("border=%q accent=%q focus=%q", ColorBorder, ColorPrimary, ColorFocusBorder)
	}
	if ColorBg != "" || PaintBackground("x", 1, 1) != "x" {
		t.Error("terminal theme without bg override must not paint")
	}
	// A bg override paints the terminal theme's background when allowed.
	ApplyThemeSettings(ThemeSettings{Background: true, Overrides: map[string]string{"bg": "#101010"}})
	if ColorBg != "#101010" || PaintBackground("x", 1, 1) == "x" {
		t.Errorf("bg override not painted: bg=%q", ColorBg)
	}
	ApplyThemeSettings(ThemeSettings{Background: false, Overrides: map[string]string{"bg": "#101010"}})
	if ColorBg != "" {
		t.Errorf("unpainted bg override: bg=%q", ColorBg)
	}
	// Without a painted background the theme's fg is dropped, an fg
	// override is kept.
	ApplyThemeSettings(ThemeSettings{Theme: kansoTheme(), Background: false})
	if ColorFg != "" {
		t.Errorf("fg = %q without a painted background", ColorFg)
	}
	ApplyThemeSettings(ThemeSettings{Theme: kansoTheme(), Background: false, Overrides: map[string]string{"fg": "#eeeeee"}})
	if ColorFg != "#eeeeee" {
		t.Errorf("fg override = %q", ColorFg)
	}
}

func TestLoadThemeSettings(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	t.Cleanup(func() { ApplyTheme(nil, false) })
	dir := t.TempDir()
	// A file theme with the kanso colors: its faint border is replaced by
	// the contrast guard, an override restores it.
	if err := os.WriteFile(filepath.Join(dir, "kanso.yml"), []byte("fg: '#c5c9c7'\nbg: '#22262d'\nmuted: '#75797f'\nborder: '#393b44'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ThemeEnvVar, "")
	t.Setenv(config.ThemeBackgroundEnvVar, "")
	t.Setenv(config.ThemeOverridesEnvVar, "")
	cfg := config.Default()
	cfg.SetConfigDir(dir)
	cfg.TUI.Theme = "file:kanso.yml"

	if err := ApplyConfiguredTheme(cfg, true); err != nil {
		t.Fatal(err)
	}
	if ColorBorder != "#75797f" {
		t.Errorf("guarded border = %q, want #75797f", ColorBorder)
	}

	cfg.TUI.ThemeOverrides = map[string]string{"border": "#393b44"}
	t.Setenv(config.ThemeOverridesEnvVar, "accent=2")
	if _, err := ApplyConfiguredThemeSettings(cfg, true); err != nil {
		t.Fatal(err)
	}
	// The palette has no slot 2, so the terminal's slot 2 is used.
	if ColorBorder != "#393b44" || ColorPrimary != "2" {
		t.Errorf("overrides not applied: border=%q accent=%q", ColorBorder, ColorPrimary)
	}

	// Invalid overrides are reported; the theme is still applied.
	t.Setenv(config.ThemeOverridesEnvVar, "accent=nope")
	s, err := ApplyConfiguredThemeSettings(cfg, true)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want invalid color error", err)
	}
	if s.Theme == nil || s.Overrides != nil || ColorBorder != "#75797f" {
		t.Errorf("invalid overrides: theme=%v overrides=%v border=%q", s.Theme, s.Overrides, ColorBorder)
	}

	// A theme that fails to load keeps valid overrides on the terminal theme.
	t.Setenv(config.ThemeOverridesEnvVar, "")
	cfg.TUI.Theme = "file:missing.yml"
	s, err = ApplyConfiguredThemeSettings(cfg, true)
	if err == nil || s.Theme != nil || ColorBorder != "#393b44" {
		t.Errorf("failed theme: err=%v theme=%v border=%q", err, s.Theme, ColorBorder)
	}
}

func TestThemeSettingsEqual(t *testing.T) {
	a := ThemeSettings{Theme: kansoTheme(), Background: true, Overrides: map[string]string{"border": "#111111"}}
	b := ThemeSettings{Theme: kansoTheme(), Background: true, Overrides: map[string]string{"border": "#111111"}}
	if !a.Equal(b) {
		t.Error("equal settings differ")
	}
	b.Theme.Border = "#222222"
	if a.Equal(b) {
		t.Error("different themes compare equal")
	}
	if a.Equal(ThemeSettings{Theme: kansoTheme(), Background: true}) {
		t.Error("different overrides compare equal")
	}
	if !(ThemeSettings{}).Equal(ThemeSettings{}) || (ThemeSettings{}).Equal(a) {
		t.Error("terminal settings comparison")
	}
}
