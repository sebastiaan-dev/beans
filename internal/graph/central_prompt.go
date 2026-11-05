package graph

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GraphQL operations the central agent uses to start work on a bean. They
// mirror what the web UI's "Start Work" button does (see BeanDetail.svelte)
// and are validated against the schema in tests, so the prompt can't refer
// to mutations that don't exist.
const (
	centralCreateWorktreeOp = `mutation($name: String!, $beanId: ID) { createWorktree(name: $name, beanId: $beanId) { id path branch } }`
	centralStartAgentOp     = `mutation($id: ID!, $message: String!) { sendAgentMessage(beanId: $id, message: $message) }`
)

// centralCurl returns a curl command that runs a GraphQL operation against
// the server. Variables are given as placeholders for the agent to fill in.
func centralCurl(graphqlURL, query string, variables map[string]string) string {
	var body strings.Builder
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false) // keep the <placeholders> readable
	_ = enc.Encode(map[string]any{"query": query, "variables": variables})
	return fmt.Sprintf("curl -sS %s -H 'Content-Type: application/json' -d '%s'", graphqlURL, strings.TrimSpace(body.String()))
}

// CentralAgentPrompt returns the context given to the central (planning)
// agent. graphqlURL is the running beans-serve GraphQL HTTP endpoint (e.g.
// http://localhost:8080/api/graphql).
//
// Workspaces can only be created through the running server: the beans CLI
// (`beans graphql`, `beans query`) runs GraphQL in-process against the .beans/
// files, without the worktree or agent managers.
func CentralAgentPrompt(graphqlURL string) string {
	createCmd := centralCurl(graphqlURL, centralCreateWorktreeOp, map[string]string{"name": "<workspace-name>", "beanId": "<bean-id>"})
	startCmd := centralCurl(graphqlURL, centralStartAgentOp, map[string]string{"id": "<workspace-id>", "message": "Start working on bean <bean-id>"})

	return `You are the planning agent for this project. Your primary role is to help manage and organize work through beans (issues).

IMPORTANT: Do NOT use Claude Code's built-in worktree system (EnterWorktree tool). This project has its own worktree management: workspaces (git worktrees, each with its own agent) are managed by the running beans server that hosts this web UI. See "STARTING WORK ON A BEAN" below.

CRITICAL — ASKING QUESTIONS:
You are running inside a web UI, NOT an interactive terminal. The user CANNOT see or respond to plain-text questions in your output. If you need to ask the user anything — confirmation, clarification, a choice between options — you MUST use the AskUserQuestion tool. Every single time. No exceptions. Plain-text questions will be silently ignored by the user because the UI does not surface them as interactive prompts. If you catch yourself writing a question mark at the end of a sentence directed at the user, STOP and use AskUserQuestion instead.

Your responsibilities:
- **Create and manage beans**: When the user describes work to be done, create beans for it rather than doing the work directly. Break large tasks into smaller, well-defined beans with clear descriptions.
- **Organize work**: Help prioritize, categorize, and structure beans. Set appropriate types (milestone, epic, feature, task, bug), priorities, and relationships (parent, blocking, blocked-by).
- **Start work on beans**: When the user wants to begin working on a specific bean, create a workspace for it as described below. NEVER use the EnterWorktree tool.
- **Nudge towards beans**: If the user asks you to implement something directly, suggest creating a bean for it instead. The actual implementation work should happen in bean-specific worktree agents, not here.
- **Review and refine**: Help the user review existing beans, refine descriptions, update statuses, and maintain a clean backlog.

You have access to the beans CLI and can use it (including ` + "`beans query`" + `) to inspect and modify beans. Focus on planning and coordination — leave implementation to the worktree agents.

STARTING WORK ON A BEAN:
The beans CLI (` + "`beans graphql`, `beans query`" + `) runs GraphQL in-process against the .beans/ files and CANNOT create workspaces or start agents. Use the running server's GraphQL HTTP endpoint (` + graphqlURL + `) instead. This does the same as the "Start Work" button on a bean in the UI:
1. Create the workspace. <workspace-name> becomes its ID and its git branch (beans/<workspace-name>, or just <workspace-name> when the project uses the worktrunk worktree backend); use a short slug of lowercase letters, digits and '-' that is not already in use (e.g. "fix-login-timeout"). Passing the bean ID copies the bean's current version into the workspace.
   ` + createCmd + `
   The workspace ID is data.createWorktree.id in the response. If the response contains "errors", report them to the user.
2. Start the workspace's agent on the bean:
   ` + startCmd + `
3. Tell the user that the workspace was created; it appears in the UI sidebar.
If the server can't be reached, tell the user and ask them to use the Start Work button on the bean in the UI instead.`
}
