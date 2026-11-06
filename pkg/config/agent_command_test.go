package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitCommandLine(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr string
	}{
		{"pi", []string{"pi"}, ""},
		{"  claude   --verbose\t-p  ", []string{"claude", "--verbose", "-p"}, ""},
		{`claude "Work on bean x"`, []string{"claude", "Work on bean x"}, ""},
		{`echo 'it''s' "a \"b\" \n" c\ d`, []string{"echo", "its", `a "b" \n`, "c d"}, ""},
		{`a "" ''`, []string{"a", "", ""}, ""},
		{`x$HOME *`, []string{"x$HOME", "*"}, ""},
		{"", nil, ""},
		{`a "b`, nil, "unterminated"},
		{`a 'b`, nil, "unterminated"},
		{`a \`, nil, "trailing backslash"},
	}
	for _, tt := range tests {
		got, err := SplitCommandLine(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("SplitCommandLine(%q) error = %v, want %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitCommandLine(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestExpandAgentCommand(t *testing.T) {
	vars := AgentCommandVars{BeanID: "b-1", BeanTitle: `Fix "it" {{x}}`, Path: "/tmp/w t"}
	tests := []struct {
		in      string
		want    []string
		wantErr string
	}{
		{"pi", []string{"pi"}, ""},
		{`claude "Work on {{bean_id}}: {{ bean_title }}"`, []string{"claude", `Work on b-1: Fix "it" {{x}}`}, ""},
		{"code {{path}}", []string{"code", "/tmp/w t"}, ""},
		{"x {{nope}}", nil, "unknown placeholder {{nope}}"},
		{"x {{bean_id", nil, "unknown placeholder"},
		{"   ", nil, "empty command"},
		{`"" x`, nil, "empty program"},
		{`a "b`, nil, "unterminated"},
	}
	for _, tt := range tests {
		got, err := ExpandAgentCommand(tt.in, vars)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ExpandAgentCommand(%q) error = %v, want %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ExpandAgentCommand(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestGetAgentCommand(t *testing.T) {
	t.Setenv(AgentCommandEnvVar, "")
	cfg := Default()
	if args, err := cfg.GetAgentCommand(AgentCommandVars{}); args != nil || err != nil {
		t.Errorf("unset: %q, %v", args, err)
	}
	if err := cfg.ValidateAgentCommand(); err != nil {
		t.Errorf("unset command is invalid: %v", err)
	}

	cfg.TUI.AgentCommand = "claude {{bean_id}}"
	if args, err := cfg.GetAgentCommand(AgentCommandVars{BeanID: "b-1"}); err != nil || !reflect.DeepEqual(args, []string{"claude", "b-1"}) {
		t.Errorf("tui.agent_command: %q, %v", args, err)
	}

	cfg.TUI.AgentCommand = "claude {{bogus}}"
	if err := cfg.ValidateAgentCommand(); err == nil || !strings.Contains(err.Error(), "tui.agent_command") {
		t.Errorf("err = %v, want an error naming tui.agent_command", err)
	}

	// $BEANS_AGENT_COMMAND overrides tui.agent_command.
	t.Setenv(AgentCommandEnvVar, "pi")
	if v, src := cfg.AgentCommandSetting(); v != "pi" || src != "$"+AgentCommandEnvVar {
		t.Errorf("AgentCommandSetting() = %q, %q", v, src)
	}
	if args, err := cfg.GetAgentCommand(AgentCommandVars{}); err != nil || !reflect.DeepEqual(args, []string{"pi"}) {
		t.Errorf("env override: %q, %v", args, err)
	}
}

func TestAgentCommandLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("beans:\n  prefix: x-\ntui:\n  agent_command: claude \"Work on {{bean_id}}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.AgentCommand != `claude "Work on {{bean_id}}"` {
		t.Fatalf("agent_command = %q", cfg.TUI.AgentCommand)
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.TUI.AgentCommand != cfg.TUI.AgentCommand {
		t.Errorf("round trip: %q", reloaded.TUI.AgentCommand)
	}
}
