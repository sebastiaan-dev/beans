package worktree

import (
	"strings"
	"testing"
)

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feat-x", "feat-x"},
		{"feat/x", "feat-x"},
		{`feat\x`, "feat-x"},
		{"user/feat/login", "user-feat-login"},
		{"Fix_Bug.2", "Fix_Bug.2"},
		{"weird name@{x}", "weird-name--x"},
		{"__central__", "central_"},
		{"x__run", "x_run"},
		{".hidden", "hidden"},
		{"-dash-", "dash"},
		{"trailing.", "trailing"},
		{"///", fallbackID},
		{"", fallbackID},
		{"ünïcode", "n-code"},
	}
	for _, tt := range tests {
		if got := SanitizeID(tt.in); got != tt.want {
			t.Errorf("SanitizeID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeIDLongNames(t *testing.T) {
	a := strings.Repeat("a", 150) + "/one"
	b := strings.Repeat("a", 150) + "/two"
	idA, idB := SanitizeID(a), SanitizeID(b)
	if len(idA) > maxNameLength || len(idB) > maxNameLength {
		t.Fatalf("IDs too long: %d, %d", len(idA), len(idB))
	}
	if idA == idB {
		t.Fatalf("long names with the same prefix must get distinct IDs, both got %q", idA)
	}
	if SanitizeID(a) != idA {
		t.Fatal("SanitizeID must be deterministic")
	}
}

func TestSanitizeIDIsValidName(t *testing.T) {
	for _, in := range []string{"feat/x", "a..b/c", "release/1.0", "x/.lock"} {
		id := SanitizeID(in)
		if strings.ContainsAny(id, `/\ `) || strings.Contains(id, "__") {
			t.Errorf("SanitizeID(%q) = %q contains unsafe characters", in, id)
		}
	}
}

func TestAssignIDs(t *testing.T) {
	known := map[string]string{"/wt/rebasing": "feat-rebase"}
	knownID := func(path string) string { return known[path] }

	tests := []struct {
		name    string
		entries []Entry
		want    []string
	}{
		{
			name: "plain and slashed branches",
			entries: []Entry{
				{Path: "/wt/a", Branch: "feat-a"},
				{Path: "/wt/b", Branch: "feat/b"},
			},
			want: []string{"feat-a", "feat-b"},
		},
		{
			name: "legacy beans/ branches keep their git backend ID",
			entries: []Entry{
				{Path: "/home/u/.beans/worktrees/p/brave-otter", Branch: "beans/brave-otter"},
			},
			want: []string{"brave-otter"},
		},
		{
			name: "exact branch wins over sanitized branch, regardless of order",
			entries: []Entry{
				{Path: "/wt/slashed", Branch: "feat/x"},
				{Path: "/wt/exact", Branch: "feat-x"},
			},
			want: []string{"feat-x-" + shortHash("feat/x"), "feat-x"},
		},
		{
			name: "exact branch wins over legacy beans/ branch",
			entries: []Entry{
				{Path: "/wt/legacy", Branch: "beans/foo"},
				{Path: "/wt/plain", Branch: "foo"},
			},
			want: []string{"beans-foo", "foo"},
		},
		{
			name: "detached worktree keeps its known ID",
			entries: []Entry{
				{Path: "/wt/rebasing", Detached: true},
			},
			want: []string{"feat-rebase"},
		},
		{
			name: "detached worktree without known ID uses its directory name",
			entries: []Entry{
				{Path: "/wt/repo.some-dir", Detached: true},
			},
			want: []string{"repo.some-dir"},
		},
		{
			name: "detached directory name colliding with a branch",
			entries: []Entry{
				{Path: "/elsewhere/feat-a", Detached: true},
				{Path: "/wt/a", Branch: "feat-a"},
			},
			want: []string{"feat-a-" + shortHash("/elsewhere/feat-a"), "feat-a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assignIDs(tt.entries, knownID)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("assignIDs = %v, want %v", got, tt.want)
			}
			// Same result in reverse order.
			rev := make([]Entry, len(tt.entries))
			for i, e := range tt.entries {
				rev[len(rev)-1-i] = e
			}
			gotRev := assignIDs(rev, knownID)
			for i := range gotRev {
				if gotRev[i] != got[len(got)-1-i] {
					t.Errorf("IDs depend on listing order: %v vs reversed %v", got, gotRev)
					break
				}
			}
		})
	}
}

func TestAssignIDsUnique(t *testing.T) {
	entries := []Entry{
		{Path: "/1", Branch: "a/b"},
		{Path: "/2", Branch: "a-b"},
		{Path: "/3", Branch: `a\b`},
		{Path: "/4", Branch: "a@b"},
		{Path: "/a-b", Detached: true},
	}
	ids := assignIDs(entries, nil)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID %q in %v", id, ids)
		}
		seen[id] = true
	}
	if ids[1] != "a-b" {
		t.Errorf("exact branch a-b got %q, want a-b", ids[1])
	}
}
