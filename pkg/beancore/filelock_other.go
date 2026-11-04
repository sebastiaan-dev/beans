//go:build !unix && !windows

package beancore

import "os"

// Platforms without file locking support fall back to no cross-process locking.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
