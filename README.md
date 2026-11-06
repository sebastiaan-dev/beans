![beans](https://github.com/user-attachments/assets/776f094c-f2c4-4724-9a0b-5b87e88bc50d)

[![License](https://img.shields.io/github/license/hmans/beans?style=for-the-badge)](LICENSE)
[![Release](https://img.shields.io/github/v/release/hmans/beans?style=for-the-badge)](https://github.com/hmans/beans/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/hmans/beans/test.yml?branch=main&label=tests&style=for-the-badge)](https://github.com/hmans/beans/actions/workflows/test.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hmans/beans?style=for-the-badge)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/hmans/beans?style=for-the-badge)](https://goreportcard.com/report/github.com/hmans/beans)

**Beans is an issue tracker for you, your team, and your coding agents.** Instead of tracking tasks in a separate application, Beans stores them right alongside your code. You can use the `beans` CLI to interact with your tasks, but more importantly, so can your favorite coding agent!

This gives your robot friends a juicy upgrade: now they get a complete view of your project, make suggestions for what to work on next, track their progress, create bug issues for problems they find, and more.

You've been programming all your life; now you get to be a product manager. Let's go! 🚀

## Announcement Trailer ✨

https://github.com/user-attachments/assets/dbe45408-d3ed-4681-a436-a5e3046163da

## Stability Warning ⚠️

Beans is still under heavy development, and its features and APIs may still change significantly. If you decide to use it now, please follow the release notes closely.

Since Beans emits its own prompt instructions for your coding agent, most changes will "just work"; but sometimes, we modify the schema of the underlying data files, which may require some manual migration steps. If you get caught by one of these changes, your agent will often be able to migrate your data for you:

```
The Beans data format has changed. Please migrate this project's beans to the new format.
```

## Features

- **Track tasks, bugs, features**, and more right alongside your code.
- **Plain old Markdown files** stored in a `.beans` directory in your project. Easy to version control, readable and editable by humans and machines alike!
- Use the `beans` CLI to create, list, view, update, and archive beans; but more importantly, **let your coding agent do it for you**!
- **Supercharge your robot friend** with full context about your project and its open tasks. A built-in **GraphQL query engine** allows your agent to get exactly the information it needs, keeping token use to a minimum.
- **Project memory**: Completed beans are archived and serve as project memory that your coding agent can query for context about past work.
- A beautiful **built-in** TUI for browsing and managing your beans from the terminal.
- Generates a **Markdown roadmap document** for your project from your data.

## Installation

We'll need to do three things:

1. Install the `beans` CLI tool.
2. Configure your project to use it.
3. Configure your coding agent to interact with it.

Either download Beans from the [Releases section](https://github.com/hmans/beans/releases), or install it via Homebrew:

```bash
brew install hmans/beans/beans
```

Alternatively, install directly via Go:

```bash
go install github.com/hmans/beans@latest
```

## Configure Your Project

Inside the root directory of your project, run:

```bash
beans init
```

This will create a `.beans/` directory and a `.beans.yml` configuration file at the project root. All of it is meant to be tracked in your version control system.

From this point onward, you can interact with your Beans through the `beans` CLI. To get a list of available commands:

```bash
beans help
```

But more importantly, you'll want to get your coding agent set up to use it. Let's dive in!

### Config and Data Locations

By default, `beans` searches upward from the current directory for `.beans.yml` and uses the data directory it configures. You can override both:

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Config file | `--config` | `BEANS_CONFIG` | nearest `.beans.yml` found searching upward |
| Data directory | `--beans-path` | `BEANS_PATH` | `beans.path` from the config |

Flags take precedence over environment variables. A config file set via `--config` or `BEANS_CONFIG` must exist; otherwise every command (including `beans prime`) fails with an error.

`beans prime` only prints its agent instructions for projects that opted in to Beans: when a `.beans.yml` is found, or when `--config`, `BEANS_CONFIG`, or `--beans-path` is given. `BEANS_PATH` alone doesn't enable it. To keep your beans outside the repository, point `BEANS_CONFIG` at their config:

```bash
export BEANS_CONFIG=~/my-beans/.beans.yml
```

### Workspaces with worktrunk

Beans UI (`beans serve`) runs each workspace in its own git worktree. By default (`worktree.backend: git`) beans creates them itself: branch `beans/<name>`, directory `~/.beans/worktrees/<project>/<name>` (or `worktree.path`), and only `beans/*` worktrees show up as workspaces.

If you use [worktrunk](https://worktrunk.dev) (`wt`), let it manage the worktrees instead:

```yaml
worktree:
  backend: worktrunk
```

`beans init` writes `backend: git` and tells you when `wt` is on your PATH; `beans check` shows the same hint. With the worktrunk backend:

- **Naming:** a workspace named `fix-login` gets the branch `fix-login` (no `beans/` prefix). Its directory comes from worktrunk's `worktree-path` template, e.g. `~/repos/myproject.fix-login`. `worktree.path` no longer decides where worktrees go; beans only keeps its per-workspace metadata (name, description, port) there.
- **All worktrees are workspaces:** every linked worktree of the repository is listed, however it was created (`wt switch -c`, `git worktree add`, beans). The main worktree is never listed. A workspace's ID is derived from its branch (`feat/login` → `feat-login`, like worktrunk's `sanitize` filter; detached worktrees keep their previous ID or use their directory name; collisions get a short hash suffix), and its default name is the branch.
- **External changes:** beans watches the repository's `.git/worktrees/` directory, so worktrees created or removed outside beans appear and disappear in Beans UI within a moment.
- **Hooks:** creating a workspace runs `wt switch --create <name> --base <worktree.base_ref> --no-hooks`, then worktrunk's creation hooks: `pre-start` (blocking), beans' own `worktree.setup` command, and `post-start` (in the background, as worktrunk does). Switch hooks (`pre-switch`, `post-switch`) don't run, because no shell is switching. The workspace's setup status covers `pre-start` and `worktree.setup`. Destroying a workspace stops its agent and terminals, then runs `wt remove <branch> --force --foreground`, so `pre-remove`/`post-remove` hooks run, uncommitted changes are discarded, and the branch is deleted only when it is merged.
- **Approvals:** beans never approves worktrunk project hooks for you (no `--yes`), and `wt` can't ask without a terminal. If a project hook isn't approved yet, the workspace's setup fails with a message saying so (and no hooks or `worktree.setup` run), and removal fails until you approve. Run `wt config approvals add` in the repository to review and approve the hooks, then `wt hook pre-start` / `wt hook post-start` in the new worktree.

`wt` must be on the PATH of `beans serve`; your own worktrunk configuration (including `WORKTRUNK_CONFIG_PATH`) applies.

### Starting a bean in a worktree from the terminal

`beans start <id> --worktree` (`-w`) is the terminal counterpart of Beans UI's Start Work: it creates a worktree for the bean (or reuses it if it already exists), copies the bean into the worktree's `.beans/` (including uncommitted edits from the main worktree) and sets it to `in-progress` there. The bean in the main worktree is left unchanged; the status change travels with the branch. On a rerun, a bean the worktree already changed is not overwritten.

```bash
beans start abc1 -w                                    # create/reuse the worktree, print its path
beans start abc1 -w --name login                       # pick the worktree/branch name
beans start abc1 -w -x claude -- "Work on bean abc1"   # then launch an agent in it
```

- **Name:** derived from the bean (`<id>-<slug>`, shortened to 40 characters, e.g. `abc1-add-login-form`) unless you pass `--name`.
- **worktrunk backend:** beans runs your normal `wt switch [--create] <name> --base <worktree.base_ref> --no-cd` in your terminal, so *all* your hooks run, including switch hooks (e.g. a tmux `post-switch` hook), and `wt` can prompt you to approve project hooks. For a new worktree, `worktree.setup` runs afterwards.
- **git backend:** the worktree is created exactly like Beans UI does (branch `beans/<name>` under `worktree.path`) and beans waits for `worktree.setup`.
- **Changing directory:** beans can't change your shell's directory, so `cd` into the printed path (or `wt switch <name>`), or use `--exec`/`-x` to run a program in the worktree once the bean is prepared; arguments after `--` are passed to it, and beans exits with its exit code. `--json` prints the worktree path in `path`.

A running `beans serve` picks up the new worktree and the in-progress bean by itself.

### Color Themes

By default the TUI (`beans tui`, `beans-tui`) and colored CLI output use your terminal's 16-color palette, so they follow your terminal theme. To make them look like your editor instead, pick a theme:

```yaml
tui:
  theme: nvim                # terminal (default) | nvim | nvim:<colorscheme> | file:<path>
  theme_background: true     # paint the theme's background behind the TUI (default for nvim/file themes)
```

Themes are a personal preference, so you can also set them for yourself without touching the project config: `BEANS_THEME` overrides `tui.theme` and `BEANS_THEME_BACKGROUND` (`true`/`false`) overrides `tui.theme_background`, e.g. `export BEANS_THEME=nvim` in your shell profile.

- **`nvim`** uses the colorscheme your Neovim config loads by default; **`nvim:<colorscheme>`** a specific one (e.g. `nvim:kanso-mist`). Beans runs `nvim --headless` once with your normal config (so plugin colorschemes load, e.g. with LazyVim), reads the highlight groups (`Normal`, `Comment`, `Function`, `Visual`, `FloatBorder`, `Diagnostic*`, …) and `g:terminal_color_0..15`, and caches the result in your user cache directory (`beans/themes/`). The cache is refreshed when the colorscheme argument, the nvim binary, or your Neovim config (`init.lua`/`init.vim`, `lazy-lock.json`, `lazyvim.json`, `lua/`, `colors/`, `plugin/`, `after/`) changes.
- **`file:<path>`** reads a palette file (YAML or JSON; relative paths are resolved against the directory of `.beans.yml`, or the current directory for `BEANS_THEME`). Every key is optional; missing roles are derived from the `ansi` slots and vice versa, so you can export a palette from any editor or terminal theme:

  ```yaml
  name: kanso-mist
  background: dark
  fg: "#c5c9c7"          # text
  bg: "#22262d"          # background
  muted: "#75797f"       # help, metadata
  border: "#393b44"      # borders, tree lines
  accent: "#8ba4b0"      # IDs, headings, focus
  selection: "#393b44"   # selected row background
  success: "#98bb6c"
  warning: "#dca561"
  danger: "#c34043"
  info: "#658594"
  hint: "#6a9589"
  ansi: ["#22262d", "#c4746e", "#8a9a7b", "#c4b28a", "#8ba4b0", "#a292a3", "#c5c9c7", "#a4a7a4",
         "#909398", "#e46876", "#87a987", "#e6c384", "#7fb4ca", "#938aa9", "#7aa89f", "#c5c9c7"]
  ```

With a theme, the color names used for statuses, types and priorities (`red`, `green`, `blue`, `purple`, `gray`, …) and palette indices (`0`–`15`) map to the theme's palette instead of the terminal's. Theme colors are true colors; on terminals without true color support they are approximated with the 256- or 16-color palette, and `NO_COLOR` or non-terminal output stays plain. With `theme_background: false` the TUI keeps your terminal's background and default text color and only uses the theme's accent colors.

If a theme can't be loaded (e.g. nvim is not installed or the colorscheme doesn't exist), beans prints a warning and falls back to terminal colors. `beans check` reports unknown or broken themes as errors, and a missing nvim as a warning.

## Agent Configuration

The most basic way to teach your agent about Beans is to simply add the following instruction to your `AGENTS.md`, `CLAUDE.md`, or equivalent file:

```
**IMPORTANT**: before you do anything else, run the `beans prime` command and heed its output.
```

Some agents provide mechanisms to automate this step:

### Claude Code

An official Beans plugin for Claude is in the works, but for the time being, please manually add the following hooks to your project's `.claude/settings.json` file:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "beans prime" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "command": "beans prime" }] }
    ]
  }
}
```

### OpenCode

Beans integrates with OpenCode via a plugin that injects task context into your sessions. To set it up, **copy the plugin** from [`.opencode/plugin/beans-prime.ts`](.opencode/plugin/beans-prime.ts) to your project's `.opencode/plugin/` directory (or `~/.opencode/plugin/` for global availability across all projects).

## Usage Hints

As a human, you can get an overview of the CLI's functionalities by running:

```bash
beans help
```

You might specifically be interested in the interactive TUI:

```bash
beans tui
```

### Example Workflows

**But the real power of Beans** comes from letting your coding agent manage your tasks for you.

Assuming you have integrated Beans into your coding agent correctly, it will already know how to create and manage beans for you. You can use the usual assortment of natural language inquiries. If you've just
added Beans to an existing project, you could try asking your agent to identify potential tasks and create beans for them:

```
Are there any tasks we should be tracking for this project? If so, please create beans for them.
```

If you already have some beans available, you can ask your agent to recommend what to work on next:

```
What should we work on next?
```

You can also specifically ask it to start working on a particular bean:

```
It's time to tackle myproj-123.
```

Consider that your agent will be just as capable to deal with beans as it is with code, so how about using it to quickly restructure your tasks?

```
Please inspect this project's beans and reorganize them into epics. Also please create 2-3 milestones to group these epics in a meaningful way.
```

You can also add Beans-specific instructions to your `AGENTS.md`, `CLAUDE.md` or equivalent file, for example:

```
When making a commit, include the relevant bean IDs in the commit message
```

## Contributing

This project currently does not accept contributions -- it's just way too early for that!
But if you do have suggestions or feedback, please feel free to open an issue.

## License

This project is licensed under the Apache-2.0 License. See the [LICENSE](LICENSE) file for details.

## Getting in Touch

If you have any questions, suggestions, or just want to say hi, feel free to reach out to me [on Bluesky](https://bsky.app/profile/hmans.dev), or [open an issue](https://github.com/hmans/beans/issues) in this repository.
