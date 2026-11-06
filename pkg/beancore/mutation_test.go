package beancore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/bean"
)

// beanState captures what readers see of a bean: its runtime etag, dirty
// flag and worktree link, plus its main file content.
type beanState struct {
	etag    string
	dirty   bool
	link    string
	content string
}

func stateOf(t *testing.T, core *Core, id string) beanState {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	content, _ := os.ReadFile(filepath.Join(core.Root(), b.Path))
	return beanState{etag: b.ETag(), dirty: core.IsDirty(id), link: core.WorktreeForBean(id), content: string(content)}
}

func TestGetReturnsCopy(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "cp-1", "Original", "todo")

	b, _ := core.Get("cp-1")
	b.Title = "Changed"
	b.Tags = append(b.Tags, "leak")

	got, _ := core.Get("cp-1")
	if got.Title != "Original" || len(got.Tags) != 0 {
		t.Errorf("modifying Get's result changed the store: %+v", got)
	}
	for _, b := range core.All() {
		if b.Title != "Original" {
			t.Errorf("All() = %q, want Original", b.Title)
		}
	}
}

func TestUpdateAndCreateStoreCopies(t *testing.T) {
	core, _ := setupTestCore(t)
	b := createTestBean(t, core, "cp-2", "Created", "todo")
	b.Title = "Modified after Create"
	if got, _ := core.Get("cp-2"); got.Title != "Created" {
		t.Errorf("modifying the bean passed to Create changed the store: %q", got.Title)
	}

	upd, _ := core.Get("cp-2")
	upd.Title = "Updated"
	if err := core.Update(upd, nil); err != nil {
		t.Fatal(err)
	}
	if upd.UpdatedAt == nil {
		t.Error("Update should report the stored version (updated_at) back")
	}
	upd.Title = "Modified after Update"
	if got, _ := core.Get("cp-2"); got.Title != "Updated" {
		t.Errorf("modifying the bean passed to Update changed the store: %q", got.Title)
	}
}

// Rejected changes (repos-zwrt): neither an etag mismatch nor a failing
// mutation or validation may leave anything behind, for beans on disk as well
// as for dirty and worktree beans.
func TestMutateFailureLeavesStateUntouched(t *testing.T) {
	setups := map[string]func(t *testing.T) (*Core, string){
		"on disk": func(t *testing.T) (*Core, string) {
			core, _ := setupTestCore(t)
			createTestBean(t, core, "fail-1", "Original", "todo")
			return core, "fail-1"
		},
		"dirty": func(t *testing.T) (*Core, string) {
			core, _ := setupTestCore(t)
			createTestBean(t, core, "fail-1", "Original", "todo")
			b, _ := core.Get("fail-1")
			b.Status = "in-progress"
			if err := core.Update(b, nil, WithPersist(false)); err != nil {
				t.Fatal(err)
			}
			return core, "fail-1"
		},
		"worktree": func(t *testing.T) (*Core, string) {
			core, _ := setupTestCore(t)
			createTestBean(t, core, "fail-1", "Original", "todo")
			wtDir := t.TempDir()
			writeWorktreeBean(t, wtDir, "fail-1--original.md", "---\ntitle: Worktree\nstatus: in-progress\ntype: task\n---\n")
			if err := core.WatchWorktreeBeans(wtDir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(core.UnwatchAllWorktrees)
			return core, "fail-1"
		},
	}

	wrong := "0123456789abcdef"
	failures := map[string]func(core *Core, id string) error{
		"etag mismatch": func(core *Core, id string) error {
			_, err := core.Mutate(id, &wrong, func(_ *Tx, b *bean.Bean) error {
				b.Title = "Rejected"
				return nil
			})
			var mismatch *ETagMismatchError
			if !errors.As(err, &mismatch) {
				return errors.New("expected ETagMismatchError")
			}
			return nil
		},
		"update etag mismatch": func(core *Core, id string) error {
			b, _ := core.Get(id)
			b.Title = "Rejected"
			if err := core.Update(b, &wrong); err == nil {
				return errors.New("expected an error")
			}
			return nil
		},
		"mutation error": func(core *Core, id string) error {
			cur, _ := core.Get(id)
			etag := cur.ETag()
			_, err := core.Mutate(id, &etag, func(_ *Tx, b *bean.Bean) error {
				b.Title = "Rejected"
				b.Tags = append(b.Tags, "rejected")
				return errors.New("invalid")
			})
			if err == nil {
				return errors.New("expected an error")
			}
			return nil
		},
		"validation error": func(core *Core, id string) error {
			_, err := core.Mutate(id, nil, func(_ *Tx, b *bean.Bean) error {
				b.Title = "Rejected"
				return nil
			}, WithValidation(func(_ *Tx, b *bean.Bean) error { return errors.New("invalid") }))
			if err == nil {
				return errors.New("expected an error")
			}
			return nil
		},
		"ID change": func(core *Core, id string) error {
			_, err := core.Mutate(id, nil, func(_ *Tx, b *bean.Bean) error {
				b.ID = "other"
				return nil
			})
			if err == nil {
				return errors.New("expected an error")
			}
			return nil
		},
	}

	for setupName, setup := range setups {
		for failName, fail := range failures {
			t.Run(setupName+"/"+failName, func(t *testing.T) {
				core, id := setup(t)
				before := stateOf(t, core, id)
				if err := fail(core, id); err != nil {
					t.Fatal(err)
				}
				if after := stateOf(t, core, id); after != before {
					t.Errorf("state changed by failed mutation:\nbefore %+v\nafter  %+v", before, after)
				}
			})
		}
	}
}

// repos-2562: the etag a reader got for a dirty or worktree bean must be
// accepted by a combined update (the in-memory bean used to be modified
// before the etag was checked, so the check always failed).
func TestMutateIfMatchForDirtyAndWorktreeBeans(t *testing.T) {
	t.Run("dirty", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "etag-1", "Original", "todo")
		b, _ := core.Get("etag-1")
		b.Status = "in-progress"
		if err := core.Update(b, nil, WithPersist(false)); err != nil {
			t.Fatal(err)
		}
		cur, _ := core.Get("etag-1")
		etag := cur.ETag()
		got, err := core.Mutate("etag-1", &etag, func(_ *Tx, b *bean.Bean) error {
			b.Title = "New"
			b.Body = bean.AppendWithSeparator(b.Body, "more")
			b.Tags = append(b.Tags, "foo")
			return nil
		}, WithPersist(false))
		if err != nil {
			t.Fatalf("Mutate with the current etag: %v", err)
		}
		if got.Title != "New" || got.Status != "in-progress" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("worktree", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "etag-2", "Original", "todo")
		wtDir := t.TempDir()
		writeWorktreeBean(t, wtDir, "etag-2--original.md", "---\ntitle: Worktree\nstatus: in-progress\ntype: task\n---\n")
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		defer core.UnwatchAllWorktrees()

		cur, _ := core.Get("etag-2")
		etag := cur.ETag()
		if _, err := core.Mutate("etag-2", &etag, func(_ *Tx, b *bean.Bean) error {
			b.Title = "New"
			b.Tags = append(b.Tags, "foo")
			return nil
		}); err != nil {
			t.Fatalf("Mutate with the current etag: %v", err)
		}
		content, _ := os.ReadFile(filepath.Join(wtDir, BeansDir, "etag-2--original.md"))
		if want := "title: New"; !strings.Contains(string(content), want) {
			t.Errorf("worktree copy should be updated, got:\n%s", content)
		}
	})
}

func TestTxSeesPendingChanges(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "tx-a", "A", "todo")
	createTestBean(t, core, "tx-b", "B", "todo")

	// Adding A blocks B and B blocks A in the same change is a cycle: the
	// second check must see the first, pending link.
	_, err := core.Mutate("tx-a", nil, func(tx *Tx, b *bean.Bean) error {
		b.AddBlocking("tx-b")
		if cycle := tx.DetectCycle("tx-b", "blocking", "tx-a"); cycle == nil {
			return errors.New("cycle not detected")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// repos-705u: Load must keep worktree beans in their worktree version, so
// writes (routed to the worktree) don't clobber the worktree's change with
// main's version.
func TestLoadKeepsWorktreeOverlay(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "load-1", "Main", "todo")
	wtDir := t.TempDir()
	writeWorktreeBean(t, wtDir, "load-1--main.md", "---\ntitle: Worktree\nstatus: in-progress\ntype: task\n---\n\nWorktree body\n")
	if err := core.WatchWorktreeBeans(wtDir); err != nil {
		t.Fatal(err)
	}
	defer core.UnwatchAllWorktrees()

	if err := core.Load(); err != nil {
		t.Fatal(err)
	}

	got, _ := core.Get("load-1")
	if got.Title != "Worktree" || !core.IsDirty("load-1") || core.WorktreeForBean("load-1") != wtDir {
		t.Fatalf("after Load: title %q, dirty %v, link %q; want the worktree version", got.Title, core.IsDirty("load-1"), core.WorktreeForBean("load-1"))
	}

	got.Priority = "high"
	if err := core.Update(got, nil); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(wtDir, BeansDir, "load-1--main.md"))
	if !strings.Contains(string(content), "title: Worktree") || !strings.Contains(string(content), "Worktree body") {
		t.Errorf("update after Load clobbered the worktree change:\n%s", content)
	}
	mainContent, _ := os.ReadFile(filepath.Join(core.Root(), "load-1--main.md"))
	if strings.Contains(string(mainContent), "high") {
		t.Errorf("update after Load was written to main:\n%s", mainContent)
	}
}

// repos-0aak: unwatching a worktree reverts its bean changes in runtime
// state to main's version (or another worktree's change).
func TestUnwatchRevertsWorktreeChanges(t *testing.T) {
	t.Run("reverts to main", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "unw-1", "Main", "todo")
		wtDir := t.TempDir()
		writeWorktreeBean(t, wtDir, "unw-1--main.md", "---\ntitle: Worktree\nstatus: in-progress\ntype: task\n---\n")
		// A bean only the worktree has disappears.
		writeWorktreeBean(t, wtDir, "unw-2--new.md", "---\ntitle: New\nstatus: todo\ntype: task\n---\n")
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		if got, _ := core.Get("unw-1"); got.Title != "Worktree" {
			t.Fatalf("title = %q, want the worktree version", got.Title)
		}

		events, unsub := core.Subscribe()
		defer unsub()
		core.UnwatchWorktreeBeans(wtDir)

		got, _ := core.Get("unw-1")
		if got.Title != "Main" || got.Status != "todo" || core.IsDirty("unw-1") || core.WorktreeForBean("unw-1") != "" {
			t.Errorf("after unwatch: title %q, status %q, dirty %v, link %q; want main's version", got.Title, got.Status, core.IsDirty("unw-1"), core.WorktreeForBean("unw-1"))
		}
		if _, err := core.Get("unw-2"); !errors.Is(err, ErrNotFound) {
			t.Errorf("worktree-only bean should be gone, Get() error = %v", err)
		}
		select {
		case batch := <-events:
			if len(batch) != 2 {
				t.Errorf("events = %+v, want an update and a delete", batch)
			}
		default:
			t.Error("expected events for the reverted beans")
		}
	})

	t.Run("falls back to another worktree", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "unw-3", "Main", "todo")
		wt1, wt2 := t.TempDir(), t.TempDir()
		writeWorktreeBean(t, wt1, "unw-3--main.md", "---\ntitle: One\nstatus: todo\ntype: task\n---\n")
		writeWorktreeBean(t, wt2, "unw-3--main.md", "---\ntitle: Two\nstatus: todo\ntype: task\n---\n")
		for _, wt := range []string{wt1, wt2} {
			if err := core.WatchWorktreeBeans(wt); err != nil {
				t.Fatal(err)
			}
		}
		defer core.UnwatchAllWorktrees()

		linked := core.WorktreeForBean("unw-3")
		other := wt1
		if linked == wt1 {
			other = wt2
		}
		core.UnwatchWorktreeBeans(linked)
		if got := core.WorktreeForBean("unw-3"); got != other {
			t.Errorf("link = %q, want the other worktree %q", got, other)
		}
	})

	t.Run("UnwatchAllWorktrees reverts too", func(t *testing.T) {
		core, _ := setupTestCore(t)
		createTestBean(t, core, "unw-4", "Main", "todo")
		wtDir := t.TempDir()
		writeWorktreeBean(t, wtDir, "unw-4--main.md", "---\ntitle: Worktree\nstatus: todo\ntype: task\n---\n")
		if err := core.WatchWorktreeBeans(wtDir); err != nil {
			t.Fatal(err)
		}
		core.UnwatchAllWorktrees()
		if got, _ := core.Get("unw-4"); got.Title != "Main" || core.IsDirty("unw-4") {
			t.Errorf("after UnwatchAllWorktrees: title %q, dirty %v; want main's version", got.Title, core.IsDirty("unw-4"))
		}
	})
}

// Link repairs must not write a worktree's version of a bean into main.
func TestRemoveLinksToRoutesWorktreeBeans(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "rl-target", "Target", "todo")
	createTestBean(t, core, "rl-1", "Main", "todo")
	wtDir := t.TempDir()
	writeWorktreeBean(t, wtDir, "rl-1--main.md", "---\ntitle: Worktree\nstatus: todo\ntype: task\nblocking:\n    - rl-target\n---\n")
	if err := core.WatchWorktreeBeans(wtDir); err != nil {
		t.Fatal(err)
	}
	defer core.UnwatchAllWorktrees()

	before, _ := core.Get("rl-1")
	if n, err := core.RemoveLinksTo("rl-target"); err != nil || n != 1 {
		t.Fatalf("RemoveLinksTo = %d, %v; want 1", n, err)
	}
	after, _ := core.Get("rl-1")
	if len(after.Blocking) != 0 || len(before.Blocking) != 1 {
		t.Errorf("blocking before %v, after %v", before.Blocking, after.Blocking)
	}
	mainContent, _ := os.ReadFile(filepath.Join(core.Root(), "rl-1--main.md"))
	if strings.Contains(string(mainContent), "Worktree") {
		t.Errorf("worktree version was written to main:\n%s", mainContent)
	}
	wtContent, _ := os.ReadFile(filepath.Join(wtDir, BeansDir, "rl-1--main.md"))
	if strings.Contains(string(wtContent), "rl-target") {
		t.Errorf("worktree copy still links to the target:\n%s", wtContent)
	}
}

func writeWorktreeBean(t *testing.T, wtDir, name, content string) {
	t.Helper()
	dir := filepath.Join(wtDir, BeansDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
