package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseThemeSpec(t *testing.T) {
	tests := []struct {
		in      string
		want    ThemeSpec
		wantErr string
	}{
		{"", ThemeSpec{Kind: ThemeTerminal}, ""},
		{"terminal", ThemeSpec{Kind: ThemeTerminal}, ""},
		{" terminal ", ThemeSpec{Kind: ThemeTerminal}, ""},
		{"nvim", ThemeSpec{Kind: ThemeNvim}, ""},
		{"nvim:kanso-mist", ThemeSpec{Kind: ThemeNvim, Colorscheme: "kanso-mist"}, ""},
		{"file:themes/x.yml", ThemeSpec{Kind: ThemeFile, Path: "themes/x.yml"}, ""},
		{"file:/abs/x.json", ThemeSpec{Kind: ThemeFile, Path: "/abs/x.json"}, ""},
		{"nvim:", ThemeSpec{}, "missing colorscheme"},
		{"nvim:bad name", ThemeSpec{}, "invalid colorscheme"},
		{"nvim:x|!rm", ThemeSpec{}, "invalid colorscheme"},
		{"file:", ThemeSpec{}, "missing path"},
		{"file", ThemeSpec{}, "unknown theme"},
		{"dracula", ThemeSpec{}, "unknown theme"},
		{"vim:x", ThemeSpec{}, "unknown theme"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseThemeSpec(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ParseThemeSpec(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if again, err := ParseThemeSpec(got.String()); err != nil || again != got {
				t.Errorf("String() %q does not round-trip: %+v, %v", got.String(), again, err)
			}
		})
	}
}

func TestGetTheme(t *testing.T) {
	t.Setenv(ThemeEnvVar, "")
	cfg := Default()
	cfg.SetConfigDir("/project")

	spec, err := cfg.GetTheme()
	if err != nil || spec.Kind != ThemeTerminal {
		t.Errorf("default theme = %+v, %v; want terminal", spec, err)
	}
	if v, src := cfg.ThemeSetting(); v != "terminal" || src != "default" {
		t.Errorf("ThemeSetting() = %q, %q", v, src)
	}

	cfg.TUI.Theme = "file:themes/mine.yml"
	spec, err = cfg.GetTheme()
	if err != nil || spec.Path != filepath.Join("/project", "themes", "mine.yml") {
		t.Errorf("tui.theme file path = %+v, %v; want resolved against the config dir", spec, err)
	}

	home, _ := os.UserHomeDir()
	cfg.TUI.Theme = "file:~/mine.yml"
	if spec, _ := cfg.GetTheme(); spec.Path != filepath.Join(home, "mine.yml") {
		t.Errorf("~ not expanded: %q", spec.Path)
	}

	cfg.TUI.Theme = "bogus"
	if _, err := cfg.GetTheme(); err == nil || !strings.Contains(err.Error(), "tui.theme") {
		t.Errorf("err = %v, want an error naming tui.theme", err)
	}

	// $BEANS_THEME overrides tui.theme; its relative paths use the cwd.
	t.Setenv(ThemeEnvVar, "file:env.yml")
	cwd, _ := os.Getwd()
	spec, err = cfg.GetTheme()
	if err != nil || spec.Path != filepath.Join(cwd, "env.yml") {
		t.Errorf("env theme = %+v, %v", spec, err)
	}
	if _, src := cfg.ThemeSetting(); src != "$BEANS_THEME" {
		t.Errorf("source = %q", src)
	}
	t.Setenv(ThemeEnvVar, "nvim:kanso-mist")
	if spec, _ := cfg.GetTheme(); spec != (ThemeSpec{Kind: ThemeNvim, Colorscheme: "kanso-mist"}) {
		t.Errorf("env theme = %+v", spec)
	}
	t.Setenv(ThemeEnvVar, "nope")
	if _, err := cfg.GetTheme(); err == nil || !strings.Contains(err.Error(), "$BEANS_THEME") {
		t.Errorf("err = %v, want an error naming $BEANS_THEME", err)
	}
}

func TestGetThemeBackground(t *testing.T) {
	t.Setenv(ThemeBackgroundEnvVar, "")
	cfg := Default()
	nvim := ThemeSpec{Kind: ThemeNvim}

	if bg, _ := cfg.GetThemeBackground(ThemeSpec{Kind: ThemeTerminal}); bg {
		t.Error("terminal theme must not paint a background")
	}
	if bg, _ := cfg.GetThemeBackground(nvim); !bg {
		t.Error("nvim theme should paint its background by default")
	}
	off := false
	cfg.TUI.ThemeBackground = &off
	if bg, _ := cfg.GetThemeBackground(nvim); bg {
		t.Error("tui.theme_background: false ignored")
	}
	t.Setenv(ThemeBackgroundEnvVar, "true")
	if bg, _ := cfg.GetThemeBackground(nvim); !bg {
		t.Error("$BEANS_THEME_BACKGROUND=true ignored")
	}
	t.Setenv(ThemeBackgroundEnvVar, "maybe")
	if _, err := cfg.GetThemeBackground(nvim); err == nil {
		t.Error("expected an error for an invalid boolean")
	}
}

func TestParseThemeOverrides(t *testing.T) {
	tests := []struct {
		in      string
		want    map[string]string
		wantErr string
	}{
		{"", map[string]string{}, ""},
		{"border=#5c6066,accent=4", map[string]string{"border": "#5c6066", "accent": "4"}, ""},
		{" Border = gray , , focus_border=bright-blue ", map[string]string{"border": "gray", "focus_border": "bright-blue"}, ""},
		{"border=#111,border=#222", map[string]string{"border": "#222"}, ""},
		{"border", nil, "use role=color"},
		{"=red", nil, "use role=color"},
		{"frame=red", nil, `unknown theme role "frame"`},
		{"border=", nil, "missing color"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseThemeOverrides(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseThemeOverrides(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestGetThemeOverrides(t *testing.T) {
	t.Setenv(ThemeOverridesEnvVar, "")
	cfg := Default()
	if o, err := cfg.GetThemeOverrides(); err != nil || len(o) != 0 {
		t.Errorf("default overrides = %v, %v", o, err)
	}

	cfg.TUI.ThemeOverrides = map[string]string{"Border": " #393b44 ", "accent": "5"}
	t.Setenv(ThemeOverridesEnvVar, "border=#5c6066,danger=red")
	o, err := cfg.GetThemeOverrides()
	want := map[string]string{"border": "#5c6066", "accent": "5", "danger": "red"}
	if err != nil || !reflect.DeepEqual(o, want) {
		t.Errorf("merged overrides = %v, %v; want %v (env wins per role)", o, err, want)
	}

	t.Setenv(ThemeOverridesEnvVar, "nope=1")
	if _, err := cfg.GetThemeOverrides(); err == nil || !strings.Contains(err.Error(), "$BEANS_THEME_OVERRIDES") {
		t.Errorf("err = %v, want an error naming $BEANS_THEME_OVERRIDES", err)
	}
	t.Setenv(ThemeOverridesEnvVar, "")
	cfg.TUI.ThemeOverrides = map[string]string{"frame": "1"}
	if _, err := cfg.GetThemeOverrides(); err == nil || !strings.Contains(err.Error(), "tui.theme_overrides") {
		t.Errorf("err = %v, want an error naming tui.theme_overrides", err)
	}
	cfg.TUI.ThemeOverrides = map[string]string{"bg": ""}
	if _, err := cfg.GetThemeOverrides(); err == nil || !strings.Contains(err.Error(), "missing color") {
		t.Errorf("err = %v, want a missing color error", err)
	}
}

// TestGetThemeBackgroundTerminalOverride: the terminal theme paints a
// background only when a bg override gives it one.
func TestGetThemeBackgroundTerminalOverride(t *testing.T) {
	t.Setenv(ThemeBackgroundEnvVar, "")
	t.Setenv(ThemeOverridesEnvVar, "bg=#101010")
	cfg := Default()
	term := ThemeSpec{Kind: ThemeTerminal}
	if bg, _ := cfg.GetThemeBackground(term); !bg {
		t.Error("bg override on the terminal theme should paint the background")
	}
	off := false
	cfg.TUI.ThemeBackground = &off
	if bg, _ := cfg.GetThemeBackground(term); bg {
		t.Error("tui.theme_background: false must win over the bg override")
	}
}

func TestTUIConfigLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("beans:\n  prefix: x-\ntui:\n  theme: nvim:kanso-mist\n  theme_background: false\n  theme_overrides:\n    border: \"#5c6066\"\n    accent: \"4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.Theme != "nvim:kanso-mist" || cfg.TUI.ThemeBackground == nil || *cfg.TUI.ThemeBackground {
		t.Fatalf("loaded tui config = %+v", cfg.TUI)
	}
	if cfg.ConfigPath() != path {
		t.Errorf("ConfigPath() = %q, want %q", cfg.ConfigPath(), path)
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# Color theme") {
		t.Errorf("saved config lacks the theme comment:\n%s", data)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.TUI.Theme != "nvim:kanso-mist" || reloaded.TUI.ThemeBackground == nil || *reloaded.TUI.ThemeBackground {
		t.Errorf("round trip lost the tui config: %+v", reloaded.TUI)
	}
	if want := map[string]string{"border": "#5c6066", "accent": "4"}; !reflect.DeepEqual(reloaded.TUI.ThemeOverrides, want) {
		t.Errorf("round trip overrides = %v, want %v", reloaded.TUI.ThemeOverrides, want)
	}

	// The default config has no tui section.
	def := Default()
	def.SetConfigDir(t.TempDir())
	if err := def.Save(""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(def.ConfigDir(), ConfigFileName))
	if strings.Contains(string(data), "tui:") {
		t.Errorf("default config should omit the tui section:\n%s", data)
	}
}
