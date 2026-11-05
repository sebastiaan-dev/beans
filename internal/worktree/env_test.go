package worktree

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestIsShellDirective(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"WORKTRUNK_DIRECTIVE_FILE", true},
		{"WORKTRUNK_DIRECTIVE_CD_FILE", true},
		{"WORKTRUNK_DIRECTIVE_EXEC_FILE", true},
		{"WORKTRUNK_CONFIG_PATH", false},
		{"WORKTRUNK_DIRECTIVE", false},
		{"MY_WORKTRUNK_DIRECTIVE_FILE", false},
		{"PATH", false},
	}
	for _, tt := range tests {
		if got := IsShellDirective(tt.name); got != tt.want {
			t.Errorf("IsShellDirective(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEnvWithout(t *testing.T) {
	env := []string{"A=1", "B=2=3", "AB=4", "C", "A=5"}
	got := EnvWithout(env, func(name string) bool { return name == "A" || name == "C" })
	if want := []string{"B=2=3", "AB=4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EnvWithout = %q, want %q", got, want)
	}
	if want := []string{"A=1", "B=2=3", "AB=4", "C", "A=5"}; !reflect.DeepEqual(env, want) {
		t.Errorf("EnvWithout modified its input: %q", env)
	}
}

func TestChildEnv(t *testing.T) {
	t.Setenv("WORKTRUNK_DIRECTIVE_FILE", "/tmp/directive")
	t.Setenv("WORKTRUNK_DIRECTIVE_CD_FILE", "/tmp/cd")
	t.Setenv("WORKTRUNK_CONFIG_PATH", "/tmp/config.toml")
	t.Setenv("WORKTREE_ENV_TEST", "kept")
	env := ChildEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "WORKTRUNK_DIRECTIVE_") {
			t.Errorf("ChildEnv kept directive %q", kv)
		}
	}
	for _, want := range []string{"WORKTRUNK_CONFIG_PATH=/tmp/config.toml", "WORKTREE_ENV_TEST=kept"} {
		if !slices.Contains(env, want) {
			t.Errorf("ChildEnv dropped %q", want)
		}
	}
}
