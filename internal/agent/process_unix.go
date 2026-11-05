//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup starts the agent in its own process group so that the
// tool processes it spawns (shell commands, dev servers, ...) can be stopped
// together with it.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup force-kills every process in the agent's process group.
// The group outlives its leader as long as members exist, so this also reaps
// children left behind after the agent itself exited.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
