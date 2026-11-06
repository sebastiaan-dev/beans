# What we're building

Beans is an agentic-first issue tracker. Issues ("beans") live as markdown files in a `.beans/` directory inside a project repo. The system has three interfaces:

- **CLI** (`beans` binary) — create, list, update, and query beans from the terminal
- **Terminal TUI** — Bubbletea-based interactive interface
- **Beans UI** (`beans serve`) — SvelteKit SPA served by an embedded Go HTTP server, communicating via GraphQL (queries, mutations, subscriptions over WebSocket)

The Beans UI is the primary development focus. It includes a backlog board, per-bean agent chat (spawning Claude Code processes), git worktree management, file change diffs, and terminal sessions.

# Commits

- Use conventional commit messages ("feat", "fix", "chore", etc.) when making commits.
- Include the relevant bean ID(s) in the commit message (please follow conventional commit conventions, e.g. `Refs: bean-xxxx`).
- Mark commits as "breaking" using the `!` notation when applicable (e.g., `feat!: ...`).
- When making commits, provide a meaningful commit message. The description should be a concise bullet point list of changes made.

# Pull Requests

- When we're working in a PR branch, make separate commits, and update the PR description to reflect the changes made.
- Include the relevant bean ID(s) in the PR title (please follow conventional commit conventions, e.g. `Refs: bean-xxxx`).

# Project Structure

Key packages:

- `pkg/bean/` — Bean data model, parsing, sorting, validation (no I/O)
- `pkg/beancore/` — Core engine: disk I/O, file watching, search indexing, worktree watching
- `internal/graph/` — GraphQL schema and resolvers (the API layer for both Beans UI and CLI)
- `internal/agent/` — Agent session manager: spawns Claude Code processes, parses JSONL output, pub/sub for real-time updates
- `internal/worktree/` — Git worktree lifecycle management
- `internal/terminal/` — PTY session management for embedded terminals
- `internal/commands/` — CLI command implementations (Cobra)
- `frontend/` — SvelteKit SPA (embedded into the Go binary via `//go:embed`)

## GraphQL

- When making changes to the GraphQL schema (`internal/graph/schema.graphqls`), run `mise codegen` to regenerate both backend (`generated.go`) and frontend (`frontend/src/lib/graphql/generated.ts`) types.
- When adding or changing frontend GraphQL operations (queries, mutations, subscriptions), update `frontend/src/lib/graphql/operations.graphql` and run `mise codegen`. Do NOT use inline `gql` strings — all operations must go through codegen for type safety.
- All CLI commands that interact with beans should internally use GraphQL queries/mutations against the local server.
- Subscriptions use WebSocket transport. The `beanChanged` subscription supports `includeInitial: true` to send a full snapshot on connect, avoiding race conditions between initial load and live updates.

## Build

- `mise build` to build a `./beans` executable
- The frontend is built and embedded into the Go binary at compile time

# GraphQL Subscriptions

- When a mutation removes or clears state (e.g., deleting a session), the subscription resolver must still send an explicit "empty" payload to the frontend. Never skip `nil` results with `continue` — the frontend needs to know the state changed.

# Worktree State Architecture

- Git worktrees are created **outside** the main repo, in `~/.beans/worktrees/<project-name>/`. This avoids nested repo confusion and accidental tool/search interference. The location is configurable via `worktree.path` in `.beans.yml` (with `worktree.backend: worktrunk`, worktrunk's path template decides instead; see "Worktree backends").
- `beans-serve` holds **runtime state** as the authoritative view of all beans. It initializes from main repo disk, then merges in changes from worktrees and the GraphQL API.
- The CLI in a worktree uses the **worktree's local `.beans/`** directory — it does NOT redirect to the main repo. This means worktree agents' bean changes travel with their PR.
- `beans-serve` watches active worktrees' `.beans/` dirs and merges file changes into runtime state as "dirty" (not persisted to main disk).
- Workspaces are created with the `createWorktree(name, beanId)` mutation (the UI's "Start Work" passes the bean ID and then starts the workspace agent with `sendAgentMessage`). The name becomes the worktree ID, directory and branch `beans/<name>` (worktrunk backend: branch `<name>`), so it must be a slug (`worktree.ValidateName`). With `beanId`, the bean as beans-serve currently sees it (including uncommitted UI edits) is written into the worktree's `.beans/` (`Core.WriteBeanToWorktree`); it is a normal change on the branch and travels with its PR. Creating a workspace does not change the bean's status.
- Bean updates made via the API to a bean that is linked to a worktree (modified there) are auto-routed to the worktree's copy (`Core.Update` → `saveToWorktree`), replacing that copy wherever it lives (e.g. `archive/`); main stays untouched and the bean stays dirty.
- Worktree and agent mutations only work against the **running server** (`beans-serve`, HTTP GraphQL at `http://localhost:<port>/api/graphql`). The CLI's `beans graphql`/`beans query` runs an in-process resolver without the worktree/agent managers ("worktree support not available"). The central agent's prompt (`graph.CentralAgentPrompt`) therefore tells it to use the server's HTTP endpoint.
- `removeWorktree` stops the workspace's run/terminal sessions (`terminal.Manager.CloseAndBlock`) and its agent (`agent.Manager.StopAndBlock`, which also kills the agent's process group) and waits for them to exit before running `git worktree remove`; no new sessions can start for the workspace meanwhile.
- The local **Integrate** action is an agent prompt (`internal/graph/agent_helpers.go`) that squashes the branch and runs `git merge --ff-only` in the main repo. Since Beans UI edits on main are uncommitted, the prompt first runs `integratePrepareMainScript`, which drops main's uncommitted changes only where they equal the branch's version and otherwise stops with a CONFLICT list. Uncommitted bean changes therefore don't disable Integrate; other uncommitted changes in main do. With the worktrunk backend the prompt is `worktrunkIntegratePrompt` instead (see "Worktree backends").
- When a PR merges and the bean file lands on main, the main watcher picks it up and the dirty flag clears.
- Each worktree has a **metadata file** (`<id>.meta.json`) stored as a sibling in the worktree root directory (e.g. `~/.beans/worktrees/<project>/<id>.meta.json`). This file persists per-worktree state that must survive server restarts: name, description, allocated port, last-active timestamp and the worktree's path. Use `worktree.Manager.SavePort`/`GetPort` etc. to read and write fields — don't access the file directly.

## Worktree backends

- `worktree.backend` (`pkg/config`) selects the `worktree.Backend` used by `worktree.Manager` (`internal/worktree/backend.go`); `serve.go` builds it with `worktree.NewBackend`. `git` (default) is the behaviour described above. `worktrunk` (`worktrunk_backend.go`) creates worktrees with `wt switch --create <name> --base <base_ref> --no-cd --no-hooks --format json` (branch = name, no `beans/` prefix; path from worktrunk's `worktree-path` template, so `worktree.path` only holds metadata) and removes them with `wt remove <branch> --foreground --force --format json` (pre/post-remove hooks run; branch deleted only if merged).
- Creation hooks in worktrunk mode run in `Manager.startSetupLocked`: `wt hook pre-start` (blocking), then `worktree.setup`, then `wt hook post-start` (wt backgrounds it). Switch hooks never run. Beans never passes `--yes`: unapproved project hooks (`wt hook show --format json` → `needs_approval`) make setup fail with a `worktree.ApprovalError` telling the user to run `wt config approvals add`; nothing runs then.
- Integrate in worktrunk mode (`worktrunkIntegratePrompt`, chosen via `actionContext.WorktreeBackend`, set by `Resolver.addWorktreeBackend`): the agent rebases, squashes with its own message (wt can't take one; its squash uses an LLM or lists subjects), runs `integratePrepareMainScript`, then `worktrunkMergeCommand`: `wt -C <worktree> merge <base branch> --no-commit --no-rebase --no-remove --format json </dev/null`. wt runs pre-merge hooks (e.g. tests) and fast-forwards the base branch (`localBaseBranch`: `origin/main` → `main`) including main's working tree; `--no-remove` keeps the worktree because the agent and terminals run in it (the user destroys the workspace from the UI). The prompt maps wt's failure messages (`wt*Message` constants in `integrate_script.go`, checked against real wt by `worktrunk_integrate_test.go`) to actions: approval needed → stop and tell the user to run `wt config approvals add`; pre-merge hook failed → fix, amend, retry; never `--yes`/`--no-hooks`.
- Listing always parses `git worktree list --porcelain` (`porcelain.go`; `wt list` is ~7x slower because it computes statuses). The main worktree (first entry), bare and prunable entries are never workspaces. In worktrunk mode every other linked worktree is a workspace; IDs come from `assignIDs` (`id.go`): branch as-is when it is a valid ID, `beans/<name>` → `<name>` (so git-backend IDs survive a backend switch), otherwise `SanitizeID(branch)` (`/` → `-`); detached worktrees keep their last ID (remembered in memory and via the `path` in metadata) or use their directory name; collisions get `-<hash>`.
- Never compute a worktree's path from its ID: use `Manager.Path(id)` or `List()` (worktrunk worktrees live wherever the template puts them).
- `Manager.WatchExternalChanges` watches `<git-common-dir>/worktrees/` (fsnotify, polling fallback) so worktrees created/removed outside beans show up; `serve.go`'s `newWorkspaceSyncer` then reconciles bean watchers (`Core.SyncWorktreeWatchers`) and workspace ports, and subscribers are notified. Metadata of worktrees removed outside beans is forgotten (`pruneRemoved`).

# Agent Architecture

- The central (main workspace) agent session uses ID `__central__` (defined as `CentralSessionID` in `internal/graph/resolver.go` and `MAIN_WORKSPACE_ID` in `frontend/src/lib/worktrees.svelte.ts`). These must stay in sync — the backend uses this ID to determine work directory and system prompt.
- Worktree agent sessions use the worktree ID as their session ID.

# Extra rules for our own beans/issues

- Use the `idea` tag for ideas and proposals.

# Testing

- Always write or update tests for the changes you make.

## Unit Tests

- Run all tests: `mise test`
- Run specific package: `go test ./internal/bean/`
- Use table-driven tests following Go conventions

## E2E Tests

- Write or update Playwright e2e tests for any web UI changes.
- Run e2e tests: `mise test:e2e`
- See `frontend/e2e/` for fixtures, page objects, and specs.

## Manual CLI Testing

- `mise beans` will compile and run the beans CLI. Use it instead of building and running `./beans` manually.
- When testing read-only functionality, feel free to use this project's own `.beans/` directory. But for anything that modifies data, create a separate test project directory. All commands support the `--beans-path` flag to specify a custom path.
