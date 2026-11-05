package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// worktrunkRepo is a temporary git repository with worktrunk project hooks,
// used with an isolated worktrunk configuration.
type worktrunkRepo struct {
	bin     string // wt executable
	base    string // directory containing the repo and its worktrees
	repo    string // main worktree
	hookLog string // file the project hooks append to
}

// setupWorktrunkRepo creates a repository whose project config
// (.config/wt.toml) defines a hook for every creation, switch and removal
// event, each appending "<hook> <branch>" to the hook log. Worktrunk runs with
// an isolated user config (worktree-path template "<repo>.<branch>" next to
// the repo), approvals file and HOME, so the developer's own worktrunk hooks
// never run. Project hooks are approved when approve is true. Skips the test
// when wt isn't installed.
func setupWorktrunkRepo(t *testing.T, approve bool) *worktrunkRepo {
	t.Helper()
	bin, err := LookupWorktrunk()
	if err != nil {
		t.Skip("worktrunk (wt) not on PATH")
	}

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &worktrunkRepo{bin: bin, base: base, repo: filepath.Join(base, "repo"), hookLog: filepath.Join(base, "hooks.log")}

	home := filepath.Join(base, "home")
	userConfig := filepath.Join(base, "wt-user.toml")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`worktree-path = "{{ repo_path }}/../{{ repo }}.{{ branch | sanitize }}"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("WORKTRUNK_CONFIG_PATH", userConfig)
	t.Setenv("WORKTRUNK_SYSTEM_CONFIG_PATH", filepath.Join(base, "wt-system.toml"))
	t.Setenv("WORKTRUNK_APPROVALS_PATH", filepath.Join(base, "wt-approvals.toml"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	if err := os.MkdirAll(filepath.Join(r.repo, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.repo, ".beans"), 0o755); err != nil {
		t.Fatal(err)
	}
	var hooks strings.Builder
	for _, h := range []string{"pre-switch", "post-switch", "pre-start", "post-start", "pre-remove", "post-remove"} {
		// {{ branch }} is unset in detached worktrees, so guard it.
		hooks.WriteString(h + ` = "echo ` + h + ` {% if branch %}{{ branch }}{% else %}detached{% endif %} >> ` + r.hookLog + `"` + "\n")
	}
	if err := os.WriteFile(filepath.Join(r.repo, ".config", "wt.toml"), []byte(hooks.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.repo, ".beans", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"add", "-A"},
		{"commit", "-m", "initial"},
	} {
		gitRun(t, r.repo, args...)
	}

	if approve {
		r.wt(t, "-C", r.repo, "config", "approvals", "add", "--yes")
	}
	// wt runs post-* hooks in the background; let them finish before the
	// temporary directory is removed (cleanups run in reverse order).
	t.Cleanup(r.waitForBackgroundHooks)
	return r
}

// waitForBackgroundHooks waits until wt's background hook processes are done:
// every post-start and post-remove hook has logged and the log stopped growing.
func (r *worktrunkRepo) waitForBackgroundHooks() {
	deadline := time.Now().Add(10 * time.Second)
	lastSize, stableSince := int64(-1), time.Now()
	for time.Now().Before(deadline) {
		var size int64
		if fi, err := os.Stat(r.hookLog); err == nil {
			size = fi.Size()
		}
		if size != lastSize {
			lastSize, stableSince = size, time.Now()
		}
		lines := r.hookLines()
		pending := 0
		for _, l := range lines {
			switch {
			case strings.HasPrefix(l, "pre-start="):
				pending++
			case strings.HasPrefix(l, "post-start="):
				pending--
			}
		}
		if pending <= 0 && time.Since(stableSince) > 300*time.Millisecond {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// wt runs worktrunk and fails the test on error.
func (r *worktrunkRepo) wt(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(r.bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wt %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// hookLines returns the lines of the hook log.
func (r *worktrunkRepo) hookLines() []string {
	data, _ := os.ReadFile(r.hookLog)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), " ", "="))
}

// waitForHook waits until the hook log contains the line "<hook> <branch>".
func (r *worktrunkRepo) waitForHook(t *testing.T, line string) {
	t.Helper()
	want := strings.ReplaceAll(line, " ", "=")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range r.hookLines() {
			if l == want {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("hook %q did not run; hook log: %v", line, r.hookLines())
}

func (r *worktrunkRepo) manager(t *testing.T, setup string) *Manager {
	t.Helper()
	return NewManager(r.repo, filepath.Join(r.base, "meta"), "main", setup,
		WithBackend(NewWorktrunkBackend(r.bin)), WithFetchTimeout(0))
}

func indexOf(lines []string, line string) int {
	want := strings.ReplaceAll(line, " ", "=")
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

func findWorktree(wts []Worktree, id string) *Worktree {
	for i := range wts {
		if wts[i].ID == id {
			return &wts[i]
		}
	}
	return nil
}

func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repo
	return cmd.Run() == nil
}

func TestWorktrunkCreateListRemove(t *testing.T) {
	r := setupWorktrunkRepo(t, true)
	if err := os.MkdirAll(filepath.Join(r.base, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := r.manager(t, "echo beans-setup x >> "+r.hookLog)

	type setupResult struct {
		id  string
		ok  bool
		out string
	}
	done := make(chan setupResult, 1)
	m.SetOnSetupDone(func(id string, ok bool, out string) { done <- setupResult{id, ok, out} })

	wt, err := m.Create("feat-one")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantPath := filepath.Join(r.base, "repo.feat-one")
	if wt.ID != "feat-one" || wt.Branch != "feat-one" || wt.Path != wantPath || wt.Name != "feat-one" {
		t.Fatalf("created %+v, want ID/branch/name feat-one at %s", wt, wantPath)
	}
	if wt.Setup != SetupRunning {
		t.Errorf("Setup = %q, want running", wt.Setup)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("worktree not at worktrunk's template path: %v", err)
	}

	select {
	case res := <-done:
		if !res.ok || res.id != "feat-one" {
			t.Fatalf("setup = %+v, want success", res)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("setup did not finish")
	}
	// post-start runs in the background (spawned by wt).
	r.waitForHook(t, "post-start feat-one")
	lines := r.hookLines()
	pre, setup, post := indexOf(lines, "pre-start feat-one"), indexOf(lines, "beans-setup x"), indexOf(lines, "post-start feat-one")
	if pre < 0 || setup < 0 || !(pre < setup && setup < post) {
		t.Errorf("want pre-start, then worktree.setup, then post-start; hook log: %v", lines)
	}
	for _, h := range []string{"pre-switch feat-one", "post-switch feat-one", "pre-switch main"} {
		if indexOf(lines, h) >= 0 {
			t.Errorf("switch hook %q must not run; hook log: %v", h, lines)
		}
	}

	wts, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 || wts[0].ID != "feat-one" || wts[0].Setup != SetupDone || wts[0].Name != "feat-one" {
		t.Fatalf("List = %+v", wts)
	}
	if p, err := m.Path("feat-one"); err != nil || p != wantPath {
		t.Errorf("Path = %q, %v; want %q", p, err, wantPath)
	}
	if _, err := m.Create("feat-one"); err == nil {
		t.Error("creating a workspace with an existing name should fail")
	}

	// Uncommitted changes are discarded on removal (like git worktree remove --force).
	if err := os.WriteFile(filepath.Join(wantPath, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("feat-one"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Errorf("worktree directory still exists after remove: %v", err)
	}
	r.waitForHook(t, "pre-remove feat-one")
	r.waitForHook(t, "post-remove feat-one")
	if branchExists(t, r.repo, "feat-one") {
		t.Error("merged (unchanged) branch should be deleted by wt remove")
	}
	if wts, _ := m.List(); len(wts) != 0 {
		t.Errorf("List after remove = %+v", wts)
	}
	if _, err := os.Stat(filepath.Join(r.base, "meta", "feat-one.meta.json")); !os.IsNotExist(err) {
		t.Error("metadata should be removed with the worktree")
	}
}

func TestWorktrunkRemoveKeepsUnmergedBranch(t *testing.T) {
	r := setupWorktrunkRepo(t, true)
	m := r.manager(t, "")
	if err := os.MkdirAll(filepath.Join(r.base, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt, err := m.Create("unmerged")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "work.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt.Path, "add", "work.txt")
	gitRun(t, wt.Path, "commit", "-m", "work")

	if err := m.Remove(wt.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree should be removed")
	}
	if !branchExists(t, r.repo, "unmerged") {
		t.Error("an unmerged branch must be kept (no -D)")
	}
	r.waitForHook(t, "post-remove unmerged")
}

func TestWorktrunkAdoptsExternalWorktrees(t *testing.T) {
	r := setupWorktrunkRepo(t, true)
	m := r.manager(t, "")
	if err := os.MkdirAll(filepath.Join(r.base, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}

	changes := make(chan struct{}, 16)
	stop, err := m.WatchExternalChanges(func() { changes <- struct{}{} })
	if err != nil {
		t.Fatalf("WatchExternalChanges: %v", err)
	}
	defer stop()
	sub := m.Subscribe()
	defer m.Unsubscribe(sub)

	// waitFor waits for a change notification after which cond holds.
	waitFor := func(what string, cond func([]Worktree) bool) []Worktree {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case <-sub:
				wts, err := m.List()
				if err != nil {
					t.Fatal(err)
				}
				if cond(wts) {
					return wts
				}
			case <-deadline:
				wts, _ := m.List()
				t.Fatalf("timed out waiting for %s; worktrees: %+v", what, wts)
			}
		}
	}

	r.wt(t, "-C", r.repo, "switch", "--create", "ext/two", "--no-cd", "--no-hooks")
	wts := waitFor("ext/two to appear", func(wts []Worktree) bool { return findWorktree(wts, "ext-two") != nil })
	ext := findWorktree(wts, "ext-two")
	if ext.Branch != "ext/two" || ext.Path != filepath.Join(r.base, "repo.ext-two") || ext.Name != "ext/two" {
		t.Errorf("adopted worktree = %+v", ext)
	}
	select {
	case <-changes:
	default:
		t.Error("onChange callback was not called")
	}

	// Metadata works for adopted worktrees.
	if err := m.SavePort("ext-two", 4242); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateDescription("ext-two", "adopted"); err != nil {
		t.Fatal(err)
	}
	if got := m.GetPort("ext-two"); got != 4242 {
		t.Errorf("GetPort = %d", got)
	}

	// A detached worktree (e.g. during a rebase) keeps its ID and metadata.
	gitRun(t, ext.Path, "checkout", "--detach")
	wts = waitFor("ext-two to be detached", func(wts []Worktree) bool {
		wt := findWorktree(wts, "ext-two")
		return wt != nil && wt.Detached
	})
	if wt := findWorktree(wts, "ext-two"); wt.Description != "adopted" {
		t.Errorf("detached worktree lost its metadata: %+v", wt)
	}
	// ...also across a restart (from the path recorded in the metadata).
	fresh := r.manager(t, "")
	if wts, _ := fresh.List(); findWorktree(wts, "ext-two") == nil {
		t.Errorf("a new manager should find the detached worktree's ID from metadata; got %+v", wts)
	}
	gitRun(t, ext.Path, "checkout", "ext/two")

	// Plain git worktrees are workspaces too.
	plain := filepath.Join(r.base, "plain-dir")
	gitRun(t, r.repo, "worktree", "add", "-b", "plain", plain)
	waitFor("plain to appear", func(wts []Worktree) bool { return findWorktree(wts, "plain") != nil })

	// Removal outside beans makes them disappear.
	r.wt(t, "-C", r.repo, "remove", "ext/two", "--foreground", "--no-hooks")
	waitFor("ext-two to disappear", func(wts []Worktree) bool { return findWorktree(wts, "ext-two") == nil })
	gitRun(t, r.repo, "worktree", "remove", plain)
	waitFor("plain to disappear", func(wts []Worktree) bool { return findWorktree(wts, "plain") == nil })

	// Stale metadata of an externally removed worktree is not inherited by a
	// new worktree that gets the same ID.
	r.wt(t, "-C", r.repo, "switch", "--create", "ext-two", "--no-cd", "--no-hooks")
	wts = waitFor("new ext-two", func(wts []Worktree) bool { return findWorktree(wts, "ext-two") != nil })
	if wt := findWorktree(wts, "ext-two"); wt.Description != "" {
		t.Errorf("new worktree inherited stale metadata: %+v", wt)
	}
}

func TestWorktrunkUnapprovedHooks(t *testing.T) {
	r := setupWorktrunkRepo(t, false)
	m := r.manager(t, "echo beans-setup x >> "+r.hookLog)
	if err := os.MkdirAll(filepath.Join(r.base, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	m.SetOnSetupDone(func(_ string, ok bool, _ string) { done <- ok })

	wt, err := m.Create("needs-approval")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.Setup != SetupFailed || !strings.Contains(wt.SetupError, "wt config approvals add") || !strings.Contains(wt.SetupError, "pre-start") {
		t.Fatalf("Setup = %q, SetupError = %q; want a failed setup explaining how to approve", wt.Setup, wt.SetupError)
	}
	select {
	case ok := <-done:
		if ok {
			t.Error("setup callback reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setup callback not called")
	}
	time.Sleep(200 * time.Millisecond)
	if lines := r.hookLines(); len(lines) != 0 {
		t.Errorf("no hooks (and no worktree.setup) may run without approval; hook log: %v", lines)
	}

	// Removal needs approval of the remove hooks; beans doesn't approve them.
	err = m.Remove(wt.ID)
	var approvalErr *ApprovalError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("Remove error = %v, want ApprovalError", err)
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Error("worktree must not be removed when its hooks are not approved")
	}

	r.wt(t, "-C", r.repo, "config", "approvals", "add", "--yes")
	if err := m.Remove(wt.ID); err != nil {
		t.Fatalf("Remove after approval: %v", err)
	}
	r.waitForHook(t, "pre-remove needs-approval")
}

func TestGitBackendWatchesExternalWorktrees(t *testing.T) {
	repoDir, _, wtRoot := initTestRepo(t)
	m := NewManager(repoDir, wtRoot, "", "")
	stop, err := m.WatchExternalChanges(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	sub := m.Subscribe()
	defer m.Unsubscribe(sub)

	gitRun(t, repoDir, "worktree", "add", "-b", "beans/external", filepath.Join(wtRoot, "external"))
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-sub:
			if wts, _ := m.List(); findWorktree(wts, "external") != nil {
				return
			}
		case <-deadline:
			t.Fatal("externally added beans/ worktree did not trigger a notification")
		}
	}
}
