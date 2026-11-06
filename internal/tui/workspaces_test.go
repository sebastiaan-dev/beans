package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

// wsRepo is a git repository with a beans project for workspace tests.
type wsRepo struct {
	repo   string
	beans  string
	wtRoot string
	cfg    *config.Config
	core   *beancore.Core
	mgr    *worktree.Manager
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newWSRepo creates a repository with two committed beans and a loaded core
// and worktree manager (git backend).
func newWSRepo(t *testing.T) *wsRepo {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &wsRepo{repo: filepath.Join(base, "repo"), wtRoot: filepath.Join(base, "worktrees")}
	r.beans = filepath.Join(r.repo, ".beans")
	if err := os.MkdirAll(r.beans, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, b := range []*bean.Bean{
		{ID: "ws-aaaa", Slug: "login", Title: "Login form", Status: "todo", Type: "task"},
		{ID: "ws-bbbb", Slug: "other", Title: "Other task", Status: "todo", Type: "task"},
	} {
		content, err := b.Render()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.beans, bean.BuildFilename(b.ID, b.Slug)), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, r.repo, "init", "-q", "-b", "main")
	gitIn(t, r.repo, "add", "-A")
	gitIn(t, r.repo, "commit", "-q", "-m", "initial")

	r.cfg = config.DefaultWithPrefix("ws-")
	r.cfg.SetConfigDir(r.repo)
	r.core = beancore.New(r.beans, r.cfg)
	r.core.SetWarnWriter(nil)
	if err := r.core.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.core.UnwatchAllWorktrees(); _ = r.core.Close() })
	r.mgr = worktree.NewManager(r.repo, r.wtRoot, "main", "", worktree.WithFetchTimeout(0))
	return r
}

// startInWorkspace creates the workspace <name> and sets the bean to
// in-progress there, like `beans start --worktree` (without the commands
// package).
func (r *wsRepo) startInWorkspace(t *testing.T, id, name string) *worktree.Worktree {
	t.Helper()
	wt, err := r.mgr.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.core.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	wb := *b
	wb.Status = "in-progress"
	content, err := wb.Render()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, ".beans", b.Path), content, 0o644); err != nil {
		t.Fatal(err)
	}
	return wt
}

// drainSlow is drainCmd for commands that run git (slower than drainCmd's
// cutoff for timers).
func drainSlow(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("command did not finish")
	}
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drainSlow(t, app, c)
		}
		return
	}
	_, next := app.Update(msg)
	drainSlow(t, app, next)
}

// app returns a sized, loaded app with workspaces enabled.
func (r *wsRepo) app(t *testing.T, start WorkspaceStarter) *App {
	t.Helper()
	app := New(r.core, r.cfg)
	app.ws = newWorkspaces(r.mgr, start, r.cfg)
	app.list.ws, app.detail.ws = app.ws, app.ws
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	drainSlow(t, app, app.Init())
	return app
}

func TestWorkspaceBadges(t *testing.T) {
	withStableRendering(t)
	r := newWSRepo(t)
	wt := r.startInWorkspace(t, "ws-aaaa", "login")
	// Uncommitted changes in the workspace: the dirty marker shows.
	app := r.app(t, nil)

	if got := r.core.WorktreeForBean("ws-aaaa"); got != wt.Path {
		t.Fatalf("WorktreeForBean = %q, want %q (the TUI must watch the workspace)", got, wt.Path)
	}
	if got := r.core.WorktreeForBean("ws-bbbb"); got != "" {
		t.Errorf("unchanged bean is linked to %q", got)
	}
	b, _ := r.core.Get("ws-aaaa")
	if b.Status != "in-progress" {
		t.Errorf("runtime status = %q, want the workspace's in-progress", b.Status)
	}

	app.list.reveal("ws-aaaa")
	app.Update(nil)
	screen := stripAnsi(app.View())
	rows := strings.Split(screen, "\n")
	var row string
	for _, l := range rows {
		if strings.Contains(l, "Login form") {
			row = l
			break
		}
	}
	if !strings.Contains(row, "⎇ login ●") {
		t.Errorf("tree row lacks the workspace badge and dirty marker: %q", row)
	}
	for _, l := range rows {
		if strings.Contains(l, "Other task") && strings.Contains(l, "⎇") {
			t.Errorf("unlinked bean has a badge: %q", l)
		}
	}
	// Detail pane: badge in the title box and the path in the history.
	if !strings.Contains(screen, "Title · ws-aaaa") || strings.Count(screen, "⎇ login") < 2 {
		t.Errorf("detail pane lacks the workspace badge:\n%s", screen)
	}
	if !strings.Contains(screen, wt.Path) && !strings.Contains(screen, filepath.Base(wt.Path)) {
		t.Errorf("detail pane lacks the workspace path:\n%s", screen)
	}
	checkFrame(t, screen, 140, 40)

	// Committing in the workspace clears the dirty marker.
	gitIn(t, wt.Path, "commit", "-q", "-am", "start")
	drainSlow(t, app, app.ws.refreshCmd(r.core))
	if info := app.ws.lookup(wt.Path); info.dirty || info.ahead != 1 || len(info.beanIDs) != 1 {
		t.Errorf("after commit: %+v", info)
	}
	if badge := stripAnsi(workspaceBadge(r.core, app.ws, "ws-aaaa", "")); badge != "⎇ login" {
		t.Errorf("badge = %q", badge)
	}
}

func TestWorkspaceStartKey(t *testing.T) {
	r := newWSRepo(t)
	var started []string
	start := func(ctx context.Context, core *beancore.Core, mgr *worktree.Manager, id string, tio worktree.TerminalIO) (*WorkspaceStart, error) {
		started = append(started, id)
		wt := r.startInWorkspace(t, id, "w1")
		return &WorkspaceStart{BeanID: id, Name: "w1", Branch: wt.Branch, Path: wt.Path, Created: true, Message: "Started " + id + " in new worktree " + wt.Branch}, nil
	}
	app := r.app(t, start)
	app.list.reveal("ws-bbbb")

	_, cmd := app.Update(keyRunes("w"))
	if !strings.Contains(app.list.statusMessage, "Starting ws-bbbb") {
		t.Errorf("status while starting = %q", app.list.statusMessage)
	}
	drainSlow(t, app, cmd)
	if len(started) != 1 || started[0] != "ws-bbbb" {
		t.Fatalf("started = %v", started)
	}
	if msg := app.list.statusMessage; !strings.Contains(msg, "Started ws-bbbb") || !strings.Contains(msg, filepath.Join(r.wtRoot, "w1")) {
		t.Errorf("status = %q, want the result with the path", msg)
	}
	if r.core.WorktreeForBean("ws-bbbb") == "" {
		t.Error("the started bean is not linked to its workspace")
	}
	if len(app.ws.list) != 1 || app.ws.list[0].ID != "w1" {
		t.Errorf("workspaces = %+v", app.ws.list)
	}

	// Failures are shown.
	app.ws.start = func(context.Context, *beancore.Core, *worktree.Manager, string, worktree.TerminalIO) (*WorkspaceStart, error) {
		return nil, errors.New("boom")
	}
	_, cmd = app.Update(keyRunes("w"))
	drainSlow(t, app, cmd)
	if !strings.Contains(app.list.statusMessage, "boom") {
		t.Errorf("status = %q, want the error", app.list.statusMessage)
	}
}

func TestWorkspaceKeysWithoutGit(t *testing.T) {
	app, _ := setupListTestModel(t)
	runCmd(t, app, app.Init())
	for _, keys := range [][]string{{"w"}, {"W"}, {"g", "w"}} {
		for _, k := range keys {
			pressKey(t, app, k)
		}
		if app.list.statusMessage != workspacesUnavailable || app.state != viewList {
			t.Errorf("%v: status %q, state %v", keys, app.list.statusMessage, app.state)
		}
	}

	// Run leaves the feature off outside git repositories without creating
	// a manager.
	prev := registeredWorkspaceSupport()
	RegisterWorkspaceSupport(WorkspaceSupport{NewManager: func(*config.Config) (*worktree.Manager, error) {
		t.Error("NewManager called outside a git repository")
		return nil, errors.New("no")
	}})
	t.Cleanup(func() { RegisterWorkspaceSupport(prev) })
	cfg := config.Default()
	cfg.SetConfigDir(t.TempDir())
	stop := setupWorkspaces(app, app.core, cfg, func(tea.Msg) {})
	stop()
	if app.ws.enabled() {
		t.Error("workspaces enabled outside a git repository")
	}
}

func TestWorkspaceAgentKey(t *testing.T) {
	t.Setenv(config.AgentCommandEnvVar, "")
	r := newWSRepo(t)
	called := false
	app := r.app(t, func(context.Context, *beancore.Core, *worktree.Manager, string, worktree.TerminalIO) (*WorkspaceStart, error) {
		called = true
		return nil, errors.New("unexpected")
	})
	app.list.reveal("ws-aaaa")

	// Without an agent command, W explains how to configure one.
	pressKey(t, app, "W")
	if app.list.statusMessage != agentCommandHint || called {
		t.Errorf("status = %q, started %v", app.list.statusMessage, called)
	}
	r.cfg.TUI.AgentCommand = "x {{nope}}"
	pressKey(t, app, "W")
	if !strings.Contains(app.list.statusMessage, "unknown placeholder") || called {
		t.Errorf("status = %q, started %v", app.list.statusMessage, called)
	}

	// With one, W suspends the TUI (tea.Exec) to start the workspace and
	// run the agent.
	r.cfg.TUI.AgentCommand = "sh -c 'echo $BEANS_BEAN_ID {{bean_id}} \"$1\" > agent.out; pwd >> agent.out' sh {{bean_title}}"
	_, cmd := app.Update(keyRunes("W"))
	if cmd == nil {
		t.Fatal("W returned no command")
	}
}

func TestWorkspaceJobRunsAgent(t *testing.T) {
	r := newWSRepo(t)
	r.cfg.TUI.AgentCommand = "sh -c 'echo $BEANS_BEAN_ID {{bean_id}} \"$1\" > agent.out; pwd >> agent.out; exit 3' sh {{bean_title}}"
	t.Setenv(config.AgentCommandEnvVar, "")
	b, _ := r.core.Get("ws-aaaa")
	var wtPath string
	job := &workspaceJob{core: r.core, cfg: r.cfg, mgr: r.mgr, bean: b, agent: true, interactive: true,
		start: func(ctx context.Context, core *beancore.Core, mgr *worktree.Manager, id string, tio worktree.TerminalIO) (*WorkspaceStart, error) {
			wt := r.startInWorkspace(t, id, "agent")
			wtPath = wt.Path
			return &WorkspaceStart{BeanID: id, Path: wt.Path, Branch: wt.Branch}, nil
		}}
	var out strings.Builder
	job.SetStdin(strings.NewReader(""))
	job.SetStdout(&out)
	job.SetStderr(&out)
	if err := job.Run(); err != nil {
		t.Fatal(err)
	}
	msg := job.result(nil)
	if msg.err != nil || msg.program != "sh" || msg.agentErr == nil || !strings.Contains(msg.agentErr.Error(), "code 3") {
		t.Errorf("result = %+v", msg)
	}
	data, err := os.ReadFile(filepath.Join(wtPath, "agent.out"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws-aaaa ws-aaaa Login form\n" + wtPath + "\n"; string(data) != want {
		t.Errorf("agent output = %q, want %q", data, want)
	}

	// A failed start waits for enter so the user can read the error.
	job = &workspaceJob{core: r.core, cfg: r.cfg, mgr: r.mgr, bean: b, interactive: true,
		start: func(context.Context, *beancore.Core, *worktree.Manager, string, worktree.TerminalIO) (*WorkspaceStart, error) {
			return nil, errors.New("wt switch failed")
		}}
	out.Reset()
	job.SetStdin(strings.NewReader("\n"))
	job.SetStdout(&out)
	job.SetStderr(&out)
	_ = job.Run()
	if !strings.Contains(out.String(), "wt switch failed") || !strings.Contains(out.String(), "Press enter") {
		t.Errorf("output = %q", out.String())
	}
}

func TestWorkspacesOverlay(t *testing.T) {
	withStableRendering(t)
	r := newWSRepo(t)
	r.startInWorkspace(t, "ws-bbbb", "other-ws")
	if _, err := r.mgr.Create("empty"); err != nil {
		t.Fatal(err)
	}
	app := r.app(t, nil)
	app.list.reveal("ws-aaaa")

	pressKey(t, app, "g")
	_, cmd := app.Update(keyRunes("w"))
	drainSlow(t, app, cmd)
	if app.state != viewWorkspaces {
		t.Fatalf("state = %v, want the workspaces overlay", app.state)
	}
	screen := stripAnsi(app.View())
	for _, want := range []string{"Workspaces · 2 · git", "⎇ other-ws", "beans/other-ws", "● uncommitted", "beans: bbbb", "/worktrees/other-ws", "⎇ empty"} {
		if !strings.Contains(screen, want) {
			t.Errorf("overlay lacks %q:\n%s", want, screen)
		}
	}
	if lines := strings.Split(screen, "\n"); len(lines) != 40 {
		t.Errorf("screen has %d lines", len(lines))
	}

	// enter jumps to the workspace's bean.
	other, empty := workspaceIndex(t, app, "other-ws"), workspaceIndex(t, app, "empty")
	app.wsOverlay.cursor = other
	pressKey(t, app, "enter")
	if app.state != viewList {
		t.Fatalf("state = %v after enter", app.state)
	}
	if b := app.list.currentBean(); b == nil || b.ID != "ws-bbbb" {
		t.Errorf("cursor on %v, want ws-bbbb", b)
	}

	// A workspace without beans shows its path; esc closes.
	pressKey(t, app, "g")
	pressKey(t, app, "w")
	for range empty {
		pressKey(t, app, "j")
	}
	pressKey(t, app, "enter")
	if !strings.Contains(app.list.statusMessage, "no changed beans") {
		t.Errorf("status = %q", app.list.statusMessage)
	}
	pressKey(t, app, "g")
	pressKey(t, app, "w")
	pressKey(t, app, "esc")
	if app.state != viewList {
		t.Errorf("esc did not close the overlay")
	}
}

func workspaceIndex(t *testing.T, app *App, id string) int {
	t.Helper()
	for i, info := range app.ws.list {
		if info.ID == id {
			return i
		}
	}
	t.Fatalf("workspace %s not listed: %+v", id, app.ws.list)
	return -1
}

func TestWorkspaceExternalChanges(t *testing.T) {
	r := newWSRepo(t)
	app := r.app(t, nil)
	msgs := make(chan tea.Msg, 10)
	stop, err := r.mgr.WatchExternalChanges(func() { msgs <- workspacesChangedMsg{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// A worktree created outside beans shows up.
	r.startInWorkspace(t, "ws-aaaa", "outside")
	select {
	case msg := <-msgs:
		_, cmd := app.Update(msg)
		drainSlow(t, app, cmd)
	case <-time.After(10 * time.Second):
		t.Fatal("no change notification")
	}
	if len(app.ws.list) != 1 || r.core.WorktreeForBean("ws-aaaa") == "" {
		t.Errorf("workspaces = %+v, link %q", app.ws.list, r.core.WorktreeForBean("ws-aaaa"))
	}
}

func TestEditLinkedBeanEditsWorkspaceCopy(t *testing.T) {
	r := newWSRepo(t)
	wt := r.startInWorkspace(t, "ws-aaaa", "edit")
	app := r.app(t, nil)
	b, _ := r.core.Get("ws-aaaa")
	path, err := app.beanFilePath(b.ID, b.Path)
	if err != nil || path != filepath.Join(wt.Path, ".beans", b.Path) {
		t.Fatalf("beanFilePath = %q, %v; want the workspace copy", path, err)
	}
	mainBefore, _ := os.ReadFile(filepath.Join(r.beans, b.Path))

	// Simulate an editor session.
	app.editingBeanID = b.ID
	app.editingBeanModTime = time.Now().Add(-time.Minute)
	edited := *b
	edited.Body = "Edited in the workspace."
	content, _ := edited.Render()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	app.touchEditedBean()

	if got, _ := os.ReadFile(filepath.Join(r.beans, b.Path)); string(got) != string(mainBefore) {
		t.Errorf("main copy changed:\n%s", got)
	}
	got, _ := r.core.Get("ws-aaaa")
	if !strings.Contains(got.Body, "Edited in the workspace.") || got.Status != "in-progress" {
		t.Errorf("runtime bean = status %q body %q", got.Status, got.Body)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Edited in the workspace.") {
		t.Errorf("workspace copy = %s", data)
	}
}
