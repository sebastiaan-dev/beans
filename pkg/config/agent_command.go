package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// AgentCommandEnvVar overrides tui.agent_command for the current user (the
// agent is a personal preference, like the theme).
const AgentCommandEnvVar = "BEANS_AGENT_COMMAND"

// Placeholders expanded in tui.agent_command.
const (
	AgentPlaceholderBeanID    = "bean_id"
	AgentPlaceholderBeanTitle = "bean_title"
	AgentPlaceholderPath      = "path"
)

// agentPlaceholderRe matches a {{name}} placeholder (spaces allowed inside
// the braces).
var agentPlaceholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]*)\s*\}\}`)

// AgentCommandVars are the values of the tui.agent_command placeholders.
type AgentCommandVars struct {
	BeanID    string // {{bean_id}}
	BeanTitle string // {{bean_title}}
	Path      string // {{path}}: the workspace directory
}

// AgentCommandSetting returns the agent command in effect and where it
// comes from ("$BEANS_AGENT_COMMAND", "tui.agent_command", or "" when unset).
func (c *Config) AgentCommandSetting() (value, source string) {
	if v := strings.TrimSpace(os.Getenv(AgentCommandEnvVar)); v != "" {
		return v, "$" + AgentCommandEnvVar
	}
	if v := strings.TrimSpace(c.TUI.AgentCommand); v != "" {
		return v, "tui.agent_command"
	}
	return "", ""
}

// ValidateAgentCommand checks the agent command in effect: its quoting and
// placeholders. An unset command is valid.
func (c *Config) ValidateAgentCommand() error {
	value, source := c.AgentCommandSetting()
	if value == "" {
		return nil
	}
	if _, err := ExpandAgentCommand(value, AgentCommandVars{}); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

// GetAgentCommand returns the agent command in effect as program and
// arguments with the placeholders expanded, or nil when no agent command is
// configured.
//
// The command (tui.agent_command, overridden by $BEANS_AGENT_COMMAND) is
// split into words like a POSIX shell would (whitespace separates words;
// single quotes, double quotes and backslashes quote) but never run through
// a shell. The placeholders {{bean_id}}, {{bean_title}} and {{path}} are then
// replaced inside each word, so values containing spaces or quotes stay one
// argument, e.g.
//
//	claude "Work on bean {{bean_id}}: {{bean_title}}"
func (c *Config) GetAgentCommand(vars AgentCommandVars) ([]string, error) {
	value, source := c.AgentCommandSetting()
	if value == "" {
		return nil, nil
	}
	args, err := ExpandAgentCommand(value, vars)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return args, nil
}

// ExpandAgentCommand splits an agent command into words (see
// GetAgentCommand) and expands the placeholders in each word.
func ExpandAgentCommand(command string, vars AgentCommandVars) ([]string, error) {
	words, err := SplitCommandLine(command)
	if err != nil {
		return nil, err
	}
	if len(words) == 0 {
		return nil, errors.New("empty command")
	}
	values := map[string]string{
		AgentPlaceholderBeanID:    vars.BeanID,
		AgentPlaceholderBeanTitle: vars.BeanTitle,
		AgentPlaceholderPath:      vars.Path,
	}
	var unknown string
	malformed := false
	for i, w := range words {
		// Every "{{" must start a placeholder (checked before expansion, so
		// values containing "{{" are fine).
		if strings.Count(w, "{{") != len(agentPlaceholderRe.FindAllString(w, -1)) && unknown == "" {
			malformed = true
			unknown = strings.SplitN(strings.SplitN(w, "{{", 2)[1], "}}", 2)[0]
		}
		words[i] = agentPlaceholderRe.ReplaceAllStringFunc(w, func(m string) string {
			name := agentPlaceholderRe.FindStringSubmatch(m)[1]
			v, ok := values[name]
			if !ok && unknown == "" {
				unknown = name
			}
			return v
		})
	}
	if unknown != "" || malformed {
		return nil, fmt.Errorf("unknown placeholder {{%s}} (use {{%s}}, {{%s}} or {{%s}})", unknown,
			AgentPlaceholderBeanID, AgentPlaceholderBeanTitle, AgentPlaceholderPath)
	}
	if words[0] == "" {
		return nil, errors.New("empty program name")
	}
	return words, nil
}

// SplitCommandLine splits s into words with POSIX shell quoting rules:
// unquoted whitespace separates words, '...' quotes literally, "..." quotes
// with backslash escapes for \, ", $ and `, and a backslash outside quotes
// escapes the next character. No expansion (variables, globs, ...) happens.
func SplitCommandLine(s string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			if quote == '"' && !strings.ContainsRune("\\\"$`\n", r) {
				cur.WriteRune('\\')
			}
			if r != '\n' { // backslash-newline is a line continuation
				cur.WriteRune(r)
			}
			escaped, inWord = false, true
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped = true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	switch {
	case escaped:
		return nil, errors.New("trailing backslash")
	case quote != 0:
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}
