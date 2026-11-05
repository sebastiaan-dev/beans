package beancore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorktreeWatcher(t *testing.T) {
	t.Run("watches worktree beans dir and merges changes", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a bean in the main repo first
		createTestBean(t, core, "wt-test-1", "Original", "todo")

		// Start watching
		if err := core.StartWatching(); err != nil {
			t.Fatalf("StartWatching() error = %v", err)
		}
		defer core.Unwatch()

		// Create a fake worktree directory with a .beans/ subdir
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.MkdirAll(wtBeansDir, 0755); err != nil {
			t.Fatalf("failed to create worktree .beans dir: %v", err)
		}

		// Subscribe to events
		events, unsub := core.Subscribe()
		defer unsub()

		// Start watching the worktree
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Write a modified version of the bean in the worktree
		content := `---
title: Updated in Worktree
status: in-progress
type: task
---

Working on this in a worktree.
`
		beanPath := filepath.Join(wtBeansDir, "wt-test-1--original.md")
		if err := os.WriteFile(beanPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write worktree bean: %v", err)
		}

		// Wait for the event to propagate
		select {
		case batch := <-events:
			found := false
			for _, ev := range batch {
				if ev.BeanID == "wt-test-1" && ev.Type == EventUpdated {
					found = true
					if ev.Bean.Title != "Updated in Worktree" {
						t.Errorf("Title = %q, want %q", ev.Bean.Title, "Updated in Worktree")
					}
					if ev.Bean.Status != "in-progress" {
						t.Errorf("Status = %q, want %q", ev.Bean.Status, "in-progress")
					}
				}
			}
			if !found {
				t.Error("expected EventUpdated for wt-test-1")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for worktree bean change event")
		}

		// Bean should be dirty (came from worktree, not persisted to main)
		if !core.IsDirty("wt-test-1") {
			t.Error("bean should be dirty after worktree update")
		}

		// In-memory state should reflect the worktree's version
		got, err := core.Get("wt-test-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Title != "Updated in Worktree" {
			t.Errorf("Title = %q, want %q", got.Title, "Updated in Worktree")
		}
	})

	t.Run("does not crash when worktree has no .beans dir", func(t *testing.T) {
		core, _ := setupTestCore(t)

		wtDir := t.TempDir()
		// No .beans/ dir inside

		err := core.WatchWorktreeBeans(wtDir)
		if err != nil {
			t.Errorf("WatchWorktreeBeans() should return nil for missing .beans/ dir, got %v", err)
		}
	})

	t.Run("delete in worktree reverts to main-repo version", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a bean in the main repo
		createTestBean(t, core, "wt-del-1", "Original Title", "todo")

		// Create a worktree with a modified version of the bean
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		content := "---\ntitle: Modified in Worktree\nstatus: in-progress\ntype: task\n---\n"
		beanPath := filepath.Join(wtBeansDir, "wt-del-1--original-title.md")
		os.WriteFile(beanPath, []byte(content), 0644)

		// Start watching the worktree
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Wait for initial load to merge the worktree version
		time.Sleep(100 * time.Millisecond)

		// Verify the worktree version was merged
		got, err := core.Get("wt-del-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Title != "Modified in Worktree" {
			t.Errorf("Title = %q, want %q (worktree version should be active)", got.Title, "Modified in Worktree")
		}

		// Subscribe to events
		events, unsub := core.Subscribe()
		defer unsub()

		// Delete the bean from the worktree
		os.Remove(beanPath)

		// Should emit an Updated event reverting to the main-repo version
		select {
		case batch := <-events:
			found := false
			for _, ev := range batch {
				if ev.BeanID == "wt-del-1" && ev.Type == EventUpdated {
					found = true
					if ev.Bean.Title != "Original Title" {
						t.Errorf("reverted Title = %q, want %q", ev.Bean.Title, "Original Title")
					}
				}
			}
			if !found {
				t.Error("expected EventUpdated reverting to main-repo version")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for revert event")
		}

		// Bean should still exist and be reverted to main-repo version
		got, err = core.Get("wt-del-1")
		if err != nil {
			t.Fatalf("bean should still exist after worktree delete, got error: %v", err)
		}
		if got.Title != "Original Title" {
			t.Errorf("Title = %q, want %q", got.Title, "Original Title")
		}

		// Should no longer be dirty
		if core.IsDirty("wt-del-1") {
			t.Error("bean should not be dirty after reverting to main-repo version")
		}
	})

	t.Run("delete worktree-only bean removes from runtime", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a worktree with a bean that doesn't exist in main
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		content := "---\ntitle: Worktree Only\nstatus: todo\ntype: task\n---\n"
		beanPath := filepath.Join(wtBeansDir, "wt-only-1--worktree-only.md")
		os.WriteFile(beanPath, []byte(content), 0644)

		// Start watching — initial scan loads the bean
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Verify the bean was loaded
		got, err := core.Get("wt-only-1")
		if err != nil {
			t.Fatalf("bean should exist after initial load, got error: %v", err)
		}
		if got.Title != "Worktree Only" {
			t.Errorf("Title = %q, want %q", got.Title, "Worktree Only")
		}

		// Subscribe to events
		events, unsub := core.Subscribe()
		defer unsub()

		// Delete the bean from the worktree
		os.Remove(beanPath)

		// Should emit a Deleted event
		select {
		case batch := <-events:
			found := false
			for _, ev := range batch {
				if ev.BeanID == "wt-only-1" && ev.Type == EventDeleted {
					found = true
				}
			}
			if !found {
				t.Error("expected EventDeleted for worktree-only bean")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for delete event")
		}

		// Bean should no longer exist
		if _, err := core.Get("wt-only-1"); err != ErrNotFound {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("worktree links are set on initial load", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a worktree with a bean
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		content := "---\ntitle: Linked Bean\nstatus: todo\ntype: task\n---\n"
		beanPath := filepath.Join(wtBeansDir, "wt-link-1--linked-bean.md")
		os.WriteFile(beanPath, []byte(content), 0644)

		// Watch the worktree — initial load should set the link
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Verify the link was set
		if got := core.WorktreeForBean("wt-link-1"); got != wtDir {
			t.Errorf("WorktreeForBean() = %q, want %q", got, wtDir)
		}

		// Verify BeansForWorktree returns the bean
		ids := core.BeansForWorktree(wtDir)
		if len(ids) != 1 || ids[0] != "wt-link-1" {
			t.Errorf("BeansForWorktree() = %v, want [wt-link-1]", ids)
		}
	})

	t.Run("worktree links are cleared on unwatch", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a worktree with a bean
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		content := "---\ntitle: Unlink Bean\nstatus: todo\ntype: task\n---\n"
		os.WriteFile(filepath.Join(wtBeansDir, "wt-unlink-1--unlink-bean.md"), []byte(content), 0644)

		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}

		// Verify link exists
		if got := core.WorktreeForBean("wt-unlink-1"); got == "" {
			t.Fatal("expected worktree link to be set")
		}

		// Unwatch — link should be cleared
		core.UnwatchWorktreeBeans(wtDir)

		if got := core.WorktreeForBean("wt-unlink-1"); got != "" {
			t.Errorf("WorktreeForBean() after unwatch = %q, want empty", got)
		}
	})

	t.Run("update auto-routes to linked worktree", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create a bean in main
		createTestBean(t, core, "wt-auto-1", "Main Version", "todo")

		// Create a worktree with a modified version
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		content := "---\ntitle: WT Version\nstatus: in-progress\ntype: task\n---\n"
		os.WriteFile(filepath.Join(wtBeansDir, "wt-auto-1--wt-version.md"), []byte(content), 0644)

		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Update the bean via Core.Update() — should auto-route to worktree
		b, err := core.Get("wt-auto-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		b.Title = "Updated via Core"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		// Bean should still be dirty (written to worktree, not main)
		if !core.IsDirty("wt-auto-1") {
			t.Error("bean should be dirty after auto-routed update")
		}

		// Verify the file was written to the worktree
		entries, err := os.ReadDir(wtBeansDir)
		if err != nil {
			t.Fatalf("ReadDir() error = %v", err)
		}
		found := false
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".md" {
				found = true
			}
		}
		if !found {
			t.Error("expected updated bean file in worktree .beans/ dir")
		}
	})

	t.Run("rebase with identical beans does not link them", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create beans in main with explicit content (stable timestamps)
		bean1Content := "---\ntitle: Bean One\nstatus: todo\ntype: task\ncreated_at: 2025-01-01T00:00:00Z\nupdated_at: 2025-01-01T00:00:00Z\n---\n"
		os.WriteFile(filepath.Join(core.Root(), "rebase-1--bean-one.md"), []byte(bean1Content), 0644)
		bean2Content := "---\ntitle: Bean Two\nstatus: todo\ntype: task\ncreated_at: 2025-01-01T00:00:00Z\nupdated_at: 2025-01-01T00:00:00Z\n---\n"
		os.WriteFile(filepath.Join(core.Root(), "rebase-2--bean-two.md"), []byte(bean2Content), 0644)
		// Reload so core picks them up
		core.Load()

		// Start watching so fsnotify picks up changes
		if err := core.StartWatching(); err != nil {
			t.Fatalf("StartWatching() error = %v", err)
		}
		defer core.Unwatch()

		// Create a worktree with one modified bean and one identical to main
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		os.MkdirAll(wtBeansDir, 0755)

		// Modified bean — should be linked
		modifiedContent := "---\ntitle: Bean One Modified\nstatus: in-progress\ntype: task\ncreated_at: 2025-01-01T00:00:00Z\nupdated_at: 2025-01-01T00:00:00Z\n---\n"
		os.WriteFile(filepath.Join(wtBeansDir, "rebase-1--bean-one.md"), []byte(modifiedContent), 0644)

		// Identical content — should NOT be linked (simulates rebase pulling in main's version)
		os.WriteFile(filepath.Join(wtBeansDir, "rebase-2--bean-two.md"), []byte(bean2Content), 0644)

		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatalf("WatchWorktreeBeans() error = %v", err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// Modified bean should be linked
		if got := core.WorktreeForBean("rebase-1"); got != wtDir {
			t.Errorf("WorktreeForBean(rebase-1) = %q, want %q", got, wtDir)
		}

		// Identical bean should NOT be linked
		if got := core.WorktreeForBean("rebase-2"); got != "" {
			t.Errorf("WorktreeForBean(rebase-2) = %q, want empty (identical to main)", got)
		}

		// Now simulate a rebase: write the main version of rebase-1 into the worktree
		// (i.e., the modification was reverted by the rebase)
		events, unsub := core.Subscribe()
		defer unsub()

		os.WriteFile(filepath.Join(wtBeansDir, "rebase-1--bean-one.md"), []byte(bean1Content), 0644)

		// Wait for the watcher to process the change
		select {
		case <-events:
			// Event received
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for rebase event")
		}

		// After the "rebase", rebase-1 should no longer be linked
		if got := core.WorktreeForBean("rebase-1"); got != "" {
			t.Errorf("WorktreeForBean(rebase-1) after rebase = %q, want empty", got)
		}

		// And should no longer be dirty
		if core.IsDirty("rebase-1") {
			t.Error("rebase-1 should not be dirty after reverting to main version")
		}
	})

	t.Run("UnwatchAllWorktrees stops all watchers", func(t *testing.T) {
		core, _ := setupTestCore(t)

		// Create two worktrees
		for i := 0; i < 2; i++ {
			wtDir := t.TempDir()
			wtBeansDir := filepath.Join(wtDir, BeansDir)
			os.MkdirAll(wtBeansDir, 0755)
			if err := core.WatchWorktreeBeans(wtDir); err != nil {
				t.Fatalf("WatchWorktreeBeans() error = %v", err)
			}
		}

		core.UnwatchAllWorktrees()

		// Verify no worktree watchers remain
		core.mu.RLock()
		count := len(core.worktreeWatchers)
		core.mu.RUnlock()

		if count != 0 {
			t.Errorf("expected 0 worktree watchers after UnwatchAll, got %d", count)
		}
	})
}

func TestWriteBeanToWorktree(t *testing.T) {
	readFile := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", path, err)
		}
		return string(data)
	}
	render := func(t *testing.T, core *Core, id string) string {
		t.Helper()
		b, err := core.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
		content, err := b.Render()
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		return string(content)
	}
	// newWorktree simulates a worktree created from the base ref: a copy of
	// the main .beans/ directory as it was at that point.
	newWorktree := func(t *testing.T, mainBeansDir string) (string, string) {
		t.Helper()
		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.CopyFS(wtBeansDir, os.DirFS(mainBeansDir)); err != nil {
			t.Fatalf("CopyFS() error = %v", err)
		}
		return wtDir, wtBeansDir
	}

	t.Run("overwrites a stale worktree copy with the runtime version", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		b := createTestBean(t, core, "sync-1", "Sync Me", "todo")
		wtDir, wtBeansDir := newWorktree(t, beansDir)

		// Edit made in the UI after the base ref was cut (uncommitted on main).
		b.Body = "Edited just before Start Work"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		changed, err := core.WriteBeanToWorktree("sync-1", wtDir)
		if err != nil {
			t.Fatalf("WriteBeanToWorktree() error = %v", err)
		}
		if !changed {
			t.Error("changed = false, want true")
		}
		got := readFile(t, filepath.Join(wtBeansDir, "sync-1--sync-me.md"))
		if want := render(t, core, "sync-1"); got != want {
			t.Errorf("worktree bean =\n%s\nwant\n%s", got, want)
		}
		if core.IsDirty("sync-1") || core.WorktreeForBean("sync-1") != "" {
			t.Error("writing to the worktree must not mark the bean dirty or linked")
		}
	})

	t.Run("writes runtime-only state", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		b := createTestBean(t, core, "sync-2", "Runtime", "todo")
		wtDir, wtBeansDir := newWorktree(t, beansDir)

		b.Status = "in-progress"
		if err := core.Update(b, nil, WithPersist(false)); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if _, err := core.WriteBeanToWorktree("sync-2", wtDir); err != nil {
			t.Fatalf("WriteBeanToWorktree() error = %v", err)
		}
		if got := readFile(t, filepath.Join(wtBeansDir, "sync-2--runtime.md")); !strings.Contains(got, "status: in-progress") {
			t.Errorf("worktree bean missing runtime status:\n%s", got)
		}
	})

	t.Run("creates beans missing from the worktree", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		wtDir, wtBeansDir := newWorktree(t, beansDir)
		createTestBean(t, core, "sync-3", "Never Committed", "todo")

		changed, err := core.WriteBeanToWorktree("sync-3", wtDir)
		if err != nil || !changed {
			t.Fatalf("WriteBeanToWorktree() = %v, %v; want true, nil", changed, err)
		}
		if got, want := readFile(t, filepath.Join(wtBeansDir, "sync-3--never-committed.md")), render(t, core, "sync-3"); got != want {
			t.Errorf("worktree bean =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("leaves identical copies untouched", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		createTestBean(t, core, "sync-4", "Same", "todo")
		// Non-canonical formatting with identical content must not be rewritten.
		mainPath := filepath.Join(beansDir, "sync-4--same.md")
		original := readFile(t, mainPath)
		wtDir, wtBeansDir := newWorktree(t, beansDir)
		wtPath := filepath.Join(wtBeansDir, "sync-4--same.md")
		reordered := strings.Replace(original, "status: todo\ntype: task\n", "type: task\nstatus: todo\n", 1)
		if reordered == original {
			t.Fatalf("unexpected bean rendering:\n%s", original)
		}
		if err := os.WriteFile(wtPath, []byte(reordered), 0644); err != nil {
			t.Fatal(err)
		}
		before := readFile(t, wtPath)

		changed, err := core.WriteBeanToWorktree("sync-4", wtDir)
		if err != nil {
			t.Fatalf("WriteBeanToWorktree() error = %v", err)
		}
		if changed {
			t.Error("changed = true, want false for identical content")
		}
		if after := readFile(t, wtPath); after != before {
			t.Errorf("identical worktree copy was rewritten:\n%q\nwant\n%q", after, before)
		}
	})

	t.Run("replaces copies under a stale filename", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		b := createTestBean(t, core, "sync-5", "Old Title", "todo")
		wtDir, wtBeansDir := newWorktree(t, beansDir)

		// Rename the bean's file on main (new slug).
		newPath := "sync-5--new-title.md"
		if err := os.Rename(filepath.Join(beansDir, b.Path), filepath.Join(beansDir, newPath)); err != nil {
			t.Fatal(err)
		}
		b.Path = newPath
		b.Slug = "new-title"
		b.Title = "New Title"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		if _, err := core.WriteBeanToWorktree("sync-5", wtDir); err != nil {
			t.Fatalf("WriteBeanToWorktree() error = %v", err)
		}
		if got := findBeanFilesIn(wtBeansDir, "sync-5"); len(got) != 1 || got[0] != filepath.Join(wtBeansDir, newPath) {
			t.Errorf("worktree copies = %v, want only %s", got, newPath)
		}
	})

	t.Run("keeps archived beans in archive", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		createTestBean(t, core, "sync-6", "Archived", "completed")
		wtDir, wtBeansDir := newWorktree(t, beansDir)
		if err := core.Archive("sync-6"); err != nil {
			t.Fatalf("Archive() error = %v", err)
		}

		if _, err := core.WriteBeanToWorktree("sync-6", wtDir); err != nil {
			t.Fatalf("WriteBeanToWorktree() error = %v", err)
		}
		want := filepath.Join(wtBeansDir, ArchiveDir, "sync-6--archived.md")
		if got := findBeanFilesIn(wtBeansDir, "sync-6"); len(got) != 1 || got[0] != want {
			t.Errorf("worktree copies = %v, want only %s", got, want)
		}
	})

	t.Run("unknown bean", func(t *testing.T) {
		core, _ := setupTestCore(t)
		if _, err := core.WriteBeanToWorktree("nope", t.TempDir()); err != ErrNotFound {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("watching afterwards does not link the synced bean", func(t *testing.T) {
		core, beansDir := setupTestCore(t)
		b := createTestBean(t, core, "sync-7", "Watched", "todo")
		wtDir, _ := newWorktree(t, beansDir)
		b.Body = "uncommitted edit"
		if err := core.Update(b, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := core.WriteBeanToWorktree("sync-7", wtDir); err != nil {
			t.Fatal(err)
		}
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)
		if core.IsDirty("sync-7") || core.WorktreeForBean("sync-7") != "" {
			t.Error("synced bean must not be dirty or linked after initial worktree scan")
		}
	})
}

func TestMainCatchesUpWithWorktree(t *testing.T) {
	// waitFor polls until cond holds or fails the test.
	waitFor := func(t *testing.T, what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	setup := func(t *testing.T) (*Core, string, string, string) {
		t.Helper()
		core, beansDir := setupTestCore(t)
		if err := core.StartWatching(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { core.Unwatch() })

		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.MkdirAll(wtBeansDir, 0755); err != nil {
			t.Fatal(err)
		}
		// A bean created by an agent in the worktree.
		content := "---\ntitle: Agent Bean\nstatus: todo\ntype: task\n---\n\nFiled by the agent.\n"
		if err := os.WriteFile(filepath.Join(wtBeansDir, "merge-1--agent-bean.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { core.UnwatchWorktreeBeans(wtDir) })
		if core.WorktreeForBean("merge-1") != wtDir || !core.IsDirty("merge-1") {
			t.Fatal("expected the worktree bean to be dirty and linked")
		}
		return core, beansDir, wtDir, content
	}

	t.Run("merging the worktree bean into main clears the link", func(t *testing.T) {
		core, beansDir, wtDir, content := setup(t)

		// The branch is merged: the identical file lands on main.
		if err := os.WriteFile(filepath.Join(beansDir, "merge-1--agent-bean.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "link to clear", func() bool { return core.WorktreeForBean("merge-1") == "" })
		if core.IsDirty("merge-1") {
			t.Error("bean should no longer be dirty once it is on main")
		}

		// Edits now go to main, not into the worktree.
		b, err := core.Get("merge-1")
		if err != nil {
			t.Fatal(err)
		}
		b.Body = "Edited after merge"
		if err := core.Update(b, nil); err != nil {
			t.Fatal(err)
		}
		mainData, _ := os.ReadFile(filepath.Join(beansDir, "merge-1--agent-bean.md"))
		if !strings.Contains(string(mainData), "Edited after merge") {
			t.Errorf("edit was not written to main:\n%s", mainData)
		}
		wtData, _ := os.ReadFile(filepath.Join(wtDir, BeansDir, "merge-1--agent-bean.md"))
		if strings.Contains(string(wtData), "Edited after merge") {
			t.Error("edit must not be routed into the merged worktree")
		}
	})

	t.Run("main lagging behind the worktree keeps the worktree version", func(t *testing.T) {
		core, beansDir, wtDir, _ := setup(t)

		older := "---\ntitle: Agent Bean\nstatus: draft\ntype: task\n---\n"
		if err := os.WriteFile(filepath.Join(beansDir, "merge-1--agent-bean.md"), []byte(older), 0644); err != nil {
			t.Fatal(err)
		}
		// Give the main watcher time to process the change.
		time.Sleep(debounceDelay + 300*time.Millisecond)

		if got := core.WorktreeForBean("merge-1"); got != wtDir {
			t.Errorf("WorktreeForBean() = %q, want %q", got, wtDir)
		}
		b, err := core.Get("merge-1")
		if err != nil {
			t.Fatal(err)
		}
		if b.Status != "todo" || !core.IsDirty("merge-1") {
			t.Errorf("runtime bean = status %q dirty %v, want worktree version (todo, dirty)", b.Status, core.IsDirty("merge-1"))
		}
	})
}

// TestUpdateRoutedToWorktreeKeepsOneFile verifies that an update written to a
// worktree replaces the worktree's copy of the bean instead of adding a second
// file with the same ID next to it.
func TestUpdateRoutedToWorktreeKeepsOneFile(t *testing.T) {
	beanFiles := func(t *testing.T, wtBeansDir, id string) []string {
		t.Helper()
		var rel []string
		for _, p := range findBeanFilesIn(wtBeansDir, id) {
			r, _ := filepath.Rel(wtBeansDir, p)
			rel = append(rel, r)
		}
		return rel
	}

	t.Run("auto-routed update of a bean archived in the worktree", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "jq-1", "Archived In Worktree", "todo")
		if err := core.StartWatching(); err != nil {
			t.Fatal(err)
		}
		defer core.Unwatch()

		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.MkdirAll(filepath.Join(wtBeansDir, ArchiveDir), 0755); err != nil {
			t.Fatal(err)
		}
		events, unsub := core.Subscribe()
		defer unsub()
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		defer core.UnwatchWorktreeBeans(wtDir)

		// The worktree's agent completed and archived the bean.
		content := "---\ntitle: Archived In Worktree\nstatus: completed\ntype: task\n---\n\nDone.\n"
		archived := filepath.Join(wtBeansDir, ArchiveDir, "jq-1--archived-in-worktree.md")
		if err := os.WriteFile(archived, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		deadline := time.After(3 * time.Second)
		for core.WorktreeForBean("jq-1") != wtDir {
			select {
			case <-events:
			case <-deadline:
				t.Fatal("timed out waiting for the worktree copy to be linked")
			}
		}

		// Edit in the UI: auto-routed to the linked worktree.
		b, err := core.Get("jq-1")
		if err != nil {
			t.Fatal(err)
		}
		b.Body = "Edited in the UI"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		files := beanFiles(t, wtBeansDir, "jq-1")
		if len(files) != 1 || files[0] != filepath.Join(ArchiveDir, "jq-1--archived-in-worktree.md") {
			t.Fatalf("worktree bean files = %v, want only the archived copy", files)
		}
		data, err := os.ReadFile(archived)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "Edited in the UI") {
			t.Errorf("archived copy was not updated:\n%s", data)
		}
	})

	t.Run("explicit worktree path with a differently named copy", func(t *testing.T) {
		core, _ := setupTestCore(t)
		b := createTestBean(t, core, "jq-2", "New Title", "todo")

		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.MkdirAll(wtBeansDir, 0755); err != nil {
			t.Fatal(err)
		}
		stale := filepath.Join(wtBeansDir, "jq-2--old-slug.md")
		if err := os.WriteFile(stale, []byte("---\ntitle: Old\nstatus: todo\ntype: task\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}

		b.Body = "Routed explicitly"
		if err := core.Update(b, nil, WithWorktreePath(wtDir)); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		files := beanFiles(t, wtBeansDir, "jq-2")
		if len(files) != 1 || files[0] != "jq-2--new-title.md" {
			t.Fatalf("worktree bean files = %v, want only jq-2--new-title.md", files)
		}
	})
}

// TestWorktreeBeanMovedWithinWorktree verifies that moving a bean file within
// a worktree (e.g. into archive/, or replacing a stale copy) keeps the bean in
// runtime state, regardless of the order in which the watcher sees the
// removal of the old file and the creation of the new one.
func TestWorktreeBeanMovedWithinWorktree(t *testing.T) {
	for i := 0; i < 5; i++ {
		core, _ := setupTestCore(t)
		if err := core.StartWatching(); err != nil {
			t.Fatal(err)
		}

		wtDir := t.TempDir()
		wtBeansDir := filepath.Join(wtDir, BeansDir)
		if err := os.MkdirAll(filepath.Join(wtBeansDir, ArchiveDir), 0755); err != nil {
			t.Fatal(err)
		}
		// A bean that only exists in the worktree.
		oldPath := filepath.Join(wtBeansDir, "mv-1--moved.md")
		if err := os.WriteFile(oldPath, []byte("---\ntitle: Moved\nstatus: todo\ntype: task\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		if _, err := core.Get("mv-1"); err != nil {
			t.Fatalf("worktree-only bean should be loaded: %v", err)
		}

		events, unsub := core.Subscribe()
		newPath := filepath.Join(wtBeansDir, ArchiveDir, "mv-1--moved.md")
		if err := os.WriteFile(newPath, []byte("---\ntitle: Moved\nstatus: completed\ntype: task\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(oldPath); err != nil {
			t.Fatal(err)
		}

		// Wait until the watcher has processed both changes.
		deadline := time.After(3 * time.Second)
	wait:
		for {
			select {
			case <-events:
				time.Sleep(300 * time.Millisecond) // allow a second debounced batch
				break wait
			case <-deadline:
				t.Fatal("timed out waiting for worktree events")
			}
		}
		unsub()

		b, err := core.Get("mv-1")
		if err != nil {
			t.Fatalf("iteration %d: bean dropped after moving it within the worktree: %v", i, err)
		}
		if b.Status != "completed" || b.Path != filepath.Join(ArchiveDir, "mv-1--moved.md") {
			t.Errorf("iteration %d: bean = status %q path %q, want the archived copy", i, b.Status, b.Path)
		}
		core.UnwatchWorktreeBeans(wtDir)
		core.Unwatch()
	}
}

func TestSyncWorktreeWatchers(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "sync-1", "Original", "todo")

	newWorktree := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, BeansDir), 0755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	a, b := newWorktree(t), newWorktree(t)
	noBeans := t.TempDir() // a worktree without .beans/ (yet)
	defer core.UnwatchAllWorktrees()

	if err := core.SyncWorktreeWatchers([]string{a, b, noBeans}); err != nil {
		t.Fatalf("SyncWorktreeWatchers: %v", err)
	}
	if got := core.WatchedWorktrees(); len(got) != 2 {
		t.Fatalf("watched = %v, want %s and %s", got, a, b)
	}

	// A path that resolves to an already-watched worktree is not watched twice.
	if resolved, err := filepath.EvalSymlinks(a); err == nil && resolved != a {
		if err := core.SyncWorktreeWatchers([]string{resolved, b, noBeans}); err != nil {
			t.Fatal(err)
		}
		if got := core.WatchedWorktrees(); len(got) != 2 {
			t.Fatalf("after symlinked path: watched = %v, want 2 entries", got)
		}
	}

	// The worktree without .beans/ gets one: the next sync picks it up.
	if err := os.MkdirAll(filepath.Join(noBeans, BeansDir), 0755); err != nil {
		t.Fatal(err)
	}
	// b is gone (removed outside beans): it is unwatched and its links cleared.
	content := "---\ntitle: Changed in b\nstatus: todo\ntype: task\n---\n"
	if err := os.WriteFile(filepath.Join(b, BeansDir, "sync-1--original.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := core.SyncWorktreeWatchers([]string{a, b, noBeans}); err != nil {
		t.Fatal(err)
	}
	// The write is picked up by b's watcher and links the bean to b.
	deadline := time.Now().Add(3 * time.Second)
	for core.WorktreeForBean("sync-1") == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if link := core.WorktreeForBean("sync-1"); link != b {
		t.Fatalf("worktree link for sync-1 = %q, want %q", link, b)
	}
	if err := core.SyncWorktreeWatchers([]string{a, noBeans}); err != nil {
		t.Fatal(err)
	}
	got := core.WatchedWorktrees()
	want := []string{a, noBeans}
	if len(got) != 2 || !containsAll(got, want) {
		t.Fatalf("watched = %v, want %v", got, want)
	}
	if link := core.WorktreeForBean("sync-1"); link != "" {
		t.Errorf("worktree link for sync-1 = %q after its worktree was unwatched, want none", link)
	}

	// An empty list stops all watchers.
	if err := core.SyncWorktreeWatchers(nil); err != nil {
		t.Fatal(err)
	}
	if got := core.WatchedWorktrees(); len(got) != 0 {
		t.Fatalf("watched = %v after syncing an empty list, want none", got)
	}
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
