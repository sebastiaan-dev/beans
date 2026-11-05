package worktree

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const mixedPorcelain = `worktree /home/u/repo
HEAD 1111
branch refs/heads/main

worktree /home/u/repo.feat-login
HEAD 2222
branch refs/heads/feat/login

worktree /home/u/.beans/worktrees/repo/brave-otter
HEAD 3333
branch refs/heads/beans/brave-otter

worktree /home/u/repo.rebasing
HEAD 4444
detached

worktree /home/u/repo.gone
HEAD 5555
branch refs/heads/gone
prunable gitdir file points to non-existent location

worktree /home/u/repo.locked
HEAD 6666
branch refs/heads/locked-one
locked reason here
`

func TestParseEntries(t *testing.T) {
	entries := parseEntries(mixedPorcelain)
	if len(entries) != 6 {
		t.Fatalf("got %d entries, want 6: %+v", len(entries), entries)
	}
	want := []Entry{
		{Path: "/home/u/repo", Head: "1111", Branch: "main"},
		{Path: "/home/u/repo.feat-login", Head: "2222", Branch: "feat/login"},
		{Path: "/home/u/.beans/worktrees/repo/brave-otter", Head: "3333", Branch: "beans/brave-otter"},
		{Path: "/home/u/repo.rebasing", Head: "4444", Detached: true},
		{Path: "/home/u/repo.gone", Head: "5555", Branch: "gone", Prunable: true},
		{Path: "/home/u/repo.locked", Head: "6666", Branch: "locked-one", Locked: true},
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}

	bare := parseEntries("worktree /srv/repo.git\nbare\n\nworktree /srv/wt\nHEAD 1\nbranch refs/heads/x\n")
	if len(bare) != 2 || !bare[0].Bare || bare[1].Branch != "x" {
		t.Errorf("bare repo entries = %+v", bare)
	}
}

func TestWorktrunkWorkspaces(t *testing.T) {
	b := &worktrunkBackend{}
	known := func(path string) string {
		if path == "/home/u/repo.rebasing" {
			return "rebasing-feature"
		}
		return ""
	}
	wts := b.workspaces(parseEntries(mixedPorcelain), "/home/u/.beans/worktrees/repo", known)

	// Main and prunable entries are excluded; every other linked worktree is
	// a workspace, whatever its branch.
	got := map[string]Worktree{}
	var ids []string
	for _, wt := range wts {
		got[wt.ID] = wt
		ids = append(ids, wt.ID)
	}
	if strings.Join(ids, ",") != "feat-login,brave-otter,rebasing-feature,locked-one" {
		t.Fatalf("IDs = %v", ids)
	}
	if wt := got["feat-login"]; wt.Branch != "feat/login" || wt.Path != "/home/u/repo.feat-login" || wt.Detached {
		t.Errorf("feat-login = %+v", wt)
	}
	if wt := got["rebasing-feature"]; wt.Branch != "" || !wt.Detached {
		t.Errorf("detached worktree = %+v", wt)
	}
	for _, wt := range wts {
		if wt.Path == "/home/u/repo" {
			t.Error("main worktree must never be a workspace")
		}
	}

	// A bare main repository is skipped too.
	bare := b.workspaces(parseEntries("worktree /srv/repo.git\nbare\n\nworktree /srv/wt\nHEAD 1\nbranch refs/heads/x\n"), "", nil)
	if len(bare) != 1 || bare[0].ID != "x" {
		t.Errorf("bare repo workspaces = %+v", bare)
	}
}

func TestGitWorkspacesUnchanged(t *testing.T) {
	wts := gitBackend{}.workspaces(parseEntries(mixedPorcelain), "/home/u/.beans/worktrees/repo", nil)
	if len(wts) != 1 || wts[0].ID != "brave-otter" || wts[0].Branch != "beans/brave-otter" {
		t.Fatalf("git backend workspaces = %+v, want only brave-otter", wts)
	}

	// The main worktree is never a workspace, even on a beans/ branch.
	onBeansBranch := "worktree /repo\nHEAD 1\nbranch refs/heads/beans/x\n\nworktree /wt/y\nHEAD 2\nbranch refs/heads/beans/y\n"
	wts = gitBackend{}.workspaces(parseEntries(onBeansBranch), "/wt", nil)
	if len(wts) != 1 || wts[0].ID != "y" {
		t.Fatalf("git backend workspaces = %+v, want only y", wts)
	}
}

func TestParseBackendKind(t *testing.T) {
	tests := []struct {
		in      string
		want    BackendKind
		wantErr bool
	}{
		{"", BackendGit, false},
		{"git", BackendGit, false},
		{"worktrunk", BackendWorktrunk, false},
		{"wt", "", true},
		{"Worktrunk", "", true},
	}
	for _, tt := range tests {
		got, err := ParseBackendKind(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseBackendKind(%q) = %q, %v; want %q, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestNewBackend(t *testing.T) {
	b, err := NewBackend("")
	if err != nil || b.Kind() != BackendGit {
		t.Fatalf("NewBackend(\"\") = %v, %v; want git backend", b, err)
	}
	if _, err := NewBackend("svn"); err == nil {
		t.Fatal("NewBackend(\"svn\") should fail")
	}

	t.Run("worktrunk without wt on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := NewBackend("worktrunk")
		if err == nil || !strings.Contains(err.Error(), "not found on PATH") {
			t.Fatalf("NewBackend(worktrunk) error = %v, want not found on PATH", err)
		}
		if WorktrunkAvailable() {
			t.Fatal("WorktrunkAvailable() = true with empty PATH")
		}
	})

	t.Run("worktrunk with wt on PATH", func(t *testing.T) {
		dir := t.TempDir()
		fake := dir + "/wt"
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		b, err := NewBackend("worktrunk")
		if err != nil || b.Kind() != BackendWorktrunk {
			t.Fatalf("NewBackend(worktrunk) = %v, %v", b, err)
		}
		if b.(*worktrunkBackend).bin != fake {
			t.Errorf("bin = %q, want %q", b.(*worktrunkBackend).bin, fake)
		}
	})
}

func TestManagerDefaultsToGitBackend(t *testing.T) {
	m := NewManager("/repo", "/wt", "main", "")
	if m.Backend() != BackendGit {
		t.Fatalf("Backend() = %q, want git", m.Backend())
	}
	m = NewManager("/repo", "/wt", "main", "", WithBackend(NewWorktrunkBackend("wt")))
	if m.Backend() != BackendWorktrunk {
		t.Fatalf("Backend() = %q, want worktrunk", m.Backend())
	}
}

func TestOperationVars(t *testing.T) {
	hooks := func(templates ...string) []wtHook {
		var hs []wtHook
		for _, tmpl := range templates {
			hs = append(hs, wtHook{Type: "pre-start", Template: tmpl})
		}
		return hs
	}
	tests := []struct {
		name  string
		hooks []wtHook
		want  string
	}{
		{"no variables", hooks("npm ci"), ""},
		{"base", hooks("git log {{ base }}..HEAD"), "--base=origin/main"},
		{"base worktree path", hooks("cp {{ base_worktree_path }}/.env ."), "--base-worktree-path=/repo"},
		{"both", hooks("echo {{ base }}", "cp {{ base_worktree_path }}/x ."), "--base=origin/main --base-worktree-path=/repo"},
		{"unrelated words", hooks("git rebase main", "createdb database"), ""},
	}
	for _, tt := range tests {
		got := strings.Join(operationVars(tt.hooks, "origin/main", "/repo"), " ")
		if got != tt.want {
			t.Errorf("%s: operationVars = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestApprovalErrorMessage(t *testing.T) {
	err := error(&ApprovalError{
		RepoRoot:     "/repo",
		WorktreePath: "/repo.feat",
		Operation:    "creation",
		Commands:     []string{"pre-start: npm ci"},
	})
	msg := err.Error()
	for _, want := range []string{"need your approval", "pre-start: npm ci", "wt config approvals add", "/repo", "wt hook pre-start", "/repo.feat"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	var ae *ApprovalError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}
}

func TestJSONDocument(t *testing.T) {
	out := []byte("some notice\n{\"path\":\"/x\"}\n")
	if got := string(jsonDocument(out)); strings.TrimSpace(got) != `{"path":"/x"}` {
		t.Errorf("jsonDocument = %q", got)
	}
	arr := []byte("[\n  {\"a\": 1}\n]\n")
	if got := string(jsonDocument(arr)); got != string(arr) {
		t.Errorf("jsonDocument(array) = %q", got)
	}
}

func TestIsApprovalFailure(t *testing.T) {
	if !isApprovalFailure("▲ repo needs approval to execute 1 command:\n✗ Cannot prompt for approval in non-interactive environment") {
		t.Error("expected approval failure")
	}
	if isApprovalFailure("✗ Cannot remove worktree: x has uncommitted changes") {
		t.Error("unexpected approval failure")
	}
}

func TestCommandEnvDropsDirectives(t *testing.T) {
	t.Setenv("WORKTRUNK_DIRECTIVE_CD_FILE", "/tmp/cd")
	t.Setenv("WORKTRUNK_CONFIG_PATH", "/tmp/config.toml")
	env := strings.Join(commandEnv(), "\n")
	if strings.Contains(env, "WORKTRUNK_DIRECTIVE_CD_FILE") {
		t.Error("directive variable must be dropped")
	}
	if !strings.Contains(env, "WORKTRUNK_CONFIG_PATH=/tmp/config.toml") {
		t.Error("other worktrunk variables must be kept")
	}
}

func TestApprovalErrorMessageRemoval(t *testing.T) {
	msg := (&ApprovalError{RepoRoot: "/repo", Operation: "removal", Output: "▲ repo needs approval"}).Error()
	for _, want := range []string{"was not removed", "▲ repo needs approval", "wt config approvals add", "try again"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}
