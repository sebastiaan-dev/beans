//go:build !windows

package terminal

import (
	"errors"
	"syscall"
)

// PTY sessions are started with setsid, so the shell is the leader of a new
// session and process group (sid == pgid == pid). Every process the shell starts
// stays in that session — including jobs that an interactive shell moves into
// their own process groups — unless it deliberately calls setsid itself. The
// helpers below therefore signal the shell's process group and every other
// process found in its session (where the platform lets us enumerate them).

// terminateSession politely asks every process in the session to exit.
// SIGHUP is what interactive shells (which ignore SIGTERM) react to; SIGTERM is
// the conventional stop signal for everything else.
func terminateSession(sid int) {
	signalSession(sid, syscall.SIGHUP)
	signalSession(sid, syscall.SIGTERM)
}

// killSession forcefully kills every process in the session.
func killSession(sid int) {
	signalSession(sid, syscall.SIGKILL)
}

// sessionAlive reports whether any process of the session still exists.
func sessionAlive(sid int) bool {
	if err := syscall.Kill(-sid, 0); err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	return len(sessionMembers(sid)) > 0
}

func signalSession(sid int, sig syscall.Signal) {
	_ = syscall.Kill(-sid, sig)
	for _, pid := range sessionMembers(sid) {
		_ = syscall.Kill(pid, sig)
	}
}
