//go:build !windows

package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// integrateFixture is a main repo with a linked worktree whose branch holds a
// squashed commit, as after steps 3a–3c of the integrate flow.
type integrateFixture struct {
	main, wt string
}

func (f integrateFixture) write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f integrateFixture) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.main, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func gitOut(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func newIntegrateFixture(t *testing.T, committed map[string]string) integrateFixture {
	t.Helper()
	root := t.TempDir()
	f := integrateFixture{main: filepath.Join(root, "main repo"), wt: filepath.Join(root, "wt")}
	if err := os.MkdirAll(f.main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, f.main, "init", "-q", "-b", "main")
	for rel, content := range committed {
		f.write(t, f.main, rel, content)
	}
	gitCmd(t, f.main, "add", "-A")
	gitCmd(t, f.main, "commit", "-q", "--allow-empty", "-m", "init")
	gitCmd(t, f.main, "worktree", "add", "-q", f.wt, "-b", "beans/wt")
	return f
}

// squash commits the worktree's changes as the integrate flow does.
func (f integrateFixture) squash(t *testing.T, branch map[string]string, deleted ...string) {
	t.Helper()
	for rel, content := range branch {
		f.write(t, f.wt, rel, content)
	}
	for _, rel := range deleted {
		gitCmd(t, f.wt, "rm", "-q", "--", rel)
	}
	gitCmd(t, f.wt, "add", "-A")
	gitCmd(t, f.wt, "commit", "-q", "-m", "work")
	gitCmd(t, f.wt, "rebase", "-q", "main")
	gitCmd(t, f.wt, "reset", "-q", "--soft", "main")
	gitCmd(t, f.wt, "commit", "-q", "-m", "squash")
}

// prepareAndMerge runs the prompt's prepare script and, if it succeeds, the
// fast-forward from step 3e.
func (f integrateFixture) prepareAndMerge(t *testing.T) (scriptOut string, scriptErr, mergeErr error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", integratePrepareMainScript(f.main))
	cmd.Dir = f.wt
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err, nil
	}
	sha, _ := gitOut(t, f.wt, "rev-parse", "HEAD")
	mergeOut, mergeErr := gitOut(t, f.main, "merge", "--ff-only", strings.TrimSpace(sha))
	if mergeErr != nil {
		t.Logf("merge output: %s", mergeOut)
	}
	return string(out), nil, mergeErr
}

func (f integrateFixture) status(t *testing.T) string {
	t.Helper()
	out, err := gitOut(t, f.main, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

const (
	beanOld    = "---\ntitle: X\nstatus: todo\n---\nold\n"
	beanEdited = "---\ntitle: X\nstatus: todo\n---\nedited in the UI\n"
	beanDone   = "---\ntitle: X\nstatus: completed\n---\nedited in the UI\n"
)

func TestIntegratePrepareMainScript(t *testing.T) {
	t.Run("plain ff-only merge aborts on identical uncommitted bean edits", func(t *testing.T) {
		// The bug this script fixes (hmans/beans repos-b35y).
		f := newIntegrateFixture(t, map[string]string{".beans/x--x.md": beanOld})
		f.write(t, f.main, ".beans/x--x.md", beanEdited)
		f.squash(t, map[string]string{".beans/x--x.md": beanEdited})
		sha, _ := gitOut(t, f.wt, "rev-parse", "HEAD")
		if out, err := gitOut(t, f.main, "merge", "--ff-only", strings.TrimSpace(sha)); err == nil {
			t.Fatalf("expected the merge to abort, got: %s", out)
		}
	})

	t.Run("drops identical modified, staged and untracked bean copies", func(t *testing.T) {
		f := newIntegrateFixture(t, map[string]string{
			".beans/x--x.md":       beanOld,
			".beans/staged--s.md":  beanOld,
			".beans/gone--g.md":    beanOld,
			".beans/with space.md": beanOld,
		})
		// Uncommitted edits in main, all identical to the branch's version.
		f.write(t, f.main, ".beans/x--x.md", beanEdited)
		f.write(t, f.main, ".beans/staged--s.md", beanEdited)
		gitCmd(t, f.main, "add", ".beans/staged--s.md")
		f.write(t, f.main, ".beans/y--new.md", beanEdited) // never committed (Start Work copied it)
		f.write(t, f.main, ".beans/with space.md", beanEdited)
		if err := os.Remove(filepath.Join(f.main, ".beans/gone--g.md")); err != nil {
			t.Fatal(err)
		}
		// An unrelated uncommitted edit the branch doesn't touch must survive.
		f.write(t, f.main, "notes.txt", "keep me\n")

		f.squash(t, map[string]string{
			".beans/x--x.md":       beanEdited,
			".beans/staged--s.md":  beanEdited,
			".beans/y--new.md":     beanEdited,
			".beans/with space.md": beanEdited,
		}, ".beans/gone--g.md")

		out, scriptErr, mergeErr := f.prepareAndMerge(t)
		if scriptErr != nil {
			t.Fatalf("script failed: %v\n%s", scriptErr, out)
		}
		if mergeErr != nil {
			t.Fatalf("merge failed after the script: %v\nscript output:\n%s", mergeErr, out)
		}
		if got := f.read(t, ".beans/x--x.md"); got != beanEdited {
			t.Errorf("x--x.md = %q, want the edited content", got)
		}
		if _, err := os.Stat(filepath.Join(f.main, ".beans/gone--g.md")); !os.IsNotExist(err) {
			t.Errorf("gone--g.md should stay deleted, stat err = %v", err)
		}
		if got := f.status(t); got != "?? notes.txt" {
			t.Errorf("main status after integrate = %q, want only the unrelated untracked file", got)
		}
	})

	t.Run("stops without touching main when contents differ", func(t *testing.T) {
		f := newIntegrateFixture(t, map[string]string{".beans/x--x.md": beanOld, ".beans/z--z.md": beanOld})
		f.write(t, f.main, ".beans/x--x.md", beanEdited)       // differs from the branch
		f.write(t, f.main, ".beans/z--z.md", beanEdited)       // identical to the branch
		f.write(t, f.main, ".beans/n--new.md", "main version") // untracked, differs
		f.squash(t, map[string]string{
			".beans/x--x.md":   beanDone,
			".beans/z--z.md":   beanEdited,
			".beans/n--new.md": "branch version",
		})
		before := f.status(t)

		out, scriptErr, _ := f.prepareAndMerge(t)
		if scriptErr == nil {
			t.Fatalf("script should fail, output:\n%s", out)
		}
		for _, want := range []string{"CONFLICT", ".beans/x--x.md", ".beans/n--new.md"} {
			if !strings.Contains(out, want) {
				t.Errorf("output should mention %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "z--z.md") {
			t.Errorf("identical file should not be reported as a conflict:\n%s", out)
		}
		if after := f.status(t); after != before {
			t.Errorf("main was modified: status %q -> %q", before, after)
		}
		if got := f.read(t, ".beans/x--x.md"); got != beanEdited {
			t.Errorf("x--x.md was modified: %q", got)
		}
		if got := f.read(t, ".beans/z--z.md"); got != beanEdited {
			t.Errorf("z--z.md was modified even though the script failed: %q", got)
		}
	})

	t.Run("stops when the staged version differs", func(t *testing.T) {
		f := newIntegrateFixture(t, map[string]string{".beans/x--x.md": beanOld})
		f.write(t, f.main, ".beans/x--x.md", beanDone)
		gitCmd(t, f.main, "add", ".beans/x--x.md")
		f.write(t, f.main, ".beans/x--x.md", beanEdited) // worktree matches branch, index doesn't
		f.squash(t, map[string]string{".beans/x--x.md": beanEdited})

		out, scriptErr, _ := f.prepareAndMerge(t)
		if scriptErr == nil || !strings.Contains(out, "CONFLICT") {
			t.Fatalf("script should report a conflict, err=%v output:\n%s", scriptErr, out)
		}
	})

	t.Run("no-op when main is clean", func(t *testing.T) {
		f := newIntegrateFixture(t, map[string]string{".beans/x--x.md": beanOld})
		f.squash(t, map[string]string{".beans/x--x.md": beanDone, "code.go": "package x\n"})
		out, scriptErr, mergeErr := f.prepareAndMerge(t)
		if scriptErr != nil || mergeErr != nil {
			t.Fatalf("script err=%v merge err=%v output:\n%s", scriptErr, mergeErr, out)
		}
		if strings.Contains(out, "Dropped") {
			t.Errorf("nothing should have been dropped:\n%s", out)
		}
		if got := f.status(t); got != "" {
			t.Errorf("main status = %q, want clean", got)
		}
	})
}

func TestIntegratePromptIncludesPrepareScript(t *testing.T) {
	action := findAgentAction("integrate")
	if action == nil {
		t.Fatal("integrate action not found")
	}
	prompt := action.PromptFunc(actionContext{MainRepoPath: "/repo/it's here"})
	if !strings.Contains(prompt, integratePrepareMainScript("/repo/it's here")) {
		t.Error("prompt should contain the prepare script")
	}
	if !strings.Contains(prompt, `git -C '/repo/it'\''s here' merge --ff-only $SQUASH_SHA`) {
		t.Errorf("prompt should contain the quoted ff-only merge:\n%s", prompt)
	}
	if !strings.Contains(prompt, "If it prints CONFLICT, STOP") {
		t.Error("prompt should tell the agent to stop on conflicts")
	}
}

func TestMainRepoHasNonBeanChanges(t *testing.T) {
	setup := func(t *testing.T) string {
		repo := t.TempDir()
		gitCmd(t, repo, "init", "-q", "-b", "main")
		for _, rel := range []string{".beans/x--x.md", "code.go"} {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, rel), []byte("v1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitCmd(t, repo, "add", "-A")
		gitCmd(t, repo, "commit", "-q", "-m", "init")
		return repo
	}
	write := func(t *testing.T, repo, rel string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, rel), []byte("v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		edits []string
		want  bool
	}{
		{"clean", nil, false},
		{"modified bean", []string{".beans/x--x.md"}, false},
		{"new and archived beans", []string{".beans/y--y.md", ".beans/archive/z--z.md"}, false},
		{"modified code", []string{"code.go"}, true},
		{"untracked file", []string{"notes.txt"}, true},
		{"bean and code", []string{".beans/x--x.md", "code.go"}, true},
		{"lookalike directory", []string{".beans-other/a.md"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setup(t)
			for _, rel := range tt.edits {
				write(t, repo, rel)
			}
			if got := mainRepoHasNonBeanChanges(repo, filepath.Join(repo, ".beans")); got != tt.want {
				t.Errorf("mainRepoHasNonBeanChanges() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("beans dir outside the repo counts everything", func(t *testing.T) {
		repo := setup(t)
		write(t, repo, ".beans/x--x.md")
		if !mainRepoHasNonBeanChanges(repo, t.TempDir()) {
			t.Error("want true when the beans dir isn't inside the repo")
		}
	})
}
