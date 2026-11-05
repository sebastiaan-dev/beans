package worktree

import (
	"os/exec"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"brave-otter-x7k2", false},
		{"run-test", false},
		{"Feature_1.2", false},
		{"a", false},
		{"", true},
		{"Run Test", true},
		{"a/b", true},
		{"../escape", true},
		{"-flag", true},
		{".hidden", true},
		{"_x", true},
		{"feature..x", true},
		{"x.lock", true},
		{"trailing.", true},
		{"name~1", true},
		{"a^b", true},
		{"a:b", true},
		{"a?b", true},
		{"a*b", true},
		{"a[b", true},
		{`a\b`, true},
		{"a@{b", true},
		{"ünïcode", true},
		{"tab\tname", true},
		{strings.Repeat("a", maxNameLength), false},
		{strings.Repeat("a", maxNameLength+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid worktree name") {
				t.Errorf("error should say the name is invalid: %v", err)
			}
			// Every accepted name must be a valid git branch name.
			if err == nil {
				if out, gitErr := exec.Command("git", "check-ref-format", "--branch", branchPrefix+tt.name).CombinedOutput(); gitErr != nil {
					t.Errorf("accepted name %q is not a valid branch name: %s", tt.name, out)
				}
			}
		})
	}
}

func TestValidateNameSuggestsSlug(t *testing.T) {
	err := ValidateName("Run Test")
	if err == nil || !strings.Contains(err.Error(), `(try "run-test")`) {
		t.Errorf("expected a slug suggestion, got %v", err)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Run Test":                "run-test",
		"  Fix: the bug!! ":       "fix-the-bug",
		"already-a-slug":          "already-a-slug",
		"../../etc":               "etc",
		"!!!":                     "",
		"x.lock":                  "x-lock",
		"brave-otter-x7k2":        "brave-otter-x7k2",
		strings.Repeat("ab-", 60): strings.TrimRight(strings.Repeat("ab-", 60)[:maxNameLength], "-"),
	}
	for in, want := range tests {
		got := Slugify(in)
		if got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
		if got != "" {
			if err := ValidateName(got); err != nil {
				t.Errorf("Slugify(%q) = %q is not a valid name: %v", in, got, err)
			}
		}
	}
}
