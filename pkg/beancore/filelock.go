package beancore

import (
	"fmt"
	"os"
)

// lockBeanFile acquires an exclusive, cross-process advisory lock associated
// with the bean file at path, blocking until it is available. The lock is used
// to make read-validate-write sequences (e.g. --if-match updates) atomic across
// concurrent beans processes. Bean files are rewritten in place, so the lock
// stays attached to the same file for its whole lifetime.
//
// If the file does not exist, no lock is taken and a no-op unlock is returned.
func lockBeanFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return func() {}, nil
		}
		return nil, fmt.Errorf("opening %s for locking: %w", path, err)
	}

	if err := lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}

	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}
