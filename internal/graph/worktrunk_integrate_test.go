//go:build !windows

package graph

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/internal/worktree"
)

// wtIntegrateRepo is a main repo with a worktrunk worktree "feat" whose
// project config (.config/wt.toml) is given by the test. Worktrunk runs with
// an isolated user config, approvals file and HOME, so the developer's own
// worktrunk hooks never run.
type wtIntegrateRepo struct {
	bin, base, main, wt, hookLog string
}

func setupWtIntegrateRepo(t *testing.T, projectConfig string, approve bool) *wtIntegrateRepo {
	t.Helper()
	bin, err := worktree.LookupWorktrunk()
	if err != nil {
		t.Skip("worktrunk (wt) not on PATH")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &wtIntegrateRepo{bin: bin, base: base, main: filepath.Join(base, "repo"), wt: filepath.Join(base, "repo.feat"), hookLog: filepath.Join(base, "hooks.log")}

	home := filepath.Join(base, "home")
	userConfig := filepath.Join(base, "wt-user.toml")
	for _, dir := range []string{home, filepath.Join(r.main, ".config"), filepath.Join(r.main, ".beans")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
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
	for _, k := range []string{"WORKTRUNK_DIRECTIVE_FILE", "WORKTRUNK_DIRECTIVE_CD_FILE", "WORKTRUNK_DIRECTIVE_EXEC_FILE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	wtWriteFile(t, filepath.Join(r.main, ".config", "wt.toml"), strings.ReplaceAll(projectConfig, "$LOG", r.hookLog))
	wtWriteFile(t, filepath.Join(r.main, ".beans", "x--x.md"), beanOld)
	gitCmd(t, r.main, "init", "-q", "-b", "main")
	gitCmd(t, r.main, "add", "-A")
	gitCmd(t, r.main, "commit", "-q", "-m", "init")
	if approve {
		r.run(t, r.main, r.bin, "-C", r.main, "config", "approvals", "add", "--yes")
	}
	r.run(t, r.main, r.bin, "-C", r.main, "switch", "--create", "feat", "--no-cd", "--no-hooks", "--format", "json")
	if _, err := os.Stat(r.wt); err != nil {
		t.Fatalf("worktree not at %s: %v", r.wt, err)
	}
	// Let background post-merge hooks finish before the temp dir goes.
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })
	return r
}

func wtWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *wtIntegrateRepo) run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func (r *wtIntegrateRepo) rev(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := gitOut(t, dir, "rev-parse", ref)
	if err != nil {
		t.Fatalf("rev-parse %s: %v\n%s", ref, err, out)
	}
	return strings.TrimSpace(out)
}

// work makes the branch's commits: two code commits and the bean completed
// (step 1 of the prompt), with the same bean edit left uncommitted in main
// like a Beans UI edit.
func (r *wtIntegrateRepo) work(t *testing.T) {
	t.Helper()
	wtWriteFile(t, filepath.Join(r.wt, "a.txt"), "one\n")
	gitCmd(t, r.wt, "add", "-A")
	gitCmd(t, r.wt, "commit", "-q", "-m", "wip 1")
	wtWriteFile(t, filepath.Join(r.wt, "a.txt"), "one\ntwo\n")
	wtWriteFile(t, filepath.Join(r.wt, ".beans", "x--x.md"), beanDone)
	gitCmd(t, r.wt, "add", "-A")
	gitCmd(t, r.wt, "commit", "-q", "-m", "wip 2")
	wtWriteFile(t, filepath.Join(r.main, ".beans", "x--x.md"), beanDone)
	wtWriteFile(t, filepath.Join(r.main, "notes.txt"), "unrelated, keep me\n")
}

const wtAgentMessage = "feat: add a.txt\n\nRefs: x-x"

// integrate runs the command sequence of the worktrunk Integrate prompt
// (steps 3a–4) as the agent would and returns the output of the wt merge
// command and its error.
func (r *wtIntegrateRepo) integrate(t *testing.T) (string, error) {
	t.Helper()
	gitCmd(t, r.wt, "rebase", "-q", "main")                                // 3a
	gitCmd(t, r.wt, "reset", "-q", "--soft", "main")                       // 3b
	gitCmd(t, r.wt, "commit", "-q", "-m", wtAgentMessage)                  //
	sha := r.rev(t, r.wt, "HEAD")                                          // 3c
	prep := exec.Command("bash", "-c", integratePrepareMainScript(r.main)) // 3d
	prep.Dir = r.wt
	prep.Env = append(os.Environ(), "SQUASH_SHA="+sha)
	if out, err := prep.CombinedOutput(); err != nil {
		t.Fatalf("prepare script: %v\n%s", err, out)
	}
	merge := exec.Command("bash", "-c", worktrunkMergeCommand(r.wt, "main")) // 4
	merge.Dir = r.wt
	out, err := merge.CombinedOutput()
	return string(out), err
}

func TestWorktrunkIntegrate(t *testing.T) {
	t.Run("merges the squashed commit and keeps the worktree", func(t *testing.T) {
		r := setupWtIntegrateRepo(t, `pre-merge = "echo pre-merge {{ branch }} >> $LOG"`+"\n", true)
		r.work(t)
		before := r.rev(t, r.main, "main")

		out, err := r.integrate(t)
		if err != nil {
			t.Fatalf("wt merge failed: %v\n%s", err, out)
		}

		// main fast-forwarded to the single squashed commit with the agent's message.
		head := r.rev(t, r.main, "main")
		if head != r.rev(t, r.wt, "HEAD") {
			t.Errorf("main %s != worktree HEAD (step 6 check)", head)
		}
		if parent := r.rev(t, r.main, "main^"); parent != before {
			t.Errorf("expected exactly one new commit on main, parent is %s, want %s", parent, before)
		}
		if msg, _ := gitOut(t, r.main, "log", "-1", "--format=%B", "main"); strings.TrimSpace(msg) != wtAgentMessage {
			t.Errorf("commit message = %q, want %q", msg, wtAgentMessage)
		}
		// The main working tree has the merged content; the identical bean
		// edit was dropped and only the unrelated change remains.
		if data, _ := os.ReadFile(filepath.Join(r.main, "a.txt")); string(data) != "one\ntwo\n" {
			t.Errorf("a.txt in main = %q", data)
		}
		if st, _ := gitOut(t, r.main, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(st) != "?? notes.txt" {
			t.Errorf("main status = %q, want only notes.txt", st)
		}
		// The pre-merge hook ran and the worktree and branch are still there.
		if data, _ := os.ReadFile(r.hookLog); strings.TrimSpace(string(data)) != "pre-merge feat" {
			t.Errorf("hook log = %q", data)
		}
		if _, err := os.Stat(r.wt); err != nil {
			t.Errorf("worktree was removed: %v", err)
		}
		var res struct {
			Branch  string `json:"branch"`
			Target  string `json:"target"`
			Removed bool   `json:"removed"`
		}
		start := strings.Index(out, "{")
		if start < 0 || json.Unmarshal([]byte(out[start:]), &res) != nil {
			t.Fatalf("no JSON result in output:\n%s", out)
		}
		if res.Branch != "feat" || res.Target != "main" || res.Removed {
			t.Errorf("unexpected result %+v", res)
		}
	})

	t.Run("reports a failing pre-merge hook and merges nothing", func(t *testing.T) {
		r := setupWtIntegrateRepo(t, "[pre-merge]\ntest = \"echo 2 tests failed; exit 3\"\n", true)
		r.work(t)
		before := r.rev(t, r.main, "main")

		out, err := r.integrate(t)
		if err == nil {
			t.Fatalf("expected wt merge to fail:\n%s", out)
		}
		if !strings.Contains(out, wtPreMergeFailedMessage) || !strings.Contains(out, "2 tests failed") {
			t.Errorf("output lacks %q and the hook output:\n%s", wtPreMergeFailedMessage, out)
		}
		if head := r.rev(t, r.main, "main"); head != before {
			t.Error("main moved although the pre-merge hook failed")
		}
	})

	t.Run("stops on unapproved project hooks without running them", func(t *testing.T) {
		r := setupWtIntegrateRepo(t, `pre-merge = "echo pre-merge >> $LOG"`+"\n", false)
		r.work(t)
		before := r.rev(t, r.main, "main")

		out, err := r.integrate(t)
		if err == nil {
			t.Fatalf("expected wt merge to fail:\n%s", out)
		}
		if !strings.Contains(out, wtApprovalMessage) || !strings.Contains(out, wtNoPromptMessage) {
			t.Errorf("output lacks the approval messages:\n%s", out)
		}
		if head := r.rev(t, r.main, "main"); head != before {
			t.Error("main moved although hooks were not approved")
		}
		if _, err := os.Stat(r.hookLog); err == nil {
			t.Error("the unapproved hook ran")
		}
	})

	t.Run("reports a moved target", func(t *testing.T) {
		r := setupWtIntegrateRepo(t, "", true)
		r.work(t)
		gitCmd(t, r.wt, "rebase", "-q", "main")
		gitCmd(t, r.wt, "reset", "-q", "--soft", "main")
		gitCmd(t, r.wt, "commit", "-q", "-m", wtAgentMessage)
		// main moves after the squash (another integration).
		wtWriteFile(t, filepath.Join(r.main, "other.txt"), "x\n")
		gitCmd(t, r.main, "add", "other.txt")
		gitCmd(t, r.main, "commit", "-q", "-m", "other")

		merge := exec.Command("bash", "-c", worktrunkMergeCommand(r.wt, "main"))
		merge.Dir = r.wt
		out, err := merge.CombinedOutput()
		if err == nil || !strings.Contains(string(out), wtNotRebasedMessage) {
			t.Errorf("expected %q failure, got %v:\n%s", wtNotRebasedMessage, err, out)
		}
	})

	t.Run("reports conflicting uncommitted changes in main", func(t *testing.T) {
		r := setupWtIntegrateRepo(t, "", true)
		r.work(t)
		gitCmd(t, r.wt, "rebase", "-q", "main")
		gitCmd(t, r.wt, "reset", "-q", "--soft", "main")
		gitCmd(t, r.wt, "commit", "-q", "-m", wtAgentMessage)
		// Without the prepare step (or after a new UI edit), the bean edit
		// in main blocks the merge.
		merge := exec.Command("bash", "-c", worktrunkMergeCommand(r.wt, "main"))
		merge.Dir = r.wt
		out, err := merge.CombinedOutput()
		if err == nil || !strings.Contains(string(out), wtTargetDirtyMessage) {
			t.Errorf("expected %q failure, got %v:\n%s", wtTargetDirtyMessage, err, out)
		}

		// Uncommitted changes in the worktree are refused too.
		wtWriteFile(t, filepath.Join(r.wt, "stray.txt"), "x\n")
		merge = exec.Command("bash", "-c", worktrunkMergeCommand(r.wt, "main"))
		merge.Dir = r.wt
		out, err = merge.CombinedOutput()
		if err == nil || !strings.Contains(string(out), wtSourceDirtyMessage) {
			t.Errorf("expected %q failure, got %v:\n%s", wtSourceDirtyMessage, err, out)
		}
	})
}
