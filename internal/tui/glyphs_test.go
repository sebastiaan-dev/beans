package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// withASCII switches to the ASCII glyph set for the test.
func withASCII(t *testing.T) {
	t.Helper()
	setASCII(true)
	t.Cleanup(func() { setASCII(false) })
}

// nonASCII returns the first line of s containing a non-ASCII byte, if any.
func nonASCII(s string) (string, bool) {
	for _, l := range strings.Split(s, "\n") {
		for i := 0; i < len(l); i++ {
			if l[i] >= 0x80 {
				return l, true
			}
		}
	}
	return "", false
}

func TestGlyphSets(t *testing.T) {
	u, a := reflect.ValueOf(unicodeGlyphs), reflect.ValueOf(asciiGlyphs)
	for i := 0; i < u.NumField(); i++ {
		name := u.Type().Field(i).Name
		uv, av := u.Field(i).Interface(), a.Field(i).Interface()
		var asciiValues []string
		switch v := av.(type) {
		case string:
			if v == "" || uv.(string) == "" {
				t.Errorf("%s is empty", name)
			}
			asciiValues = []string{v}
		case []string:
			asciiValues = v
		case map[string]string:
			if len(v) != len(uv.(map[string]string)) {
				t.Errorf("%s: ASCII set has %d entries, Unicode set %d", name, len(v), len(uv.(map[string]string)))
			}
			for _, s := range v {
				asciiValues = append(asciiValues, s)
			}
		}
		for _, s := range asciiValues {
			if _, bad := nonASCII(s); bad {
				t.Errorf("ASCII glyph %s = %q is not ASCII", name, s)
			}
		}
	}

	// Glyphs in fixed-width layouts have the same width in both sets.
	same := func(name string, us, as string) {
		if lipgloss.Width(us) != lipgloss.Width(as) {
			t.Errorf("%s: widths differ (%q vs %q)", name, us, as)
		}
	}
	for _, f := range []string{"TopLeft", "TopRight", "BottomLeft", "BottomRight", "Horizontal", "Vertical",
		"Ellipsis", "Sep", "Cursor", "Marked", "FoldOpen", "FoldClosed", "ScrollLeft", "ScrollRight", "Prompt", "TextCursor", "Radio", "ActiveField", "Blocking", "NotBlocking"} {
		same(f, u.FieldByName(f).String(), a.FieldByName(f).String())
	}
}

func TestSetASCII(t *testing.T) {
	if asciiMode() || glyphs() != &unicodeGlyphs {
		t.Fatal("the Unicode set is not the default")
	}
	withASCII(t)
	if !asciiMode() || glyphs().TopLeft != "+" {
		t.Fatal("setASCII(true) did not select the ASCII set")
	}
	b := glyphs().border()
	if b.TopLeft != "+" || b.Top != "-" || b.Left != "|" {
		t.Errorf("ASCII border = %+v", b)
	}
	if got := glyphs().localize("↑↓ j k · …and ⇧tab ←→"); got != "up/dn j k - ...and S-tab lt/rt" {
		t.Errorf("localize = %q", got)
	}
}

func TestASCIIGolden(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	app := newDemoApp(t, 100, 30)
	pressKeys(t, app, "j", "j")
	screen := stripAnsi(app.View())
	checkFrame(t, screen, 100, 30)
	if l, bad := nonASCII(screen); bad {
		t.Errorf("non-ASCII output: %q", l)
	}

	path := filepath.Join("testdata", "ascii-100x30.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(screen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/tui -run TestASCIIGolden -update)", err)
	}
	if got := screen + "\n"; got != string(want) {
		t.Errorf("screen differs from %s (run with -update to accept):\n%s", path, got)
	}
}

// TestASCIIViews renders every view of the browse view in ASCII mode and
// checks that the output contains only ASCII bytes.
func TestASCIIViews(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	states := []struct {
		name string
		keys []string
	}{
		{"tree", nil},
		{"collapsed", []string{"z", "M"}},
		{"selection", []string{"j", " ", "j", " "}},
		{"detail", []string{"j", "j", "enter"}},
		{"detail-body-checkbox", []string{"j", "j", "enter", "tab", "]"}},
		{"relation-opened", []string{"j", "j", "enter", "enter"}},
		{"search", []string{"/", "x"}},
		{"search-applied", []string{"/", "a", "u", "t", "h", "enter"}},
		{"filter-bar", []string{"F"}},
		{"filter-bar-scrolled", []string{"F", "l", "l", "l", "l", "l", "l"}},
		{"dropdown-type", []string{"F", "l", "enter"}},
		{"dropdown-priority", []string{"F", "l", "l", "enter", ">"}},
		{"dropdown-archived", []string{"F", "l", "l", "l", "l", "enter"}},
		{"dropdown-tags", []string{"g", "t", "s"}},
		{"priority-filtered", []string{"/", "p", "r", "i", "o", "r", "i", "t", "y", ":", ">", "=", "h", "i", "g", "h", "enter"}},
		{"help", []string{"?"}},
		{"status-picker", []string{"j", "j", "s"}},
		{"type-picker", []string{"j", "j", "t"}},
		{"priority-picker", []string{"j", "j", "P"}},
		{"parent-picker", []string{"j", "j", "p"}},
		{"blocking-picker", []string{"j", "j", "b"}},
		{"create", []string{"c", "N", "e", "w"}},
		{"rename", []string{"j", "r"}},
		{"moved", []string{"j", "j", "J"}},
	}
	sizes := [][2]int{{160, 40}, {100, 30}, {70, 24}, {50, 16}, {40, 12}, {20, 8}}
	for _, st := range states {
		for _, size := range sizes {
			app := newDemoApp(t, size[0], size[1])
			pressKeys(t, app, st.keys...)
			if l, bad := nonASCII(app.View()); bad {
				t.Errorf("%s at %dx%d: non-ASCII output: %q", st.name, size[0], size[1], stripAnsi(l))
			}
		}
	}
}

func TestASCIIHelpFits(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	app := newDemoApp(t, 70, 24)
	pressKey(t, app, "?")
	view := stripAnsi(app.View())
	checkFrame(t, view, 70, 24)
	for _, sec := range helpSections {
		for _, k := range sec.keys {
			if !strings.Contains(view, glyphs().localize(k[1])) {
				t.Errorf("70x24 ASCII help lacks %q:\n%s", k[1], view)
			}
		}
	}
}

func TestASCIIMarkdown(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	body := "# Title\n\n## Sub\n\n- item\n  - nested\n1. first\n\n- [ ] open\n- [x] done\n\n> quote\n\n---\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n`code` **bold** _em_ ~~gone~~ [link](https://example.com) ![img](https://example.com/x.png)\n\n```go\nfunc main() {}\n```\n"
	lines := renderMarkdown(body, 60)
	if l, bad := nonASCII(strings.Join(lines, "\n")); bad {
		t.Errorf("non-ASCII markdown: %q\n%s", l, strings.Join(lines, "\n"))
	}
}

func TestASCIIWorkspaces(t *testing.T) {
	withStableRendering(t)
	withASCII(t)
	r := newWSRepo(t)
	r.startInWorkspace(t, "ws-aaaa", "login")
	app := r.app(t, nil)
	app.list.reveal("ws-aaaa")
	app.Update(nil)
	screen := app.View()
	if l, bad := nonASCII(screen); bad {
		t.Errorf("non-ASCII tree/detail: %q", l)
	}
	if !strings.Contains(stripAnsi(screen), "@ login *") {
		t.Errorf("ASCII workspace badge missing:\n%s", stripAnsi(screen))
	}

	pressKey(t, app, "g")
	_, cmd := app.Update(keyRunes("w"))
	drainSlow(t, app, cmd)
	if app.state != viewWorkspaces {
		t.Fatalf("state = %v, want the workspaces overlay", app.state)
	}
	if l, bad := nonASCII(app.View()); bad {
		t.Errorf("non-ASCII workspaces overlay: %q", l)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
}
