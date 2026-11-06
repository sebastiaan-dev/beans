package worktree

import (
	"os"
	"strings"
)

// worktrunkDirectivePrefix starts the names of worktrunk's shell-integration
// variables (e.g. WORKTRUNK_DIRECTIVE_FILE). The `wt` shell wrapper sets them
// for one wt invocation so wt can tell the calling shell to cd or run a
// command afterwards.
const worktrunkDirectivePrefix = "WORKTRUNK_DIRECTIVE_"

// IsShellDirective reports whether the environment variable name is a shell
// integration directive, which only applies to the shell that set it.
func IsShellDirective(name string) bool {
	return strings.HasPrefix(name, worktrunkDirectivePrefix)
}

// EnvWithout returns a copy of env (KEY=VALUE entries) without the entries
// whose name drop reports true for.
func EnvWithout(env []string, drop func(name string) bool) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if drop(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ChildEnv returns the environment for processes started by beans-serve (wt,
// agents, terminal shells): the server's environment without shell
// integration directives. If beans-serve was started from a shell with
// worktrunk's shell integration active (e.g. through an alias run by wt),
// inheriting them would let any wt run by a child write cd/exec directives
// meant for that shell.
func ChildEnv() []string {
	return EnvWithout(os.Environ(), IsShellDirective)
}
