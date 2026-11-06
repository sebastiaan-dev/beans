package commands

import (
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
