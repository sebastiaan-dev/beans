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
		name        string
		theme, env  string
		bgEnv       string
		nvim        bool
		load        func(config.ThemeSpec) error
		wantOK      string
		wantErr     string
		wantWarning string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.ThemeEnvVar, tt.env)
			t.Setenv(config.ThemeBackgroundEnvVar, tt.bgEnv)
			c := config.Default()
			c.TUI.Theme = tt.theme
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
