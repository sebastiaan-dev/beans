package agent

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"
)

const describePrompt = `You are given the first message a user sent to an AI coding agent. Summarize what the user asked for in 3-8 words. Be specific and concrete — mention the actual feature, bug, or task. Output ONLY the summary, nothing else.

Examples of good summaries:
- "Fix auth token refresh bug"
- "Add dark mode to settings"
- "Refactor GraphQL subscription resolvers"
- "Workspace description generation"

User message:`

// buildDescribePrompt constructs the prompt for the description generator
// from the first user message. Exported for testing.
func buildDescribePrompt(message string) string {
	return describePrompt + "\n\n" + truncate(message, 500)
}

// cleanDescription trims whitespace and strips surrounding quotes from
// a raw model response.
func cleanDescription(raw string) string {
	desc := strings.TrimSpace(raw)
	desc = strings.Trim(desc, "\"'")
	return desc
}

// truncate returns s truncated to maxLen characters with "..." appended if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GenerateDescription runs a lightweight Claude call to summarize what a workspace
// is doing based on the first user message, using DefaultClaudeBinary.
// Returns the description or empty string on error.
func GenerateDescription(message string) string {
	return generateDescription(DefaultClaudeBinary, message)
}

// GenerateDescription is like the package-level GenerateDescription but uses
// the manager's configured Claude Code executable.
func (m *Manager) GenerateDescription(message string) string {
	return generateDescription(m.claudeBinary(), message)
}

func generateDescription(claudeBin, message string) string {
	out, err := runHaiku(claudeBin, buildDescribePrompt(message))
	if err != nil {
		log.Printf("[describe] failed to generate workspace description: %v", err)
		return ""
	}
	return cleanDescription(out)
}

// runHaiku runs a one-shot, tool-less Claude Haiku call and returns its output.
// The prompt is passed via stdin (not as CLI arg) to avoid leaking
// conversation content into `ps` output and to avoid OS argument length limits.
func runHaiku(claudeBin, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, claudeBin,
		"--print", "--model", "haiku",
		"--no-session-persistence",
		"--disable-slash-commands",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
	)
	cmd.Env = buildClaudeEnv()
	cmd.Stdin = strings.NewReader(prompt)

	out, err := cmd.Output()
	return string(out), err
}
