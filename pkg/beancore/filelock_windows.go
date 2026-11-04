//go:build windows

package beancore

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows byte-range locks are mandatory, so lock a single byte far beyond
// any realistic end of file. This serializes lock holders without blocking
// ordinary reads and writes of the bean content.
const (
	lockOffsetLow  = 0
	lockOffsetHigh = 0x40000000
)

func lockFile(f *os.File) error {
	ol := &windows.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
}

func unlockFile(f *os.File) error {
	ol := &windows.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
