package agent

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hmans/beans/internal/agent/agenttest"
)

var testQuestions = []AskUserQuestion{
	{
		Header:   "Database",
		Question: "Which database should we use?",
		Options:  []AskUserOption{{Label: "PostgreSQL"}, {Label: "SQLite"}},
	},
	{
		Header:      "Features",
		Question:    "Which features should be included?",
		MultiSelect: true,
		Options:     []AskUserOption{{Label: "Auth"}, {Label: "Billing"}, {Label: "Search"}},
	},
}

func TestFormatQuestionAnswers(t *testing.T) {
	tests := []struct {
		name      string
		questions []AskUserQuestion
		answers   []QuestionAnswer
		want      string
		wantErr   string
	}{
		{
			name:      "all questions answered, in question order",
			questions: testQuestions,
			answers: []QuestionAnswer{
				{Question: "Which features should be included?", Answers: []string{"Auth", "Search"}},
				{Question: "Which database should we use?", Answers: []string{"PostgreSQL"}},
			},
			want: "Here are my answers to your questions:\n" +
				"\n- \"Which database should we use?\"=\"PostgreSQL\"" +
				"\n- \"Which features should be included?\"=\"Auth\", \"Search\"",
		},
		{
			name:      "free-text answer is trimmed and quotes are escaped",
			questions: testQuestions[:1],
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"  a \"custom\" one  "}},
			},
			want: "Here are my answers to your questions:\n" +
				"\n- \"Which database should we use?\"=\"a \\\"custom\\\" one\"",
		},
		{
			name:      "no pending questions",
			questions: nil,
			answers:   []QuestionAnswer{{Question: "x", Answers: []string{"y"}}},
			wantErr:   ErrNoPendingQuestions.Error(),
		},
		{
			name:      "missing answer",
			questions: testQuestions,
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"SQLite"}},
			},
			wantErr: `question "Which features should be included?" has no answer`,
		},
		{
			name:      "blank answer counts as missing",
			questions: testQuestions[:1],
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"   "}},
			},
			wantErr: "has no answer",
		},
		{
			name:      "unknown question",
			questions: testQuestions[:1],
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"SQLite"}},
				{Question: "Something else?", Answers: []string{"yes"}},
			},
			wantErr: "unknown question",
		},
		{
			name:      "duplicate answer",
			questions: testQuestions[:1],
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"SQLite"}},
				{Question: "Which database should we use?", Answers: []string{"PostgreSQL"}},
			},
			wantErr: "more than once",
		},
		{
			name:      "multiple answers for single-select question",
			questions: testQuestions[:1],
			answers: []QuestionAnswer{
				{Question: "Which database should we use?", Answers: []string{"SQLite", "PostgreSQL"}},
			},
			wantErr: "accepts a single answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatQuestionAnswers(tt.questions, tt.answers)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FormatQuestionAnswers() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FormatQuestionAnswers() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("FormatQuestionAnswers() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// awaitSession polls until cond holds for the session or the timeout expires.
func awaitSession(t *testing.T, m *Manager, beanID string, cond func(*Session) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		s := m.sessions[beanID]
		ok := s != nil && cond(s)
		m.mu.RUnlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %q condition", beanID)
}

// TestAskUserQuestionMultipleQuestionsRoundTrip verifies that a single
// AskUserQuestion call with several questions is surfaced as one pending
// interaction containing all questions, and that answering sends all answers
// back in one user message.
func TestAskUserQuestionMultipleQuestionsRoundTrip(t *testing.T) {
	pr, pw := io.Pipe()
	m := &Manager{
		claudePath:  agenttest.FakeClaude(t),
		sessions:    make(map[string]*Session),
		processes:   make(map[string]*runningProcess),
		subscribers: make(map[string][]chan struct{}),
	}
	m.sessions["bean-ask"] = &Session{
		ID:           "bean-ask",
		AgentType:    "claude",
		Status:       StatusRunning,
		WorkDir:      t.TempDir(),
		Messages:     []Message{{Role: RoleUser, Content: "set up the project"}},
		streamingIdx: -1,
	}
	proc := &runningProcess{done: make(chan struct{})}
	m.processes["bean-ask"] = proc

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.readOutput("bean-ask", pr, "", proc)
	}()
	writeLine := func(line string) { _, _ = pw.Write([]byte(line + "\n")) }

	writeLine(`{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion"}}`)
	// Tool input arrives in several partial chunks.
	writeLine(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"questions\":[{\"header\":\"Database\",\"question\":\"Which database should we use?\",\"multiSelect\":false,"}}`)
	writeLine(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"\"options\":[{\"label\":\"PostgreSQL\",\"description\":\"\"},{\"label\":\"SQLite\",\"description\":\"\"}]},"}}`)
	writeLine(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"header\":\"Features\",\"question\":\"Which features should be included?\",\"multiSelect\":true,\"options\":[{\"label\":\"Auth\",\"description\":\"\"},{\"label\":\"Billing\",\"description\":\"\"}]}]}"}}`)
	writeLine(`{"type":"content_block_stop"}`)

	awaitSession(t, m, "bean-ask", func(s *Session) bool { return s.PendingInteraction != nil })
	m.mu.RLock()
	pending := *m.sessions["bean-ask"].PendingInteraction
	m.mu.RUnlock()
	if pending.Type != InteractionAskUser || len(pending.Questions) != 2 {
		t.Fatalf("expected ask_user interaction with 2 questions, got %+v", pending)
	}
	if pending.Questions[1].Question != "Which features should be included?" || !pending.Questions[1].MultiSelect {
		t.Errorf("unexpected second question: %+v", pending.Questions[1])
	}
	pw.Close()
	<-done

	err := m.AnswerQuestions("bean-ask", "", []QuestionAnswer{
		{Question: "Which database should we use?", Answers: []string{"SQLite"}},
		{Question: "Which features should be included?", Answers: []string{"Auth", "Billing"}},
	})
	if err != nil {
		t.Fatalf("AnswerQuestions() error = %v", err)
	}

	// Wait for the resume spawn (of the fake claude) to finish its turn.
	awaitSession(t, m, "bean-ask", func(s *Session) bool { return s.Status != StatusRunning })

	m.mu.RLock()
	s := m.sessions["bean-ask"]
	msgs := append([]Message(nil), s.Messages...)
	stillPending := s.PendingInteraction
	m.mu.RUnlock()

	if stillPending != nil {
		t.Errorf("expected pending interaction to be cleared, got %+v", stillPending)
	}
	var userMsgs []string
	for _, msg := range msgs {
		if msg.Role == RoleUser {
			userMsgs = append(userMsgs, msg.Content)
		}
	}
	want := "Here are my answers to your questions:\n" +
		"\n- \"Which database should we use?\"=\"SQLite\"" +
		"\n- \"Which features should be included?\"=\"Auth\", \"Billing\""
	if len(userMsgs) != 2 || userMsgs[1] != want {
		t.Errorf("expected exactly one answer message %q, got user messages %q", want, userMsgs)
	}

	// The interaction is consumed: answering again must fail without sending.
	if err := m.AnswerQuestions("bean-ask", "", []QuestionAnswer{
		{Question: "Which database should we use?", Answers: []string{"SQLite"}},
	}); err != ErrNoPendingQuestions {
		t.Errorf("second AnswerQuestions() error = %v, want ErrNoPendingQuestions", err)
	}
}

func TestAnswerQuestionsRejectsInvalidAnswersWithoutSending(t *testing.T) {
	m := NewManager("", nil)
	m.SetClaudePath(agenttest.FakeClaude(t))
	m.sessions["test"] = &Session{
		ID:                 "test",
		Status:             StatusIdle,
		Messages:           []Message{{Role: RoleUser, Content: "hi"}},
		PendingInteraction: &PendingInteraction{Type: InteractionAskUser, Questions: testQuestions},
		streamingIdx:       -1,
	}

	err := m.AnswerQuestions("test", "", []QuestionAnswer{
		{Question: "Which database should we use?", Answers: []string{"SQLite"}},
	})
	if err == nil {
		t.Fatal("expected error for partially answered questions")
	}
	s := m.sessions["test"]
	if s.PendingInteraction == nil || len(s.PendingInteraction.Questions) != 2 {
		t.Error("expected pending questions to be preserved after rejected answers")
	}
	if len(s.Messages) != 1 || s.Status != StatusIdle {
		t.Errorf("expected no message to be sent, got %d messages, status %s", len(s.Messages), s.Status)
	}

	if err := m.AnswerQuestions("missing", "", nil); err != ErrNoPendingQuestions {
		t.Errorf("AnswerQuestions() on unknown session error = %v, want ErrNoPendingQuestions", err)
	}
	if _, ok := m.sessions["missing"]; ok {
		t.Error("AnswerQuestions() must not create a session")
	}
}
