package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScanCommits(t *testing.T) {
	dir := initTestRepo(t)
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feat: first thing\n\nRefs: beans-abc1")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "fix: second thing (beans-xyz9)")

	var commits []Commit
	if err := ScanCommits(dir, "main", "", func(c Commit) bool {
		commits = append(commits, c)
		return true
	}); err != nil {
		t.Fatal(err)
	}

	if len(commits) != 3 {
		t.Fatalf("expected 3 commits, got %d: %+v", len(commits), commits)
	}

	tests := []struct {
		idx     int
		subject string
		message string
	}{
		{0, "fix: second thing (beans-xyz9)", "fix: second thing (beans-xyz9)"},
		{1, "feat: first thing", "feat: first thing\n\nRefs: beans-abc1"},
		{2, "initial", "initial"},
	}
	for _, tt := range tests {
		c := commits[tt.idx]
		if c.Subject != tt.subject {
			t.Errorf("commit %d subject = %q, want %q", tt.idx, c.Subject, tt.subject)
		}
		if c.Message != tt.message {
			t.Errorf("commit %d message = %q, want %q", tt.idx, c.Message, tt.message)
		}
		if len(c.Hash) != 40 {
			t.Errorf("commit %d hash = %q, want 40 hex chars", tt.idx, c.Hash)
		}
		if c.ShortHash == "" || len(c.ShortHash) >= len(c.Hash) {
			t.Errorf("commit %d short hash = %q", tt.idx, c.ShortHash)
		}
	}
}

func TestScanCommits_StopEarly(t *testing.T) {
	dir := initTestRepo(t)
	for i := 0; i < 5; i++ {
		gitRun(t, dir, "commit", "--allow-empty", "-m", "more")
	}

	n := 0
	if err := ScanCommits(dir, "main", "", func(Commit) bool {
		n++
		return n < 2
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("visited %d commits, want 2", n)
	}
}

func TestScanCommits_Since(t *testing.T) {
	dir := initTestRepo(t)
	tests := []struct {
		since string
		want  int
	}{
		{"", 1},
		{"2000-01-01T00:00:00", 1},
		{"2099-12-31T00:00:00", 0},
	}
	for _, tt := range tests {
		n := 0
		if err := ScanCommits(dir, "main", tt.since, func(Commit) bool {
			n++
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if n != tt.want {
			t.Errorf("since %q: visited %d commits, want %d", tt.since, n, tt.want)
		}
	}
}

func TestScanCommits_BadRef(t *testing.T) {
	dir := initTestRepo(t)
	if err := ScanCommits(dir, "does-not-exist", "", func(Commit) bool { return true }); err == nil {
		t.Error("expected error for unknown ref")
	}
	if err := ScanCommits(dir, "--all", "", func(Commit) bool { return true }); err == nil {
		t.Error("expected error for option-like ref")
	}
}

func TestIsRepoAndRefExists(t *testing.T) {
	dir := initTestRepo(t)
	if !IsRepo(dir) {
		t.Error("IsRepo = false for git repo")
	}
	if IsRepo(t.TempDir()) {
		t.Error("IsRepo = true for plain dir")
	}

	tests := []struct {
		ref  string
		want bool
	}{
		{"main", true},
		{"HEAD", true},
		{"origin/main", false},
		{"", false},
		{"--all", false},
	}
	for _, tt := range tests {
		if got := RefExists(dir, tt.ref); got != tt.want {
			t.Errorf("RefExists(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

func TestChangedPaths(t *testing.T) {
	dir := initTestRepo(t)
	writeAndCommit := func(msg string, files ...string) string {
		t.Helper()
		for _, f := range files {
			p := filepath.Join(dir, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(msg), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, dir, "add", f)
		}
		gitRun(t, dir, "commit", "--allow-empty", "-m", msg)
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}

	beansOnly := writeAndCommit("beans", ".beans/a.md", ".beans/sub/b.md")
	mixed := writeAndCommit("mixed", ".beans/a.md", "main.go")
	empty := writeAndCommit("empty")

	got, err := ChangedPaths(dir, []string{beansOnly, mixed, empty})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		beansOnly: {".beans/a.md", ".beans/sub/b.md"},
		mixed:     {".beans/a.md", "main.go"},
		empty:     {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChangedPaths = %v, want %v", got, want)
	}

	if got, err := ChangedPaths(dir, nil); err != nil || len(got) != 0 {
		t.Errorf("ChangedPaths(nil) = %v, %v", got, err)
	}
}
