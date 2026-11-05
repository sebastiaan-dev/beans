// Package agenttest keeps Go tests from ever executing the real Claude Code
// CLI. Packages whose tests (directly or indirectly) drive an agent.Manager
// must run through Main and inject FakeClaude via Manager.SetClaudePath:
//
//	func TestMain(m *testing.M) { agenttest.Main(m) }
//
//	mgr := agent.NewManager("", nil)
//	mgr.SetClaudePath(agenttest.FakeClaude(t))
package agenttest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeClaudePath is the injectable no-op fake, created by Main.
var fakeClaudePath string

// fakeScript stands in for Claude Code: it ignores its input and exits
// immediately with an empty stream, so a spawned "agent" just ends its turn.
const fakeScript = `#!/bin/sh
echo "agenttest: fake claude invoked" >&2
exit 0
`

// guardScript shadows any real `claude` on PATH. It records each invocation
// in a marker file so Main can fail the test binary loudly.
const guardScript = `#!/bin/sh
printf '%%s\n' "claude $*" >> %q
echo "agenttest: blocked execution of the real claude CLI; inject agenttest.FakeClaude via Manager.SetClaudePath" >&2
exit 1
`

// Main runs the package's tests with a guard `claude` prepended to PATH, so
// code falling back to agent.DefaultClaudeBinary can never reach the real CLI
// (no real agents, no API cost, no goroutines outliving tests). If any test
// hits the guard, Main reports it and exits non-zero. It never returns.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "beans-agenttest-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agenttest: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	guardDir := filepath.Join(dir, "guard")
	marker := filepath.Join(dir, "real-claude-invocations")
	fakeClaudePath = filepath.Join(dir, "fake", "claude")
	if err := writeExecutable(filepath.Join(guardDir, "claude"), fmt.Sprintf(guardScript, marker)); err != nil {
		fmt.Fprintf(os.Stderr, "agenttest: %v\n", err)
		return 1
	}
	if err := writeExecutable(fakeClaudePath, fakeScript); err != nil {
		fmt.Fprintf(os.Stderr, "agenttest: %v\n", err)
		return 1
	}
	if err := os.Setenv("PATH", guardDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintf(os.Stderr, "agenttest: %v\n", err)
		return 1
	}

	code := m.Run()

	// Agent goroutines may still be running: make sure they can only ever
	// find the guard, even while the temp dir is being removed.
	_ = os.Setenv("PATH", guardDir)

	if data, err := os.ReadFile(marker); err == nil {
		fmt.Fprintf(os.Stderr, "agenttest: FAIL: tests tried to execute the real `claude` CLI from PATH "+
			"(inject agenttest.FakeClaude via Manager.SetClaudePath):\n%s", data)
		return 1
	}
	return code
}

// FakeClaude returns the path of a no-op fake Claude Code executable that is
// valid for the lifetime of the test binary (so agent goroutines that outlive
// a test can still exec it). The package must use Main as its TestMain.
func FakeClaude(tb testing.TB) string {
	tb.Helper()
	if fakeClaudePath == "" {
		tb.Fatal("agenttest.FakeClaude requires TestMain to call agenttest.Main")
	}
	return fakeClaudePath
}

func writeExecutable(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o755)
}
