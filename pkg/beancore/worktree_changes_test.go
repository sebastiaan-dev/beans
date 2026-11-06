package beancore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/config"
)

const changesTestBean = "chg-x--shared.md"

// changesTestRepo is a main repository with a committed bean, plus helpers to
// create real git worktrees from it.
type changesTestRepo struct {
	t    *testing.T
	main string // main worktree root
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func beanContent(status, body string) string {
	return "---\ntitle: Shared\nstatus: " + status + "\ntype: task\ncreated_at: 2025-01-01T00:00:00Z\nupdated_at: 2025-01-01T00:00:00Z\n---\n\n" + body + "\n"
}

func writeWtFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newChangesTestRepo(t *testing.T) *changesTestRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	main := t.TempDir()
	runGit(t, main, "init", "-q", "-b", "main")
	writeWtFile(t, filepath.Join(main, BeansDir, changesTestBean), beanContent("todo", "Original body."))
	writeWtFile(t, filepath.Join(main, BeansDir, "chg-y--other.md"), strings.Replace(beanContent("todo", "Other."), "Shared", "Other", 1))
	runGit(t, main, "add", "-A")
	runGit(t, main, "commit", "-q", "-m", "initial")
	return &changesTestRepo{t: t, main: main}
}

// worktree creates a linked worktree on a new branch from main's HEAD.
func (r *changesTestRepo) worktree(name string) string {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), name)
	runGit(r.t, r.main, "worktree", "add", "-q", "-b", name, dir, "main")
	return dir
}

func (r *changesTestRepo) core() *Core {
	r.t.Helper()
	cfg := config.Default()
	core := New(filepath.Join(r.main, BeansDir), cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { core.UnwatchAllWorktrees() })
	return core
}

func getBean(t *testing.T, core *Core, id string) (status, body string) {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return b.Status, strings.TrimSpace(b.Body)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestUntouchedWorktreeCopiesDoNotOverride reproduces repos-c8ce: a worktree's
// unchanged (committed base) copy of a bean must never override main's
// uncommitted edit or another worktree's real change.
func TestUntouchedWorktreeCopiesDoNotOverride(t *testing.T) {
	for _, order := range []string{"a-first", "b-first"} {
		t.Run("worktree change wins/"+order, func(t *testing.T) {
			repo := newChangesTestRepo(t)
			a, b := repo.worktree("a"), repo.worktree("b")
			// Uncommitted edit in main, made after the worktrees were created.
			writeWtFile(t, filepath.Join(repo.main, BeansDir, changesTestBean), beanContent("todo", "Edited in main."))
			// A sets X in-progress.
			writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "Original body."))

			core := repo.core()
			paths := []string{a, b}
			if order == "b-first" {
				paths = []string{b, a}
			}
			for _, p := range paths {
				if err := core.WatchWorktreeBeans(p); err != nil {
					t.Fatal(err)
				}
			}

			if status, _ := getBean(t, core, "chg-x"); status != "in-progress" {
				t.Errorf("status = %q, want in-progress (A's change)", status)
			}
			if got := core.WorktreeForBean("chg-x"); got != a {
				t.Errorf("WorktreeForBean = %q, want %q", got, a)
			}
			if got := core.WorktreeForBean("chg-y"); got != "" {
				t.Errorf("untouched bean chg-y linked to %q", got)
			}
		})
	}

	t.Run("main edit wins without worktree changes", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a, b := repo.worktree("a"), repo.worktree("b")
		writeWtFile(t, filepath.Join(repo.main, BeansDir, changesTestBean), beanContent("todo", "Edited in main."))

		core := repo.core()
		for _, p := range []string{a, b} {
			if err := core.WatchWorktreeBeans(p); err != nil {
				t.Fatal(err)
			}
		}
		if _, body := getBean(t, core, "chg-x"); body != "Edited in main." {
			t.Errorf("body = %q, want main's uncommitted edit", body)
		}
		if core.IsDirty("chg-x") || core.WorktreeForBean("chg-x") != "" {
			t.Error("chg-x must be neither dirty nor linked")
		}
	})

	t.Run("committed branch changes count", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a := repo.worktree("a")
		writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("completed", "Original body."))
		runGit(t, a, "commit", "-q", "-am", "complete x")

		core := repo.core()
		if err := core.WatchWorktreeBeans(a); err != nil {
			t.Fatal(err)
		}
		if status, _ := getBean(t, core, "chg-x"); status != "completed" {
			t.Errorf("status = %q, want completed", status)
		}
	})

	t.Run("two worktrees changed the bean: most recent modification wins", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a, b := repo.worktree("a"), repo.worktree("b")
		writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "A."))
		writeWtFile(t, filepath.Join(b, BeansDir, changesTestBean), beanContent("completed", "B."))
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(a, BeansDir, changesTestBean), old, old); err != nil {
			t.Fatal(err)
		}

		for _, paths := range [][]string{{a, b}, {b, a}} {
			core := repo.core()
			for _, p := range paths {
				if err := core.WatchWorktreeBeans(p); err != nil {
					t.Fatal(err)
				}
			}
			if _, body := getBean(t, core, "chg-x"); body != "B." || core.WorktreeForBean("chg-x") != b {
				t.Errorf("order %v: body = %q link = %q, want B's (newer) change", paths, body, core.WorktreeForBean("chg-x"))
			}
			core.UnwatchAllWorktrees()
		}
	})
}

func TestWorktreeEventsRespectBaseChanges(t *testing.T) {
	t.Run("checkout in an untouched worktree does not flip state", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a, b := repo.worktree("a"), repo.worktree("b")
		writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "Original body."))

		// A branch in b that changes the bean, then back: b's working tree is
		// rewritten twice but ends up at the base version.
		runGit(t, b, "checkout", "-q", "-b", "b-side")
		writeWtFile(t, filepath.Join(b, BeansDir, changesTestBean), beanContent("draft", "Side."))
		runGit(t, b, "commit", "-q", "-am", "side")
		runGit(t, b, "checkout", "-q", "b")

		core := repo.core()
		if err := core.StartWatching(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { core.Unwatch() })
		for _, p := range []string{a, b} {
			if err := core.WatchWorktreeBeans(p); err != nil {
				t.Fatal(err)
			}
		}
		events, unsub := core.Subscribe()
		defer unsub()

		runGit(t, b, "checkout", "-q", "b-side")
		runGit(t, b, "checkout", "-q", "b")
		// Simulate a rebase rewriting the base version (new mtime, same content).
		writeWtFile(t, filepath.Join(b, BeansDir, changesTestBean), beanContent("todo", "Original body."))

		time.Sleep(debounceDelay + 500*time.Millisecond)
		for len(events) > 0 {
			for _, ev := range <-events {
				if ev.BeanID == "chg-x" {
					t.Errorf("unexpected event for chg-x: %+v", ev)
				}
			}
		}
		if status, _ := getBean(t, core, "chg-x"); status != "in-progress" || core.WorktreeForBean("chg-x") != a {
			t.Errorf("status = %q link = %q, want A's change", status, core.WorktreeForBean("chg-x"))
		}
	})

	t.Run("worktree that reverts its change unlinks", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a := repo.worktree("a")
		writeWtFile(t, filepath.Join(repo.main, BeansDir, changesTestBean), beanContent("todo", "Edited in main."))

		core := repo.core()
		if err := core.WatchWorktreeBeans(a); err != nil {
			t.Fatal(err)
		}
		writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "Original body."))
		waitUntil(t, "link to A", func() bool { return core.WorktreeForBean("chg-x") == a })
		if status, _ := getBean(t, core, "chg-x"); status != "in-progress" {
			t.Errorf("status = %q, want in-progress", status)
		}

		runGit(t, a, "checkout", "--", filepath.Join(BeansDir, changesTestBean))
		waitUntil(t, "link to clear", func() bool { return core.WorktreeForBean("chg-x") == "" })
		if _, body := getBean(t, core, "chg-x"); body != "Edited in main." || core.IsDirty("chg-x") {
			t.Errorf("body = %q dirty = %v, want main's version, not dirty", body, core.IsDirty("chg-x"))
		}
	})

	t.Run("revert hands the bean to another worktree's change", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a, b := repo.worktree("a"), repo.worktree("b")
		writeWtFile(t, filepath.Join(b, BeansDir, changesTestBean), beanContent("completed", "B."))
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(b, BeansDir, changesTestBean), old, old); err != nil {
			t.Fatal(err)
		}
		core := repo.core()
		for _, p := range []string{a, b} {
			if err := core.WatchWorktreeBeans(p); err != nil {
				t.Fatal(err)
			}
		}
		writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "A."))
		waitUntil(t, "link to A", func() bool { return core.WorktreeForBean("chg-x") == a })

		runGit(t, a, "checkout", "--", filepath.Join(BeansDir, changesTestBean))
		waitUntil(t, "link to B", func() bool { return core.WorktreeForBean("chg-x") == b })
		if _, body := getBean(t, core, "chg-x"); body != "B." {
			t.Errorf("body = %q, want B's change", body)
		}
	})

	t.Run("new untracked bean in a worktree is linked", func(t *testing.T) {
		repo := newChangesTestRepo(t)
		a := repo.worktree("a")
		core := repo.core()
		if err := core.WatchWorktreeBeans(a); err != nil {
			t.Fatal(err)
		}
		writeWtFile(t, filepath.Join(a, BeansDir, "chg-new--new.md"), beanContent("todo", "New."))
		waitUntil(t, "new bean", func() bool { return core.WorktreeForBean("chg-new") == a })
	})
}

func TestMainDeleteKeepsLinkedWorktreeChange(t *testing.T) {
	repo := newChangesTestRepo(t)
	a := repo.worktree("a")
	writeWtFile(t, filepath.Join(a, BeansDir, changesTestBean), beanContent("in-progress", "A."))

	core := repo.core()
	if err := core.StartWatching(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Unwatch() })
	if err := core.WatchWorktreeBeans(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo.main, BeansDir, changesTestBean)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(debounceDelay + 500*time.Millisecond)
	if status, _ := getBean(t, core, "chg-x"); status != "in-progress" || core.WorktreeForBean("chg-x") != a {
		t.Errorf("status = %q link = %q, want A's change to stay", status, core.WorktreeForBean("chg-x"))
	}
}

func TestBeanIDFromRepoPath(t *testing.T) {
	tests := map[string]string{
		".beans/abc-1--title.md":         "abc-1",
		".beans/archive/abc-2--title.md": "abc-2",
		".beans/.conversations/x.md":     "",
		".beans/archive/deep/abc-3.md":   "",
		".beans/abc-4--title.txt":        "",
		"docs/abc-5--title.md":           "",
		"":                               "",
	}
	for path, want := range tests {
		if got := beanIDFromRepoPath(path); got != want {
			t.Errorf("beanIDFromRepoPath(%q) = %q, want %q", path, got, want)
		}
	}
}
