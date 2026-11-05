//go:build windows

package terminal

// On Windows there are no POSIX sessions or process groups. Closing the pseudo
// console (done by Session.terminate) terminates the processes attached to it,
// and the shell process itself is force-killed if it does not exit in time.

func terminateSession(pid int) {}

func killSession(pid int) {}

func sessionAlive(pid int) bool { return false }
