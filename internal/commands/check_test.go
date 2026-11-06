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

func TestCheckAgentCommand(t *testing.T) {
	tests := []struct {
		name, command, env string
		wantOK, wantErr    string
	}{
		{name: "unset"},
		{name: "valid", command: `claude "Work on {{bean_id}}: {{bean_title}}"`, wantOK: "Agent command (tui.agent_command) is valid"},
		{name: "unknown placeholder", command: "claude {{bean}}", wantErr: "tui.agent_command: unknown placeholder {{bean}}"},
		{name: "bad quoting", command: `claude "oops`, wantErr: "tui.agent_command: unterminated"},
		{name: "env overrides", command: "claude {{bean}}", env: "pi {{path}}", wantOK: "($BEANS_AGENT_COMMAND)"},
		{name: "invalid env", env: "pi {{nope}}", wantErr: "$BEANS_AGENT_COMMAND: unknown placeholder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.AgentCommandEnvVar, tt.env)
			c := config.Default()
			c.TUI.AgentCommand = tt.command
			ok, errMsg := checkAgentCommand(c)
			if tt.wantErr != "" {
				if !strings.Contains(errMsg, tt.wantErr) || ok != "" {
					t.Errorf("checkAgentCommand() = %q, %q; want error %q", ok, errMsg, tt.wantErr)
				}
				return
			}
			if errMsg != "" || (tt.wantOK == "" && ok != "") || !strings.Contains(ok, tt.wantOK) {
				t.Errorf("checkAgentCommand() = %q, %q; want ok %q", ok, errMsg, tt.wantOK)
			}
		})
	}
}

func TestCheckASCII(t *testing.T) {
	tests := []struct {
		name    string
		ascii   bool
		env     string
		wantOK  string
		wantErr string
	}{
		{name: "unset"},
		{name: "config", ascii: true, wantOK: "ASCII mode is on (tui.ascii)"},
		{name: "env on", env: "1", wantOK: "ASCII mode is on ($BEANS_TUI_ASCII)"},
		{name: "env off", ascii: true, env: "false", wantOK: "ASCII mode is off ($BEANS_TUI_ASCII)"},
		{name: "invalid env", ascii: true, env: "sometimes", wantErr: `$BEANS_TUI_ASCII: invalid boolean "sometimes"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.ASCIIEnvVar, tt.env)
			c := config.Default()
			c.TUI.ASCII = tt.ascii
			ok, errMsg := checkASCII(c)
			if tt.wantErr != "" {
				if !strings.Contains(errMsg, tt.wantErr) || ok != "" {
					t.Errorf("checkASCII() = %q, %q; want error %q", ok, errMsg, tt.wantErr)
				}
				return
			}
			if errMsg != "" || ok != tt.wantOK {
				t.Errorf("checkASCII() = %q, %q; want ok %q", ok, errMsg, tt.wantOK)
			}
		})
	}
}
