package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

const showTestMarkdown = "# Heading One\n\n## Sub\n\n" +
	"Some *em* and **bold** text with a [link](https://example.com) and `inline`.\n\n" +
	"- item one\n- item two\n\n1. first\n\n" +
	"```go\nfunc main() { fmt.Println(\"hi\") }\n```\n\n> quote\n"

var (
	extendedColorSeq = regexp.MustCompile(`\x1b\[[0-9;]*(?:38|48);[25];`)
	anyEscapeSeq     = regexp.MustCompile(`\x1b\[`)
	sgrSeq           = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

func TestRenderMarkdownBody(t *testing.T) {
	tests := []struct {
		name        string
		isTTY       bool
		profile     termenv.Profile
		wantEscapes bool
	}{
		{name: "tty truecolor", isTTY: true, profile: termenv.TrueColor, wantEscapes: true},
		{name: "tty ansi256", isTTY: true, profile: termenv.ANSI256, wantEscapes: true},
		{name: "tty ansi", isTTY: true, profile: termenv.ANSI, wantEscapes: true},
		{name: "tty no color", isTTY: true, profile: termenv.Ascii, wantEscapes: false},
		{name: "not a tty", isTTY: false, profile: termenv.TrueColor, wantEscapes: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderMarkdownBody(showTestMarkdown, tt.isTTY, tt.profile)
			if err != nil {
				t.Fatalf("renderMarkdownBody() error = %v", err)
			}

			// Only the 16-color ANSI palette may be used, even when the
			// terminal supports more colors.
			if m := extendedColorSeq.FindString(out); m != "" {
				t.Errorf("output contains 256-color/truecolor sequence %q:\n%q", m, out)
			}

			if got := anyEscapeSeq.MatchString(out); got != tt.wantEscapes {
				t.Errorf("output contains escapes = %v, want %v:\n%q", got, tt.wantEscapes, out)
			}

			plain := sgrSeq.ReplaceAllString(out, "")
			for _, want := range []string{"Heading One", "item two", "fmt.Println", "https://example.com"} {
				if !strings.Contains(plain, want) {
					t.Errorf("output missing %q:\n%s", want, plain)
				}
			}
		})
	}
}
