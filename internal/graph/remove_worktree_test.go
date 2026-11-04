//go:build !windows

package graph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hmans/beans/internal/terminal"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

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
