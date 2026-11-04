package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Shared helpers for interactive confirmation prompts.
//
// Rule: commands only prompt when stdin is a terminal. When stdin is not a
// terminal (pipes, /dev/null, coding agents, CI), they never block on a prompt
// and never act silently; instead they fail fast with an error telling the
// caller which flag skips the confirmation.

// isInteractive reports whether stdin is attached to a terminal.
// It is a variable so tests can override it.
var isInteractive = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptInput and promptOutput are the streams used for confirmation prompts.
// They are variables so tests can override them.
var (
	promptInput  io.Reader = os.Stdin
	promptOutput io.Writer = os.Stdout
)

// errNonInteractive is wrapped by errors returned when a confirmation is
// required but stdin is not a terminal.
var errNonInteractive = errors.New("confirmation required but stdin is not a terminal")

// nonInteractiveError builds the error returned when a prompt cannot be shown.
// forceFlag names the flag that skips the confirmation (e.g. "-f/--force").
func nonInteractiveError(forceFlag string) error {
	return fmt.Errorf("%w; re-run with %s to proceed without prompting", errNonInteractive, forceFlag)
}

// confirm asks the user a yes/no question, defaulting to no.
//
// writePrompt renders the prompt (including the trailing "[y/N] ") to w.
// forceFlag names the flag that skips the confirmation and is used in the
// error returned when stdin is not a terminal; in that case nothing is printed
// and no input is read.
//
// If the user declines, or stdin is closed before an answer is given, a
// cancellation message is printed and confirm returns false with a nil error.
func confirm(writePrompt func(w io.Writer), forceFlag string) (bool, error) {
	if !isInteractive() {
		return false, nonInteractiveError(forceFlag)
	}

	writePrompt(promptOutput)

	answer, err := bufio.NewReader(promptInput).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if err != nil && answer == "" {
		// EOF (e.g. Ctrl-D) or read error before any answer: treat as "no".
		fmt.Fprintln(promptOutput)
		fmt.Fprintln(promptOutput, "Cancelled (no input received)")
		return false, nil
	}

	if answer == "y" || answer == "yes" {
		return true, nil
	}
	fmt.Fprintln(promptOutput, "Cancelled")
	return false, nil
}
