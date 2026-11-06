package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) *NvimExport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var exp NvimExport
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatal(err)
	}
	return &exp
}

// TestFromNvimKansoMist maps a real export of kanso-mist (captured with
// LazyVim on macOS) to theme roles.
func TestFromNvimKansoMist(t *testing.T) {
	got, err := FromNvim(loadFixture(t, "nvim-kanso-mist.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	want := &Theme{
		Name:       "kanso",
		Background: "dark",
		Fg:         "#c5c9c7", // Normal
		Bg:         "#22262d", // Normal
		Muted:      "#75797f", // Comment
		// FloatBorder #393b44 is too faint on #22262d (contrast 1.35) and
		// WinSeparator equals Bg: LineNr is the first legible candidate.
		Border:    "#75797f",
		Accent:    "#8ba4b0", // Function
		Selection: "#393b44", // Visual bg
		Success:   "#98bb6c", // DiagnosticOk
		Warning:   "#dca561", // DiagnosticWarn
		Danger:    "#c34043", // DiagnosticError
		Info:      "#658594", // DiagnosticInfo
		Hint:      "#6a9589", // DiagnosticHint
		ANSI: [16]string{ // g:terminal_color_0..15, lowercased
			"#22262d", "#c4746e", "#8a9a7b", "#c4b28a", "#8ba4b0", "#a292a3", "#c5c9c7", "#a4a7a4",
			"#909398", "#e46876", "#87a987", "#e6c384", "#7fb4ca", "#938aa9", "#7aa89f", "#c5c9c7",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromNvim(kanso-mist) =\n%+v\nwant\n%+v", got, want)
	}
}

// TestFromNvimWithoutTerminalColors derives the ANSI slots and missing roles
// from highlight groups when the colorscheme sets no g:terminal_color_N.
func TestFromNvimWithoutTerminalColors(t *testing.T) {
	got, err := FromNvim(loadFixture(t, "nvim-minimal.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	want := &Theme{
		Name:       "minimal",
		Background: "light",
		Fg:         "#222222",
		Bg:         "#fafafa",
		Muted:      "#888888",
		Border:     "#aaaaaa", // WinSeparator equals Bg and is skipped; LineNr
		Accent:     "#0055aa",
		Selection:  "",        // Visual is reverse video only
		Success:    "#228822", // String
		Warning:    "#aa7700",
		Danger:     "#cc0000",
		Info:       "#0055aa", // slot 4 (Function)
		Hint:       "#008888", // slot 6 (Special)
		ANSI: [16]string{
			"#fafafa", "#cc0000", "#228822", "#aa7700", "#0055aa", "#aa00aa", "#008888", "#222222",
			"#888888", "#cc0000", "#228822", "#aa7700", "#0055aa", "#aa00aa", "#008888", "#222222",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromNvim(minimal) =\n%+v\nwant\n%+v", got, want)
	}
	if got.IsDark() {
		t.Error("light theme reported as dark")
	}
}

func TestFromNvimPartialTerminalColors(t *testing.T) {
	exp := loadFixture(t, "nvim-minimal.json")
	exp.TerminalColors = map[string]string{"1": "#FF0000", "9": "not-a-color"}
	got, err := FromNvim(exp, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ANSI[1] != "#ff0000" {
		t.Errorf("slot 1 = %q, want the terminal color #ff0000", got.ANSI[1])
	}
	if got.ANSI[9] != "#cc0000" {
		t.Errorf("slot 9 = %q, want invalid terminal color replaced by DiagnosticError", got.ANSI[9])
	}
}

func TestFromNvimEmpty(t *testing.T) {
	if _, err := FromNvim(&NvimExport{}, "x"); err == nil {
		t.Error("expected an error for an export without colors")
	}
	got, err := FromNvim(&NvimExport{Highlights: map[string]NvimHighlight{"Normal": {Fg: "#ffffff"}}}, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "fallback" || got.Background != "dark" {
		t.Errorf("name/background = %q/%q, want fallback/dark", got.Name, got.Background)
	}
}

func TestParsePalette(t *testing.T) {
	t.Run("yaml with partial ansi", func(t *testing.T) {
		got, err := ParsePalette([]byte("name: x\nbackground: light\nfg: '#111'\nbg: '#EEEEEE'\naccent: '#0000ff'\nansi: ['#000000', '#aa0000']\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got.Fg != "#111111" || got.Bg != "#eeeeee" {
			t.Errorf("colors not normalized: fg=%q bg=%q", got.Fg, got.Bg)
		}
		if got.Danger != "#aa0000" || got.ANSI[9] != "#aa0000" {
			t.Errorf("danger=%q ansi[9]=%q, want derived from ansi[1]", got.Danger, got.ANSI[9])
		}
		if got.ANSI[5] != "#0000ff" || got.ANSI[7] != "#111111" || got.ANSI[15] != "#111111" {
			t.Errorf("slots not derived from roles: %v", got.ANSI)
		}
		if got.Muted != "" || got.ANSI[8] != "" || got.ANSI[3] != "" {
			t.Errorf("roles/slots without any source should stay unset: muted=%q ansi=%v", got.Muted, got.ANSI)
		}
	})
	t.Run("json round trip of an exported theme", func(t *testing.T) {
		th, err := FromNvim(loadFixture(t, "nvim-kanso-mist.json"), "")
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(th)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParsePalette(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, th) {
			t.Errorf("round trip changed the theme:\n%+v\n%+v", got, th)
		}
	})
	for name, tc := range map[string]struct{ in, err string }{
		"empty":         {"", "empty palette"},
		"no colors":     {"name: x\n", "no colors"},
		"invalid hex":   {"fg: red\n", "fg: invalid color"},
		"invalid slot":  {"ansi: ['#12345']\n", "ansi[0]"},
		"too many":      {"ansi: [" + strings.Repeat("'#000000',", 17) + "]\n", "at most 16"},
		"unknown key":   {"foreground: '#ffffff'\n", "foreground"},
		"bad bg mode":   {"background: dim\nfg: '#fff'\n", "background"},
		"invalid input": {"[", ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePalette([]byte(tc.in))
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Errorf("error %q does not mention %q", err, tc.err)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(path, []byte(`{"fg": "#c5c9c7", "bg": "#22262d"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fg != "#c5c9c7" || got.ANSI[0] != "#22262d" {
		t.Errorf("LoadFile = %+v", got)
	}
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestNormalizeHex(t *testing.T) {
	for in, want := range map[string]string{"": "", "#ABC": "#aabbcc", " #a1B2c3 ": "#a1b2c3"} {
		if got, err := NormalizeHex(in); err != nil || got != want {
			t.Errorf("NormalizeHex(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"abc", "#abcd", "#ggg", "red", "#1234567"} {
		if _, err := NormalizeHex(in); err == nil {
			t.Errorf("NormalizeHex(%q) succeeded, want error", in)
		}
	}
}

func TestContrastRatio(t *testing.T) {
	for _, tc := range []struct {
		a, b     string
		min, max float64
	}{
		{"#000000", "#ffffff", 20.99, 21.01},
		{"#ffffff", "#000000", 20.99, 21.01},
		{"#22262d", "#22262d", 1, 1},
		{"#393b44", "#22262d", 1.3, 1.4}, // kanso-mist FloatBorder: too faint
		{"#75797f", "#22262d", 3.4, 3.7}, // kanso-mist LineNr
		{"#fff", "#000", 20.99, 21.01},
		{"red", "#000000", 0, 0},
		{"", "#000000", 0, 0},
	} {
		if r := ContrastRatio(tc.a, tc.b); r < tc.min || r > tc.max {
			t.Errorf("ContrastRatio(%s, %s) = %.3f, want %.2f..%.2f", tc.a, tc.b, r, tc.min, tc.max)
		}
	}
}

func TestPickBorder(t *testing.T) {
	const bg, fg = "#22262d", "#c5c9c7"
	for name, tc := range map[string]struct {
		bg    string
		cands []string
		want  string
	}{
		"first legible":    {bg, []string{"#393b44", "", "#22262d", "#5c6066", "#75797f"}, "#5c6066"},
		"legible first":    {bg, []string{"#5c6066", "#75797f"}, "#5c6066"},
		"text color last":  {bg, []string{"#393b44", fg, "#75797f"}, "#75797f"},
		"only text color":  {bg, []string{"#393b44", fg}, fg},
		"best when faint":  {bg, []string{"#2a2e35", "#393b44", "#30333a"}, "#393b44"},
		"no background":    {"", []string{"", "#393b44", "#75797f"}, "#393b44"},
		"no candidates":    {bg, []string{"", ""}, ""},
		"light background": {"#fafafa", []string{"#eeeeee", "#aaaaaa"}, "#aaaaaa"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pickBorder(tc.bg, fg, tc.cands...); got != tc.want {
				t.Errorf("pickBorder = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFromNvimBorderGuard: FloatBorder too close to the background falls
// through WinSeparator, LineNr, NonText and Comment.
func TestFromNvimBorderGuard(t *testing.T) {
	exp := func(hl map[string]NvimHighlight) *NvimExport {
		hl["Normal"] = NvimHighlight{Fg: "#c5c9c7", Bg: "#22262d"}
		return &NvimExport{Highlights: hl}
	}
	for name, tc := range map[string]struct {
		hl   map[string]NvimHighlight
		want string
	}{
		"legible FloatBorder": {map[string]NvimHighlight{"FloatBorder": {Fg: "#6a6e75"}, "WinSeparator": {Fg: "#5c6066"}}, "#6a6e75"},
		"WinSeparator":        {map[string]NvimHighlight{"FloatBorder": {Fg: "#393b44"}, "WinSeparator": {Fg: "#5c6066"}}, "#5c6066"},
		"NonText":             {map[string]NvimHighlight{"FloatBorder": {Fg: "#393b44"}, "LineNr": {Fg: "#2a2e35"}, "NonText": {Fg: "#54585e"}, "Comment": {Fg: "#75797f"}}, "#54585e"},
		"Comment":             {map[string]NvimHighlight{"FloatBorder": {Fg: "#393b44"}, "Comment": {Fg: "#75797f"}}, "#75797f"},
		"all faint":           {map[string]NvimHighlight{"FloatBorder": {Fg: "#2a2e35"}, "WinSeparator": {Fg: "#393b44"}}, "#393b44"},
	} {
		t.Run(name, func(t *testing.T) {
			th, err := FromNvim(exp(tc.hl), "x")
			if err != nil {
				t.Fatal(err)
			}
			if th.Border != tc.want {
				t.Errorf("border = %q, want %q", th.Border, tc.want)
			}
		})
	}
}

func TestParsePaletteBorderGuard(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"faint border uses muted": {"bg: '#22262d'\nmuted: '#75797f'\nborder: '#393b44'\n", "#75797f"},
		"faint border uses ansi8": {"bg: '#22262d'\nmuted: '#2a2e35'\nborder: '#393b44'\nansi: ['', '', '', '', '', '', '', '', '#909398']\n", "#909398"},
		"legible border kept":     {"bg: '#22262d'\nmuted: '#75797f'\nborder: '#5c6066'\n", "#5c6066"},
		"no bg keeps border":      {"muted: '#75797f'\nborder: '#393b44'\n", "#393b44"},
		"focus border":            {"bg: '#22262d'\nborder: '#5c6066'\nfocus_border: '#ABC'\n", "#5c6066"},
	} {
		t.Run(name, func(t *testing.T) {
			th, err := ParsePalette([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if th.Border != tc.want {
				t.Errorf("border = %q, want %q", th.Border, tc.want)
			}
			if strings.Contains(tc.in, "focus_border") && th.FocusBorder != "#aabbcc" {
				t.Errorf("focus_border = %q", th.FocusBorder)
			}
		})
	}
}
