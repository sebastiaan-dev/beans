package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// stubPrompt overrides the interactive helpers for the duration of a test.
func stubPrompt(t *testing.T, interactive bool, input io.Reader) *bytes.Buffer {
	t.Helper()
	oldInteractive, oldIn, oldOut := isInteractive, promptInput, promptOutput
	out := &bytes.Buffer{}
	isInteractive = func() bool { return interactive }
	promptInput = input
	promptOutput = out
	t.Cleanup(func() {
		isInteractive, promptInput, promptOutput = oldInteractive, oldIn, oldOut
	})
	return out
}

// errReader always fails, simulating a broken stdin.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestConfirm(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		input       io.Reader
		wantOK      bool
		wantErr     bool
		wantOutput  []string
		notOutput   []string
	}{
		{name: "yes", interactive: true, input: strings.NewReader("y\n"), wantOK: true, wantOutput: []string{"Proceed? [y/N] "}, notOutput: []string{"Cancelled"}},
		{name: "YES uppercase", interactive: true, input: strings.NewReader("YES\n"), wantOK: true},
		{name: "yes without newline before EOF", interactive: true, input: strings.NewReader("y"), wantOK: true},
		{name: "no", interactive: true, input: strings.NewReader("n\n"), wantOutput: []string{"Cancelled"}},
		{name: "empty line defaults to no", interactive: true, input: strings.NewReader("\n"), wantOutput: []string{"Cancelled"}},
		{name: "other answer is no", interactive: true, input: strings.NewReader("maybe\n"), wantOutput: []string{"Cancelled"}},
		{name: "EOF is no", interactive: true, input: strings.NewReader(""), wantOutput: []string{"Cancelled (no input received)"}},
		{name: "read error is no", interactive: true, input: errReader{}, wantOutput: []string{"Cancelled (no input received)"}},
		{name: "non-interactive fails without prompting", interactive: false, input: strings.NewReader("y\n"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := stubPrompt(t, tt.interactive, tt.input)

			ok, err := confirm(func(w io.Writer) { fmt.Fprint(w, "Proceed? [y/N] ") }, "-f/--force")

			if ok != tt.wantOK {
				t.Errorf("confirm() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantErr {
				if !errors.Is(err, errNonInteractive) {
					t.Fatalf("confirm() err = %v, want errNonInteractive", err)
				}
				if !strings.Contains(err.Error(), "-f/--force") {
					t.Errorf("error %q should mention -f/--force", err)
				}
				if out.Len() != 0 {
					t.Errorf("non-interactive confirm should not print, got %q", out.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("confirm() unexpected err = %v", err)
			}
			for _, s := range tt.wantOutput {
				if !strings.Contains(out.String(), s) {
					t.Errorf("output %q should contain %q", out.String(), s)
				}
			}
			for _, s := range tt.notOutput {
				if strings.Contains(out.String(), s) {
					t.Errorf("output %q should not contain %q", out.String(), s)
				}
			}
		})
	}
}
