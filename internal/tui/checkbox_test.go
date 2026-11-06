package tui

import (
	"strings"
	"testing"
)

func TestFindCheckboxes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string // "x text" / "  text"
	}{
		{"none", "Just text\n\n- a list\n- [] not a box\n-[ ] no space", nil},
		{"simple", "- [ ] one\n- [x] two\n- [X] three", []string{"  one", "x two", "x three"}},
		{"markers", "* [ ] star\n+ [ ] plus\n1. [ ] dot\n2) [x] paren", []string{"  star", "  plus", "  dot", "x paren"}},
		{"nested", "- [ ] parent\n  - [x] child\n    - [ ] grandchild\n\t- [ ] tab", []string{"  parent", "x child", "  grandchild", "  tab"}},
		{"empty item", "- [ ]\n- [x] ", []string{"  ", "x "}},
		{"needs space after box", "- [x]done\n- [ ]\tok", []string{"  ok"}},
		{"blockquote", "> - [ ] quoted\n>> - [x] deeper", []string{"  quoted", "x deeper"}},
		{
			"fenced code ignored",
			"- [ ] before\n```md\n- [ ] in code\n```\n~~~\n- [x] tilde code\n~~~\n- [ ] after",
			[]string{"  before", "  after"},
		},
		{
			"fence needs matching closer",
			"````\n```\n- [ ] still code\n````\n- [ ] out",
			[]string{"  out"},
		},
		{
			"tilde does not close backtick fence",
			"```\n~~~\n- [ ] code\n```\n- [ ] out",
			[]string{"  out"},
		},
		{"indented fence in list", "- [ ] a\n  ```\n  - [ ] code\n  ```\n- [ ] b", []string{"  a", "  b"}},
		{"unclosed fence", "```\n- [ ] code", nil},
		{"crlf", "- [ ] one\r\n- [x] two\r\n", []string{"  one", "x two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, it := range findCheckboxes(tt.body) {
				mark := " "
				if it.checked {
					mark = "x"
				}
				got = append(got, mark+" "+it.text)
				if c := tt.body[it.state]; c != ' ' && c != 'x' && c != 'X' {
					t.Errorf("state offset %d points at %q", it.state, c)
				}
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("checkboxes = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToggleCheckboxPreservesBody(t *testing.T) {
	body := "Intro  \r\n\n- [ ] one\r\n  - [X] two\n```\n- [ ] code\n```\n> 3. [ ] three\n\ntrailing   "
	cases := []struct {
		idx  int
		want string
	}{
		{0, strings.Replace(body, "- [ ] one", "- [x] one", 1)},
		{1, strings.Replace(body, "- [X] two", "- [ ] two", 1)},
		{2, strings.Replace(body, "3. [ ] three", "3. [x] three", 1)},
	}
	for _, c := range cases {
		got, ok := toggleCheckbox(body, c.idx)
		if !ok || got != c.want {
			t.Errorf("toggle %d = %q, %v; want %q", c.idx, got, ok, c.want)
		}
		// Toggling twice restores the body (except X becoming x).
		back, _ := toggleCheckbox(got, c.idx)
		if c.idx != 1 && back != body {
			t.Errorf("toggle %d twice = %q, want the original", c.idx, back)
		}
	}
	if got, ok := toggleCheckbox(body, 3); ok || got != body {
		t.Error("toggling a missing checkbox must not change the body")
	}
	if _, ok := toggleCheckbox(body, -1); ok {
		t.Error("negative index must fail")
	}
}

func TestMarkCheckbox(t *testing.T) {
	body := "- [ ] one\n- [x] two ▶"
	got, mk := markCheckbox(body, 0)
	if mk == "" || mk == "▶" {
		t.Fatalf("marker %q must not occur in the body", mk)
	}
	if want := "- [ ] " + mk + " one\n- [x] two ▶"; got != want {
		t.Errorf("marked = %q, want %q", got, want)
	}
	if _, mk := markCheckbox(body, 5); mk != "" {
		t.Error("no marker for a missing checkbox")
	}
}
