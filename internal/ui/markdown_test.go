package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/muesli/termenv"
)

// markdownSamples cover the inline content the renderer rewrites and the
// content it must leave alone.
var markdownSamples = []string{
	"plain-text, with. all; the+ breakpoints| here",
	"escaped \\- \\. \\+ \\| \\, \\; and \\\\- double \\\\\\- backslashes",
	"`code-span, with. \\- breakpoints` and ``a-`b`` spans",
	"[link-text](https://example.com/a-b.c?x=1;y=2+3) and [rel-link](docs/a-b.md) and [anchor](#a-b) and <https://auto-link.dev/x-y> and https://bare-link.dev/p-q and www.www-link.com and mail-me@ex-ample.com",
	"![alt-text, here](https://img.dev/a-b.png) and ![rel](img/a-b.png)",
	"**bold-text.** _emph-asis;_ ~~strike-through+~~ ***both-of-them|***",
	"# Heading-one, with. stuff\n\n## heading-two\n\nSetext-heading\n--------------",
	"- item-one, a.b\n- [ ] task-item;\n- [x] done-item+\n  1. nested-one|\n  2. nested-two",
	"> quote-text, a.b\n> > nested-quote; c+d",
	"```go\nx := a-b, c.d; e+f | g\n```\n\n    indented-code, a.b",
	"<!-- html-comment, a.b -->\n<div class=\"a-b\">html-block</div>\n\ninline <span data-x=\"a-b\">raw-html</span> here",
	"| col-a | col.b |\n|-------|------:|\n| a-b, c | [l-k](https://t-l.dev/a-b) |",
	"Term-one\n: definition-one, a.b",
	"hard-break  \nnext-line\\\nthird-line",
	"entities &amp; &#45; &mdash; a-b",
	"wide 日本語-テキスト, ünï-cödé.",
}

// sgrPattern matches the SGR escape sequences glamour emits.
var sgrPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return sgrPattern.ReplaceAllString(s, "") }

func newTestRenderers(t *testing.T, width int) (*MarkdownRenderer, *glamour.TermRenderer) {
	t.Helper()
	style := MarkdownStyleConfig()
	// Like the TUI, which draws its own margins.
	zero := uint(0)
	style.Document.Margin = &zero
	ours := NewMarkdownRenderer(ansi.Options{
		Styles:          style,
		ChromaFormatter: MarkdownChromaFormatter,
		ColorProfile:    termenv.ANSI,
		WordWrap:        width,
	})
	theirs, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithChromaFormatter(MarkdownChromaFormatter),
		glamour.WithColorProfile(termenv.ANSI),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		t.Fatal(err)
	}
	return ours, theirs
}

// TestMarkdownRendererMatchesGlamour checks that, where nothing wraps, the
// renderer's output is exactly glamour's: hiding the breakpoints from the
// word wraps changes nothing else.
func TestMarkdownRendererMatchesGlamour(t *testing.T) {
	ours, theirs := newTestRenderers(t, 1000)
	samples := append([]string{strings.Join(markdownSamples, "\n\n")}, markdownSamples...)
	// Input using the placeholders itself, and input using so many of them
	// that the renderer falls back to plain glamour.
	samples = append(samples, "uses "+string(placeholderPool[:3])+" a-b, c.d", "all "+string(placeholderPool)+" a-b, c.d")
	for _, md := range samples {
		got, err := ours.Render(md)
		if err != nil {
			t.Fatal(err)
		}
		want, err := theirs.Render(md)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("output differs from glamour for %q:\n got: %q\nwant: %q", md, got, want)
		}
	}
}

// TestMarkdownRendererWrapsAtSpaces checks the glamour wrap bugs the
// renderer works around.
func TestMarkdownRendererWrapsAtSpaces(t *testing.T) {
	cases := []struct {
		name, md string
		width    int
		want     []string
	}{
		// reflow does not count hyphens: the line overflowed and the
		// document wrap moved "by" onto a line of its own.
		{"hyphen", "talks to beans-serve via a custom GraphQL API backed by Claude Code CLI processes", 40,
			[]string{"talks to beans-serve via a custom", "GraphQL API backed by Claude Code CLI", "processes"}},
		{"heading", "# talks to beans-serve via a custom GraphQL API backed by Claude", 40,
			[]string{"talks to beans-serve via a custom", "GraphQL API backed by Claude"}},
		// List items are only wrapped by x/ansi, which broke inside
		// "example.com" and let "words," run past the width.
		{"list item", "- see https://example.com/a-b-c for words, then more", 30,
			[]string{"• see", "https://example.com/a-b-c for", "words, then more"}},
		{"list item comma", "- aaaa bbbb cccc dddd eeee words, then", 30,
			[]string{"• aaaa bbbb cccc dddd eeee", "words, then"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ours, _ := newTestRenderers(t, c.width)
			out, err := ours.Render(c.md)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, l := range strings.Split(stripANSI(out), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					got = append(got, l)
				}
			}
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

func TestPlaceholderPool(t *testing.T) {
	if len(placeholderPool) < 2*len(wrapBreakpoints) {
		t.Fatalf("only %d placeholders", len(placeholderPool))
	}
	for _, r := range placeholderPool {
		if strings.ContainsRune(wrapBreakpoints, r) {
			t.Errorf("placeholder %q is a breakpoint", r)
		}
	}
}
