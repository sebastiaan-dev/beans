package agent

import (
	"errors"
	"fmt"
	"strings"
)

// QuestionAnswer is the user's answer to a single AskUserQuestion question.
// Answers holds the selected option labels and/or free-text replies.
type QuestionAnswer struct {
	Question string
	Answers  []string
}

// ErrNoPendingQuestions is returned when answers are submitted but the session
// isn't waiting on a structured AskUserQuestion interaction.
var ErrNoPendingQuestions = errors.New("agent has no pending questions")

// FormatQuestionAnswers validates the user's answers against the pending
// questions of an AskUserQuestion call and renders them as a single message.
//
// Claude asks several questions in one AskUserQuestion call and expects all
// answers back at once; answering one at a time makes it re-ask the rest.
// Every question must be answered exactly once, and single-select questions
// accept exactly one answer. The output mirrors the shape of Claude Code's own
// AskUserQuestion tool result ("question"="answer"), in question order.
func FormatQuestionAnswers(questions []AskUserQuestion, answers []QuestionAnswer) (string, error) {
	if len(questions) == 0 {
		return "", ErrNoPendingQuestions
	}

	byQuestion := make(map[string][]string, len(answers))
	for _, a := range answers {
		known := false
		for _, q := range questions {
			if q.Question == a.Question {
				known = true
				break
			}
		}
		if !known {
			return "", fmt.Errorf("answer for unknown question %q", a.Question)
		}
		if _, dup := byQuestion[a.Question]; dup {
			return "", fmt.Errorf("question %q answered more than once", a.Question)
		}
		var values []string
		for _, v := range a.Answers {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		byQuestion[a.Question] = values
	}

	var sb strings.Builder
	sb.WriteString("Here are my answers to your questions:\n")
	for _, q := range questions {
		values, ok := byQuestion[q.Question]
		if !ok || len(values) == 0 {
			return "", fmt.Errorf("question %q has no answer", q.Question)
		}
		if !q.MultiSelect && len(values) > 1 {
			return "", fmt.Errorf("question %q accepts a single answer, got %d", q.Question, len(values))
		}
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = quoteAnswer(v)
		}
		fmt.Fprintf(&sb, "\n- %s=%s", quoteAnswer(q.Question), strings.Join(quoted, ", "))
	}
	return sb.String(), nil
}

// quoteAnswer wraps s in double quotes, escaping backslashes and quotes so
// the boundaries between questions and answers stay unambiguous.
func quoteAnswer(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// AnswerQuestions answers every question of the session's pending
// AskUserQuestion interaction in a single user message and resumes the agent.
// The pending interaction is validated and cleared atomically, so concurrent
// or repeated submissions can't send the answers twice.
func (m *Manager) AnswerQuestions(beanID, workDir string, answers []QuestionAnswer) error {
	return m.sendMessage(beanID, workDir, nil, nil, func(s *Session) (string, error) {
		if s == nil || s.PendingInteraction == nil || s.PendingInteraction.Type != InteractionAskUser {
			return "", ErrNoPendingQuestions
		}
		return FormatQuestionAnswers(s.PendingInteraction.Questions, answers)
	})
}
