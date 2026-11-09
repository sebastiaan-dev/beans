package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/muesli/termenv"
)

// acpParagraph is the paragraph of bean beans-qc6g that rendered "by" on a
// line of its own at width 88.
const acpParagraph = "ACP (https://agentcommunicationprotocol.dev/) is an open protocol (v0.2.0, Apache 2.0, Linux Foundation) that standardizes how clients communicate with AI agents via REST. Currently, beans-serve talks to agents exclusively through a custom GraphQL API backed by Claude Code CLI processes. Adding ACP support would allow any ACP-compatible client to interact with beans-managed agents, and potentially allow beans to orchestrate agents hosted on external ACP servers."

// wrapParagraphs are paragraphs with hyphens, links, code spans, emphasis
// and wide characters, which all go through glamour's word wrap.
var wrapParagraphs = []string{
	acpParagraph,
	"Use beans-serve with the --beans-path flag and `code-span-x` plus self-hosted multi-word-hyphenated terms like state-of-the-art and up-to-date docs; see https://github.com/hmans/beans-ui-x/pull/12-a for the follow-up work-in-progress, and escaped \\- dashes.",
	"Some **bold-ish text** and _emph-asis_ with a [link-text here](https://example.com/a-b-c) and ~~strike-through~~ words, then a plain sentence that keeps going long enough to wrap several times over and over.",
	"Ünïcödé wörds like naïve café-au-lait and 日本語の文章 mixed with prose-like text that should wrap greedily as well, across several lines of the rendered output.",
	"short words a b c d e f g h i j k l m n o p q r s t u v w x y z aa bb-cc dd ee-ff gg hh ii jj-kk ll mm nn oo pp-qq rr ss tt uu vv ww xx yy zz",
}

// wrapContainers place a paragraph in different blocks; each has the
// prefix every rendered line of the block starts with (stripped before
// the checks) for the Unicode and the ASCII glyph set.
var wrapContainers = []struct {
	name                string
	md                  func(string) string
	prefix, prefixASCII string
}{
	{"paragraph", func(p string) string { return p }, "", ""},
	{"quote", func(p string) string { return "> " + p }, "│ ", "| "},
	{"nested quote", func(p string) string { return "> > " + p }, "│ │ ", "| | "},
	{"list item", func(p string) string { return "- " + p }, "", ""},
	{"nested list item", func(p string) string { return "- top\n  - " + p }, "", ""},
}

// checkGreedyWrap asserts that lines (one block, rendered at width) are a
// greedy word wrap of want: no line exceeds width, the words are those of
// want in order, and no line could have taken the first word of the next.
func checkGreedyWrap(t *testing.T, lines []string, width int, prefix string, want []string) {
	t.Helper()
	var words []string
	var contents []string
	for i, l := range lines {
		plain := stripAnsi(l)
		if w := lipgloss.Width(plain); w > width {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i, w, width, plain)
		}
		if !strings.HasPrefix(plain, prefix) {
			t.Errorf("line %d lacks the block prefix %q: %q", i, prefix, plain)
		}
		content := strings.TrimRight(strings.TrimPrefix(plain, prefix), " ")
		contents = append(contents, content)
		words = append(words, strings.Fields(content)...)
	}
	if strings.Join(words, " ") != strings.Join(want, " ") {
		t.Errorf("words changed:\n got: %q\nwant: %q", strings.Join(words, " "), strings.Join(want, " "))
	}
	for i := 0; i+1 < len(contents); i++ {
		next := strings.Fields(contents[i+1])
		if len(next) == 0 {
			continue
		}
		used := lipgloss.Width(prefix + contents[i])
		if used+1+lipgloss.Width(next[0]) <= width {
			t.Errorf("line %d (%d cells) could take %q from the next line:\n%s", i, used, next[0], strings.Join(contents, "\n"))
		}
	}
}

// nonBlank drops blank lines (e.g. the item before a nested list).
func nonBlank(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(stripAnsi(l)) != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestRenderMarkdownACPParagraph(t *testing.T) {
	withColorProfile(t, termenv.ANSI)
	got := renderMarkdown(acpParagraph, 88)
	want := []string{
		"ACP (https://agentcommunicationprotocol.dev/) is an open protocol (v0.2.0, Apache 2.0,",
		"Linux Foundation) that standardizes how clients communicate with AI agents via REST.",
		"Currently, beans-serve talks to agents exclusively through a custom GraphQL API backed",
		"by Claude Code CLI processes. Adding ACP support would allow any ACP-compatible client",
		"to interact with beans-managed agents, and potentially allow beans to orchestrate agents",
		"hosted on external ACP servers.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), stripAnsi(strings.Join(got, "\n")))
	}
	for i := range want {
		if l := strings.TrimRight(stripAnsi(got[i]), " "); l != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, l, want[i])
		}
	}
}

// TestRenderMarkdownGreedyWrap checks the word wrap of paragraphs in
// several blocks, widths, glyph sets and color profiles.
func TestRenderMarkdownGreedyWrap(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, profile := range []termenv.Profile{termenv.Ascii, termenv.ANSI} {
			t.Run(fmt.Sprintf("ascii=%v/profile=%d", ascii, profile), func(t *testing.T) {
				setASCII(ascii)
				t.Cleanup(func() { setASCII(false) })
				withColorProfile(t, profile)
				for _, c := range wrapContainers {
					prefix := c.prefix
					if ascii {
						prefix = c.prefixASCII
					}
					for pi, p := range wrapParagraphs {
						md := c.md(p)
						// The unwrapped rendering has the words of the
						// block, the nested list also the parent item.
						wide := nonBlank(renderMarkdown(md, 2000))
						last := strings.TrimPrefix(stripAnsi(wide[len(wide)-1]), prefix)
						want := strings.Fields(last)
						longest := 0
						for _, w := range want {
							longest = max(longest, lipgloss.Width(w))
						}
						for width := 20; width <= 120; width += 7 {
							// An over-long word is cut at the width.
							if longest+lipgloss.Width(prefix)+4 > width {
								continue
							}
							lines := nonBlank(renderMarkdown(md, width))
							if len(wide) == 2 { // nested list: drop "• top"
								lines = lines[1:]
							}
							t.Run(fmt.Sprintf("%s/%d/width=%d", c.name, pi, width), func(t *testing.T) {
								checkGreedyWrap(t, lines, width, prefix, want)
							})
						}
					}
				}
			})
		}
	}
}

// TestBodyWrapInDetailAndReader renders the paragraph of beans-qc6g in the
// detail pane and the reader at several terminal sizes.
func TestBodyWrapInDetailAndReader(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, size := range [][2]int{{200, 50}, {160, 40}, {120, 35}, {100, 30}, {80, 24}} {
			t.Run(fmt.Sprintf("ascii=%v/%dx%d", ascii, size[0], size[1]), func(t *testing.T) {
				setASCII(ascii)
				t.Cleanup(func() { setASCII(false) })
				withColorProfile(t, termenv.ANSI)
				app := newDemoApp(t, size[0], size[1])
				text, typ, parent := acpParagraph+"\n", "task", "demo-e001"
				b, err := app.resolver.CreateBean(context.Background(), model.CreateBeanInput{Title: "ACP", Type: &typ, Parent: &parent, Body: &text})
				if err != nil {
					t.Fatal(err)
				}
				app.reload()
				app.syncLayout()
				want := strings.Fields(acpParagraph)

				moveTo(t, app, b.ID)
				pressKey(t, app, "enter")
				_ = app.View()
				if app.detail.bean == nil || app.detail.bean.ID != b.ID {
					t.Fatalf("detail shows %+v, want %s", app.detail.bean, b.ID)
				}
				if w := app.detail.bodyWidth(); w >= 45 {
					checkGreedyWrap(t, app.detail.body, w, "", want)
				}

				pressKey(t, app, "esc")
				openReaderOn(t, app, b.ID)
				_ = app.View()
				if w := app.readerGeometry().col; w >= 45 {
					checkGreedyWrap(t, app.reader.body, w, "", want)
				}
			})
		}
	}
}
