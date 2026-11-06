// Package theme loads color themes for the terminal UI and colored CLI
// output: palettes exported from Neovim colorschemes or read from palette
// files. A Theme only holds hex colors; internal/ui turns it into styles
// (see ui.ApplyTheme).
package theme

import (
	"fmt"
	"strings"
)

// ANSISlots is the number of terminal palette slots a theme defines.
const ANSISlots = 16

// Theme is a color palette with semantic roles plus the 16 ANSI palette
// slots. Every color is a lowercase "#rrggbb" hex value or "" (unset).
//
// Unset roles mean "use the terminal's default" (Fg, Bg) or "fall back to
// the terminal-palette behaviour" (Selection: reverse video). Unset ANSI
// slots fall back to the terminal's own palette entry.
//
// The JSON/YAML form of a Theme is the palette file format read by
// LoadFile, so a theme exported from one tool can be saved and reused.
type Theme struct {
	// Name is a human-readable name, e.g. the Neovim colorscheme name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// Background is "dark" or "light".
	Background string `json:"background,omitempty" yaml:"background,omitempty"`

	// Fg is the default text color (Neovim: Normal fg).
	Fg string `json:"fg,omitempty" yaml:"fg,omitempty"`
	// Bg is the background color (Neovim: Normal bg).
	Bg string `json:"bg,omitempty" yaml:"bg,omitempty"`
	// Muted is used for de-emphasized text: help, metadata (Neovim: Comment).
	Muted string `json:"muted,omitempty" yaml:"muted,omitempty"`
	// Border is used for borders and tree lines (Neovim: FloatBorder).
	Border string `json:"border,omitempty" yaml:"border,omitempty"`
	// Accent is the primary color: IDs, headings, focus (Neovim: Function).
	Accent string `json:"accent,omitempty" yaml:"accent,omitempty"`
	// Selection is the background of selected rows (Neovim: Visual bg).
	Selection string `json:"selection,omitempty" yaml:"selection,omitempty"`
	// Success, Warning, Danger, Info and Hint are status colors (Neovim:
	// Diagnostic{Ok,Warn,Error,Info,Hint}).
	Success string `json:"success,omitempty" yaml:"success,omitempty"`
	Warning string `json:"warning,omitempty" yaml:"warning,omitempty"`
	Danger  string `json:"danger,omitempty" yaml:"danger,omitempty"`
	Info    string `json:"info,omitempty" yaml:"info,omitempty"`
	Hint    string `json:"hint,omitempty" yaml:"hint,omitempty"`

	// ANSI holds the 16 terminal palette slots (0-7 normal, 8-15 bright).
	// Named config colors (red, green, ...) and palette indices map to them.
	ANSI [ANSISlots]string `json:"ansi" yaml:"ansi"`
}

// IsDark reports whether the theme has a dark background (the default when
// unknown).
func (t *Theme) IsDark() bool {
	return t.Background != "light"
}

// complete fills unset roles from the ANSI slots and unset ANSI slots from
// the roles, so a partial palette still yields a consistent theme. Slots or
// roles without any source stay unset.
func (t *Theme) complete() {
	first := func(cands ...string) string {
		for _, c := range cands {
			if c != "" {
				return c
			}
		}
		return ""
	}
	a := &t.ANSI

	// Slots from roles (normal colors first, then bright from normal).
	a[0] = first(a[0], t.Bg)
	a[1] = first(a[1], t.Danger)
	a[2] = first(a[2], t.Success)
	a[3] = first(a[3], t.Warning)
	a[4] = first(a[4], t.Info, t.Accent)
	a[5] = first(a[5], t.Accent)
	a[6] = first(a[6], t.Hint)
	a[7] = first(a[7], t.Fg)
	a[8] = first(a[8], t.Muted, t.Border)
	for i := 9; i <= 14; i++ {
		a[i] = first(a[i], a[i-8])
	}
	a[15] = first(a[15], a[7], t.Fg)
	for i := 1; i <= 7; i++ {
		a[i] = first(a[i], a[i+8])
	}

	// Roles from slots.
	t.Muted = first(t.Muted, a[8])
	t.Border = first(t.Border, t.Muted)
	t.Accent = first(t.Accent, a[5], a[4])
	t.Success = first(t.Success, a[2])
	t.Warning = first(t.Warning, a[3])
	t.Danger = first(t.Danger, a[1])
	t.Info = first(t.Info, a[4])
	t.Hint = first(t.Hint, a[6])
	if t.Background == "" {
		t.Background = "dark"
	}
}

// isEmpty reports whether the theme defines no color at all.
func (t *Theme) isEmpty() bool {
	for _, c := range t.roles() {
		if *c != "" {
			return false
		}
	}
	for _, c := range t.ANSI {
		if c != "" {
			return false
		}
	}
	return true
}

func (t *Theme) roles() map[string]*string {
	return map[string]*string{
		"fg": &t.Fg, "bg": &t.Bg, "muted": &t.Muted, "border": &t.Border,
		"accent": &t.Accent, "selection": &t.Selection, "success": &t.Success,
		"warning": &t.Warning, "danger": &t.Danger, "info": &t.Info, "hint": &t.Hint,
	}
}

// normalize validates and canonicalizes all colors (to lowercase #rrggbb).
func (t *Theme) normalize() error {
	for name, c := range t.roles() {
		n, err := NormalizeHex(*c)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*c = n
	}
	for i, c := range t.ANSI {
		n, err := NormalizeHex(c)
		if err != nil {
			return fmt.Errorf("ansi[%d]: %w", i, err)
		}
		t.ANSI[i] = n
	}
	switch t.Background {
	case "", "dark", "light":
	default:
		return fmt.Errorf("background: %q is not \"dark\" or \"light\"", t.Background)
	}
	return nil
}

// NormalizeHex validates a "#rgb" or "#rrggbb" color and returns it as
// lowercase "#rrggbb". The empty string is returned unchanged.
func NormalizeHex(c string) (string, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", nil
	}
	if !strings.HasPrefix(c, "#") || (len(c) != 4 && len(c) != 7) {
		return "", fmt.Errorf("invalid color %q (use #rrggbb)", c)
	}
	for _, r := range c[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return "", fmt.Errorf("invalid color %q (use #rrggbb)", c)
		}
	}
	c = strings.ToLower(c)
	if len(c) == 4 {
		c = "#" + strings.Repeat(c[1:2], 2) + strings.Repeat(c[2:3], 2) + strings.Repeat(c[3:4], 2)
	}
	return c, nil
}
