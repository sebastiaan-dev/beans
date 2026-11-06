package agent

import (
	"os"

	"github.com/hmans/beans/internal/worktree"
)

// buildClaudeEnv creates the environment for claude processes: the server's
// environment without CLAUDECODE (so nested sessions are allowed) and without
// shell-integration directives (see worktree.ChildEnv), so a wt run by the
// agent (e.g. `wt merge` during Integrate) can't send cd/exec directives to
// the shell that started beans-serve.
func buildClaudeEnv() []string {
	return claudeEnv(os.Environ())
}

// claudeEnv filters env for claude processes (see buildClaudeEnv).
func claudeEnv(env []string) []string {
	return worktree.EnvWithout(env, func(name string) bool {
		return name == "CLAUDECODE" || worktree.IsShellDirective(name)
	})
}
