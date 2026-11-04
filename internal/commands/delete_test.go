package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteCmdConfirmation(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		input       string
		force       bool
		json        bool
		wantErr     bool
		wantDeleted bool
		wantPrompt  bool
	}{
		{name: "non-interactive without force fails", interactive: false, input: "y\n", wantErr: true},
		{name: "non-interactive with force deletes", interactive: false, force: true, wantDeleted: true},
		{name: "non-interactive with json deletes", interactive: false, json: true, wantDeleted: true},
		{name: "interactive yes deletes", interactive: true, input: "y\n", wantDeleted: true, wantPrompt: true},
		{name: "interactive no keeps bean", interactive: true, input: "n\n", wantPrompt: true},
		{name: "interactive EOF keeps bean", interactive: true, input: "", wantPrompt: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCore, cleanup := setupQueryTestCore(t)
			defer cleanup()
			b := createQueryTestBean(t, testCore, "del-1", "Doomed", "todo")
			path := filepath.Join(testCore.Root(), b.Path)

			out := stubPrompt(t, tt.interactive, strings.NewReader(tt.input))

			oldForce, oldJSON := forceDelete, deleteJSON
			forceDelete, deleteJSON = tt.force, tt.json
			t.Cleanup(func() { forceDelete, deleteJSON = oldForce, oldJSON })

			// Silence command stdout (success messages / JSON).
			oldStdout := os.Stdout
			devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = devNull
			err = deleteCmd.RunE(deleteCmd, []string{b.ID})
			os.Stdout = oldStdout
			devNull.Close()

			if tt.wantErr {
				if !errors.Is(err, errNonInteractive) {
					t.Fatalf("RunE err = %v, want errNonInteractive", err)
				}
				if !strings.Contains(err.Error(), "-f/--force") {
					t.Errorf("error %q should hint at -f/--force", err)
				}
			} else if err != nil {
				t.Fatalf("RunE unexpected err = %v", err)
			}

			if got := strings.Contains(out.String(), "[y/N]"); got != tt.wantPrompt {
				t.Errorf("prompt shown = %v, want %v (output %q)", got, tt.wantPrompt, out.String())
			}

			_, statErr := os.Stat(path)
			deleted := os.IsNotExist(statErr)
			if deleted != tt.wantDeleted {
				t.Errorf("deleted = %v, want %v", deleted, tt.wantDeleted)
			}
		})
	}
}
