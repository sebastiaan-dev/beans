package worktree

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBeanIDsFromPaths(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"none", nil, nil},
		{"sorted and unique", []string{".beans/b-2--two.md", ".beans/a-1--one.md", ".beans/b-2--two.md"}, []string{"a-1", "b-2"}},
		{"id without slug", []string{".beans/abc1.md"}, []string{"abc1"}},
		{"subdirectories ignored", []string{".beans/archive/a-1.md", ".beans/.conversations/a-2.md"}, nil},
		{"non-markdown ignored", []string{".beans/config.yaml", ".beans/.gitignore"}, nil},
		{"outside .beans ignored", []string{"docs/a-1.md", "a-1.md"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := beanIDsFromPaths(tt.paths); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("beanIDsFromPaths(%q) = %q, want %q", tt.paths, got, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestDetectBeanIDs_ChangeKinds covers every way a bean file can differ from
// the merge-base: committed, staged, unstaged, untracked and renamed, while
// changes on the base branch after the merge-base and non-bean changes don't
// count.
func TestDetectBeanIDs_ChangeKinds(t *testing.T) {
	repoDir, beansDir, wtRoot := initTestRepo(t)
	for _, name := range []string{"m-unst--unstaged.md", "m-stag--staged.md", "m-same--same.md", "m-renm--old.md"} {
		writeFile(t, filepath.Join(beansDir, name), "---\ntitle: "+name+"\n---\n")
	}
	gitRun(t, repoDir, "add", "-A")
	gitRun(t, repoDir, "commit", "-m", "beans on main")

	mgr := NewManager(repoDir, wtRoot, "main", "")
	wt, err := mgr.Create("kinds")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wtBeans := filepath.Join(wt.Path, ".beans")

	writeFile(t, filepath.Join(wtBeans, "w-comm--committed.md"), "committed\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "committed bean")

	writeFile(t, filepath.Join(wtBeans, "m-stag--staged.md"), "staged edit\n")
	gitRun(t, wt.Path, "add", ".beans/m-stag--staged.md")
	writeFile(t, filepath.Join(wtBeans, "m-unst--unstaged.md"), "unstaged edit\n")
	writeFile(t, filepath.Join(wtBeans, "w-untr--untracked.md"), "untracked\n")
	gitRun(t, wt.Path, "mv", ".beans/m-renm--old.md", ".beans/m-renm--new.md")
	writeFile(t, filepath.Join(wt.Path, "README.md"), "non-bean change\n")

	// A bean added on main after the worktree branched is not the worktree's.
	writeFile(t, filepath.Join(beansDir, "m-late--late.md"), "late\n")
	gitRun(t, repoDir, "add", "-A")
	gitRun(t, repoDir, "commit", "-m", "late bean on main")

	got := mgr.DetectBeanIDs(wt.Path)
	want := []string{"m-renm", "m-stag", "m-unst", "w-comm", "w-untr"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DetectBeanIDs = %q, want %q", got, want)
	}
}

func TestDetectBeanIDs_NoMergeBaseUsesHEAD(t *testing.T) {
	repoDir, _, wtRoot := initTestRepo(t)
	// A base ref that doesn't exist: no merge-base, so changes vs HEAD count.
	mgr := NewManager(repoDir, wtRoot, "no-such-branch", "")
	wtPath := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repoDir, "worktree", "add", "-b", "beans/nomb", wtPath)

	writeFile(t, filepath.Join(wtPath, ".beans", "n-comm--committed.md"), "committed\n")
	gitRun(t, wtPath, "add", "-A")
	gitRun(t, wtPath, "commit", "-m", "committed bean")
	writeFile(t, filepath.Join(wtPath, ".beans", "n-untr--untracked.md"), "untracked\n")

	got := mgr.DetectBeanIDs(wtPath)
	if want := []string{"n-untr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DetectBeanIDs = %q, want %q", got, want)
	}
}

func TestList_DetectsBeanIDsPerWorktree(t *testing.T) {
	repoDir, beansDir, wtRoot := initTestRepo(t)
	writeFile(t, filepath.Join(beansDir, ".gitkeep"), "")
	gitRun(t, repoDir, "add", "-A")
	gitRun(t, repoDir, "commit", "-m", "beans dir")
	mgr := NewManager(repoDir, wtRoot, "main", "")

	want := map[string][]string{}
	for _, name := range []string{"wt-a", "wt-b", "wt-c", "wt-d", "wt-e", "wt-f"} {
		wt, err := mgr.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if name == "wt-f" {
			want[wt.ID] = nil // no bean changes
			continue
		}
		id := "x-" + name[3:]
		writeFile(t, filepath.Join(wt.Path, ".beans", id+"--bean.md"), name+"\n")
		want[wt.ID] = []string{id}
	}

	wts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string][]string{}
	for _, wt := range wts {
		got[wt.ID] = wt.BeanIDs
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List BeanIDs = %v, want %v", got, want)
	}

	basic, err := mgr.ListBasic()
	if err != nil {
		t.Fatalf("ListBasic: %v", err)
	}
	if len(basic) != len(wts) {
		t.Fatalf("ListBasic returned %d worktrees, List %d", len(basic), len(wts))
	}
	for i, wt := range basic {
		if wt.BeanIDs != nil {
			t.Errorf("ListBasic()[%d].BeanIDs = %q, want nil", i, wt.BeanIDs)
		}
		full := wts[i]
		full.BeanIDs = nil
		if !reflect.DeepEqual(wt, full) {
			t.Errorf("ListBasic()[%d] = %+v, want %+v", i, wt, full)
		}
	}
}
