//go:build !windows

package graph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/internal/portalloc"
	"github.com/hmans/beans/internal/terminal"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
)

// setupWorktrunkResolver creates a repository with worktrunk project hooks
// (each appending "<hook> <branch>" to the returned log) and a resolver using
// the worktrunk backend. Worktrunk runs with an isolated user config (path
// template "<repo>.<branch>" next to the repo), approvals and HOME, so the
// developer's own worktrunk hooks never run. Skips when wt isn't installed.
func setupWorktrunkResolver(t *testing.T) (r *Resolver, base, repo, hookLog string) {
	t.Helper()
	wtBin, err := worktree.LookupWorktrunk()
	if err != nil {
		t.Skip("worktrunk (wt) not on PATH")
	}
	base, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(base, "repo")
	hookLog = filepath.Join(base, "hooks.log")
	home := filepath.Join(base, "home")
	userConfig := filepath.Join(base, "wt-user.toml")
	for _, dir := range []string{home, filepath.Join(repo, ".config"), filepath.Join(repo, ".beans")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(userConfig, []byte(`worktree-path = "{{ repo_path }}/../{{ repo }}.{{ branch | sanitize }}"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("WORKTRUNK_CONFIG_PATH", userConfig)
	t.Setenv("WORKTRUNK_SYSTEM_CONFIG_PATH", filepath.Join(base, "wt-system.toml"))
	t.Setenv("WORKTRUNK_APPROVALS_PATH", filepath.Join(base, "wt-approvals.toml"))

	var hooks strings.Builder
	for _, h := range []string{"pre-switch", "post-switch", "pre-start", "post-start", "pre-remove", "post-remove"} {
		hooks.WriteString(h + ` = "echo ` + h + ` {{ branch }} >> ` + hookLog + `"` + "\n")
	}
	if err := os.WriteFile(filepath.Join(repo, ".config", "wt.toml"), []byte(hooks.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Worktree.Backend = config.WorktreeBackendWorktrunk
	core := beancore.New(filepath.Join(repo, ".beans"), cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	createTestBean(t, core, "wtk-1", "Worktrunk Bean", "todo")
	gitCmd(t, repo, "init", "-b", "main")
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-m", "init")

	approve := exec.Command(wtBin, "-C", repo, "config", "approvals", "add", "--yes")
	if out, err := approve.CombinedOutput(); err != nil {
		t.Fatalf("wt config approvals add: %v\n%s", err, out)
	}

	backend, err := worktree.NewBackend(string(cfg.GetWorktreeBackend()))
	if err != nil {
		t.Fatal(err)
	}
	metaDir := filepath.Join(base, "meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	termMgr := terminal.NewManager(nil)
	r = &Resolver{
		CoreResolver: &beangraph.CoreResolver{Core: core},
		WorktreeMgr:  worktree.NewManager(repo, metaDir, "main", "", worktree.WithFetchTimeout(0), worktree.WithBackend(backend)),
		TerminalMgr:  termMgr,
		PortAlloc:    portalloc.NewDefault(),
		ProjectRoot:  repo,
	}
	t.Cleanup(func() {
		termMgr.Shutdown()
		core.UnwatchAllWorktrees()
		// Let wt's background post-* hooks finish before the temp dir goes.
		time.Sleep(500 * time.Millisecond)
	})
	return r, base, repo, hookLog
}

func waitForFile(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		time.Sleep(25 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	return ""
}

func findModelWorktree(wts []*model.Worktree, id string) *model.Worktree {
	for _, wt := range wts {
		if wt.ID == id {
			return wt
		}
	}
	return nil
}

func TestWorktrunkBackendCreateListRemove(t *testing.T) {
	r, base, repo, hookLog := setupWorktrunkResolver(t)
	ctx := context.Background()
	mr, qr := r.Mutation(), r.Query()

	if got, err := qr.WorktreeBackend(ctx); err != nil || got != "worktrunk" {
		t.Fatalf("worktreeBackend = %q, %v", got, err)
	}

	beanID := "wtk-1"
	wt, err := mr.CreateWorktree(ctx, "feat-graph", &beanID)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	wantPath := filepath.Join(base, "repo.feat-graph")
	if wt.ID != "feat-graph" || wt.Branch != "feat-graph" || wt.Path != wantPath {
		t.Fatalf("created %+v, want ID and branch feat-graph at %s", wt, wantPath)
	}
	if _, err := os.Stat(filepath.Join(wantPath, ".beans", "wtk-1--worktrunk-bean.md")); err != nil {
		t.Errorf("bean not written into the worktree: %v", err)
	}
	waitForFile(t, hookLog, "post-start feat-graph")
	if log, _ := os.ReadFile(hookLog); strings.Contains(string(log), "switch") {
		t.Errorf("switch hooks must not run:\n%s", log)
	}
	if port, _ := qr.WorkspacePort(ctx, "feat-graph"); port == 0 {
		t.Error("workspace port not allocated")
	}

	// The terminal resolves the workspace's real (template) path.
	pwdFile := filepath.Join(base, "pwd.txt")
	if _, err := mr.WriteTerminalInput(ctx, "feat-graph", "pwd > "+pwdFile+"\n"); err != nil {
		t.Fatalf("WriteTerminalInput: %v", err)
	}
	waitForFile(t, pwdFile, wantPath)

	// A worktree created outside beans is listed too, named after its branch.
	gitCmd(t, repo, "worktree", "add", "-b", "ext/graph", filepath.Join(base, "external"))
	wts, err := qr.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 2 || findModelWorktree(wts, "feat-graph") == nil {
		t.Fatalf("worktrees = %+v", wts)
	}
	ext := findModelWorktree(wts, "ext-graph")
	if ext == nil || ext.Branch != "ext/graph" || ext.Name == nil || *ext.Name != "ext/graph" {
		t.Fatalf("external worktree = %+v", ext)
	}
	for _, w := range wts {
		if w.Path == repo {
			t.Fatal("the main worktree must not be listed")
		}
	}

	if ok, err := mr.RemoveWorktree(ctx, "feat-graph"); err != nil || !ok {
		t.Fatalf("RemoveWorktree: %v, %v", ok, err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Errorf("worktree still exists: %v", err)
	}
	waitForFile(t, hookLog, "pre-remove feat-graph")
	waitForFile(t, hookLog, "post-remove feat-graph")
	if r.TerminalMgr.Get("feat-graph") != nil {
		t.Error("terminal session should be closed")
	}

	if ok, err := mr.RemoveWorktree(ctx, "ext-graph"); err != nil || !ok {
		t.Fatalf("RemoveWorktree(ext-graph): %v, %v", ok, err)
	}
	wts, _ = qr.Worktrees(ctx)
	if len(wts) != 0 {
		t.Fatalf("worktrees after removal = %+v", wts)
	}
	waitForFile(t, hookLog, "post-remove ext/graph")
}
