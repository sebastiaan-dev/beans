package graph

import (
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
)

// TestCentralAgentPromptOperationsMatchSchema guards against the prompt
// referring to operations that don't exist (it once used a removed startWork
// mutation).
func TestCentralAgentPromptOperationsMatchSchema(t *testing.T) {
	schema := NewExecutableSchema(Config{Resolvers: &Resolver{}}).Schema()
	for _, op := range []string{centralCreateWorktreeOp, centralStartAgentOp} {
		if _, errs := gqlparser.LoadQuery(schema, op); len(errs) > 0 {
			t.Errorf("operation %q is invalid: %v", op, errs)
		}
	}

	prompt := CentralAgentPrompt("http://localhost:1234/api/graphql")
	for _, want := range []string{"http://localhost:1234/api/graphql", "createWorktree", "sendAgentMessage", "EnterWorktree"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt should mention %q", want)
		}
	}
	if strings.Contains(prompt, "startWork") {
		t.Error("prompt must not mention the removed startWork mutation")
	}
}
