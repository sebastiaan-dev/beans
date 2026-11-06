package beangraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
)

func setupResolver(t *testing.T) (*CoreResolver, string) {
	t.Helper()
	beansDir := filepath.Join(t.TempDir(), beancore.BeansDir)
	if err := os.MkdirAll(beansDir, 0755); err != nil {
		t.Fatal(err)
	}
	core := beancore.New(beansDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	return &CoreResolver{Core: core}, beansDir
}

func mustCreate(t *testing.T, r *CoreResolver, title, typ string) *bean.Bean {
	t.Helper()
	b, err := r.CreateBean(context.Background(), model.CreateBeanInput{Title: title, Type: &typ})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ptr[T any](v T) *T { return &v }

// repos-zwrt: a rejected UpdateBean (etag mismatch or failed validation) must
// leave the bean as it was, in memory and on disk.
func TestUpdateBeanFailureLeavesBeanUntouched(t *testing.T) {
	ctx := context.Background()
	r, beansDir := setupResolver(t)
	b := mustCreate(t, r, "Original", "task")

	inputs := map[string]model.UpdateBeanInput{
		"etag mismatch": {
			Title:   ptr("Rejected"),
			Status:  ptr("in-progress"),
			AddTags: []string{"rejected"},
			BodyMod: &model.BodyModification{Append: ptr("rejected")},
			IfMatch: ptr("0123456789abcdef"),
		},
		"invalid parent": {
			Title:  ptr("Rejected"),
			Parent: ptr("does-not-exist"),
		},
		"invalid blocker": {
			Title:        ptr("Rejected"),
			AddBlockedBy: []string{"does-not-exist"},
		},
		"failed body replacement": {
			Title:   ptr("Rejected"),
			BodyMod: &model.BodyModification{Replace: []*model.ReplaceOperation{{Old: "missing", New: "x"}}},
		},
		"self blocking": {
			Title:       ptr("Rejected"),
			AddBlocking: []string{b.ID},
		},
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			id := b.ID
			before, _ := r.Core.Get(id)
			file := filepath.Join(beansDir, before.Path)
			content, _ := os.ReadFile(file)

			if _, err := r.UpdateBean(ctx, id, input); err == nil {
				t.Fatal("expected UpdateBean to fail")
			}

			after, _ := r.Core.Get(id)
			if after.ETag() != before.ETag() || after.Title != "Original" {
				t.Errorf("bean changed by rejected update: %+v", after)
			}
			for _, listed := range r.Core.All() {
				if listed.ID == id && listed.Title != "Original" {
					t.Errorf("All() shows rejected title %q", listed.Title)
				}
			}
			if now, _ := os.ReadFile(file); string(now) != string(content) {
				t.Errorf("file changed by rejected update:\n%s", now)
			}
		})
	}
}

// repos-2562: an etag read from a dirty bean must be accepted by a combined
// metadata and body update.
func TestUpdateBeanIfMatchOnDirtyBean(t *testing.T) {
	ctx := context.Background()
	r, _ := setupResolver(t)
	parent := mustCreate(t, r, "Parent", "epic")
	b := mustCreate(t, r, "Child", "task")

	if _, err := r.UpdateBean(ctx, b.ID, model.UpdateBeanInput{Status: ptr("in-progress")}, beancore.WithPersist(false)); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Bean(ctx, b.ID)
	updated, err := r.UpdateBean(ctx, b.ID, model.UpdateBeanInput{
		IfMatch: ptr(cur.ETag()),
		Parent:  ptr(parent.ID),
		AddTags: []string{"foo"},
		BodyMod: &model.BodyModification{Append: ptr("more")},
	}, beancore.WithPersist(false))
	if err != nil {
		t.Fatalf("UpdateBean with the current etag: %v", err)
	}
	if updated.Parent != parent.ID || !slices.Contains(updated.Tags, "foo") || updated.Status != "in-progress" {
		t.Errorf("updated = %+v", updated)
	}
}

// Relationship checks see the changes made earlier in the same update.
func TestUpdateBeanDetectsCycleWithinOneUpdate(t *testing.T) {
	ctx := context.Background()
	r, _ := setupResolver(t)
	a := mustCreate(t, r, "A", "task")
	b := mustCreate(t, r, "B", "task")
	_, err := r.UpdateBean(ctx, a.ID, model.UpdateBeanInput{AddBlocking: []string{b.ID}, AddBlockedBy: []string{b.ID}})
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	if got, _ := r.Core.Get(a.ID); len(got.Blocking) != 0 || len(got.BlockedBy) != 0 {
		t.Errorf("rejected update left links: %+v", got)
	}
}

// repos-0xy7: Bean.blocking and Bean.blockedBy are symmetric: each includes
// the links declared on the other side.
func TestBlockingRelationsAreSymmetric(t *testing.T) {
	ctx := context.Background()
	r, _ := setupResolver(t)
	a := mustCreate(t, r, "A", "task")
	c := mustCreate(t, r, "C", "task")
	b, err := r.CreateBean(ctx, model.CreateBeanInput{Title: "B", BlockedBy: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddBlocking(ctx, a.ID, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	a, _ = r.Core.Get(a.ID)

	blocking, err := r.BeanBlocking(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := beanIDs(blocking); !slices.Contains(got, b.ID) || !slices.Contains(got, c.ID) || len(got) != 2 {
		t.Errorf("A.blocking = %v, want [%s %s]", got, b.ID, c.ID)
	}
	for _, target := range []*bean.Bean{b, c} {
		blockedBy, _ := r.BeanBlockedBy(ctx, target, nil)
		if got := beanIDs(blockedBy); len(got) != 1 || got[0] != a.ID {
			t.Errorf("%s.blockedBy = %v, want [%s]", target.Title, got, a.ID)
		}
	}
}

// TestConcurrentMutationsAndReads runs mutations, readers and watcher events
// in parallel. Under -race it fails if a mutation modifies a bean that
// readers (All, Get, Search, subscriptions) can see.
func TestConcurrentMutationsAndReads(t *testing.T) {
	ctx := context.Background()
	r, beansDir := setupResolver(t)
	core := r.Core

	const n = 8
	var ids []string
	for i := range n {
		ids = append(ids, mustCreate(t, r, fmt.Sprintf("Bean %d", i), "task").ID)
	}
	epic := mustCreate(t, r, "Epic", "epic")
	external := mustCreate(t, r, "External", "task")

	if err := core.StartWatching(); err != nil {
		t.Fatal(err)
	}
	events, unsub := core.Subscribe()
	defer unsub()

	// A worktree whose copy of a bean keeps changing.
	wtDir := t.TempDir()
	wtBeans := filepath.Join(wtDir, beancore.BeansDir)
	if err := os.MkdirAll(wtBeans, 0755); err != nil {
		t.Fatal(err)
	}
	wtBean, _ := core.Get(ids[0])
	wtFile := filepath.Join(wtBeans, filepath.Base(wtBean.Path))
	writeCopy := func(i int) {
		c := *wtBean
		c.Title = fmt.Sprintf("Worktree %d", i)
		content, _ := c.Render()
		_ = os.WriteFile(wtFile, content, 0644)
	}
	writeCopy(0)
	if err := core.WatchWorktreeBeans(wtDir); err != nil {
		t.Fatal(err)
	}
	defer core.UnwatchAllWorktrees()

	readBean := func(b *bean.Bean) {
		_ = b.ETag() // renders every field
		_ = len(b.Tags) + len(b.Blocking) + len(b.BlockedBy) + len(b.Body)
	}

	deadline := time.Now().Add(1500 * time.Millisecond)
	var wg sync.WaitGroup
	run := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				f(i)
			}
		}()
	}

	var errMu sync.Mutex
	var unexpected []error
	report := func(err error) {
		var mismatch *beancore.ETagMismatchError
		if err != nil && !errors.As(err, &mismatch) {
			errMu.Lock()
			unexpected = append(unexpected, err)
			errMu.Unlock()
		}
	}

	// Writers: field updates, relationship changes and optimistic updates.
	for w := range 3 {
		run(func(i int) {
			id := ids[(i+w)%n]
			_, err := r.UpdateBean(ctx, id, model.UpdateBeanInput{
				Title:   ptr(fmt.Sprintf("Title %d-%d", w, i)),
				AddTags: []string{fmt.Sprintf("tag-%d", i%3)},
				BodyMod: &model.BodyModification{Append: ptr("x")},
			})
			report(err)
		})
	}
	run(func(i int) {
		id := ids[i%n]
		if i%2 == 0 {
			_, err := r.SetParent(ctx, id, &epic.ID, nil)
			report(err)
		} else {
			_, err := r.SetParent(ctx, id, nil, nil)
			report(err)
		}
	})
	run(func(i int) {
		from, to := ids[i%n], ids[(i+1)%n]
		if i%2 == 0 {
			_, _ = r.AddBlocking(ctx, from, to, nil) // may be rejected as a cycle
		} else {
			_, err := r.RemoveBlocking(ctx, from, to, nil)
			report(err)
		}
	})
	run(func(i int) {
		cur, err := core.Get(ids[i%n])
		if err != nil {
			report(err)
			return
		}
		_, err = r.UpdateBean(ctx, cur.ID, model.UpdateBeanInput{Status: ptr("in-progress"), IfMatch: ptr(cur.ETag())})
		report(err)
	})

	// Readers.
	run(func(int) {
		for _, b := range core.All() {
			readBean(b)
		}
	})
	run(func(i int) {
		if b, err := core.Get(ids[i%n]); err == nil {
			readBean(b)
		}
		for _, link := range core.FindIncomingLinks(epic.ID) {
			readBean(link.FromBean)
		}
	})
	run(func(int) {
		beans, err := r.Beans(ctx, &model.BeanFilter{Search: ptr("Title")})
		report(err)
		for _, b := range beans {
			readBean(b)
		}
	})
	run(func(int) {
		select {
		case batch := <-events:
			for _, ev := range batch {
				if ev.Bean != nil {
					readBean(ev.Bean)
				}
			}
		case <-time.After(10 * time.Millisecond):
		}
	})

	// Reloads and worktree (un)watching replace runtime state wholesale.
	run(func(i int) {
		if i%2 == 0 {
			report(core.Load())
		} else {
			core.UnwatchWorktreeBeans(wtDir)
			report(core.WatchWorktreeBeans(wtDir))
		}
		time.Sleep(30 * time.Millisecond)
	})

	// Watcher events: external edits in main and in the worktree.
	run(func(i int) {
		c := *external
		c.Title = fmt.Sprintf("External %d", i)
		content, _ := c.Render()
		_ = os.WriteFile(filepath.Join(beansDir, external.Path), content, 0644)
		writeCopy(i)
		time.Sleep(20 * time.Millisecond)
	})

	wg.Wait()
	for _, err := range unexpected {
		t.Error(err)
	}
}
