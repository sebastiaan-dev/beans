package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASCIISetting(t *testing.T) {
	tests := []struct {
		name       string
		config     bool
		env        string
		want       bool
		wantSource string
		wantErr    string
	}{
		{name: "default", want: false, wantSource: "default"},
		{name: "config", config: true, want: true, wantSource: "tui.ascii"},
		{name: "env on", env: "1", want: true, wantSource: "$" + ASCIIEnvVar},
		{name: "env true", env: "true", want: true, wantSource: "$" + ASCIIEnvVar},
		{name: "env off overrides config", config: true, env: "0", want: false, wantSource: "$" + ASCIIEnvVar},
		{name: "blank env is unset", config: true, env: "  ", want: true, wantSource: "tui.ascii"},
		{name: "invalid env falls back to config", config: true, env: "maybe", want: true, wantSource: "tui.ascii", wantErr: "$BEANS_TUI_ASCII: invalid boolean \"maybe\""},
		{name: "invalid env, default", env: "yes please", want: false, wantSource: "default", wantErr: "invalid boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ASCIIEnvVar, tt.env)
			cfg := Default()
			cfg.TUI.ASCII = tt.config
			got, source, err := cfg.ASCIISetting()
			if got != tt.want || source != tt.wantSource {
				t.Errorf("ASCIISetting() = %v, %q; want %v, %q", got, source, tt.want, tt.wantSource)
			}
			if tt.wantErr == "" {
				if err != nil || cfg.ValidateASCII() != nil {
					t.Errorf("unexpected error %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) || cfg.ValidateASCII() == nil {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
			if ascii, _ := cfg.GetTUIASCII(); ascii != tt.want {
				t.Errorf("GetTUIASCII() = %v", ascii)
			}
		})
	}
}

func TestASCIILoadAndSave(t *testing.T) {
	t.Setenv(ASCIIEnvVar, "")
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("beans:\n  prefix: x-\ntui:\n  ascii: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI.ASCII {
		t.Fatal("tui.ascii not loaded")
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ascii: true") {
		t.Errorf("saved config lacks tui.ascii:\n%s", data)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.TUI.ASCII {
		t.Error("round trip lost tui.ascii")
	}

	// A non-boolean value is rejected when loading.
	if err := os.WriteFile(path, []byte("beans:\n  prefix: x-\ntui:\n  ascii: maybe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("tui.ascii: maybe was accepted")
	}
}
