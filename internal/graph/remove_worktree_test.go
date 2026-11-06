//go:build !windows

package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hmans/beans/internal/agent"
	"github.com/hmans/beans/internal/terminal"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
)

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("bad pid file %s: %q", path, data)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return 0
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// TestRemoveWorktreeStopsRunAndTerminal verifies that destroying a workspace
// stops its run process (including children) and shell terminal *before* the
// worktree is deleted, and that the sessions are gone afterwards.
func TestRemoveWorktreeStopsRunAndTerminal(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")

	repo := t.TempDir()
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "commit", "--allow-empty", "-m", "init")

	pidDir := t.TempDir()
	runPIDFile := filepath.Join(pidDir, "run.pid")
	shellPIDFile := filepath.Join(pidDir, "shell.pid")

	beansDir := filepath.Join(repo, ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	// The run process records whether its cwd still existed when it was stopped.
	cfg.Worktree.Run = `trap 'test -d "$PWD" && echo present > ` + pidDir + `/cwd-on-stop; exit 0' HUP TERM; ` +
		`sleep 300 & echo $! > ` + runPIDFile + `; wait`
	core := beancore.New(beansDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}

	termMgr := terminal.NewManager(nil)
	defer termMgr.Shutdown()
	r := &Resolver{
		CoreResolver: &beangraph.CoreResolver{Core: core},
		WorktreeMgr:  worktree.NewManager(repo, t.TempDir(), "main", "", worktree.WithFetchTimeout(0)),
		TerminalMgr:  termMgr,
		ProjectRoot:  repo,
	}
	ctx := context.Background()
	mr := r.Mutation()
	qr := r.Query()

	wt, err := mr.CreateWorktree(ctx, "run-test", nil)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if _, err := mr.StartRun(ctx, wt.ID); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	runChild := readPID(t, runPIDFile)

	// Shell terminal session for the workspace with a background child
	if _, err := mr.WriteTerminalInput(ctx, wt.ID, "sleep 300 & echo $! > "+shellPIDFile+"\n"); err != nil {
		t.Fatalf("WriteTerminalInput: %v", err)
	}
	shellChild := readPID(t, shellPIDFile)

	if running, _ := qr.IsRunning(ctx, wt.ID); !running {
		t.Fatal("run should be active before removal")
	}

	ok, err := mr.RemoveWorktree(ctx, wt.ID)
	if err != nil || !ok {
		t.Fatalf("RemoveWorktree: ok=%v err=%v", ok, err)
	}

	if running, _ := qr.IsRunning(ctx, wt.ID); running {
		t.Error("run should no longer be active after removal")
	}
	if termMgr.Get(wt.ID) != nil || termMgr.Get(wt.ID+RunSessionSuffix) != nil {
		t.Error("terminal sessions should be removed")
	}
	for name, pid := range map[string]int{"run child": runChild, "shell child": shellChild} {
		if pidAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("%s (pid %d) still alive after RemoveWorktree", name, pid)
		}
	}
	if _, err := os.Stat(filepath.Join(pidDir, "cwd-on-stop")); err != nil {
		t.Errorf("run process was not stopped before its worktree was deleted: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be removed, stat err = %v", err)
	}

	// The workspace's sessions can be created again once removal has finished
	// (e.g. for a new worktree reusing the ID).
	if _, _, err := termMgr.GetOrCreate(wt.ID, os.TempDir(), 80, 24); err != nil {
		t.Errorf("session ID should be unblocked after removal: %v", err)
	}
}

// writeFakeClaude installs a fake `claude` CLI on PATH. In streaming mode it
// ignores SIGINT and stdin EOF, starts a long-running child that inherits its
// stdout, and records both PIDs — the worst case for stopping an agent.
func writeFakeClaude(t *testing.T, pidDir string) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  *stream-json*) ;;
  *) cat >/dev/null; exit 0 ;;
esac
trap '' INT
sleep 300 &
echo $! > ` + pidDir + `/agent-child.pid
echo $$ > ` + pidDir + `/agent.pid
wait
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRemoveWorktreeStopsAgent verifies that destroying a workspace stops its
// agent process (and the agent's children) before the worktree is deleted,
// and that no new agent can be started for the workspace while it is removed.
func TestRemoveWorktreeStopsAgent(t *testing.T) {
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "commit", "--allow-empty", "-m", "init")

	pidDir := t.TempDir()
	writeFakeClaude(t, pidDir)

	beansDir := filepath.Join(repo, ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	core := beancore.New(beansDir, config.Default())
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}

	agentMgr := agent.NewManager("", nil)
	defer agentMgr.Shutdown()
	r := &Resolver{
		CoreResolver: &beangraph.CoreResolver{Core: core},
		WorktreeMgr:  worktree.NewManager(repo, t.TempDir(), "main", "", worktree.WithFetchTimeout(0)),
		AgentMgr:     agentMgr,
		ProjectRoot:  repo,
	}
	ctx := context.Background()
	mr := r.Mutation()

	wt, err := mr.CreateWorktree(ctx, "agent-test", nil)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if _, err := mr.SendAgentMessage(ctx, wt.ID, "hello", nil, nil); err != nil {
		t.Fatalf("SendAgentMessage: %v", err)
	}
	agentPID := readPID(t, filepath.Join(pidDir, "agent.pid"))
	childPID := readPID(t, filepath.Join(pidDir, "agent-child.pid"))
	defer func() {
		_ = syscall.Kill(agentPID, syscall.SIGKILL)
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	}()

	done := make(chan struct{})
	var ok bool
	var rmErr error
	go func() {
		defer close(done)
		ok, rmErr = mr.RemoveWorktree(ctx, wt.ID)
	}()

	// While the removal is in progress, the workspace's agent can't be restarted.
	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("RemoveWorktree returned before the agent (which ignores SIGINT) was stopped")
	default:
	}
	if err := agentMgr.SendMessage(wt.ID, wt.Path, "again", nil); err == nil {
		t.Error("SendMessage should fail while the workspace is being removed")
	}

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("RemoveWorktree did not return")
	}
	if rmErr != nil || !ok {
		t.Fatalf("RemoveWorktree: ok=%v err=%v", ok, rmErr)
	}

	for name, pid := range map[string]int{"agent": agentPID, "agent child": childPID} {
		if pidAlive(pid) {
			t.Errorf("%s (pid %d) still alive after RemoveWorktree", name, pid)
		}
	}
	if s := agentMgr.GetSession(wt.ID); s != nil && s.Status == agent.StatusRunning {
		t.Errorf("agent session still running after RemoveWorktree")
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be removed, stat err = %v", err)
	}

	// The ID is unblocked once the removal has finished.
	if err := os.Remove(filepath.Join(pidDir, "agent.pid")); err != nil {
		t.Fatal(err)
	}
	if err := agentMgr.SendMessage(wt.ID, t.TempDir(), "after", nil); err != nil {
		t.Errorf("SendMessage should work again after removal: %v", err)
	} else {
		// Wait for the new agent to start (with the fake claude still on PATH)
		// before shutting it down.
		readPID(t, filepath.Join(pidDir, "agent.pid"))
	}
	agentMgr.Shutdown()
}

// TestRemoveWorktreeClearsAgentConversation verifies that destroying a
// workspace forgets its agent conversation (messages and Claude session ID),
// so a new workspace with the same name starts fresh instead of resuming the
// old conversation, and that subscribers are sent an explicit empty session.
func TestRemoveWorktreeClearsAgentConversation(t *testing.T) {
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "commit", "--allow-empty", "-m", "init")

	beansDir := filepath.Join(repo, ".beans")
	if err := os.MkdirAll(beansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	core := beancore.New(beansDir, config.Default())
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}

	agentMgr := agent.NewManager(beansDir, nil)
	defer agentMgr.Shutdown()
	r := &Resolver{
		CoreResolver: &beangraph.CoreResolver{Core: core},
		WorktreeMgr:  worktree.NewManager(repo, t.TempDir(), "main", "", worktree.WithFetchTimeout(0)),
		AgentMgr:     agentMgr,
		ProjectRoot:  repo,
	}
	ctx := context.Background()
	mr := r.Mutation()

	wt, err := mr.CreateWorktree(ctx, "reuse-me", nil)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	conversation := filepath.Join(beansDir, ".conversations", wt.ID+".jsonl")
	seed := `{"type":"message","role":"user","content":"Old task"}` + "\n" +
		`{"type":"meta","session_id":"old-claude-session"}` + "\n"
	if err := os.WriteFile(conversation, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := agentMgr.GetSession(wt.ID); s == nil || s.SessionID != "old-claude-session" {
		t.Fatalf("seeded session not loaded: %+v", s)
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates, err := r.Subscription().AgentSessionChanged(subCtx, wt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial := <-updates; len(initial.Messages) != 1 {
		t.Fatalf("initial payload has %d messages, want 1", len(initial.Messages))
	}

	if ok, err := mr.RemoveWorktree(ctx, wt.ID); err != nil || !ok {
		t.Fatalf("RemoveWorktree: ok=%v err=%v", ok, err)
	}

	// Subscribers get an explicit empty session.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-updates:
			if len(s.Messages) == 0 {
				goto cleared
			}
		case <-deadline:
			t.Fatal("no empty session payload after RemoveWorktree")
		}
	}
cleared:
	if _, err := os.Stat(conversation); !os.IsNotExist(err) {
		t.Errorf("conversation file should be deleted, stat err = %v", err)
	}
	if s := agentMgr.GetSession(wt.ID); s != nil {
		t.Errorf("session after RemoveWorktree = %+v, want none", s)
	}

	// A new workspace with the same name starts without the old conversation.
	gitCmd(t, repo, "branch", "-D", wt.Branch)
	again, err := mr.CreateWorktree(ctx, "reuse-me", nil)
	if err != nil {
		t.Fatalf("CreateWorktree again: %v", err)
	}
	if again.ID != wt.ID {
		t.Fatalf("recreated ID = %q, want %q", again.ID, wt.ID)
	}
	if s := agentMgr.GetSession(again.ID); s != nil {
		t.Errorf("recreated workspace has session %+v, want none", s)
	}
}
