//go:build unix

package theme

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup runs cmd in its own process group and makes
// cancellation kill the whole group, so processes spawned by Neovim plugins
// don't outlive a timed-out export.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
