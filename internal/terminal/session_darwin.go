package terminal

import "golang.org/x/sys/unix"

// sessionMembers returns the PIDs of all live processes in session sid.
func sessionMembers(sid int) []int {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	var pids []int
	for i := range procs {
		p := &procs[i]
		pid := int(p.Proc.P_pid)
		if pid <= 0 || p.Proc.P_stat == sZOMB {
			continue
		}
		if s, err := unix.Getsid(pid); err == nil && s == sid {
			pids = append(pids, pid)
		}
	}
	return pids
}

// sZOMB is the process state of a zombie (see <sys/proc.h>).
const sZOMB = 5
