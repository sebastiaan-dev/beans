package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/config"
)

func TestWorktreeBackendNotice(t *testing.T) {
	tests := []struct {
		name        string
		backend     config.WorktreeBackend
		wtAvailable bool
		want        string // substring; "" means no notice
	}{
		{"git without wt", config.WorktreeBackendGit, false, ""},
		{"git with wt suggests worktrunk", config.WorktreeBackendGit, true, "set worktree.backend: worktrunk"},
		{"worktrunk with wt", config.WorktreeBackendWorktrunk, true, ""},
		{"worktrunk without wt", config.WorktreeBackendWorktrunk, false, "wt was not found on PATH"},
		{"invalid backend is a config error, not a notice", "svn", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worktreeBackendNotice(tt.backend, tt.wtAvailable)
			if tt.want == "" {
				if got != "" {
					t.Errorf("notice = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("notice = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestCheckTheme(t *testing.T) {
	ok := func(config.ThemeSpec) error { return nil }
	fail := func(config.ThemeSpec) error { return errors.New(`nvim colorscheme "nope": E185`) }
	tests := []struct {
		name         string
		theme, env   string
		overrides    map[string]string
		overridesEnv string
		bgEnv        string
		nvim         bool
		load         func(config.ThemeSpec) error
		wantOK       string
		wantErr      string
		wantWarning  string
	}{
		{name: "default", nvim: true, load: fail, wantOK: "Theme 'terminal' is valid"},
		{name: "nvim", theme: "nvim", nvim: true, load: ok, wantOK: "Theme 'nvim' is valid"},
		{name: "nvim colorscheme", theme: "nvim:kanso-mist", nvim: true, load: ok, wantOK: "nvim:kanso-mist"},
		{name: "unknown theme", theme: "dracula", nvim: true, load: ok, wantErr: "unknown theme"},
		{name: "missing colorscheme", theme: "nvim:nope", nvim: true, load: fail, wantErr: "E185"},
		{name: "nvim not installed", theme: "nvim", load: fail, wantWarning: "nvim is not installed"},
		{name: "env override", theme: "nvim", env: "bogus", nvim: true, load: ok, wantErr: "$BEANS_THEME"},
		{name: "env nvim not installed", env: "nvim:x", load: ok, wantWarning: "$BEANS_THEME"},
		{name: "bad file", theme: "file:x.yml", nvim: true, load: func(config.ThemeSpec) error { return errors.New("no such file") }, wantErr: "no such file"},
		{name: "bad background env", theme: "nvim", bgEnv: "sometimes", nvim: true, load: ok, wantErr: "BEANS_THEME_BACKGROUND"},
		{name: "overrides", theme: "nvim", overrides: map[string]string{"border": "#5c6066", "Accent": "4", "danger": "bright-red"}, nvim: true, load: ok, wantOK: "3 role override(s)"},
		{name: "overrides on terminal", overrides: map[string]string{"border": "gray"}, load: fail, wantOK: "Theme 'terminal' is valid (1 role"},
		{name: "unknown override role", overrides: map[string]string{"frame": "#5c6066"}, load: ok, wantErr: `tui.theme_overrides: unknown theme role "frame"`},
		{name: "invalid override color", overrides: map[string]string{"border": "#5c606"}, load: ok, wantErr: `border: invalid color "#5c606"`},
		{name: "invalid override name", overrides: map[string]string{"border": "chartreuse"}, load: ok, wantErr: "chartreuse"},
		{name: "override env", overridesEnv: "border=99", load: ok, wantErr: `invalid color "99"`},
		{name: "override env syntax", overridesEnv: "border", load: ok, wantErr: "$BEANS_THEME_OVERRIDES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.ThemeEnvVar, tt.env)
			t.Setenv(config.ThemeBackgroundEnvVar, tt.bgEnv)
			t.Setenv(config.ThemeOverridesEnvVar, tt.overridesEnv)
			c := config.Default()
			c.TUI.Theme = tt.theme
			c.TUI.ThemeOverrides = tt.overrides
			gotOK, gotErr, gotWarning := checkTheme(c, tt.nvim, tt.load)
			for _, x := range []struct{ name, got, want string }{
				{"ok", gotOK, tt.wantOK}, {"error", gotErr, tt.wantErr}, {"warning", gotWarning, tt.wantWarning},
			} {
				if (x.want == "") != (x.got == "") || !strings.Contains(x.got, x.want) {
					t.Errorf("%s = %q, want %q", x.name, x.got, x.want)
				}
			}
		})
	}
}
