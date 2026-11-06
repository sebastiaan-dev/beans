package tui_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/hmans/beans/internal/commands" // registers the workspace support
	"github.com/hmans/beans/internal/tui"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// TestRegisteredWorkspaceStart starts a bean through the workspace support
// the commands package registers for the TUI's w key, with both backends.
// Worktrunk runs with an isolated config, approvals file and HOME, so the
// developer's own worktrunk hooks never run.
func TestRegisteredWorkspaceStart(t *testing.T) {
	support := tui.RegisteredWorkspaceSupport()
	if support.NewManager == nil || support.Start == nil {
		t.Fatal("the commands package registers no workspace support")
	}
	for _, backend := range []config.WorktreeBackend{config.WorktreeBackendGit, config.WorktreeBackendWorktrunk} {
		t.Run(string(backend), func(t *testing.T) {
			if backend == config.WorktreeBackendWorktrunk {
				if _, err := worktree.LookupWorktrunk(); err != nil {
					t.Skip("worktrunk (wt) not on PATH")
				}
			}
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo, hookLog := filepath.Join(base, "repo"), filepath.Join(base, "hooks.log")
			home := filepath.Join(base, "home")
			userConfig := filepath.Join(base, "wt-user.toml")
			if err := os.MkdirAll(filepath.Join(repo, ".config"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(userConfig, []byte(`worktree-path = "{{ repo_path }}/../{{ repo }}.{{ branch | sanitize }}"`+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for k, v := range map[string]string{
				"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
				"WORKTRUNK_CONFIG_PATH":        userConfig,
				"WORKTRUNK_SYSTEM_CONFIG_PATH": filepath.Join(base, "wt-system.toml"),
				"WORKTRUNK_APPROVALS_PATH":     filepath.Join(base, "wt-approvals.toml"),
				"GIT_CONFIG_NOSYSTEM":          "1",
				"GIT_AUTHOR_NAME":              "T", "GIT_AUTHOR_EMAIL": "t@t",
				"GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@t",
			} {
				t.Setenv(k, v)
			}
			hooks := `pre-start = "echo pre-start {{ branch }} >> ` + hookLog + `"` + "\n"
			if err := os.WriteFile(filepath.Join(repo, ".config", "wt.toml"), []byte(hooks), 0o644); err != nil {
				t.Fatal(err)
			}

			c := config.DefaultWithPrefix("tw-")
			c.Worktree.Backend = backend
			c.Worktree.Path = filepath.Join(base, "worktrees")
			c.Worktree.FetchTimeout = new(int)
			if err := c.Save(repo); err != nil {
				t.Fatal(err)
			}
			b := &bean.Bean{ID: "tw-abcd", Slug: "login", Title: "Login", Status: "todo", Type: "task"}
			content, _ := b.Render()
			if err := os.MkdirAll(filepath.Join(repo, ".beans"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".beans", "tw-abcd--login.md"), content, 0o644); err != nil {
				t.Fatal(err)
			}
			run(t, repo, "git", "init", "-q", "-b", "main")
			run(t, repo, "git", "add", "-A")
			run(t, repo, "git", "commit", "-q", "-m", "initial")
			if backend == config.WorktreeBackendWorktrunk {
				run(t, repo, "wt", "-C", repo, "config", "approvals", "add", "--yes")
			}

			cfg, err := config.Load(filepath.Join(repo, config.ConfigFileName))
			if err != nil {
				t.Fatal(err)
			}
			core := beancore.New(cfg.ResolveBeansPath(), cfg)
			core.SetWarnWriter(nil)
			if err := core.Load(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(core.UnwatchAllWorktrees)
			mgr, err := support.NewManager(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			res, err := support.Start(context.Background(), core, mgr, "tw-abcd",
				worktree.TerminalIO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out})
			if err != nil {
				t.Fatalf("start: %v\n%s", err, out.String())
			}
			name := "tw-abcd-login"
			wantPath, wantBranch := filepath.Join(base, "worktrees", name), "beans/"+name
			if backend == config.WorktreeBackendWorktrunk {
				wantPath, wantBranch = filepath.Join(base, "repo."+name), name
				// The user's wt switch ran with the project's hooks.
				deadline := time.Now().Add(10 * time.Second)
				for {
					data, _ := os.ReadFile(hookLog)
					if strings.Contains(string(data), "pre-start "+name) {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("pre-start hook did not run: %q", data)
					}
					time.Sleep(25 * time.Millisecond)
				}
			}
			if res.Path != wantPath || res.Branch != wantBranch || res.Name != name || !res.Created || res.BeanID != "tw-abcd" {
				t.Errorf("result = %+v", res)
			}
			wtCore := beancore.New(filepath.Join(res.Path, ".beans"), cfg)
			if err := wtCore.Load(); err != nil {
				t.Fatal(err)
			}
			if wb, err := wtCore.Get("tw-abcd"); err != nil || wb.Status != "in-progress" {
				t.Errorf("workspace bean = %+v, %v", wb, err)
			}
			if mb, _ := core.Get("tw-abcd"); mb.Status != "todo" {
				t.Errorf("main bean status = %q, want unchanged todo", mb.Status)
			}
		})
	}
}
