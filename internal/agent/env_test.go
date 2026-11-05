package agent

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestClaudeEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"CLAUDECODE=1",
		"CLAUDECODE_OTHER=kept",
		"WORKTRUNK_DIRECTIVE_FILE=/tmp/directive",
		"WORKTRUNK_DIRECTIVE_CD_FILE=/tmp/cd",
		"WORKTRUNK_CONFIG_PATH=/tmp/config.toml",
		"HOME=/home/me",
	}
	got := claudeEnv(env)
	want := []string{"PATH=/usr/bin", "CLAUDECODE_OTHER=kept", "WORKTRUNK_CONFIG_PATH=/tmp/config.toml", "HOME=/home/me"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claudeEnv = %q, want %q", got, want)
	}
}

func TestBuildClaudeEnv(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("WORKTRUNK_DIRECTIVE_FILE", "/tmp/directive")
	t.Setenv("AGENT_ENV_TEST", "kept")
	env := buildClaudeEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "WORKTRUNK_DIRECTIVE_") {
			t.Errorf("buildClaudeEnv kept %q", kv)
		}
	}
	if !slices.Contains(env, "AGENT_ENV_TEST=kept") {
		t.Error("buildClaudeEnv dropped AGENT_ENV_TEST")
	}
}
