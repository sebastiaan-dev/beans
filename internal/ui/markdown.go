package ui

import (
	"bytes"
	"html"
	"net/url"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// MarkdownRenderer renders markdown for the terminal like
// glamour.TermRenderer (same goldmark extensions, same glamour ANSI renderer,
// same output), but works around the bugs of glamour's word wrap, so that
// text wraps greedily at spaces only.
//
// glamour wraps paragraphs and headings with github.com/muesli/reflow's
// wordwrap, which breaks after '-' but does not count the hyphen towards the
// line length: every hyphen lets a line grow one column past the width.
// glamour then re-wraps each document, list and quote block with
// github.com/charmbracelet/x/ansi's Wordwrap, which moves the overflowing word
// onto a line of its own ("...backed" / "by" / "Claude Code ..."). That wrap
// (the only one for list items) also breaks inside words at ",.;-+|" (e.g.
// "https://example." / "com") and lets such a character run one column past
// the width.
//
// The renderer hides these characters from both wraps: after parsing, it
// replaces them in the inline text (text, code spans, autolinks and absolute
// link and image destinations) with placeholder letters that occupy one
// column and are no breakpoints, and maps the placeholders back in the
// output. Code blocks, raw HTML and tables (wrapped by lipgloss) are left
// untouched.
type MarkdownRenderer struct {
	md goldmark.Markdown
}

// NewMarkdownRenderer returns a renderer with the given glamour options
// (styles, word wrap, color profile, chroma formatter, ...).
func NewMarkdownRenderer(options ansi.Options) *MarkdownRenderer {
	// The setup mirrors glamour.NewTermRenderer.
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	md.SetRenderer(renderer.NewRenderer(
		renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(options), 1000)),
	))
	return &MarkdownRenderer{md: md}
}

// wrapBreakpoints are the characters glamour's word wraps break at, besides
// whitespace.
const wrapBreakpoints = "-,.;+|"

// escapedBreakpoints are the breakpoints glamour unescapes itself ("\-" ->
// "-") in the text it renders.
const escapedBreakpoints = "-.+|"

// placeholderPool are the candidate placeholders: caseless letters (style
// case transforms leave them alone) that are one column wide everywhere
// (go-runewidth, used by reflow, also in East Asian locales, and x/ansi,
// used by glamour's other wraps and lipgloss) and are
// neither whitespace nor breakpoints.
var placeholderPool = func() []rune {
	eastAsian := runewidth.NewCondition()
	eastAsian.EastAsianWidth = true
	var pool []rune
	for r := rune(0x100); r < 0x3000 && len(pool) < 64; r++ {
		if unicode.IsLetter(r) && unicode.ToUpper(r) == r && unicode.ToLower(r) == r && unicode.ToTitle(r) == r &&
			runewidth.RuneWidth(r) == 1 && eastAsian.RuneWidth(r) == 1 && lipgloss.Width(string(r)) == 1 {
			pool = append(pool, r)
		}
	}
	return pool
}()

// Render renders markdown.
func (r *MarkdownRenderer) Render(in string) (string, error) {
	source := []byte(in)
	doc := r.md.Parser().Parse(text.NewReader(source))

	var restore *strings.Replacer
	if strings.ContainsAny(in, wrapBreakpoints) {
		if h := newBreakpointHider(in); h != nil {
			source = h.hide(doc, source)
			restore = h.restore
		}
	}

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, source, doc); err != nil {
		return "", err
	}
	if restore != nil {
		return restore.Replace(buf.String()), nil
	}
	return buf.String(), nil
}

// breakpointHider replaces the wrap breakpoints in a document.
type breakpointHider struct {
	escaped *strings.Replacer // for text glamour unescapes
	literal *strings.Replacer // for code spans
	restore *strings.Replacer // maps the placeholders back
}

// newBreakpointHider picks placeholders that do not occur in the input, or
// returns nil if there are not enough.
func newBreakpointHider(in string) *breakpointHider {
	var escaped, literal, restore []string
	// Keep escaped backslashes, so that "\\-" stays a backslash and a hyphen.
	escaped = append(escaped, `\\`, `\\`)
	pool := placeholderPool
	for _, c := range wrapBreakpoints {
		for len(pool) > 0 && strings.ContainsRune(in, pool[0]) {
			pool = pool[1:]
		}
		if len(pool) == 0 {
			return nil
		}
		p := string(pool[0])
		pool = pool[1:]
		if strings.ContainsRune(escapedBreakpoints, c) {
			escaped = append(escaped, `\`+string(c), p)
		}
		escaped = append(escaped, string(c), p)
		literal = append(literal, string(c), p)
		restore = append(restore, p, string(c))
	}
	return &breakpointHider{
		escaped: strings.NewReplacer(escaped...),
		literal: strings.NewReplacer(literal...),
		restore: strings.NewReplacer(restore...),
	}
}

// hide replaces the breakpoints in the inline content of doc. Text nodes are
// re-pointed to rewritten copies appended to the source, so all other
// segments stay valid; the returned source must be used for rendering.
func (h *breakpointHider) hide(doc ast.Node, source []byte) []byte {
	out := source
	appendText := func(s string) text.Segment {
		start := len(out)
		out = append(out, s...)
		return text.NewSegment(start, len(out))
	}

	var autoLinks []*ast.AutoLink
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *extast.Table:
			// lipgloss wraps table cells correctly.
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			v := string(n.Segment.Value(source))
			if !strings.ContainsAny(v, wrapBreakpoints) {
				break
			}
			n.Segment = appendText(h.text(n, v))
		case *ast.Link:
			n.Destination = h.destination(n.Destination)
		case *ast.Image:
			n.Destination = h.destination(n.Destination)
		case *ast.AutoLink:
			autoLinks = append(autoLinks, n)
		}
		return ast.WalkContinue, nil
	})

	// An autolink's URL lives in an unexported text node; replace the
	// autolink with an equivalent one.
	for _, a := range autoLinks {
		label := string(a.Label(source))
		if !strings.ContainsAny(label, wrapBreakpoints) {
			continue
		}
		repl := ast.NewAutoLink(a.AutoLinkType, ast.NewTextSegment(appendText(h.escaped.Replace(label))))
		repl.Protocol = a.Protocol
		for c := a.FirstChild(); c != nil; {
			next := c.NextSibling()
			repl.AppendChild(repl, c)
			c = next
		}
		a.Parent().ReplaceChild(a.Parent(), a, repl)
	}
	return out
}

// text returns the value v of the text node n with hidden breakpoints.
func (h *breakpointHider) text(n *ast.Text, v string) string {
	switch n.Parent().(type) {
	case *ast.Image:
		// glamour prints image texts as they are.
		return h.escaped.Replace(v)
	case *ast.CodeSpan:
		// glamour decodes entities in code spans but does not unescape.
		return strings.ReplaceAll(h.literal.Replace(html.UnescapeString(v)), "&", "&amp;")
	}
	// glamour decodes the entities of text (also "&#45;"), so decode them
	// first and re-encode the ampersands; then it unescapes.
	return strings.ReplaceAll(h.escaped.Replace(html.UnescapeString(v)), "&", "&amp;")
}

// destination hides the breakpoints of an absolute URL, which glamour
// prints as is (it resolves and re-encodes relative ones).
func (h *breakpointHider) destination(d []byte) []byte {
	if u, err := url.Parse(string(d)); err != nil || !u.IsAbs() {
		return d
	}
	return []byte(h.escaped.Replace(string(d)))
}
