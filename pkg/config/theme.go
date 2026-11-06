package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Environment variables that override the theme settings of .beans.yml for
// the current user (themes are a personal preference, so they should not
// have to be committed to the project's config).
const (
	// ThemeEnvVar overrides tui.theme.
	ThemeEnvVar = "BEANS_THEME"
	// ThemeBackgroundEnvVar overrides tui.theme_background ("true"/"false").
	ThemeBackgroundEnvVar = "BEANS_THEME_BACKGROUND"
	// ThemeOverridesEnvVar overrides single roles of tui.theme_overrides,
	// e.g. "border=#5c6066,accent=4".
	ThemeOverridesEnvVar = "BEANS_THEME_OVERRIDES"
)

// ThemeRoles are the theme roles that tui.theme_overrides can set:
//
//	fg            default text color
//	bg            painted background (only used when the background is painted)
//	muted         de-emphasized text: labels, hints, metadata
//	border        unfocused borders, rules and tree lines
//	focus_border  border of the focused pane (default: accent)
//	accent        primary color: IDs, headings, keys, focus
//	selection     background of the selected row
//	success, warning, danger, info  status colors
var ThemeRoles = []string{
	"fg", "bg", "muted", "border", "focus_border", "accent", "selection",
	"success", "warning", "danger", "info",
}

// IsThemeRole reports whether role is one of ThemeRoles.
func IsThemeRole(role string) bool {
	for _, r := range ThemeRoles {
		if r == role {
			return true
		}
	}
	return false
}

// ParseThemeOverrides parses a $BEANS_THEME_OVERRIDES value: comma-separated
// role=color pairs, e.g. "border=#5c6066, accent=4". Roles must be one of
// ThemeRoles; colors are validated when the theme is applied (see
// ui.ResolveThemeOverrides), since named colors belong to the UI.
func ParseThemeOverrides(value string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		role, color, ok := strings.Cut(part, "=")
		role, color = strings.ToLower(strings.TrimSpace(role)), strings.TrimSpace(color)
		if !ok || role == "" {
			return nil, fmt.Errorf("invalid override %q (use role=color)", part)
		}
		if err := checkOverride(role, color); err != nil {
			return nil, err
		}
		out[role] = color
	}
	return out, nil
}

func checkOverride(role, color string) error {
	if !IsThemeRole(role) {
		return fmt.Errorf("unknown theme role %q (use one of %s)", role, strings.Join(ThemeRoles, ", "))
	}
	if color == "" {
		return fmt.Errorf("role %q: missing color", role)
	}
	return nil
}

// ThemeKind identifies where a theme's colors come from.
type ThemeKind string

const (
	// ThemeTerminal uses the terminal's own 16-color ANSI palette (default).
	ThemeTerminal ThemeKind = "terminal"
	// ThemeNvim exports the colors of a Neovim colorscheme.
	ThemeNvim ThemeKind = "nvim"
	// ThemeFile reads a palette from a JSON or YAML file.
	ThemeFile ThemeKind = "file"
)

// ThemeSpec is a parsed theme setting. Valid settings are:
//
//	terminal          the terminal's ANSI palette (default)
//	nvim              the colorscheme Neovim loads with the user's config
//	nvim:<name>       a specific Neovim colorscheme
//	file:<path>       a palette file (JSON or YAML)
type ThemeSpec struct {
	Kind ThemeKind
	// Colorscheme is the Neovim colorscheme for ThemeNvim ("" = Neovim's default).
	Colorscheme string
	// Path is the palette file for ThemeFile. After Config.GetTheme it is
	// absolute.
	Path string
}

// String returns the canonical setting for the spec.
func (s ThemeSpec) String() string {
	switch s.Kind {
	case ThemeNvim:
		if s.Colorscheme != "" {
			return "nvim:" + s.Colorscheme
		}
		return "nvim"
	case ThemeFile:
		return "file:" + s.Path
	default:
		return string(ThemeTerminal)
	}
}

// ParseThemeSpec parses a theme setting. The empty string means "terminal".
func ParseThemeSpec(value string) (ThemeSpec, error) {
	v := strings.TrimSpace(value)
	kind, arg, hasArg := strings.Cut(v, ":")
	switch {
	case v == "" || v == string(ThemeTerminal):
		return ThemeSpec{Kind: ThemeTerminal}, nil
	case kind == string(ThemeNvim):
		arg = strings.TrimSpace(arg)
		if hasArg && arg == "" {
			return ThemeSpec{}, fmt.Errorf("theme %q: missing colorscheme name after \"nvim:\"", value)
		}
		if strings.ContainsAny(arg, " \t\n|\"'\\") {
			return ThemeSpec{}, fmt.Errorf("theme %q: invalid colorscheme name %q", value, arg)
		}
		return ThemeSpec{Kind: ThemeNvim, Colorscheme: arg}, nil
	case kind == string(ThemeFile) && hasArg:
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return ThemeSpec{}, fmt.Errorf("theme %q: missing path after \"file:\"", value)
		}
		return ThemeSpec{Kind: ThemeFile, Path: arg}, nil
	default:
		return ThemeSpec{}, fmt.Errorf("unknown theme %q (use terminal, nvim, nvim:<colorscheme> or file:<path>)", value)
	}
}

// TUIConfig defines settings for the terminal UI and colored CLI output.
type TUIConfig struct {
	// Theme selects the color theme: "terminal" (default), "nvim",
	// "nvim:<colorscheme>" or "file:<path>". Overridden by $BEANS_THEME.
	Theme string `yaml:"theme,omitempty"`
	// ThemeBackground paints the theme's background color behind the TUI.
	// Default: true for nvim/file themes. Overridden by $BEANS_THEME_BACKGROUND.
	ThemeBackground *bool `yaml:"theme_background,omitempty"`
	// ThemeOverrides sets single theme roles (see ThemeRoles) to a color:
	// "#rrggbb", an ANSI palette index "0".."15" or a color name (red,
	// bright-blue, ...; resolved against the theme's palette). Applied on
	// top of every theme, including "terminal". Single roles are overridden
	// by $BEANS_THEME_OVERRIDES.
	ThemeOverrides map[string]string `yaml:"theme_overrides,omitempty"`
}

// GetThemeOverrides returns the role overrides in effect: tui.theme_overrides
// merged with $BEANS_THEME_OVERRIDES (the environment wins per role). Role
// names are lowercased and validated; color values are not (see
// ParseThemeOverrides).
func (c *Config) GetThemeOverrides() (map[string]string, error) {
	out := map[string]string{}
	roles := make([]string, 0, len(c.TUI.ThemeOverrides))
	for role := range c.TUI.ThemeOverrides {
		roles = append(roles, role)
	}
	sort.Strings(roles) // deterministic error reporting
	for _, role := range roles {
		color := strings.TrimSpace(c.TUI.ThemeOverrides[role])
		r := strings.ToLower(strings.TrimSpace(role))
		if err := checkOverride(r, color); err != nil {
			return nil, fmt.Errorf("tui.theme_overrides: %w", err)
		}
		out[r] = color
	}
	if v := os.Getenv(ThemeOverridesEnvVar); strings.TrimSpace(v) != "" {
		env, err := ParseThemeOverrides(v)
		if err != nil {
			return nil, fmt.Errorf("$%s: %w", ThemeOverridesEnvVar, err)
		}
		for role, color := range env {
			out[role] = color
		}
	}
	return out, nil
}

// ThemeSetting returns the raw theme setting in effect and where it comes
// from ("$BEANS_THEME", "tui.theme" or "default").
func (c *Config) ThemeSetting() (value, source string) {
	if v := os.Getenv(ThemeEnvVar); strings.TrimSpace(v) != "" {
		return v, "$" + ThemeEnvVar
	}
	if c.TUI.Theme != "" {
		return c.TUI.Theme, "tui.theme"
	}
	return string(ThemeTerminal), "default"
}

// GetTheme parses the theme setting in effect ($BEANS_THEME, then
// tui.theme). Relative file: paths are resolved against the current
// directory for the environment variable and against the config file's
// directory for tui.theme; ~ is expanded.
func (c *Config) GetTheme() (ThemeSpec, error) {
	value, source := c.ThemeSetting()
	spec, err := ParseThemeSpec(value)
	if err != nil {
		return ThemeSpec{}, fmt.Errorf("%s: %w", source, err)
	}
	if spec.Kind == ThemeFile {
		p := spec.Path
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~") && source == "tui.theme" && c.configDir != "" {
			p = filepath.Join(c.configDir, p)
		}
		abs, err := expandHome(p)
		if err != nil {
			return ThemeSpec{}, fmt.Errorf("%s: resolving %q: %w", source, spec.Path, err)
		}
		spec.Path = abs
	}
	return spec, nil
}

// GetThemeBackground reports whether the theme's background should be
// painted behind the TUI for the given theme. $BEANS_THEME_BACKGROUND
// overrides tui.theme_background; the default is true for every theme
// except "terminal", which only paints a background when a "bg" role
// override (tui.theme_overrides) gives it one.
func (c *Config) GetThemeBackground(spec ThemeSpec) (bool, error) {
	if spec.Kind == ThemeTerminal || spec.Kind == "" {
		// Errors in the overrides are reported by GetThemeOverrides.
		if o, _ := c.GetThemeOverrides(); o["bg"] == "" {
			return false, nil
		}
	}
	if v := strings.TrimSpace(os.Getenv(ThemeBackgroundEnvVar)); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return true, fmt.Errorf("$%s: invalid boolean %q", ThemeBackgroundEnvVar, v)
		}
		return b, nil
	}
	if c.TUI.ThemeBackground != nil {
		return *c.TUI.ThemeBackground, nil
	}
	return true, nil
}
