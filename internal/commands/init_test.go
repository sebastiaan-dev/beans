package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/config"
)

func TestWorktrunkInitHint(t *testing.T) {
	t.Run("no hint without wt on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if hint := worktrunkInitHint("/project"); hint != "" {
			t.Errorf("hint = %q, want none", hint)
		}
	})

	t.Run("hint with wt on PATH", func(t *testing.T) {
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "wt"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		hint := worktrunkInitHint("/project")
		if !strings.Contains(hint, "worktree.backend: worktrunk") || !strings.Contains(hint, filepath.Join("/project", config.ConfigFileName)) {
			t.Errorf("hint = %q", hint)
		}
		if hint := worktrunkInitHint(""); hint != "" {
			t.Errorf("no config file written (--beans-path): hint = %q, want none", hint)
		}
	})
}

func TestInitWritesGitBackend(t *testing.T) {
	// Init never opts into worktrunk by itself, even when wt is installed.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "wt"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	t.Chdir(dir)
	oldBeansPath, oldJSON := beansPath, initJSON
	beansPath, initJSON = "", false
	t.Cleanup(func() { beansPath, initJSON = oldBeansPath, oldJSON })

	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	cfg, err := config.Load(filepath.Join(dir, config.ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.Backend != config.WorktreeBackendGit {
		t.Errorf("worktree.backend = %q, want git", cfg.Worktree.Backend)
	}
}
