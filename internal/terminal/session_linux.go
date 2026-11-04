package terminal

import (
	"os"
	"strconv"
	"strings"
)

// sessionMembers returns the PIDs of all live processes in session sid.
func sessionMembers(sid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Format: pid (comm) state ppid pgrp session ... — comm may contain
		// spaces and parentheses, so parse from the last ')'.
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(s[i+1:])
		if len(fields) < 4 || fields[0] == "Z" {
			continue
		}
		if sess, err := strconv.Atoi(fields[3]); err == nil && sess == sid {
			pids = append(pids, pid)
		}
	}
	return pids
}
