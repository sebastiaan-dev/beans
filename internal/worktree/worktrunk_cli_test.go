package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testTerminal() (TerminalIO, *bytes.Buffer) {
	var out bytes.Buffer
	return TerminalIO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out}, &out
}

func TestOpenGitCreatesWaitsAndReuses(t *testing.T) {
	repo, _, root := initTestRepo(t)
	m := NewManager(repo, root, "main", "sleep 0.2 && touch setup-ran", WithFetchTimeout(0))
	tio, _ := testTerminal()

	wt, created, err := m.Open("feat", tio)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !created || wt.ID != "feat" || wt.Branch != "beans/feat" || wt.Path != filepath.Join(root, "feat") {
		t.Fatalf("Open = %+v, created %v", wt, created)
	}
	// Open waits for worktree.setup.
	if _, err := os.Stat(filepath.Join(wt.Path, "setup-ran")); err != nil {
		t.Errorf("setup not finished when Open returned: %v", err)
	}

	again, created, err := m.Open("feat", tio)
	if err != nil || created || !samePath(again.Path, wt.Path) {
		t.Fatalf("reopen = %+v, created %v, err %v", again, created, err)
	}
}

func TestOpenGitSetupFailure(t *testing.T) {
	repo, _, root := initTestRepo(t)
	m := NewManager(repo, root, "main", "echo boom; exit 1", WithFetchTimeout(0))
	tio, _ := testTerminal()

	wt, created, err := m.Open("feat", tio)
	if err == nil || !strings.Contains(err.Error(), "setup failed") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want setup failure with output", err)
	}
	if wt == nil || !created {
		t.Fatalf("the worktree should still be returned as created: %+v, %v", wt, created)
	}
}

func TestOpenInvalidName(t *testing.T) {
	repo, _, root := initTestRepo(t)
	m := NewManager(repo, root, "main", "", WithFetchTimeout(0))
	tio, _ := testTerminal()
	if _, _, err := m.Open("feat/x", tio); err == nil {
		t.Fatal("expected an invalid name error")
	}
}

func TestOpenWorktrunk(t *testing.T) {
	r := setupWorktrunkRepo(t, true)
	m := r.manager(t, "touch setup-ran")
	if err := os.MkdirAll(filepath.Join(r.base, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	tio, out := testTerminal()

	wt, created, err := m.Open("feat", tio)
	if err != nil {
		t.Fatalf("Open: %v\n%s", err, out)
	}
	wantPath := filepath.Join(r.base, "repo.feat")
	if !created || wt.Branch != "feat" || wt.ID != "feat" || wt.Path != wantPath || wt.Name != "feat" {
		t.Fatalf("Open = %+v, created %v", wt, created)
	}
	// The user's normal switch: creation and switch hooks all run.
	for _, h := range []string{"pre-switch feat", "pre-start feat", "post-start feat", "post-switch feat"} {
		r.waitForHook(t, h)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "setup-ran")); err != nil {
		t.Errorf("worktree.setup did not run: %v", err)
	}
	// Metadata like Create's, so beans-serve names it.
	if wts, err := m.List(); err != nil || len(wts) != 1 || wts[0].Name != "feat" || wts[0].LastActiveAt.IsZero() {
		t.Errorf("List() = %+v, %v", wts, err)
	}

	// Reuse: wt switches to the existing worktree (switch hooks only).
	r.waitForBackgroundHooks()
	if err := os.WriteFile(r.hookLog, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	again, created, err := m.Open("feat", tio)
	if err != nil || created || again.Path != wantPath {
		t.Fatalf("reopen = %+v, created %v, err %v\n%s", again, created, err, out)
	}
	r.waitForHook(t, "post-switch feat")
	if lines := r.hookLines(); indexOf(lines, "pre-start feat") >= 0 {
		t.Errorf("creation hooks ran on reuse: %v", lines)
	}

	// An existing branch without a worktree is checked out, not re-created.
	gitRun(t, r.repo, "branch", "other")
	wt, created, err = m.Open("other", tio)
	if err != nil || !created || wt.Branch != "other" {
		t.Fatalf("Open(existing branch) = %+v, created %v, err %v\n%s", wt, created, err, out)
	}
}

func TestOpenWorktrunkNeedsApproval(t *testing.T) {
	r := setupWorktrunkRepo(t, false)
	m := r.manager(t, "")
	tio, out := testTerminal()

	if _, _, err := m.Open("feat", tio); err == nil {
		t.Fatal("expected an error without a terminal to approve hooks")
	}
	if !strings.Contains(out.String(), "approval") {
		t.Errorf("wt's approval message not passed through: %q", out)
	}
	if wts, err := m.List(); err != nil || len(wts) != 0 {
		t.Errorf("List() = %+v, %v; want no worktrees", wts, err)
	}
}
