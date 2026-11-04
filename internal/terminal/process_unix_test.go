//go:build !windows

package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startWithPidFile runs command in a session (via /bin/sh) and returns the PID
// the command writes to $PIDFILE.
func startWithPidFile(t *testing.T, mgr *Manager, id, command string) (*Session, int) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	command = strings.ReplaceAll(command, "$PIDFILE", pidFile)

	sess, err := mgr.CreateWithCommand(id, dir, 80, 24, command)
	if err != nil {
		t.Fatalf("CreateWithCommand failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("bad pid file content %q", data)
			}
			return sess, pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pid file")
	return nil, 0
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func TestCloseTerminatesChildProcesses(t *testing.T) {
	mgr := NewManager(nil)
	defer mgr.Shutdown()

	// The child (like a dev server spawned by a run command) must not survive.
	_, childPID := startWithPidFile(t, mgr, "test-children",
		`sleep 300 & echo $! > $PIDFILE; wait`)
	if !processAlive(childPID) {
		t.Fatal("child process should be running before Close")
	}

	mgr.Close("test-children")

	if processAlive(childPID) {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatal("child process still alive after Close returned")
	}
}

func TestCloseKillsProcessesIgnoringSignals(t *testing.T) {
	oldGrace := terminateGracePeriod
	terminateGracePeriod = 300 * time.Millisecond
	defer func() { terminateGracePeriod = oldGrace }()

	mgr := NewManager(nil)
	defer mgr.Shutdown()

	// Ignored signal dispositions are inherited by the child, so neither the
	// shell nor sleep react to SIGHUP/SIGTERM — only the SIGKILL fallback works.
	sess, childPID := startWithPidFile(t, mgr, "test-stubborn",
		`trap '' HUP TERM; sleep 300 & echo $! > $PIDFILE; wait`)

	start := time.Now()
	mgr.Close("test-stubborn")
	elapsed := time.Since(start)

	if processAlive(childPID) {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatal("stubborn child process still alive after Close returned")
	}
	if sess.Alive() {
		t.Fatal("session should not be alive after Close")
	}
	if elapsed < terminateGracePeriod {
		t.Fatalf("Close returned after %s, before the grace period; were signals not ignored?", elapsed)
	}
	if elapsed > terminateGracePeriod+killGracePeriod+time.Second {
		t.Fatalf("Close took too long: %s", elapsed)
	}
}

func TestCloseAndBlock(t *testing.T) {
	mgr := NewManager(nil)
	defer mgr.Shutdown()

	_, shellChild := startWithPidFile(t, mgr, "ws", `sleep 300 & echo $! > $PIDFILE; wait`)
	_, runChild := startWithPidFile(t, mgr, "ws__run", `sleep 300 & echo $! > $PIDFILE; wait`)
	other, err := mgr.Create("other", os.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	release := mgr.CloseAndBlock("ws", "ws__run")

	for _, pid := range []int{shellChild, runChild} {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d still alive after CloseAndBlock returned", pid)
		}
	}
	if mgr.Get("ws") != nil || mgr.Get("ws__run") != nil {
		t.Fatal("closed sessions should be removed")
	}
	if mgr.Get("other") != other || !other.Alive() {
		t.Fatal("unrelated session should be untouched")
	}

	// While blocked, no session with these IDs can be (re)created.
	if _, _, err := mgr.GetOrCreate("ws", os.TempDir(), 80, 24); !errors.Is(err, ErrSessionBlocked) {
		t.Fatalf("GetOrCreate: expected ErrSessionBlocked, got %v", err)
	}
	if _, err := mgr.Create("ws", os.TempDir(), 80, 24); !errors.Is(err, ErrSessionBlocked) {
		t.Fatalf("Create: expected ErrSessionBlocked, got %v", err)
	}
	if _, err := mgr.CreateWithCommand("ws__run", os.TempDir(), 80, 24, "true"); !errors.Is(err, ErrSessionBlocked) {
		t.Fatalf("CreateWithCommand: expected ErrSessionBlocked, got %v", err)
	}

	release()
	release() // idempotent

	if _, _, err := mgr.GetOrCreate("ws", os.TempDir(), 80, 24); err != nil {
		t.Fatalf("GetOrCreate after release failed: %v", err)
	}
}

func TestCloseTerminatesInteractiveShellJobs(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	mgr := NewManager(nil)
	defer mgr.Shutdown()

	sess, err := mgr.Create("test-jobs", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// An interactive shell with job control puts background jobs into their own
	// process group; they still belong to the PTY session and must be stopped.
	pidFile := filepath.Join(t.TempDir(), "pid")
	if _, err := sess.Write([]byte("sleep 300 & echo $! > " + pidFile + "\n")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	var jobPID int
	deadline := time.Now().Add(10 * time.Second)
	for jobPID == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(data), "\n") {
			jobPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if jobPID == 0 {
		t.Fatal("timed out waiting for job pid")
	}

	mgr.Close("test-jobs")

	if processAlive(jobPID) {
		_ = syscall.Kill(jobPID, syscall.SIGKILL)
		t.Fatal("background job still alive after Close returned")
	}
}
