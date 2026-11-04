package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/config"
	"github.com/muesli/termenv"
)

func TestRenderBeanRow_NarrowWidth(t *testing.T) {
	// Test that RenderBeanRow doesn't panic with very small MaxTitleWidth values
	// This was a bug where MaxTitleWidth < 4 caused a slice bounds panic

	tests := []struct {
		name          string
		maxTitleWidth int
		title         string
	}{
		{"zero width", 0, "Test Title"},
		{"width 1", 1, "Test Title"},
		{"width 2", 2, "Test Title"},
		{"width 3", 3, "Test Title"},
		{"width 4", 4, "Test Title"},
		{"width 5", 5, "Test Title"},
		{"short title fits", 10, "Hi"},
		{"exact fit", 10, "0123456789"},
		{"needs truncation", 10, "This is a longer title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderBeanRow panicked with MaxTitleWidth=%d: %v", tt.maxTitleWidth, r)
				}
			}()

			cfg := BeanRowConfig{
				MaxTitleWidth: tt.maxTitleWidth,
				StatusColor:   "green",
				TypeColor:     "blue",
			}

			result := RenderBeanRow("abc123", "todo", "task", tt.title, cfg)
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestRenderBeanRow_NarrowWidthWithPriority(t *testing.T) {
	// Priority symbol takes 2 extra chars, which reduces available title width
	// This tests that the adjustment doesn't cause negative slice bounds

	tests := []struct {
		name          string
		maxTitleWidth int
		priority      string
	}{
		{"width 1 with priority", 1, "high"},
		{"width 2 with priority", 2, "high"},
		{"width 3 with priority", 3, "critical"},
		{"width 4 with priority", 4, "high"},
		{"width 5 with priority", 5, "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderBeanRow panicked with MaxTitleWidth=%d and priority=%s: %v",
						tt.maxTitleWidth, tt.priority, r)
				}
			}()

			cfg := BeanRowConfig{
				MaxTitleWidth: tt.maxTitleWidth,
				Priority:      tt.priority,
				PriorityColor: "red",
				StatusColor:   "green",
				TypeColor:     "blue",
			}

			result := RenderBeanRow("abc123", "todo", "task", "Long title that needs truncation", cfg)
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestShortType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"milestone", "M"},
		{"epic", "E"},
		{"bug", "B"},
		{"feature", "F"},
		{"task", "T"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ShortType(tt.input)
			if result != tt.expected {
				t.Errorf("ShortType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShortStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"draft", "D"},
		{"todo", "T"},
		{"in-progress", "I"},
		{"completed", "C"},
		{"scrapped", "S"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ShortStatus(tt.input)
			if result != tt.expected {
				t.Errorf("ShortStatus(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// nonPaletteColor matches SGR color parameters outside the 16-color ANSI
// palette: truecolor (38;2 / 48;2) and 256-color (38;5 / 48;5) sequences.
var nonPaletteColor = regexp.MustCompile(`[34]8;[25];`)

func withColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// renderSharedStyles renders every shared style and helper with every color
// used by the default configuration.
func renderSharedStyles(t *testing.T, profile termenv.Profile) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, st := range []struct {
		name  string
		style lipgloss.Style
	}{
		{"StatusOpen", StatusOpen}, {"StatusDone", StatusDone}, {"StatusInProgress", StatusInProgress},
		{"StatusOpenText", StatusOpenText}, {"StatusDoneText", StatusDoneText}, {"StatusInProgressText", StatusInProgressText},
		{"TagBadge", TagBadge}, {"Bold", Bold}, {"Muted", Muted}, {"Primary", Primary}, {"Success", Success},
		{"Warning", Warning}, {"Danger", Danger}, {"Secondary", Secondary}, {"ID", ID}, {"TreeLine", TreeLine},
		{"Title", Title}, {"Path", Path}, {"Header", Header},
	} {
		out[st.name] = st.style.Render("x")
	}
	for _, s := range []string{"todo", "draft", "completed", "scrapped", "in-progress", "other"} {
		out["RenderStatus/"+s] = RenderStatus(s)
		out["RenderStatusText/"+s] = RenderStatusText(s)
	}
	out["RenderTags"] = RenderTags([]string{"a", "b"})
	out["RenderTagsCompact"] = RenderTagsCompact([]string{"a", "b", "c"}, 1)

	cfg := config.Default()
	for _, s := range config.DefaultStatuses {
		out["status/"+s.Name] = RenderStatusWithColor(s.Name, s.Color, s.Archive) +
			RenderStatusTextWithColor(s.Name, s.Color, s.Archive)
	}
	for _, ty := range config.DefaultTypes {
		out["type/"+ty.Name] = RenderTypeWithColor(ty.Name, ty.Color) + RenderTypeText(ty.Name, ty.Color)
	}
	for _, p := range config.DefaultPriorities {
		out["priority/"+p.Name] = RenderPriorityWithColor(p.Name, p.Color) +
			RenderPriorityText(p.Name, p.Color) + RenderPrioritySymbol(p.Name, p.Color)
	}
	for _, s := range config.DefaultStatuses {
		for _, ty := range config.DefaultTypes {
			for _, p := range config.DefaultPriorities {
				c := cfg.GetBeanColors(s.Name, ty.Name, p.Name)
				for _, row := range []BeanRowConfig{
					{ShowCursor: true, IsSelected: true, ShowTags: true, Tags: []string{"t"}, TreePrefix: "├─"},
					{ShowCursor: true, IsMarked: true, UseFullNames: true, ImplicitStatus: "scrapped"},
					{Dimmed: true, ShowTags: true, Tags: []string{"t"}},
				} {
					row.StatusColor, row.TypeColor, row.PriorityColor = c.StatusColor, c.TypeColor, c.PriorityColor
					row.Priority, row.IsArchive = p.Name, c.IsArchive
					out["row/"+s.Name+"/"+ty.Name+"/"+p.Name] += RenderBeanRow("abc", s.Name, ty.Name, "Title", row)
				}
			}
		}
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(MarkdownStyleConfig()),
		glamour.WithChromaFormatter(MarkdownChromaFormatter),
		glamour.WithColorProfile(profile),
	)
	if err != nil {
		t.Fatal(err)
	}
	md, err := r.Render("# H1\n\n## H2\n\n*em* **strong** `code` [link](https://x.y) ![img](a.png)\n\n" +
		"> quote\n\n- item\n\n---\n\n```go\npackage main\n\n// comment\nfunc main() { fmt.Println(\"s\", 1) }\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	out["markdown"] = md
	return out
}

// TestSharedStylesUseANSIPalette guards against hardcoded truecolor (and
// 256-color) values in the shared styles: colors must come from the
// terminal's 16-color palette so the UI follows the user's theme.
func TestSharedStylesUseANSIPalette(t *testing.T) {
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256} {
		t.Run(profile.Name(), func(t *testing.T) {
			withColorProfile(t, profile)
			styled := false
			for name, s := range renderSharedStyles(t, profile) {
				if m := nonPaletteColor.FindString(s); m != "" {
					t.Errorf("%s emits non-palette color escape %q: %q", name, m, s)
				}
				styled = styled || strings.Contains(s, "\x1b[3")
			}
			if !styled {
				t.Error("expected some ANSI foreground colors; test would be vacuous")
			}
		})
	}
}

// TestSharedStylesASCIIProfile checks that with no color support (NO_COLOR or
// non-TTY) no color escapes are emitted. Lipgloss styles emit no escapes at
// all; glamour may still emit text attributes such as bold.
func TestSharedStylesASCIIProfile(t *testing.T) {
	withColorProfile(t, termenv.Ascii)
	for name, s := range renderSharedStyles(t, termenv.Ascii) {
		if name != "markdown" && strings.Contains(s, "\x1b[") {
			t.Errorf("%s emits escape sequences under the Ascii profile: %q", name, s)
		}
		for p := range sgrParams(s) {
			if n, err := strconv.Atoi(p); err == nil && (n >= 30 && n <= 49 || n >= 90 && n <= 107) {
				t.Errorf("%s emits color parameter %d under the Ascii profile: %q", name, n, s)
			}
		}
	}
}

func TestNamedColorsUseANSIPalette(t *testing.T) {
	for name, c := range NamedColors {
		if c == ColorDefault {
			continue
		}
		if !isANSIIndex(string(c)) {
			t.Errorf("NamedColors[%q] = %q, want an ANSI palette index 0-15", name, c)
		}
	}
	for _, c := range []lipgloss.Color{ColorPrimary, ColorSecondary, ColorSuccess, ColorWarning,
		ColorDanger, ColorMuted, ColorSubtle, ColorBlue, ColorCyan} {
		if !isANSIIndex(string(c)) {
			t.Errorf("palette color %q is not an ANSI palette index 0-15", c)
		}
	}
}

var sgrSeq = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// sgrParams returns all SGR parameters used in s.
func sgrParams(s string) map[string]bool {
	params := map[string]bool{}
	for _, m := range sgrSeq.FindAllStringSubmatch(s, -1) {
		for _, p := range strings.Split(m[1], ";") {
			params[p] = true
		}
	}
	return params
}

// TestBadgesUseReverseVideo checks that badges swap the theme's own colors
// instead of combining a palette background with a fixed foreground, which
// would be illegible on some badge colors in some themes.
func TestBadgesUseReverseVideo(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	badges := map[string]string{
		"status": RenderStatusWithColor("todo", "green", false),
		"type":   RenderTypeWithColor("bug", "red"),
		"tag":    RenderTag("frontend"),
		"legacy": RenderStatus("in-progress"),
	}
	for name, got := range badges {
		params := sgrParams(got)
		if !params["7"] {
			t.Errorf("%s badge should use reverse video, got %q", name, got)
		}
		for p := range params {
			if p == "48" || (len(p) == 2 && p[0] == '4') || (len(p) == 3 && p[:2] == "10") {
				t.Errorf("%s badge sets an explicit background (%s): %q", name, p, got)
			}
		}
	}
}

func TestResolveColor(t *testing.T) {
	tests := []struct {
		in   string
		want lipgloss.Color
	}{
		{"green", "2"},
		{"GREEN", "2"},
		{"blue", "4"},
		{"purple", "5"},
		{"magenta", "5"},
		{"gray", "8"},
		{"grey", "8"},
		{"white", "7"},
		{"default", ""},
		{"bright-blue", "12"},
		{"12", "12"},
		{"#ff0000", "#ff0000"},
		{"unknown", ColorMuted},
		{"16", ColorMuted},
	}
	for _, tt := range tests {
		if got := ResolveColor(tt.in); got != tt.want {
			t.Errorf("ResolveColor(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsValidColor(t *testing.T) {
	valid := []string{"green", "Red", "default", "bright-cyan", "0", "8", "15", "#fff", "#00ff00"}
	invalid := []string{"pink", "", "16", "-1", "08", "#ff", "#fffff"}
	for _, c := range valid {
		if !IsValidColor(c) {
			t.Errorf("IsValidColor(%q) = false, want true", c)
		}
	}
	for _, c := range invalid {
		if IsValidColor(c) {
			t.Errorf("IsValidColor(%q) = true, want false", c)
		}
	}
	for _, s := range config.DefaultStatuses {
		if !IsValidColor(s.Color) {
			t.Errorf("default status %q has invalid color %q", s.Name, s.Color)
		}
	}
	for _, ty := range config.DefaultTypes {
		if !IsValidColor(ty.Color) {
			t.Errorf("default type %q has invalid color %q", ty.Name, ty.Color)
		}
	}
	for _, p := range config.DefaultPriorities {
		if !IsValidColor(p.Color) {
			t.Errorf("default priority %q has invalid color %q", p.Name, p.Color)
		}
	}
}
