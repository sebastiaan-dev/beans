package beancore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchKeepsArchivedBean archives a bean while watching, through Core
// (as the archiveBean mutation does) and externally (as `beans archive` in
// another process does, including creating archive/ for the first time).
// The removal of the old file must not delete the moved bean.
func TestWatchKeepsArchivedBean(t *testing.T) {
	for _, external := range []bool{false, true} {
		name := "core"
		if external {
			name = "external"
		}
		t.Run(name, func(t *testing.T) {
			core, beansDir := setupTestCore(t)
			createTestBean(t, core, "arc1", "Archived", "completed")
			createTestBean(t, core, "arc2", "Also archived", "completed")
			if err := core.StartWatching(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = core.Unwatch() })
			time.Sleep(50 * time.Millisecond)

			for _, id := range []string{"arc1", "arc2"} {
				if external {
					b, _ := core.Get(id)
					dst := filepath.Join(beansDir, ArchiveDir, filepath.Base(b.Path))
					if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(filepath.Join(beansDir, b.Path), dst); err != nil {
						t.Fatal(err)
					}
				} else if err := core.Archive(id); err != nil {
					t.Fatal(err)
				}
			}

			for _, id := range []string{"arc1", "arc2"} {
				waitUntil(t, id+" archived", func() bool { return core.IsArchived(id) })
			}
			// Let any late events (the old files' removal) be processed.
			time.Sleep(3 * debounceDelay)
			for _, id := range []string{"arc1", "arc2"} {
				b, err := core.Get(id)
				if err != nil {
					t.Fatalf("%s disappeared after archiving: %v", id, err)
				}
				if !core.IsArchived(id) {
					t.Errorf("%s path = %s, want under archive/", id, b.Path)
				}
			}
		})
	}
}
