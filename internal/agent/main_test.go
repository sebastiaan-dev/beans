package agent

import (
	"testing"

	"github.com/hmans/beans/internal/agent/agenttest"
)

// TestMain guarantees no test in this package executes the real Claude Code CLI.
func TestMain(m *testing.M) { agenttest.Main(m) }
