package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
	"github.com/hmans/beans/pkg/safepath"
)

// Workspaces ------------------------------------------------------------------
//
// Beans changed in a git worktree ("workspace", see CLAUDE.md's Worktree State
// Architecture) show their worktree version with a ⎇ badge, like in
// beans-serve: the TUI watches the .beans/ directories of all workspaces and
// the repository for worktrees added or removed outside beans.
//
//	w    start the bean in its workspace (like `beans start <id> --worktree`)
//	W    the same, then launch tui.agent_command in the workspace
//	g w  list the workspaces
//
// Outside a git repository the feature is off.

// WorkspaceStart describes a bean started in a workspace.
type WorkspaceStart struct {
	BeanID   string
	Name     string // workspace name
	Branch   string
	Path     string
	Created  bool     // the worktree was created (not reused)
	Message  string   // e.g. "Started x in new worktree y"
	Warnings []string // e.g. other beans in progress
}

// WorkspaceStarter starts a bean in its workspace exactly like
// `beans start <id> --worktree`: it opens (creates or reuses) the bean's
// worktree, copies the bean into it and sets it to in-progress there. Commands
// the backend runs (e.g. worktrunk's `wt switch` and its hooks) use tio.
type WorkspaceStarter func(ctx context.Context, core *beancore.Core, mgr *worktree.Manager, beanID string, tio worktree.TerminalIO) (*WorkspaceStart, error)

// WorkspaceSupport connects the TUI to beans' worktree management, which
// lives in the commands package (it imports this package, so it registers
// itself with RegisterWorkspaceSupport).
type WorkspaceSupport struct {
	// NewManager returns a worktree manager configured like beans-serve's.
	NewManager func(*config.Config) (*worktree.Manager, error)
	// Start starts a bean in its workspace.
	Start WorkspaceStarter
}

var (
	workspaceSupportMu sync.Mutex
	workspaceSupport   WorkspaceSupport
)

// RegisterWorkspaceSupport enables workspaces in Run.
func RegisterWorkspaceSupport(s WorkspaceSupport) {
	workspaceSupportMu.Lock()
	defer workspaceSupportMu.Unlock()
	workspaceSupport = s
}

// RegisteredWorkspaceSupport returns the registered workspace support.
func RegisteredWorkspaceSupport() WorkspaceSupport { return registeredWorkspaceSupport() }

func registeredWorkspaceSupport() WorkspaceSupport {
	workspaceSupportMu.Lock()
	defer workspaceSupportMu.Unlock()
	return workspaceSupport
}

// workspaceInfo is a workspace with its cheap git status.
type workspaceInfo struct {
	worktree.Worktree
	dirty         bool     // uncommitted changes
	ahead, behind int      // commits vs the base ref
	beanIDs       []string // beans linked to the workspace (changed there)
}

// displayName is the workspace's name, or its ID (e.g. for git-backend
// worktrees created outside beans), or its directory name.
func (w workspaceInfo) displayName() string {
	switch {
	case w.Name != "":
		return w.Name
	case w.ID != "":
		return w.ID
	}
	return filepath.Base(w.Path)
}

// workspaces is the TUI's workspace state. It is only modified by the App's
// Update; the list and detail panes read it when rendering. A nil manager
// means the feature is off.
type workspaces struct {
	mgr   *worktree.Manager
	start WorkspaceStarter
	cfg   *config.Config

	list   []workspaceInfo
	byPath map[string]int // workspace path -> index into list
	err    error          // last listing error

	refreshing   bool
	refreshAgain bool
	starting     string // bean being started in the background (git backend)
}

func newWorkspaces(mgr *worktree.Manager, start WorkspaceStarter, cfg *config.Config) *workspaces {
	return &workspaces{mgr: mgr, start: start, cfg: cfg, byPath: map[string]int{}}
}

// enabled reports whether workspaces are available.
func (w *workspaces) enabled() bool { return w != nil && w.mgr != nil }

// lookup returns the workspace at path, falling back to one named after the
// directory if it was not listed (yet).
func (w *workspaces) lookup(path string) workspaceInfo {
	if w != nil {
		if i, ok := w.byPath[path]; ok {
			return w.list[i]
		}
		if i, ok := w.byPath[canonicalDir(path)]; ok {
			return w.list[i]
		}
	}
	return workspaceInfo{Worktree: worktree.Worktree{Path: path}}
}

// canonicalDir resolves symlinks in p, or returns it cleaned.
func canonicalDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// workspaceFor returns the workspace a bean is linked to (it was changed
// there), if any.
func workspaceFor(core *beancore.Core, ws *workspaces, id string) (workspaceInfo, bool) {
	if core == nil {
		return workspaceInfo{}, false
	}
	path := core.WorktreeForBean(id)
	if path == "" {
		return workspaceInfo{}, false
	}
	return ws.lookup(path), true
}

// workspaceBadge renders "⎇ name" (plus ● if the workspace has uncommitted
// changes) for a bean linked to a workspace, or "" for other beans.
func workspaceBadge(core *beancore.Core, ws *workspaces, id string, bg lipgloss.Color) string {
	info, ok := workspaceFor(core, ws, id)
	if !ok {
		return ""
	}
	badge := withBackground(accentStyle(), bg).Render(glyphs().Workspace + " " + info.displayName())
	if info.dirty {
		badge += withBackground(fg(colWarning()), bg).Render(glyphs().Dirty)
	}
	return badge
}

// workspaceTokens returns the tree row tokens of a bean's workspace badge.
func workspaceTokens(core *beancore.Core, ws *workspaces, id string, st func(lipgloss.Style) lipgloss.Style) []token {
	info, ok := workspaceFor(core, ws, id)
	if !ok {
		return nil
	}
	toks := []token{{text: glyphs().Workspace + " " + info.displayName(), style: st(accentStyle())}}
	if info.dirty {
		toks = append(toks, token{text: glyphs().Dirty, style: st(fg(colWarning()))})
	}
	return toks
}

// historyHeight is the height of the detail pane's History box: created,
// updated and etag, plus the workspace of a bean changed in one.
func (m detailModel) historyHeight() int {
	if m.bean != nil {
		if _, ok := workspaceFor(m.resolver.Core, m.ws, m.bean.ID); ok {
			return 6
		}
	}
	return 5
}

// beanFilePath returns the file of a bean to edit: its workspace copy for
// beans changed in a workspace (edits go there, like all updates of such
// beans), otherwise the file in the main .beans/ directory.
func (a *App) beanFilePath(id, relPath string) (string, error) {
	root := a.core.Root()
	if wt := a.core.WorktreeForBean(id); wt != "" {
		root = filepath.Join(wt, beancore.BeansDir)
	}
	return safepath.SafeJoin(root, relPath)
}

// readEditedBean parses an edited bean file as an update of stored.
func readEditedBean(path string, stored *bean.Bean) (*bean.Bean, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := bean.Parse(f)
	if err != nil {
		return nil, err
	}
	b.ID, b.Slug, b.Path = stored.ID, stored.Slug, stored.Path
	if b.CreatedAt == nil {
		b.CreatedAt = stored.CreatedAt
	}
	return b, nil
}

// Messages ---------------------------------------------------------------------

// workspacesChangedMsg is sent when worktrees were added or removed.
type workspacesChangedMsg struct{}

// workspacesLoadedMsg carries a fresh workspace listing.
type workspacesLoadedMsg struct {
	list []workspaceInfo
	err  error
}

// workspaceStartedMsg reports the result of w/W.
type workspaceStartedMsg struct {
	beanID   string
	res      *WorkspaceStart
	err      error
	program  string // agent program that was run ("" = none)
	agentErr error  // the agent could not be run or failed
}

// Refreshing -------------------------------------------------------------------

// refreshCmd lists the workspaces, syncs the bean watchers with them and
// reads their git status. Refreshes never overlap: one requested while
// another runs is done afterwards.
func (w *workspaces) refreshCmd(core *beancore.Core) tea.Cmd {
	if !w.enabled() {
		return nil
	}
	if w.refreshing {
		w.refreshAgain = true
		return nil
	}
	w.refreshing = true
	mgr := w.mgr
	return func() tea.Msg { return loadWorkspaces(core, mgr) }
}

// loadWorkspaces lists the workspaces and makes core watch their .beans/
// directories (and stop watching removed ones), so beans changed in a
// workspace show their workspace version.
func loadWorkspaces(core *beancore.Core, mgr *worktree.Manager) workspacesLoadedMsg {
	wts, err := mgr.ListBasic()
	if err != nil {
		return workspacesLoadedMsg{err: err}
	}
	paths := make([]string, len(wts))
	for i, wt := range wts {
		paths[i] = wt.Path
	}
	syncErr := core.SyncWorktreeWatchers(paths)

	infos := make([]workspaceInfo, len(wts))
	var wg sync.WaitGroup
	for i, wt := range wts {
		infos[i].Worktree = wt
		wg.Add(1)
		go func(info *workspaceInfo) {
			defer wg.Done()
			info.dirty = gitutil.HasChanges(info.Path)
			if base := mgr.BaseRef(); base != "" {
				info.ahead, info.behind, _ = gitutil.AheadBehind(info.Path, base)
			}
			info.beanIDs = core.BeansForWorktree(info.Path)
			sort.Strings(info.beanIDs)
		}(&infos[i])
	}
	wg.Wait()
	return workspacesLoadedMsg{list: infos, err: syncErr}
}

// apply stores a listing and returns a follow-up refresh if one was
// requested meanwhile.
func (w *workspaces) apply(msg workspacesLoadedMsg, core *beancore.Core) tea.Cmd {
	w.refreshing = false
	w.err = msg.err
	if msg.list != nil || msg.err == nil {
		w.list = msg.list
		w.byPath = make(map[string]int, 2*len(msg.list))
		for i, info := range msg.list {
			w.byPath[info.Path] = i
			w.byPath[canonicalDir(info.Path)] = i
		}
	}
	if w.refreshAgain {
		w.refreshAgain = false
		return w.refreshCmd(core)
	}
	return nil
}

// Starting work ----------------------------------------------------------------

// handleWorkspaceKey handles w and W in the browse view and reports whether
// key was one of them.
func (a *App) handleWorkspaceKey(key string) (tea.Cmd, bool) {
	if key != "w" && key != "W" {
		return nil, false
	}
	b := a.focusedBean()
	if b == nil {
		return nil, true
	}
	return a.startWorkspace(b, key == "W"), true
}

// workspacesUnavailable is the hint shown when workspaces are off.
const workspacesUnavailable = "Workspaces need a git repository"

// agentCommandHint explains how to configure the agent for W.
const agentCommandHint = `Set tui.agent_command in .beans.yml (or $BEANS_AGENT_COMMAND), e.g. claude "Work on bean {{bean_id}}"`

// startWorkspace starts b in its workspace (w) and optionally launches the
// agent there (W). Commands that may need the terminal (worktrunk's
// `wt switch` with its hooks and approval prompts, the agent) run with the
// TUI suspended; the git backend creates the worktree in the background.
func (a *App) startWorkspace(b *bean.Bean, withAgent bool) tea.Cmd {
	ws := a.ws
	if !ws.enabled() || ws.start == nil {
		a.list.statusMessage = workspacesUnavailable
		return nil
	}
	if withAgent {
		value, _ := a.config.AgentCommandSetting()
		if value == "" {
			a.list.statusMessage = agentCommandHint
			return nil
		}
		if err := a.config.ValidateAgentCommand(); err != nil {
			a.list.statusMessage = err.Error()
			return nil
		}
	}

	job := &workspaceJob{core: a.core, cfg: a.config, mgr: ws.mgr, start: ws.start, bean: b, agent: withAgent}
	if ws.starting != "" {
		a.list.statusMessage = fmt.Sprintf("Still starting %s in a workspace%s", ws.starting, glyphs().Loading)
		return nil
	}
	if !withAgent && ws.mgr.Backend() != worktree.BackendWorktrunk {
		ws.starting = b.ID
		a.list.statusMessage = fmt.Sprintf("Starting %s in a workspace%s", b.ID, glyphs().Loading)
		return func() tea.Msg {
			var out strings.Builder
			job.SetStdin(strings.NewReader(""))
			job.SetStdout(&out)
			job.SetStderr(&out)
			job.interactive = false
			_ = job.Run()
			return job.result(nil)
		}
	}
	job.interactive = true
	return tea.Exec(job, func(err error) tea.Msg { return job.result(err) })
}

// workspaceJob starts a bean in its workspace and optionally runs the agent
// there. It is a tea.ExecCommand, so it can run with the TUI suspended.
type workspaceJob struct {
	core  *beancore.Core
	cfg   *config.Config
	mgr   *worktree.Manager
	start WorkspaceStarter
	bean  *bean.Bean
	agent bool

	// interactive: the job owns the terminal; errors wait for enter so the
	// user can read them (and wt's output) before the TUI returns.
	interactive bool

	stdin          io.Reader
	stdout, stderr io.Writer

	res      *WorkspaceStart
	err      error
	program  string
	agentErr error
}

func (j *workspaceJob) SetStdin(r io.Reader)  { j.stdin = r }
func (j *workspaceJob) SetStdout(w io.Writer) { j.stdout = w }
func (j *workspaceJob) SetStderr(w io.Writer) { j.stderr = w }

// Run starts the bean in its workspace and runs the agent. Failures are
// recorded in the job, not returned.
func (j *workspaceJob) Run() error {
	if j.stdin == nil {
		j.stdin = os.Stdin
	}
	if j.stdout == nil {
		j.stdout = os.Stdout
	}
	if j.stderr == nil {
		j.stderr = os.Stderr
	}
	if j.interactive {
		// Clear what earlier suspended commands left on the main screen.
		fmt.Fprintf(j.stdout, "\x1b[H\x1b[2Jbeans: starting %s in its workspace%s\n", j.bean.ID, glyphs().Loading)
	}
	tio := worktree.TerminalIO{Stdin: j.stdin, Stdout: j.stdout, Stderr: j.stderr}
	j.res, j.err = j.start(context.Background(), j.core, j.mgr, j.bean.ID, tio)
	if j.err != nil {
		j.pause(j.err)
		return nil
	}
	if !j.agent {
		return nil
	}

	args, err := j.cfg.GetAgentCommand(config.AgentCommandVars{BeanID: j.bean.ID, BeanTitle: j.bean.Title, Path: j.res.Path})
	if err == nil && len(args) == 0 {
		err = errors.New(agentCommandHint)
	}
	if err != nil {
		j.agentErr = err
		j.pause(err)
		return nil
	}
	j.program = args[0]
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = j.res.Path
	cmd.Env = append(worktree.ChildEnv(), "BEANS_BEAN_ID="+j.bean.ID)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = j.stdin, j.stdout, j.stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The agent ran; its own output explains the failure.
			j.agentErr = fmt.Errorf("%s exited with code %d", args[0], exitErr.ExitCode())
			return nil
		}
		j.agentErr = fmt.Errorf("running %s in %s: %w", args[0], j.res.Path, err)
		j.pause(j.agentErr)
	}
	return nil
}

// pause shows err and waits for enter when the job owns the terminal.
func (j *workspaceJob) pause(err error) {
	if !j.interactive {
		return
	}
	fmt.Fprintf(j.stderr, "\nbeans: %v\n\nPress enter to return to beans%s", err, glyphs().Loading)
	_, _ = bufio.NewReader(j.stdin).ReadString('\n')
}

// result returns the job's outcome as a message.
func (j *workspaceJob) result(execErr error) workspaceStartedMsg {
	msg := workspaceStartedMsg{beanID: j.bean.ID, res: j.res, err: j.err, agentErr: j.agentErr, program: j.program}
	if msg.err == nil && execErr != nil {
		msg.err = execErr
	}
	return msg
}

// handleWorkspaceStarted shows the outcome of w/W and refreshes.
func (a *App) handleWorkspaceStarted(msg workspaceStartedMsg) tea.Cmd {
	a.ws.starting = ""
	switch {
	case msg.err != nil:
		a.list.statusMessage = fmt.Sprintf("Could not start %s in a workspace: %v", msg.beanID, msg.err)
	case msg.agentErr != nil:
		a.list.statusMessage = fmt.Sprintf("%s%sagent: %v", startedText(msg.res), glyphs().sep(), msg.agentErr)
	case msg.program != "":
		a.list.statusMessage = fmt.Sprintf("%s%s%s finished", startedText(msg.res), glyphs().sep(), msg.program)
	default:
		a.list.statusMessage = startedText(msg.res)
		if n := len(msg.res.Warnings); n > 0 {
			a.list.statusMessage += glyphs().sep() + msg.res.Warnings[0]
		}
	}
	return tea.Batch(a.ws.refreshCmd(a.core), a.list.loadBeans)
}

// startedText describes a started workspace with its path.
func startedText(res *WorkspaceStart) string {
	if res == nil {
		return "Started"
	}
	msg := res.Message
	if msg == "" {
		msg = fmt.Sprintf("Started %s in %s", res.BeanID, res.Branch)
	}
	return msg + glyphs().sep() + res.Path
}

// Workspaces overlay (g w) -----------------------------------------------------

// workspacesOverlay lists the workspaces.
type workspacesOverlay struct {
	cursor int
}

// openWorkspacesOverlay shows the workspace list and refreshes it.
func (a *App) openWorkspacesOverlay() tea.Cmd {
	if !a.ws.enabled() {
		a.list.statusMessage = workspacesUnavailable
		return nil
	}
	a.state = viewWorkspaces
	a.wsOverlay = workspacesOverlay{}
	return a.ws.refreshCmd(a.core)
}

func (o *workspacesOverlay) acceptsText() bool { return false }

// click jumps to workspace i.
func (o *workspacesOverlay) click(a *App, i int) tea.Cmd {
	if i < 0 || i >= len(a.ws.list) {
		return nil
	}
	o.cursor = i
	return o.update(a, tea.KeyMsg{Type: tea.KeyEnter})
}

// scroll moves the cursor.
func (o *workspacesOverlay) scroll(a *App, delta int) {
	o.cursor = max(0, min(o.cursor+delta, len(a.ws.list)-1))
}

// update handles keys in the workspace list.
func (o *workspacesOverlay) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	n := len(a.ws.list)
	switch key.String() {
	case "esc", "q":
		a.state = viewList
	case "up", "k":
		o.cursor = max(o.cursor-1, 0)
	case "down", "j":
		o.cursor = min(o.cursor+1, max(n-1, 0))
	case "y":
		if info, ok := a.selectedWorkspace(); ok {
			return func() tea.Msg { return copyBeanIDMsg{ids: []string{info.Path}} }
		}
	case "enter":
		info, ok := a.selectedWorkspace()
		if !ok {
			break
		}
		a.state = viewList
		if len(info.beanIDs) == 0 {
			a.list.statusMessage = fmt.Sprintf("%s %s has no changed beans%s%s", glyphs().Workspace, info.displayName(), glyphs().sep(), info.Path)
			break
		}
		a.focusTree()
		a.showBean(info.beanIDs[0], false)
		a.list.statusMessage = fmt.Sprintf("%s %s%s%s", glyphs().Workspace, info.displayName(), glyphs().sep(), info.Path)
	}
	return nil
}

func (a *App) selectedWorkspace() (workspaceInfo, bool) {
	if c := a.wsOverlay.cursor; c >= 0 && c < len(a.ws.list) {
		return a.ws.list[c], true
	}
	return workspaceInfo{}, false
}

// view renders the workspace list as a modal.
func (o *workspacesOverlay) view(a *App, rec hitRecorder) string {
	modalWidth := min(max(a.width-4, 30), 90)
	inner := modalWidth - 4
	var body []string
	switch {
	case a.ws.err != nil && len(a.ws.list) == 0:
		body = append(body, fg(colDanger()).Render(truncateText(a.ws.err.Error(), inner)))
	case len(a.ws.list) == 0 && a.ws.refreshing:
		body = append(body, mutedStyle().Render("Loading"+glyphs().Loading))
	case len(a.ws.list) == 0:
		body = append(body, mutedStyle().Render(truncateText("No workspaces. Press w on a bean to start one.", inner)))
	}
	maxRows := max((a.height-8)/2, 1)
	offset := max(0, o.cursor-maxRows+1)
	for i := offset; i < len(a.ws.list) && i < offset+maxRows; i++ {
		// Below the border, the title and a blank line; inside border and padding.
		rec.add(2, 3+len(body), inner, 2, hitTarget{kind: hitModalItem, index: i})
		body = append(body, a.workspaceLines(a.ws.list[i], i == o.cursor, inner)...)
	}

	backend := "git"
	if a.ws.mgr.Backend() == worktree.BackendWorktrunk {
		backend = "worktrunk"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(colAccent()).Render("Workspaces") +
		mutedStyle().Render(fmt.Sprintf("%s%d%s%s", glyphs().sep(), len(a.ws.list), glyphs().sep(), backend))
	footer := renderHints([]keyHint{{"enter", "jump to bean"}, {"y", "copy path"}, {"esc", "close"}}, nil, inner)
	lines := append(append([]string{title, ""}, body...), "", footer)
	return strings.Join(box{lines: lines, width: modalWidth, focused: true, padding: 1}.render(), "\n")
}

// workspaceLines renders a workspace as two lines: name, branch, status and
// changed beans, then its path.
func (a *App) workspaceLines(info workspaceInfo, selected bool, width int) []string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle().Bold(true)
	if selected {
		cursor = accentStyle().Render(glyphs().Cursor + " ")
		nameStyle = accentStyle().Bold(true)
	}
	line := cursor + nameStyle.Render(glyphs().Workspace+" "+info.displayName())
	if info.Branch != "" && info.Branch != info.displayName() {
		line += mutedStyle().Render("  " + info.Branch)
	} else if info.Detached {
		line += mutedStyle().Render("  (detached)")
	}
	var status []string
	if info.dirty {
		status = append(status, fg(colWarning()).Render(glyphs().Dirty+" uncommitted"))
	}
	if info.ahead > 0 {
		status = append(status, fmt.Sprintf("%s%d", glyphs().Ahead, info.ahead))
	}
	if info.behind > 0 {
		status = append(status, mutedStyle().Render(fmt.Sprintf("%s%d", glyphs().Behind, info.behind)))
	}
	if len(status) > 0 {
		line += "  " + strings.Join(status, " ")
	}
	if n := len(info.beanIDs); n > 0 {
		refs := make([]string, n)
		for i, id := range info.beanIDs {
			refs[i] = shortID(id, a.config.Beans.Prefix)
		}
		line += mutedStyle().Render("  beans: ") + strings.Join(refs, ", ")
	}
	return []string{
		truncateText(line, width),
		"    " + mutedStyle().Render(truncatePathLeft(info.Path, width-4)),
	}
}

// truncatePathLeft shortens a path to w cells by cutting its start, keeping
// the informative end ("…/worktrees/name").
func truncatePathLeft(p string, w int) string {
	if lipgloss.Width(p) <= w {
		return p
	}
	r := []rune(p)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[1:]
	}
	return glyphs().Ellipsis + string(r)
}

// Run integration ----------------------------------------------------------------

// setupWorkspaces enables workspaces for the project if it is a git
// repository and beans' worktree support is registered. The returned
// function stops watching.
func setupWorkspaces(app *App, core *beancore.Core, cfg *config.Config, send func(tea.Msg)) (stop func()) {
	support := registeredWorkspaceSupport()
	if support.NewManager == nil || cfg == nil {
		return func() {}
	}
	if _, err := worktree.GitCommonDir(cfg.ConfigDir()); err != nil {
		return func() {} // not a git repository: feature off
	}
	mgr, err := support.NewManager(cfg)
	if err != nil {
		app.list.statusMessage = fmt.Sprintf("Workspaces unavailable: %v", err)
		return func() {}
	}
	app.ws = newWorkspaces(mgr, support.Start, cfg)
	app.list.ws, app.detail.ws = app.ws, app.ws

	stopWatch, err := mgr.WatchExternalChanges(func() { send(workspacesChangedMsg{}) })
	if err != nil {
		return core.UnwatchAllWorktrees
	}
	return func() {
		stopWatch()
		core.UnwatchAllWorktrees()
	}
}
