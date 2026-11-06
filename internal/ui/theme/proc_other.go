//go:build !unix

package theme

import "os/exec"

// isolateProcessGroup is a no-op where process groups are unavailable;
// cancellation kills nvim itself.
func isolateProcessGroup(cmd *exec.Cmd) {}
