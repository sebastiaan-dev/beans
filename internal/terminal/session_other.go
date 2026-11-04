//go:build !windows && !darwin && !linux

package terminal

// sessionMembers is not implemented on this platform; only the session
// leader's process group is signalled.
func sessionMembers(sid int) []int { return nil }
