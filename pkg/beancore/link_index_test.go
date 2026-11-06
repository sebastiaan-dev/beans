package beancore

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
)

// assertLinkIndexConsistent checks that the link index equals one rebuilt
// from the stored beans.
func assertLinkIndexConsistent(t *testing.T, c *Core) {
	t.Helper()
	c.mu.RLock()
	defer c.mu.RUnlock()

	want := newLinkIndex()
	for _, b := range c.beans {
		want.add(b)
	}
	if !reflect.DeepEqual(c.links, want) {
		t.Errorf("link index out of sync with the stored beans:\n got  %v\n want %v", c.links, want)
	}
}

func beanIDs(beans []*bean.Bean) []string {
	var ids []string
	for _, b := range beans {
		ids = append(ids, b.ID)
	}
	return ids
}

func incomingStrings(links []IncomingLink) []string {
	var out []string
	for _, l := range links {
		out = append(out, l.LinkType+":"+l.FromBean.ID)
	}
	return out
}

func TestLinkIndexRelations(t *testing.T) {
	core, _ := setupTestCore(t)
	for _, b := range []*bean.Bean{
		{ID: "epic", Title: "Epic", Status: "todo", Type: "epic"},
		{ID: "a", Title: "A", Status: "todo", Parent: "epic", Blocking: []string{"b", "missing"}},
		{ID: "b", Title: "B", Status: "todo", Parent: "epic"},
		{ID: "c", Title: "C", Status: "completed", BlockedBy: []string{"a", "a"}},
		{ID: "d", Title: "D", Status: "todo", BlockedBy: []string{"b"}, Blocking: []string{"b"}},
	} {
		if err := core.Create(b); err != nil {
			t.Fatal(err)
		}
	}

	check := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("Children(epic)", beanIDs(core.Children("epic")), []string{"a", "b"})
	check("Blocking(a)", beanIDs(core.Blocking(mustGet(t, core, "a"))), []string{"b", "c"})
	check("BlockedBy(b)", beanIDs(core.BlockedBy(mustGet(t, core, "b"))), []string{"a", "d"})
	check("Blocking(b)", beanIDs(core.Blocking(mustGet(t, core, "b"))), []string{"d"})
	check("BlockedBy(d)", beanIDs(core.BlockedBy(mustGet(t, core, "d"))), []string{"b"})
	check("BlockedBy(c)", beanIDs(core.BlockedBy(mustGet(t, core, "c"))), []string{"a"})
	check("FindIncomingLinks(b)", incomingStrings(core.FindIncomingLinks("b")), []string{"blocking:a", "blocking:d", "blocked_by:d"})
	check("FindIncomingLinks(missing)", incomingStrings(core.FindIncomingLinks("missing")), []string{"blocking:a"})
	if p := core.Parent(mustGet(t, core, "a")); p == nil || p.ID != "epic" {
		t.Errorf("Parent(a) = %v, want epic", p)
	}
	if core.Parent(mustGet(t, core, "epic")) != nil {
		t.Error("Parent(epic) should be nil")
	}

	for id, want := range map[string]bool{"a": true, "b": true, "c": false, "d": true, "epic": false} {
		if got := core.HasBlocking(mustGet(t, core, id)); got != want {
			t.Errorf("HasBlocking(%s) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"a": false, "b": true, "c": true, "d": true, "epic": false} {
		if got := core.HasBlockedBy(mustGet(t, core, id)); got != want {
			t.Errorf("HasBlockedBy(%s) = %v, want %v", id, got, want)
		}
	}

	// Changing links updates the relations.
	if _, err := core.Mutate("a", nil, func(_ *Tx, b *bean.Bean) error {
		b.Parent = ""
		b.Blocking = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	check("Children(epic) after mutate", beanIDs(core.Children("epic")), []string{"b"})
	check("BlockedBy(b) after mutate", beanIDs(core.BlockedBy(mustGet(t, core, "b"))), []string{"d"})
	check("FindIncomingLinks(missing) after mutate", incomingStrings(core.FindIncomingLinks("missing")), nil)

	// Deleting a bean drops its links; links to it stay (broken).
	if err := core.Delete("d"); err != nil {
		t.Fatal(err)
	}
	check("Blocking(b) after delete", beanIDs(core.Blocking(mustGet(t, core, "b"))), nil)
	if core.HasBlockedBy(mustGet(t, core, "b")) {
		t.Error("b should not be blocked after deleting d")
	}

	// RemoveLinksTo only touches the linking beans.
	n, err := core.RemoveLinksTo("a")
	if err != nil || n != 2 {
		t.Fatalf("RemoveLinksTo(a) = %d, %v; want 2 (both entries in c's blocked_by)", n, err)
	}
	check("Blocking(a) after RemoveLinksTo", beanIDs(core.Blocking(mustGet(t, core, "a"))), nil)

	// Reloading from disk rebuilds the index.
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	check("Children(epic) after Load", beanIDs(core.Children("epic")), []string{"b"})
}

func TestLinkIndexArchiveAndWatcher(t *testing.T) {
	core, beansDir := setupTestCore(t)
	createTestBean(t, core, "p1", "Parent", "todo")
	child := &bean.Bean{ID: "c1", Slug: "child", Title: "Child", Status: "todo", Parent: "p1"}
	if err := core.Create(child); err != nil {
		t.Fatal(err)
	}

	if err := core.Archive("c1"); err != nil {
		t.Fatal(err)
	}
	if got := beanIDs(core.Children("p1")); !reflect.DeepEqual(got, []string{"c1"}) {
		t.Errorf("Children(p1) after archive = %v", got)
	}
	if err := core.Unarchive("c1"); err != nil {
		t.Fatal(err)
	}
	assertLinkIndexConsistent(t, core)

	if err := core.StartWatching(); err != nil {
		t.Fatal(err)
	}
	defer core.Unwatch()
	events, unsub := core.Subscribe()
	defer unsub()

	// An external edit adds a blocker.
	content := "---\ntitle: External\nstatus: todo\ntype: task\nblocking:\n    - c1\n---\n"
	if err := os.WriteFile(filepath.Join(beansDir, "x1--external.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "x1")
	if got := beanIDs(core.BlockedBy(mustGet(t, core, "c1"))); !reflect.DeepEqual(got, []string{"x1"}) {
		t.Errorf("BlockedBy(c1) after external create = %v", got)
	}
	if !core.IsBlocked("c1") {
		t.Error("c1 should be blocked by x1")
	}

	// Removing the file drops its links.
	if err := os.Remove(filepath.Join(beansDir, "x1--external.md")); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "x1")
	if got := core.BlockedBy(mustGet(t, core, "c1")); len(got) != 0 {
		t.Errorf("BlockedBy(c1) after external delete = %v", beanIDs(got))
	}
}

func TestLinkIndexWorktreeChanges(t *testing.T) {
	core, _ := setupTestCore(t)
	createTestBean(t, core, "p1", "Parent", "todo")
	createTestBean(t, core, "c1", "Child", "todo")
	if err := core.StartWatching(); err != nil {
		t.Fatal(err)
	}
	defer core.Unwatch()

	wtDir := t.TempDir()
	wtBeans := filepath.Join(wtDir, BeansDir)
	if err := os.MkdirAll(wtBeans, 0755); err != nil {
		t.Fatal(err)
	}
	events, unsub := core.Subscribe()
	defer unsub()
	if err := core.WatchWorktreeBeans(wtDir); err != nil {
		t.Fatal(err)
	}

	// The worktree sets c1's parent.
	content := "---\ntitle: Child\nstatus: todo\ntype: task\nparent: p1\n---\n"
	if err := os.WriteFile(filepath.Join(wtBeans, "c1--child.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "c1")
	if got := beanIDs(core.Children("p1")); !reflect.DeepEqual(got, []string{"c1"}) {
		t.Errorf("Children(p1) with worktree change = %v", got)
	}

	// Unwatching reverts c1 to main's version, without a parent.
	core.UnwatchWorktreeBeans(wtDir)
	if got := core.Children("p1"); len(got) != 0 {
		t.Errorf("Children(p1) after unwatch = %v", beanIDs(got))
	}
}

func mustGet(t *testing.T, c *Core, id string) *bean.Bean {
	t.Helper()
	b, err := c.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return b
}

func waitForEvent(t *testing.T, events <-chan []BeanEvent, id string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case batch := <-events:
			for _, ev := range batch {
				if ev.BeanID == id {
					return
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for an event for %s", id)
		}
	}
}

// TestLinkIndexRandomMutations applies random link changes, concurrently
// with readers, and checks the relations against a full scan.
func TestLinkIndexRandomMutations(t *testing.T) {
	core, _ := setupTestCore(t)
	const n = 30
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("r%02d", i)
		if err := core.Create(&bean.Bean{ID: ids[i], Title: ids[i], Status: "todo"}, WithPersist(false)); err != nil {
			t.Fatal(err)
		}
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, id := range ids {
					_ = core.FindIncomingLinks(id)
					if b, err := core.Get(id); err == nil {
						_ = core.Blocking(b)
					}
					_ = core.IsBlocked(id)
				}
			}
		})
	}

	rng := rand.New(rand.NewSource(1))
	pick := func() string { return ids[rng.Intn(n)] }
	for range 500 {
		id := pick()
		if _, err := core.Mutate(id, nil, func(_ *Tx, b *bean.Bean) error {
			switch rng.Intn(4) {
			case 0:
				b.Parent = pick()
			case 1:
				b.Parent = ""
			case 2:
				b.Blocking = append(b.Blocking, pick())
			case 3:
				b.BlockedBy = []string{pick()}
				b.Blocking = nil
			}
			return nil
		}, WithPersist(false)); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	assertLinkIndexConsistent(t, core)
	all := core.All()
	for _, target := range ids {
		var want []string
		for _, b := range all {
			if b.Parent == target {
				want = append(want, b.ID)
			}
		}
		sort.Strings(want)
		if got := beanIDs(core.Children(target)); !reflect.DeepEqual(got, want) {
			t.Errorf("Children(%s) = %v, want %v", target, got, want)
		}
	}
}
