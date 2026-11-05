//go:build !windows

package agent

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

// installFakeClaude puts a fake `claude` on PATH whose streaming-mode body is
// the given shell snippet. $PIDDIR is where the snippet records PIDs.
func installFakeClaude(t *testing.T, body string) (pidDir string) {
	t.Helper()
	binDir := t.TempDir()
	pidDir = t.TempDir()
	script := "#!/bin/sh\nPIDDIR=" + pidDir + "\n" +
		"case \"$*\" in *stream-json*) ;; *) cat >/dev/null; exit 0 ;; esac\n" +
		"echo $$ > $PIDDIR/agent.pid.tmp && mv $PIDDIR/agent.pid.tmp $PIDDIR/agent.pid\n" +
		body
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return pidDir
}

func waitPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return 0
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func setStopTimeouts(t *testing.T, graceful, force time.Duration) {
	t.Helper()
	oldGraceful, oldForce := gracefulStopTimeout, forceStopTimeout
	gracefulStopTimeout, forceStopTimeout = graceful, force
	t.Cleanup(func() { gracefulStopTimeout, forceStopTimeout = oldGraceful, oldForce })
}

func TestStopAndBlock(t *testing.T) {
	// childStart starts a background child recording its PID in child.pid.
	const childStart = "sleep 300 &\necho $! > $PIDDIR/child.pid.tmp && mv $PIDDIR/child.pid.tmp $PIDDIR/child.pid\n"
	tests := []struct {
		name string
		body string
		// maxDuration bounds how long StopAndBlock may take.
		maxDuration time.Duration
		// childEscapes is set when the child is outside the agent's process
		// group and therefore isn't killed.
		childEscapes bool
	}{
		{
			// Exits on SIGINT but leaves its child behind (holding stdout):
			// the child must be killed too.
			name:        "graceful exit leaving a child",
			body:        childStart + "trap 'exit 0' INT\nwhile :; do sleep 1 & wait $!; done\n",
			maxDuration: 5 * time.Second,
		},
		{
			// Ignores SIGINT and stdin EOF; its child holds stdout open.
			name:        "ignores SIGINT",
			body:        "trap '' INT\n" + childStart + "wait\n",
			maxDuration: 5 * time.Second,
		},
		{
			// The child runs in its own process group (job control), so it
			// survives the group kill and keeps stdout open: StopAndBlock must
			// still return.
			name:         "child outside the process group holds stdout",
			body:         "trap '' INT\nset -m\n" + childStart + "wait\n",
			maxDuration:  5 * time.Second,
			childEscapes: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setStopTimeouts(t, 500*time.Millisecond, 500*time.Millisecond)
			pidDir := installFakeClaude(t, tt.body)

			m := NewManager("", nil)
			if err := m.SendMessage("ws", t.TempDir(), "hello", nil); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			agentPID := waitPIDFile(t, filepath.Join(pidDir, "agent.pid"))
			childPID := waitPIDFile(t, filepath.Join(pidDir, "child.pid"))
			t.Cleanup(func() {
				_ = syscall.Kill(agentPID, syscall.SIGKILL)
				_ = syscall.Kill(childPID, syscall.SIGKILL)
			})

			start := time.Now()
			release := m.StopAndBlock("ws")
			if d := time.Since(start); d > tt.maxDuration {
				t.Errorf("StopAndBlock took %v, want <= %v", d, tt.maxDuration)
			}

			if processAlive(agentPID) {
				t.Errorf("agent (pid %d) still alive after StopAndBlock", agentPID)
			}
			if !tt.childEscapes && processAlive(childPID) {
				t.Errorf("agent child (pid %d) still alive after StopAndBlock", childPID)
			}
			if s := m.GetSession("ws"); s == nil || s.Status != StatusIdle {
				t.Errorf("session should be idle after StopAndBlock, got %+v", s)
			}
			m.mu.RLock()
			nLive, nProc := len(m.live["ws"]), len(m.processes)
			m.mu.RUnlock()
			if nLive != 0 || nProc != 0 {
				t.Errorf("processes still tracked after StopAndBlock: live=%d processes=%d", nLive, nProc)
			}

			if err := m.SendMessage("ws", t.TempDir(), "blocked", nil); !errors.Is(err, ErrSessionBlocked) {
				t.Errorf("SendMessage while blocked = %v, want ErrSessionBlocked", err)
			}
			// Other sessions are unaffected.
			if m.isBlocked("other") {
				t.Error("unrelated session should not be blocked")
			}

			release()
			release() // idempotent
			if m.isBlocked("ws") {
				t.Error("session should be unblocked after release")
			}
		})
	}
}

// TestStopAndBlockPreventsPendingSpawn covers a spawn that is about to start
// when the block is placed: it must not start a process.
func TestStopAndBlockPreventsPendingSpawn(t *testing.T) {
	pidDir := installFakeClaude(t, "while :; do sleep 1; done\n")
	m := NewManager("", nil)

	session := m.newBaseSession("ws")
	session.WorkDir = t.TempDir()
	session.Messages = []Message{{Role: RoleUser, Content: "hello"}}
	session.Status = StatusRunning
	m.sessions["ws"] = session

	release := m.StopAndBlock("ws")
	defer release()
	m.spawnAndRun("ws", session) // returns immediately when blocked

	if _, err := os.Stat(filepath.Join(pidDir, "agent.pid")); !os.IsNotExist(err) {
		t.Errorf("no agent process should have been started (stat err = %v)", err)
	}
	if s := m.GetSession("ws"); s.Status != StatusIdle {
		t.Errorf("status = %v, want idle", s.Status)
	}
}
