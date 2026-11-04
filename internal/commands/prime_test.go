package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/config"
)

// runPrime executes `beans prime` with the given extra args and returns its
// stdout, stderr and error.
func runPrime(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := testRootCmd()
	t.Cleanup(func() {
		root.SetArgs(nil)
		root.SetOut(nil)
		root.SetErr(nil)
		configPath = ""
		beansPath = ""
		primeCmd.SilenceUsage = false
	})

	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"prime"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestPrime(t *testing.T) {
	projectDir := t.TempDir()
	projectConfig := filepath.Join(projectDir, config.ConfigFileName)
	if err := os.WriteFile(projectConfig, []byte("beans:\n  path: .beans\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	missingConfig := filepath.Join(outsideDir, "missing.yml")

	tests := []struct {
		name       string
		dir        string
		env        string
		beansEnv   string
		args       []string
		wantOutput bool
		wantErr    string
	}{
		{name: "inside project", dir: projectDir, wantOutput: true},
		{name: "outside project is silent", dir: outsideDir},
		{name: "BEANS_PATH alone stays silent", dir: outsideDir, beansEnv: filepath.Join(projectDir, ".beans")},
		{name: "BEANS_CONFIG outside project", dir: outsideDir, env: projectConfig, wantOutput: true},
		{name: "--config outside project", dir: outsideDir, args: []string{"--config", projectConfig}, wantOutput: true},
		{name: "--beans-path outside project", dir: outsideDir, args: []string{"--beans-path", outsideDir}, wantOutput: true},
		{name: "missing BEANS_CONFIG errors", dir: projectDir, env: missingConfig, wantErr: "BEANS_CONFIG does not exist"},
		{name: "missing --config errors", dir: outsideDir, env: projectConfig, args: []string{"--config", missingConfig}, wantErr: "--config does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
			t.Setenv(configEnvVar, tt.env)
			t.Setenv("BEANS_PATH", tt.beansEnv)

			stdout, stderr, err := runPrime(t, tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("prime error = %v, want containing %q", err, tt.wantErr)
				}
				if stdout != "" {
					t.Errorf("prime printed primer despite error: %q", stdout)
				}
				if strings.Contains(stderr, "Usage:") {
					t.Errorf("prime error should not print usage, got stderr %q", stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("prime error = %v (stderr: %s)", err, stderr)
			}
			if got := stdout != ""; got != tt.wantOutput {
				t.Errorf("prime output present = %v, want %v (output: %.80q)", got, tt.wantOutput, stdout)
			}
		})
	}
}

func TestPrimeGuidance(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(configEnvVar, "")
	t.Setenv("BEANS_PATH", "")
	stdout, _, err := runPrime(t, "--beans-path", t.TempDir())
	if err != nil {
		t.Fatalf("prime error = %v", err)
	}

	for _, want := range []string{
		"only create it",
		"one bean at a time",
		"- [ ]",
		"<<'EOF'",
		"-d -",
		"--body-append -",
		"`--blocking <other-id>`: THIS bean blocks",
		"`--blocked-by <other-id>`: <other-id> blocks THIS bean",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("prime output missing %q", want)
		}
	}
}
