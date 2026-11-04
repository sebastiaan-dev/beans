package commands

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

func TestResolveBeansPath(t *testing.T) {
	// Create a valid beans directory for tests that need one
	tmpDir := t.TempDir()
	validBeansDir := filepath.Join(tmpDir, ".beans")
	if err := os.MkdirAll(validBeansDir, 0755); err != nil {
		t.Fatalf("failed to create test .beans dir: %v", err)
	}

	altBeansDir := filepath.Join(tmpDir, "alt-beans")
	if err := os.MkdirAll(altBeansDir, 0755); err != nil {
		t.Fatalf("failed to create alt beans dir: %v", err)
	}

	// Config that points to the valid beans dir
	cfg := config.Default()
	cfg.SetConfigDir(tmpDir)

	t.Run("flag takes highest precedence", func(t *testing.T) {
		t.Setenv("BEANS_PATH", altBeansDir)

		got, err := resolveBeansPath(validBeansDir, cfg)
		if err != nil {
			t.Fatalf("resolveBeansPath() error = %v", err)
		}
		if got != validBeansDir {
			t.Errorf("expected flag path %q, got %q", validBeansDir, got)
		}
	})

	t.Run("flag overrides env var", func(t *testing.T) {
		t.Setenv("BEANS_PATH", "/nonexistent/should/not/be/used")

		got, err := resolveBeansPath(validBeansDir, cfg)
		if err != nil {
			t.Fatalf("resolveBeansPath() error = %v", err)
		}
		if got != validBeansDir {
			t.Errorf("expected flag path %q, got %q", validBeansDir, got)
		}
	})

	t.Run("env var used when flag is empty", func(t *testing.T) {
		t.Setenv("BEANS_PATH", altBeansDir)

		got, err := resolveBeansPath("", cfg)
		if err != nil {
			t.Fatalf("resolveBeansPath() error = %v", err)
		}
		if got != altBeansDir {
			t.Errorf("expected env var path %q, got %q", altBeansDir, got)
		}
	})

	t.Run("config used when flag and env var are empty", func(t *testing.T) {
		t.Setenv("BEANS_PATH", "")

		got, err := resolveBeansPath("", cfg)
		if err != nil {
			t.Fatalf("resolveBeansPath() error = %v", err)
		}
		expected := cfg.ResolveBeansPath()
		if got != expected {
			t.Errorf("expected config path %q, got %q", expected, got)
		}
	})

	t.Run("invalid flag path returns error", func(t *testing.T) {
		_, err := resolveBeansPath("/nonexistent/path", cfg)
		if err == nil {
			t.Fatal("expected error for invalid flag path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid env var path returns error", func(t *testing.T) {
		t.Setenv("BEANS_PATH", "/nonexistent/env/path")

		_, err := resolveBeansPath("", cfg)
		if err == nil {
			t.Fatal("expected error for invalid env var path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid config path returns init suggestion", func(t *testing.T) {
		t.Setenv("BEANS_PATH", "")

		// Config pointing to a nonexistent directory
		badCfg := config.Default()
		badCfg.SetConfigDir("/nonexistent/config/dir")

		_, err := resolveBeansPath("", badCfg)
		if err == nil {
			t.Fatal("expected error for invalid config path, got nil")
		}
		if !strings.Contains(err.Error(), "beans init") {
			t.Errorf("expected error to suggest 'beans init', got %q", err.Error())
		}
	})

	t.Run("file path rejected as not a directory", func(t *testing.T) {
		// Create a regular file (not a directory)
		filePath := filepath.Join(tmpDir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		_, err := resolveBeansPath(filePath, cfg)
		if err == nil {
			t.Fatal("expected error for file path (not directory), got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})
}

// testRootCmd returns a fully registered root command. Command definitions are
// package-level singletons, so they can only be registered once per process.
var testRootCmd = sync.OnceValue(func() *cobra.Command {
	root := NewRootCmd()
	RegisterCoreCommands(root)
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	return root
})

func TestRequiresProject(t *testing.T) {
	root := testRootCmd()

	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"list"}, true},
		{[]string{"create"}, true},
		{[]string{"init"}, false},
		{[]string{"prime"}, false},
		{[]string{"version"}, false},
		{[]string{"help"}, false},
		{[]string{"completion"}, false},
		{[]string{"completion", "bash"}, false},
		{[]string{"completion", "zsh"}, false},
		{[]string{"completion", "fish"}, false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			cmd, _, err := root.Find(tt.args)
			if err != nil {
				t.Fatalf("Find(%v) error = %v", tt.args, err)
			}
			if got := requiresProject(cmd); got != tt.want {
				t.Errorf("requiresProject(%s) = %v, want %v", cmd.CommandPath(), got, tt.want)
			}
		})
	}
}

func TestCompletionWorksWithoutProject(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("BEANS_PATH", "")

	root := testRootCmd()
	t.Cleanup(func() { root.SetArgs(nil); root.SetOut(nil); root.SetErr(nil) })

	for _, args := range [][]string{
		{"completion", "bash"},
		{"completion", "zsh"},
		{cobra.ShellCompRequestCmd, "li"},
		{"help", "list"},
	} {
		var out strings.Builder
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Errorf("%v outside a project error = %v (output: %s)", args, err, out.String())
		}
	}
}

func TestResolveConfigFile(t *testing.T) {
	projectDir := t.TempDir()
	projectConfig := filepath.Join(projectDir, config.ConfigFileName)
	if err := os.WriteFile(projectConfig, []byte("beans:\n  path: .beans\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(projectDir, "sub", "dir")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	otherDir := t.TempDir()
	flagConfig := filepath.Join(otherDir, "flag.yml")
	envConfig := filepath.Join(otherDir, "env.yml")
	for _, p := range []string{flagConfig, envConfig} {
		if err := os.WriteFile(p, []byte("beans:\n  path: .beans\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(otherDir, "missing.yml")
	emptyDir := t.TempDir()

	tests := []struct {
		name     string
		flag     string
		env      string
		startDir string
		want     string
		wantErr  string
	}{
		{name: "flag beats env and search", flag: flagConfig, env: envConfig, startDir: nestedDir, want: flagConfig},
		{name: "env beats search", env: envConfig, startDir: nestedDir, want: envConfig},
		{name: "upward search when unset", startDir: nestedDir, want: projectConfig},
		{name: "nothing found", startDir: emptyDir, want: ""},
		{name: "missing flag file errors", flag: missing, env: envConfig, startDir: nestedDir, wantErr: "set via --config does not exist"},
		{name: "missing env file errors", env: missing, startDir: nestedDir, wantErr: "set via BEANS_CONFIG does not exist"},
		{name: "directory env path errors", env: otherDir, startDir: nestedDir, wantErr: "is a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(configEnvVar, tt.env)
			got, err := resolveConfigFile(tt.flag, tt.startDir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveConfigFile() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfigFile() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveConfigFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBeansConfigEnvVar runs a project command from outside the project with
// BEANS_CONFIG pointing at the project's config.
func TestBeansConfigEnvVar(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, config.ConfigFileName), []byte("beans:\n  path: data\n  prefix: envtest-\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "data"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	t.Setenv("BEANS_PATH", "")
	root := testRootCmd()
	t.Cleanup(func() { root.SetArgs(nil); root.SetOut(nil); root.SetErr(nil); configPath = ""; beansPath = "" })

	t.Run("loads config from env var", func(t *testing.T) {
		t.Setenv(configEnvVar, filepath.Join(projectDir, config.ConfigFileName))
		var out strings.Builder
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"list", "--json"})
		if err := root.Execute(); err != nil {
			t.Fatalf("list with BEANS_CONFIG error = %v (output: %s)", err, out.String())
		}
		if got, want := core.Root(), filepath.Join(projectDir, "data"); got != want {
			t.Errorf("core root = %q, want %q", got, want)
		}
		if got := cfg.Beans.Prefix; got != "envtest-" {
			t.Errorf("config prefix = %q, want %q", got, "envtest-")
		}
	})

	t.Run("missing env file is an error", func(t *testing.T) {
		t.Setenv(configEnvVar, filepath.Join(projectDir, "missing.yml"))
		var out strings.Builder
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"list", "--json"})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "BEANS_CONFIG does not exist") {
			t.Fatalf("list with missing BEANS_CONFIG error = %v, want BEANS_CONFIG error", err)
		}
	})
}
