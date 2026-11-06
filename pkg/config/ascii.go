package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ASCIIEnvVar overrides tui.ascii for the current user ("1"/"0",
// "true"/"false"; whether the terminal can draw Unicode is a property of the
// user's terminal and font, not of the project).
const ASCIIEnvVar = "BEANS_TUI_ASCII"

// ASCIISetting returns whether the TUI draws ASCII characters only and where
// the setting comes from ("$BEANS_TUI_ASCII", "tui.ascii" or "default"). An
// invalid $BEANS_TUI_ASCII is an error; the value is then tui.ascii.
func (c *Config) ASCIISetting() (ascii bool, source string, err error) {
	if v := strings.TrimSpace(os.Getenv(ASCIIEnvVar)); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr == nil {
			return b, "$" + ASCIIEnvVar, nil
		}
		err = fmt.Errorf("$%s: invalid boolean %q (use 1/0 or true/false)", ASCIIEnvVar, v)
	}
	if c.TUI.ASCII {
		return true, "tui.ascii", err
	}
	return false, "default", err
}

// GetTUIASCII reports whether the TUI draws ASCII characters only:
// $BEANS_TUI_ASCII, then tui.ascii (default false).
func (c *Config) GetTUIASCII() (bool, error) {
	ascii, _, err := c.ASCIISetting()
	return ascii, err
}

// ValidateASCII checks the ASCII setting in effect.
func (c *Config) ValidateASCII() error {
	_, err := c.GetTUIASCII()
	return err
}
