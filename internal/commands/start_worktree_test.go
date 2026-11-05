package commands

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

func TestWorktreeNameForBean(t *testing.T) {
	tests := []struct {
		name string
		bean bean.Bean
		want string
	}{
		{"id and slug", bean.Bean{ID: "beans-x7k2", Slug: "add-login-form"}, "beans-x7k2-add-login-form"},
		{"title when no slug", bean.Bean{ID: "beans-x7k2", Title: "Fix: the Login Bug!"}, "beans-x7k2-fix-the-login-bug"},
		{"id only", bean.Bean{ID: "beans-x7k2"}, "beans-x7k2"},
		{"shortened at word boundary", bean.Bean{ID: "beans-x7k2", Slug: "make-the-worktree-names-derived-from-beans-short"}, "beans-x7k2-make-the-worktree-names"},
		{"long word is dropped", bean.Bean{ID: "beans-x7k2", Slug: strings.Repeat("a", 50)}, "beans-x7k2"},
		{"invalid id is slugified", bean.Bean{ID: "_Weird__ID", Slug: "x"}, "weird-id-x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worktreeNameForBean(&tt.bean)
			if got != tt.want {
				t.Errorf("worktreeNameForBean() = %q, want %q", got, tt.want)
			}
			if err := worktree.ValidateName(got); err != nil {
				t.Errorf("derived name is invalid: %v", err)
			}
			if len(got) > maxDerivedNameLength {
				t.Errorf("derived name %q is longer than %d", got, maxDerivedNameLength)
			}
			if id := worktree.SanitizeID(got); id != got {
				t.Errorf("derived name %q is not its own workspace ID (%q)", got, id)
			}
		})
	}
}

func TestParseStartArgs(t *testing.T) {
	tests := []struct {
		args     []string
		dashAt   int
		wantID   string
		wantExec []string
		wantErr  bool
	}{
		{[]string{"abc"}, -1, "abc", nil, false},
		{[]string{"abc", "run", "--fast"}, 1, "abc", []string{"run", "--fast"}, false},
		{[]string{"abc"}, 1, "abc", []string{}, false},
		{[]string{}, -1, "", nil, true},
		{[]string{"abc", "def"}, -1, "", nil, true},
		{[]string{"x"}, 0, "", nil, true},
	}
	for _, tt := range tests {
		id, execArgs, err := parseStartArgs(tt.args, tt.dashAt)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseStartArgs(%v, %d) error = %v, wantErr %v", tt.args, tt.dashAt, err, tt.wantErr)
			continue
		}
		if err == nil && (id != tt.wantID || strings.Join(execArgs, " ") != strings.Join(tt.wantExec, " ")) {
			t.Errorf("parseStartArgs(%v, %d) = %q, %v; want %q, %v", tt.args, tt.dashAt, id, execArgs, tt.wantID, tt.wantExec)
		}
	}
}

func TestStartWorktreeOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    startWorktreeOptions
		wantErr string
	}{
		{"plain start", startWorktreeOptions{}, ""},
		{"plain start json", startWorktreeOptions{json: true}, ""},
		{"worktree", startWorktreeOptions{worktree: true}, ""},
		{"worktree json", startWorktreeOptions{worktree: true, json: true}, ""},
		{"worktree name", startWorktreeOptions{worktree: true, name: "feat-x"}, ""},
		{"worktree exec args", startWorktreeOptions{worktree: true, exec: "pi", execArgs: []string{"hi"}}, ""},
		{"name without worktree", startWorktreeOptions{name: "x"}, "--name requires --worktree"},
		{"exec without worktree", startWorktreeOptions{exec: "pi"}, "--exec requires --worktree"},
		{"args without worktree", startWorktreeOptions{execArgs: []string{"a"}}, "require --worktree"},
		{"args without exec", startWorktreeOptions{worktree: true, execArgs: []string{"a"}}, "require --exec"},
		{"exec with json", startWorktreeOptions{worktree: true, exec: "pi", json: true}, "cannot be combined with --json"},
		{"invalid name", startWorktreeOptions{worktree: true, name: "feat/x"}, "invalid worktree name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// runStartCmd runs `beans <args>` through a fresh root command with the
// start command (flag state persists between executions of one command).
func runStartCmd(t *testing.T, args ...string) error {
	t.Helper()
	oldCore, oldCfg := core, cfg
	t.Cleanup(func() { core, cfg = oldCore, oldCfg })
	root := NewRootCmd()
	RegisterStartCmd(root)
	root.SetArgs(args)
	return root.Execute()
}

func TestStartCmdFlags(t *testing.T) {
	root := testRootCmd()
	start, _, err := root.Find([]string{"start"})
	if err != nil {
		t.Fatal(err)
	}
	for short, long := range map[string]string{"w": "worktree", "x": "exec"} {
		f := start.Flags().ShorthandLookup(short)
		if f == nil || f.Name != long {
			t.Errorf("-%s should be the shorthand of --%s", short, long)
		}
	}
	if start.Flags().Lookup("name") == nil {
		t.Error("--name flag missing")
	}

	setupWorkflowCore(t, []wfBean{{id: "a1"}})
	beansDir := core.Root()
	tests := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"start", "a1", "-x", "pi"}, "--exec requires --worktree"},
		{[]string{"start", "a1", "--name", "foo"}, "--name requires --worktree"},
		{[]string{"start", "a1", "-w", "--", "hi"}, "require --exec"},
		{[]string{"start", "a1", "-w", "-x", "pi", "--json"}, "cannot be combined with --json"},
		{[]string{"start", "a1", "a2"}, "accepts 1 bean ID"},
	}
	for _, tt := range tests {
		err := runStartCmd(t, append([]string{"--beans-path", beansDir}, tt.args...)...)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("beans %v: error = %v, want containing %q", tt.args, err, tt.wantErr)
		}
	}
}

// startRepo is a temporary project repository for `beans start --worktree`.
type startRepo struct {
	base     string // temp dir holding the repo and its worktrees
	repo     string // main worktree
	config   string // .beans.yml
	beanFile string // relative path of the test bean in .beans/
	beanID   string
	hookLog  string
}

// setupStartRepo creates a git repository with a beans project
// (worktree.backend set to backend) holding one committed todo bean with an
// uncommitted body edit in main. HOME and worktrunk's config, approvals and
// system config point into the temp dir, so the developer's own worktrunk
// config and hooks never run.
func setupStartRepo(t *testing.T, backend config.WorktreeBackend, setup string) *startRepo {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &startRepo{
		base:     base,
		repo:     filepath.Join(base, "repo"),
		beanID:   "test-ab12",
		beanFile: "test-ab12--add-login-form.md",
		hookLog:  filepath.Join(base, "hooks.log"),
	}
	r.config = filepath.Join(r.repo, ".beans.yml")

	home := filepath.Join(base, "home")
	userConfig := filepath.Join(base, "wt-user.toml")
	mustMkdir(t, home)
	mustWrite(t, userConfig, `worktree-path = "{{ repo_path }}/../{{ repo }}.{{ branch | sanitize }}"`+"\n")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("WORKTRUNK_CONFIG_PATH", userConfig)
	t.Setenv("WORKTRUNK_SYSTEM_CONFIG_PATH", filepath.Join(base, "wt-system.toml"))
	t.Setenv("WORKTRUNK_APPROVALS_PATH", filepath.Join(base, "wt-approvals.toml"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@test.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@test.com")

	mustMkdir(t, filepath.Join(r.repo, ".beans"))
	mustMkdir(t, filepath.Join(r.repo, ".config"))
	var hooks strings.Builder
	for _, h := range []string{"pre-switch", "post-switch", "pre-start", "post-start"} {
		hooks.WriteString(h + ` = "echo ` + h + ` {{ branch }} >> ` + r.hookLog + `"` + "\n")
	}
	mustWrite(t, filepath.Join(r.repo, ".config", "wt.toml"), hooks.String())

	c := config.Default()
	c.Beans.Prefix = "test-"
	c.Worktree.Backend = backend
	c.Worktree.Path = filepath.Join(base, "worktrees")
	c.Worktree.Setup = setup
	if err := c.Save(r.repo); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	b := &bean.Bean{ID: r.beanID, Slug: "add-login-form", Title: "Add login form", Status: "todo", Type: "task", CreatedAt: &now, UpdatedAt: &now}
	content, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(r.repo, ".beans", r.beanFile), string(content))
	runGit(t, r.repo, "init", "-q", "-b", "main")
	runGit(t, r.repo, "add", "-A")
	runGit(t, r.repo, "commit", "-q", "-m", "initial")

	// An uncommitted edit in main, like a Beans UI edit: it must be copied.
	b.Body = "Uncommitted edit in main."
	content, err = b.Render()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(r.repo, ".beans", r.beanFile), string(content))
	return r
}

func (r *startRepo) mainBeanContent(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.repo, ".beans", r.beanFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// loadWorktreeBean loads the test bean from a worktree's .beans/.
func (r *startRepo) loadWorktreeBean(t *testing.T, wtPath string) *bean.Bean {
	t.Helper()
	c := beancore.New(filepath.Join(wtPath, ".beans"), config.Default())
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	b, err := c.Get(r.beanID)
	if err != nil {
		t.Fatalf("bean %s not in worktree %s: %v", r.beanID, wtPath, err)
	}
	return b
}

// loadCore loads the project's config and main core like the CLI does.
func (r *startRepo) loadCore(t *testing.T) (*config.Config, *beancore.Core) {
	t.Helper()
	c, err := config.Load(r.config)
	if err != nil {
		t.Fatal(err)
	}
	mc := beancore.New(c.ResolveBeansPath(), c)
	if err := mc.Load(); err != nil {
		t.Fatal(err)
	}
	return c, mc
}

func (r *startRepo) waitForHooks(t *testing.T, want ...string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(r.hookLog)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		missing := false
		for _, w := range want {
			if !containsString(lines, w) {
				missing = true
			}
		}
		if !missing {
			// Let other background hooks finish before the temp dir goes.
			time.Sleep(200 * time.Millisecond)
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("hooks %v did not all run; hook log:\n%s", want, data)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func worktreeCount(t *testing.T, repo string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(out), "worktree ")
}

func TestStartWorktreeWorktrunk(t *testing.T) {
	wtBin, err := worktree.LookupWorktrunk()
	if err != nil {
		t.Skip("worktrunk (wt) not on PATH")
	}
	r := setupStartRepo(t, config.WorktreeBackendWorktrunk, "touch setup-ran")
	if out, err := exec.Command(wtBin, "-C", r.repo, "config", "approvals", "add", "--yes").CombinedOutput(); err != nil {
		t.Fatalf("approving hooks: %v\n%s", err, out)
	}
	mainBefore := r.mainBeanContent(t)
	name := "test-ab12-add-login-form"
	wtPath := filepath.Join(r.base, "repo."+name)
	execOut := filepath.Join(r.base, "exec.out")

	// Full CLI path, launching a program in the worktree with -x.
	err = runStartCmd(t, "--config", r.config, "start", r.beanID, "-w", "-x", "sh", "--",
		"-c", `pwd > "$0"; grep '^status:' .beans/`+r.beanFile+` >> "$0"; echo "$1" >> "$0"`, execOut, "arg with spaces")
	if err != nil {
		t.Fatalf("beans start -w: %v", err)
	}

	// Worktree at worktrunk's template path on branch <name>.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not at template path %s: %v", wtPath, err)
	}
	out, err := exec.Command("git", "-C", wtPath, "branch", "--show-current").Output()
	if err != nil || strings.TrimSpace(string(out)) != name {
		t.Fatalf("worktree branch = %q (%v), want %q", out, err, name)
	}

	// All of the test config's hooks ran, switch hooks included, plus worktree.setup.
	r.waitForHooks(t, "pre-switch "+name, "pre-start "+name, "post-start "+name, "post-switch "+name)
	if _, err := os.Stat(filepath.Join(wtPath, "setup-ran")); err != nil {
		t.Errorf("worktree.setup did not run in the worktree: %v", err)
	}

	// Bean copied (with main's uncommitted edit), in-progress in the worktree only.
	wb := r.loadWorktreeBean(t, wtPath)
	if wb.Status != "in-progress" {
		t.Errorf("worktree bean status = %q, want in-progress", wb.Status)
	}
	if !strings.Contains(wb.Body, "Uncommitted edit in main.") {
		t.Errorf("worktree bean lacks main's uncommitted edit: %q", wb.Body)
	}
	if got := r.mainBeanContent(t); got != mainBefore {
		t.Errorf("main bean changed:\n%s", got)
	}

	// -x ran in the worktree, after the bean was set to in-progress.
	data, err := os.ReadFile(execOut)
	if err != nil {
		t.Fatalf("-x program did not run: %v", err)
	}
	if want := wtPath + "\nstatus: in-progress\narg with spaces\n"; string(data) != want {
		t.Errorf("-x output = %q, want %q", data, want)
	}

	// Rerun: reuses the worktree (switch hooks run again), keeps the
	// worktree's own bean changes.
	agentEdit := filepath.Join(wtPath, ".beans", r.beanFile)
	content, _ := os.ReadFile(agentEdit)
	mustWrite(t, agentEdit, string(content)+"\nAgent progress.\n")
	if err := os.WriteFile(r.hookLog, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runStartCmd(t, "--config", r.config, "start", r.beanID, "-w"); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	lines := r.waitForHooks(t, "pre-switch "+name, "post-switch "+name)
	if containsString(lines, "pre-start "+name) {
		t.Errorf("creation hooks ran again on reuse: %v", lines)
	}
	if n := worktreeCount(t, r.repo); n != 2 {
		t.Errorf("worktree count = %d, want 2", n)
	}
	wb = r.loadWorktreeBean(t, wtPath)
	if wb.Status != "in-progress" || !strings.Contains(wb.Body, "Agent progress.") {
		t.Errorf("rerun clobbered the worktree's bean: status %q, body %q", wb.Status, wb.Body)
	}
	if got := r.mainBeanContent(t); got != mainBefore {
		t.Errorf("main bean changed on rerun:\n%s", got)
	}
}

func TestStartWorktreeWorktrunkUnapproved(t *testing.T) {
	if _, err := worktree.LookupWorktrunk(); err != nil {
		t.Skip("worktrunk (wt) not on PATH")
	}
	r := setupStartRepo(t, config.WorktreeBackendWorktrunk, "")
	c, mc := r.loadCore(t)
	mgr, err := newWorktreeManager(c)
	if err != nil {
		t.Fatal(err)
	}
	mainBefore := r.mainBeanContent(t)

	// No terminal to prompt on: wt refuses, nothing is created or changed.
	var stderr bytes.Buffer
	tio := worktree.TerminalIO{Stdin: strings.NewReader(""), Stdout: &stderr, Stderr: &stderr}
	_, err = startInWorktree(context.Background(), mc, mgr, r.beanID, &startWorktreeOptions{worktree: true}, tio)
	if err == nil {
		t.Fatal("expected an error for unapproved hooks without a terminal")
	}
	if !strings.Contains(stderr.String(), "approval") {
		t.Errorf("wt's approval message was not passed through: %q", stderr.String())
	}
	if n := worktreeCount(t, r.repo); n != 1 {
		t.Errorf("worktree count = %d, want 1", n)
	}
	if got := r.mainBeanContent(t); got != mainBefore {
		t.Errorf("main bean changed:\n%s", got)
	}
}

func TestStartWorktreeGitBackend(t *testing.T) {
	r := setupStartRepo(t, config.WorktreeBackendGit, "touch setup-ran")
	c, mc := r.loadCore(t)
	mgr, err := newWorktreeManager(c)
	if err != nil {
		t.Fatal(err)
	}
	mainBefore := r.mainBeanContent(t)
	var out bytes.Buffer
	tio := worktree.TerminalIO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out}
	opts := &startWorktreeOptions{worktree: true, name: "login"}

	res, err := startInWorktree(context.Background(), mc, mgr, r.beanID, opts, tio)
	if err != nil {
		t.Fatalf("startInWorktree: %v", err)
	}
	wantPath := filepath.Join(r.base, "worktrees", "login")
	if !res.Created || res.Worktree.Path != wantPath || res.Worktree.Branch != "beans/login" || res.Worktree.ID != "login" {
		t.Fatalf("worktree = %+v (created %v), want new beans/login at %s", res.Worktree, res.Created, wantPath)
	}
	// worktree.setup finished before startInWorktree returned.
	if _, err := os.Stat(filepath.Join(wantPath, "setup-ran")); err != nil {
		t.Errorf("worktree.setup did not run: %v", err)
	}
	if !res.Copied || res.Bean.Status != "in-progress" || res.Res.Unchanged {
		t.Errorf("result = copied %v, status %q, unchanged %v", res.Copied, res.Bean.Status, res.Res.Unchanged)
	}
	wb := r.loadWorktreeBean(t, wantPath)
	if wb.Status != "in-progress" || !strings.Contains(wb.Body, "Uncommitted edit in main.") {
		t.Errorf("worktree bean: status %q, body %q", wb.Status, wb.Body)
	}
	if got := r.mainBeanContent(t); got != mainBefore {
		t.Errorf("main bean changed:\n%s", got)
	}

	// The workspace is visible to beans-serve's manager like one it created.
	if wts, err := mgr.List(); err != nil || len(wts) != 1 || wts[0].ID != "login" || wts[0].Name != "login" {
		t.Errorf("List() = %+v, %v", wts, err)
	}

	// Rerun reuses the worktree without failing.
	res, err = startInWorktree(context.Background(), mc, mgr, r.beanID, opts, tio)
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if res.Created || res.Copied || !res.Res.Unchanged || res.Worktree.Path != wantPath {
		t.Errorf("rerun = created %v, copied %v, unchanged %v, path %s", res.Created, res.Copied, res.Res.Unchanged, res.Worktree.Path)
	}
}

func TestStartWorktreeDerivedName(t *testing.T) {
	r := setupStartRepo(t, config.WorktreeBackendGit, "")
	c, mc := r.loadCore(t)
	mgr, err := newWorktreeManager(c)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res, err := startInWorktree(context.Background(), mc, mgr, r.beanID, &startWorktreeOptions{worktree: true},
		worktree.TerminalIO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	if res.Worktree.ID != "test-ab12-add-login-form" {
		t.Errorf("worktree ID = %q, want derived test-ab12-add-login-form", res.Worktree.ID)
	}

	if _, err := startInWorktree(context.Background(), mc, mgr, "test-nope", &startWorktreeOptions{worktree: true},
		worktree.TerminalIO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown bean: error = %v", err)
	}
}

func TestRunInWorktree(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	tio := worktree.TerminalIO{Stdin: strings.NewReader("in"), Stdout: &out, Stderr: &out}
	code, err := runInWorktree(dir, "sh", []string{"-c", `pwd; cat; exit 3`}, tio)
	if err != nil || code != 3 {
		t.Fatalf("runInWorktree = %d, %v; want 3, nil", code, err)
	}
	if out.String() != dir+"\nin" {
		t.Errorf("output = %q", out.String())
	}
	if _, err := runInWorktree(dir, "no-such-program-beans-test", nil, tio); err == nil {
		t.Error("expected an error for a missing program")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
