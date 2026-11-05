package agent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Timeouts for stopping an agent process (see runningProcess.kill).
var (
	// gracefulStopTimeout is how long the agent gets to exit after SIGINT
	// (Claude Code saves its session state for --resume on SIGINT).
	gracefulStopTimeout = 3 * time.Second
	// forceStopTimeout is how long to wait for the output reader to finish
	// after the process group was killed, before closing stdout ourselves.
	forceStopTimeout = 2 * time.Second
)

// runningProcess wraps an active claude CLI process.
type runningProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	cancel  context.CancelFunc
	started chan struct{} // closed by spawnAndRun once cmd.Start has returned (nil in tests)
	done    chan struct{} // closed by spawnAndRun when the process exits
}

// waitStarted blocks until the process has been started (or failed to start).
func (p *runningProcess) waitStarted() {
	if p.started != nil {
		<-p.started
	}
}

// signal sends SIGINT and closes stdin to request a graceful shutdown.
// Does not block — the process may still be running when this returns.
// Used by handleBlockingTool (which runs inside the readOutput goroutine
// and cannot wait for the process to exit without deadlocking).
func (p *runningProcess) signal() {
	p.waitStarted()
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	_ = p.cmd.Process.Signal(syscall.SIGINT)
}

// kill gracefully terminates the running process and waits for it to exit.
// Sends SIGINT first to give Claude Code a chance to save its session state
// (needed for --resume to work). Falls back to killing the whole process
// group after a timeout. Once kill returns, the process and the tool
// processes it started are gone and spawnAndRun has finished its cleanup.
// Must not be called from the process's own spawnAndRun goroutine.
func (p *runningProcess) kill() {
	p.signal()

	// The done channel is closed by spawnAndRun after cmd.Wait() returns.
	select {
	case <-p.done:
		// Process exited cleanly — session state should be saved. Reap any
		// tool processes it left behind in its process group.
		killProcessGroup(p.cmd)
		return
	case <-time.After(gracefulStopTimeout):
	}

	if p.cancel != nil {
		p.cancel()
	}
	killProcessGroup(p.cmd)
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}

	select {
	case <-p.done:
		return
	case <-time.After(forceStopTimeout):
	}
	// Something outside the process group (e.g. a daemonized grandchild)
	// still holds stdout open, so the output reader never sees EOF. Close
	// our end so spawnAndRun can finish.
	log.Printf("[agent] process output still open after kill; closing stdout")
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	<-p.done
}

// sendToProcess writes a user message to an existing process's stdin
// using Claude Code's stream-json input format. When images are present,
// the content field is sent as an array of content blocks (text + image)
// matching the Anthropic API format.
func (m *Manager) sendToProcess(proc *runningProcess, beanID, message string, images []ImageRef) error {
	var content interface{} = message

	if len(images) > 0 && m.store != nil {
		var blocks []interface{}
		if message != "" {
			blocks = append(blocks, map[string]string{"type": "text", "text": message})
		}
		for _, img := range images {
			path, err := m.store.attachmentPath(beanID, img.ID)
			if err != nil {
				log.Printf("[agent:%s] skip image %s: %v", beanID, img.ID, err)
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[agent:%s] skip image %s: %v", beanID, img.ID, err)
				continue
			}
			blocks = append(blocks, map[string]interface{}{
				"type": "image",
				"source": map[string]string{
					"type":       "base64",
					"media_type": img.MediaType,
					"data":       base64.StdEncoding.EncodeToString(data),
				},
			})
		}
		content = blocks
	}

	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": content,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')
	_, err = proc.stdin.Write(data)
	return err
}

// DefaultClaudeBinary is the Claude Code executable used when a Manager has no
// explicit path configured (see Manager.SetClaudePath). It is resolved via PATH.
const DefaultClaudeBinary = "claude"

// spawnAndRun spawns a claude CLI process and reads its output.
// This runs in a goroutine — it blocks until the process exits.
func (m *Manager) spawnAndRun(beanID string, session *Session) {
	ctx, cancel := context.WithCancel(context.Background())

	args := buildClaudeArgs(session)
	cmd := exec.CommandContext(ctx, m.claudeBinary(), args...)
	cmd.Dir = session.WorkDir
	cmd.Env = buildClaudeEnv()
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.setError(beanID, fmt.Sprintf("stdin pipe: %v", err))
		cancel()
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.setError(beanID, fmt.Sprintf("stdout pipe: %v", err))
		cancel()
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.setError(beanID, fmt.Sprintf("stderr pipe: %v", err))
		cancel()
		return
	}

	proc := &runningProcess{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		cancel:  cancel,
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}

	// Register the process as live before starting it, so StopAndBlock either
	// sees (and stops) it or this spawn sees the block and doesn't start.
	m.mu.Lock()
	if m.blocked[beanID] > 0 {
		if s, ok := m.sessions[beanID]; ok && s.Status == StatusRunning {
			s.Status = StatusIdle
		}
		m.mu.Unlock()
		cancel()
		m.notify(beanID)
		return
	}
	m.addLive(beanID, proc)
	m.mu.Unlock()

	startErr := cmd.Start()
	close(proc.started)
	if startErr != nil {
		m.mu.Lock()
		m.removeLive(beanID, proc)
		m.mu.Unlock()
		close(proc.done)
		m.setError(beanID, fmt.Sprintf("start claude: %v", startErr))
		cancel()
		return
	}

	m.mu.Lock()
	// If the session was blocked while starting, StopAndBlock is already
	// stopping this process; don't make it the session's current process.
	if m.blocked[beanID] == 0 {
		m.processes[beanID] = proc
	}
	m.mu.Unlock()

	// Drain stderr silently — Claude Code writes verbose progress info here
	// that overwhelms server logs. Errors that matter surface as stream events.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			_ = scanner.Text()
		}
	}()

	log.Printf("[agent:%s] spawned claude process (pid=%d, dir=%s)", beanID, cmd.Process.Pid, session.WorkDir)

	// Send the initial user message, prepending bean context on first spawn
	// and any file attachment context from @-mentions
	lastMsg := session.Messages[len(session.Messages)-1]
	initialMsg := lastMsg.ContextPrefix + lastMsg.Content
	if session.SessionID == "" && m.contextProvider != nil {
		if ctx := m.contextProvider(beanID); ctx != "" {
			initialMsg = ctx + "\n\n---\n\n" + initialMsg
		}
	}
	if err := m.sendToProcess(proc, beanID, initialMsg, lastMsg.Images); err != nil {
		m.setError(beanID, fmt.Sprintf("send initial message: %v", err))
		// Can't use proc.kill() here: it waits for this goroutine to close
		// proc.done. Kill the process and fall through to the normal cleanup.
		cancel()
		killProcessGroup(cmd)
	} else {
		// Read stdout line by line
		m.readOutput(beanID, stdout, session.WorkDir, proc)
	}

	// Process exited — clean up.
	// Only modify state if this proc is still the current one for this beanID.
	// A new process may have already been spawned (e.g. after handleBlockingTool
	// signaled us and the user sent a new message), so we must not clobber it.
	_ = cmd.Wait()

	m.mu.Lock()
	m.removeLive(beanID, proc)
	shouldNotify := false
	if m.processes[beanID] == proc {
		delete(m.processes, beanID)
		if s, ok := m.sessions[beanID]; ok && s.Status == StatusRunning {
			s.Status = StatusIdle
			shouldNotify = true
		}
	}
	m.mu.Unlock()
	close(proc.done)

	if shouldNotify {
		m.notify(beanID)
	}
}

// readOutput reads Claude Code's stream-json output line by line,
// updates the session state, and notifies subscribers.
func (m *Manager) readOutput(beanID string, stdout io.Reader, workDir string, proc *runningProcess) {
	scanner := bufio.NewScanner(stdout)
	// Increase buffer for long lines (1MB)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	// Track tool input accumulation for extracting summaries.
	// Tool messages are persisted lazily: we wait until the summary is available
	// (or the next event arrives) before writing to JSONL, so the persisted
	// content includes the human-readable description (e.g. "Bash: Build binary").
	var toolInputBuf strings.Builder
	var toolMsgIdx int = -1
	var toolName string
	var toolInvIdx int = -1     // index into session.ToolInvocations
	var pendingToolPersist bool // true when current tool msg hasn't been persisted yet
	var deferredAskUser bool    // true when AskUserQuestion detected, waiting for input to complete
	var blocked bool            // true after handleBlockingTool — suppresses ensureRunning

	// Write tool diff tracking: capture old file content before the write happens.
	var writeOldContent string
	var writeOldCaptured bool

	// Subagent activities are tracked via task_progress events and cleared on eventResult.

	// flushToolMsg persists the current tool message to JSONL if one is pending.
	// Called before persisting any other message or at end of stream.
	// Also computes diffs for Write tool messages.
	flushToolMsg := func() {
		if !pendingToolPersist {
			return
		}
		pendingToolPersist = false

		// Compute diff for Write tool before persisting
		if toolName == "Write" && writeOldCaptured {
			if newContent := extractFileContent(toolInputBuf.String()); newContent != "" {
				filePath := extractFilePath(toolInputBuf.String())
				// Strip workDir prefix for display
				label := filePath
				if workDir != "" {
					label = strings.TrimPrefix(label, workDir+"/")
				}
				diff := computeUnifiedDiff(writeOldContent, newContent, label)
				if diff != "" {
					m.mu.Lock()
					if s, ok := m.sessions[beanID]; ok && toolMsgIdx >= 0 && toolMsgIdx < len(s.Messages) {
						s.Messages[toolMsgIdx].Diff = diff
					}
					m.mu.Unlock()
					m.notify(beanID)
				}
			}
		}
		// Reset write tracking state
		writeOldContent = ""
		writeOldCaptured = false

		if m.store == nil {
			return
		}
		m.mu.RLock()
		s, ok := m.sessions[beanID]
		var msg Message
		if ok && toolMsgIdx >= 0 && toolMsgIdx < len(s.Messages) {
			msg = s.Messages[toolMsgIdx]
		}
		m.mu.RUnlock()
		if msg.Role == RoleTool {
			if err := m.store.appendMessage(beanID, msg); err != nil {
				log.Printf("[agent:%s] failed to persist tool message: %v", beanID, err)
			}
		}
	}

	// ensureRunning transitions the session back to StatusRunning if it's
	// currently Idle. This handles multi-turn processes where Claude Code
	// starts a new turn (e.g. after a Stop hook) without us spawning a new
	// process. Skipped after a blocking tool has been handled — the process
	// is winding down and shouldn't flip back to RUNNING. Also skipped if
	// this process has been replaced (e.g. after autoApproveModeSwitch).
	ensureRunning := func() {
		if blocked {
			return
		}
		m.mu.Lock()
		if m.processes[beanID] != proc {
			m.mu.Unlock()
			return
		}
		s, ok := m.sessions[beanID]
		if ok && s.Status == StatusIdle {
			s.Status = StatusRunning
			m.mu.Unlock()
			m.notify(beanID)
			return
		}
		m.mu.Unlock()
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		ev := parseStreamLine(line)
		if ev.Type == eventUnknown {
			// Log only the event type, not the full payload (which can be huge)
			eventType := "?"
			var peek struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &peek) == nil && peek.Type != "" {
				eventType = peek.Type
			}
			log.Printf("[agent:%s] unhandled event type: %s", beanID, eventType)
		}

		// Finalize deferred AskUserQuestion blocking when tool input is complete.
		// Tool input is complete when we receive any event that isn't a delta.
		if deferredAskUser && ev.Type != eventToolInputDelta {
			interaction := &PendingInteraction{Type: InteractionAskUser}
			if questions := parseAskUserInput(toolInputBuf.String()); questions != nil {
				interaction.Questions = questions
			}
			m.handleBlockingTool(beanID, interaction)
			deferredAskUser = false
			blocked = true
		}

		switch ev.Type {
		case eventAssistantMessage:
			// Flush any pending tool message before the assistant message
			flushToolMsg()
			ensureRunning()

			// Full assistant message — arrives after stream_event deltas.
			// Only use the text as fallback if deltas didn't already build it,
			// to avoid replacing streamed content with the same text (visual flash).
			if ev.Text != "" {
				m.mu.Lock()
				if s, ok := m.sessions[beanID]; ok {
					idx := s.streamingIdx
					hasStreamedContent := idx >= 0 && idx < len(s.Messages) &&
						s.Messages[idx].Role == RoleAssistant && s.Messages[idx].Content != ""
					if !hasStreamedContent {
						// No delta-built content — use the full message
						s.Messages = append(s.Messages, Message{Role: RoleAssistant, Content: ev.Text})
						s.streamingIdx = len(s.Messages) - 1
						m.mu.Unlock()
						m.notify(beanID)
					} else {
						m.mu.Unlock()
					}
				} else {
					m.mu.Unlock()
				}
			}
			if ev.SessionID != "" {
				m.mu.Lock()
				if s, ok := m.sessions[beanID]; ok {
					s.SessionID = ev.SessionID
				}
				m.mu.Unlock()
			}

		case eventToolUse:
			ensureRunning()

			// Handle blocking tools that require user interaction.
			// Check session state to avoid re-intercepting mode switches
			// that already took effect (e.g. after --resume).
			m.mu.RLock()
			sess := m.sessions[beanID]
			m.mu.RUnlock()
			if interaction := blockingInteraction(ev.ToolName, sess); interaction != nil {
				if interaction.Type == InteractionAskUser {
					// Defer blocking — we need to accumulate tool input first
					// to extract structured question data.
					deferredAskUser = true
				} else if interaction.Type == InteractionEnterPlan {
					// Auto-approve entering plan mode — no user prompt needed.
					m.autoApproveModeSwitch(beanID, interaction)
				} else {
					m.handleBlockingTool(beanID, interaction)
					blocked = true
				}
			}

			// No subagent clearing here — activities are only cleared on eventResult
			// (turn end). Parent text/tool events can interleave with active subagents.

			// Flush any previous pending tool message before starting a new one
			flushToolMsg()

			// Tool use start — show tool name in the conversation.
			// Reset streamingIdx so subsequent text deltas create a
			// new assistant message *after* this tool message, preserving
			// chronological order.
			toolInputBuf.Reset()
			toolName = ev.ToolName
			m.mu.Lock()
			if s, ok := m.sessions[beanID]; ok {
				// Persist the pre-tool assistant message before resetting
				idx := s.streamingIdx
				if m.store != nil && idx >= 0 && idx < len(s.Messages) && s.Messages[idx].Role == RoleAssistant && s.Messages[idx].Content != "" {
					msg := s.Messages[idx]
					m.mu.Unlock()
					if err := m.store.appendMessage(beanID, msg); err != nil {
						log.Printf("[agent:%s] failed to persist pre-tool assistant message: %v", beanID, err)
					}
					m.mu.Lock()
					// Re-check session still exists after re-acquiring lock
					s = m.sessions[beanID]
					if s == nil {
						m.mu.Unlock()
						m.notify(beanID)
						continue
					}
				}
				s.streamingIdx = -1
				toolMsg := Message{Role: RoleTool, Content: ev.ToolName}
				s.Messages = append(s.Messages, toolMsg)
				toolMsgIdx = len(s.Messages) - 1
				// Track structured tool invocation
				s.ToolInvocations = append(s.ToolInvocations, ToolInvocation{Tool: ev.ToolName})
				toolInvIdx = len(s.ToolInvocations) - 1
				// Don't persist yet — wait for tool input summary
				pendingToolPersist = true
			}
			m.mu.Unlock()
			m.notify(beanID)

		case eventToolInputDelta:
			// Accumulate tool input JSON and try to extract a summary
			toolInputBuf.WriteString(ev.Text)
			if toolMsgIdx >= 0 {
				// Try parsing accumulated JSON (may be incomplete — that's fine)
				summary := extractToolSummary(toolInputBuf.String(), workDir)
				if summary != "" {
					m.mu.Lock()
					if s, ok := m.sessions[beanID]; ok && toolMsgIdx < len(s.Messages) {
						s.Messages[toolMsgIdx].Content = toolName + ": " + summary
						// Update structured tool invocation input.
						// For file-based tools, store the raw (untruncated) file_path
						// so findPlanFilePath works even with long paths.
						if toolInvIdx >= 0 && toolInvIdx < len(s.ToolInvocations) {
							if fp := extractFilePath(toolInputBuf.String()); fp != "" {
								s.ToolInvocations[toolInvIdx].Input = fp
							} else {
								s.ToolInvocations[toolInvIdx].Input = summary
							}
						}
					}
					m.mu.Unlock()
					m.notify(beanID)
				}

				// Capture old file content for Write tool diffs.
				// We read the file as soon as we can parse file_path from the
				// (possibly incomplete) input JSON — at this point the tool
				// hasn't executed yet, so the file is still in its pre-write state.
				if toolName == "Write" && !writeOldCaptured {
					if filePath := extractFilePath(toolInputBuf.String()); filePath != "" {
						writeOldCaptured = true
						data, err := os.ReadFile(filePath)
						if err == nil {
							writeOldContent = string(data)
						}
						// If file doesn't exist (new file), writeOldContent stays empty
					}
				}
			}

		case eventNewTextBlock:
			// Flush any pending tool message before new text
			flushToolMsg()
			ensureRunning()

			// New text content block starting — insert paragraph break if
			// the current message already has content (e.g. after tool use).
			m.mu.Lock()
			if s, ok := m.sessions[beanID]; ok {
				idx := s.streamingIdx
				if idx >= 0 && idx < len(s.Messages) &&
					s.Messages[idx].Role == RoleAssistant && s.Messages[idx].Content != "" {
					s.Messages[idx].Content += "\n\n"
				}
			}
			m.mu.Unlock()
			if ev.Text != "" {
				m.appendAssistantText(beanID, ev.Text)
				m.notify(beanID)
			}

		case eventTextDelta:
			// Flush any pending tool message before text starts
			flushToolMsg()
			ensureRunning()

			// Streaming text delta (with --include-partial-messages)
			m.appendAssistantText(beanID, ev.Text)
			m.notify(beanID)

		case eventResult:
			// Flush any pending tool message before result
			flushToolMsg()

			if ev.SessionID != "" {
				m.mu.Lock()
				if s, ok := m.sessions[beanID]; ok {
					s.SessionID = ev.SessionID
				}
				m.mu.Unlock()

				// Persist session ID for --resume
				if m.store != nil {
					if err := m.store.saveSessionID(beanID, ev.SessionID); err != nil {
						log.Printf("[agent:%s] failed to persist session ID: %v", beanID, err)
					}
				}
			}

			// Persist the completed assistant message and reset streaming target
			m.mu.Lock()
			if s, ok := m.sessions[beanID]; ok {
				idx := s.streamingIdx
				if m.store != nil && idx >= 0 && idx < len(s.Messages) && s.Messages[idx].Role == RoleAssistant {
					msg := s.Messages[idx]
					m.mu.Unlock()
					if err := m.store.appendMessage(beanID, msg); err != nil {
						log.Printf("[agent:%s] failed to persist assistant message: %v", beanID, err)
					}
					m.mu.Lock()
				}
				// Reset streaming target
				s.streamingIdx = -1
				// Only modify session status if this is still the current process.
				// A dying process (e.g. after autoApproveModeSwitch) must not
				// reset status to Idle when a new process is already Running.
				if m.processes[beanID] == proc {
					s.Status = StatusIdle
					s.SystemStatus = ""
					s.SubagentActivities = nil
				}
			}
			m.mu.Unlock()
			m.notify(beanID)

			// Fire turn complete callback (e.g. to update workspace activity timestamp)
			if m.onTurnComplete != nil {
				go m.onTurnComplete(beanID)
			}

			// Generate quick reply suggestions asynchronously
			go m.generateQuickReplies(beanID)

			// After compact, prune orphaned image attachments
			if m.wasLastUserMessage(beanID, "/compact") {
				m.pruneOrphanedAttachments(beanID)
			}

		case eventError:
			flushToolMsg()
			m.setError(beanID, ev.Error)

		case eventSystemStatus:
			m.mu.Lock()
			if s, ok := m.sessions[beanID]; ok {
				s.SystemStatus = ev.Text
			}
			m.mu.Unlock()
			m.notify(beanID)

		case eventTaskProgress:
			ensureRunning()

			// Subagent progress update — upsert by task_id.
			m.mu.Lock()
			if s, ok := m.sessions[beanID]; ok {
				// Find existing activity by task_id, or create new one
				var activity *SubagentActivity
				for _, a := range s.SubagentActivities {
					if a.TaskID == ev.TaskID {
						activity = a
						break
					}
				}
				if activity == nil {
					activity = &SubagentActivity{
						TaskID: ev.TaskID,
						Index:  len(s.SubagentActivities) + 1,
					}
					s.SubagentActivities = append(s.SubagentActivities, activity)
				}
				if ev.ToolName != "" {
					activity.CurrentTool = ev.ToolName
				}
				if ev.Text != "" {
					activity.Description = ev.Text
				}
			}
			m.mu.Unlock()
			m.notify(beanID)

		}
	}

	// Finalize deferred AskUserQuestion if stream ended before input completed
	if deferredAskUser {
		interaction := &PendingInteraction{Type: InteractionAskUser}
		if questions := parseAskUserInput(toolInputBuf.String()); questions != nil {
			interaction.Questions = questions
		}
		m.handleBlockingTool(beanID, interaction)
	}

	// Flush any remaining pending tool message at end of stream
	flushToolMsg()
}

// wasLastUserMessage checks if the most recent user message in the session
// matches the given content (trimmed). Used to detect /compact for pruning.
func (m *Manager) wasLastUserMessage(beanID, content string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[beanID]
	if !ok {
		return false
	}
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == RoleUser {
			return strings.TrimSpace(s.Messages[i].Content) == content
		}
	}
	return false
}

// appendAssistantText appends text to the current streaming assistant message.
// Uses streamingIdx to ensure deltas from an ongoing turn always go to the
// correct message, even if user messages are interleaved mid-turn.
func (m *Manager) appendAssistantText(beanID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[beanID]
	if !ok {
		return
	}

	idx := s.streamingIdx
	if idx < 0 || idx >= len(s.Messages) || s.Messages[idx].Role != RoleAssistant {
		// No valid streaming target — create a new assistant message
		s.Messages = append(s.Messages, Message{Role: RoleAssistant})
		idx = len(s.Messages) - 1
		s.streamingIdx = idx
	}

	s.Messages[idx].Content += text
}

// setError sets the session to error state and notifies subscribers.
func (m *Manager) setError(beanID, errMsg string) {
	m.mu.Lock()
	if s, ok := m.sessions[beanID]; ok {
		s.Status = StatusError
		s.Error = errMsg
	}
	m.mu.Unlock()
	m.notify(beanID)
}

// blockingInteraction returns a PendingInteraction if the tool name is a blocking
// tool that requires user approval, or nil for regular tools.
// Mode-switch tools (ExitPlanMode/EnterPlanMode) are only blocking when the
// session is actually in the mode being exited/entered — this prevents infinite
// loops when a resumed process retries the same tool call after we already
// toggled the mode.
func blockingInteraction(toolName string, session *Session) *PendingInteraction {
	switch toolName {
	case "ExitPlanMode":
		if session != nil && (!session.PlanMode || session.ActMode) {
			return nil // already exited plan mode or approved (e.g. after resume)
		}
		return &PendingInteraction{Type: InteractionExitPlan}
	case "EnterPlanMode":
		if session != nil && session.PlanMode {
			return nil // already in plan mode
		}
		return &PendingInteraction{Type: InteractionEnterPlan}
	case "AskUserQuestion":
		return &PendingInteraction{Type: InteractionAskUser}
	default:
		return nil
	}
}

// handleBlockingTool processes a blocking tool call by setting the pending
// interaction, killing the process, and notifying subscribers. The session ID
// is preserved so the conversation can be resumed with --resume.
func (m *Manager) handleBlockingTool(beanID string, interaction *PendingInteraction) {
	m.mu.Lock()
	s, ok := m.sessions[beanID]
	if !ok {
		m.mu.Unlock()
		return
	}

	// For ExitPlanMode, find and read the plan file from recent Write tool messages.
	// Falls back to the last assistant message if no plan file was written (Claude
	// sometimes describes the plan inline instead of writing a file).
	if interaction.Type == InteractionExitPlan {
		if path := findPlanFilePath(s.ToolInvocations); path != "" {
			if content, err := os.ReadFile(path); err == nil {
				interaction.PlanContent = string(content)
			}
		}
		if interaction.PlanContent == "" {
			for i := len(s.Messages) - 1; i >= 0; i-- {
				if s.Messages[i].Role == RoleAssistant && s.Messages[i].Content != "" {
					interaction.PlanContent = s.Messages[i].Content
					break
				}
			}
		}
	}

	s.PendingInteraction = interaction
	s.Status = StatusIdle

	// NOTE: Mode is NOT toggled here — that happens when the user approves
	// or rejects the interaction. This prevents the UI from showing the wrong
	// mode while the user is still reviewing.

	proc, hasProc := m.processes[beanID]
	if hasProc {
		delete(m.processes, beanID)
	}
	m.mu.Unlock()

	if hasProc && proc != nil {
		// Use signal() not kill() — we're inside the readOutput goroutine
		// (same goroutine as spawnAndRun), so blocking on proc.done would deadlock.
		proc.signal()
	}

	m.notify(beanID)
}

// autoApproveModeSwitch handles EnterPlanMode/ExitPlanMode by toggling the mode,
// killing the current process, and immediately respawning with --resume.
// No pending interaction is set and no message is added to the conversation —
// the mode switch is transparent to the user.
func (m *Manager) autoApproveModeSwitch(beanID string, interaction *PendingInteraction) {
	m.mu.Lock()
	s, ok := m.sessions[beanID]
	if !ok {
		m.mu.Unlock()
		return
	}

	switch interaction.Type {
	case InteractionExitPlan:
		s.PlanMode = false
		s.ActMode = true
	case InteractionEnterPlan:
		s.PlanMode = true
		s.ActMode = false
	}

	s.PendingInteraction = nil
	s.Status = StatusRunning

	proc, hasProc := m.processes[beanID]
	if hasProc {
		delete(m.processes, beanID)
	}
	m.mu.Unlock()

	if hasProc && proc != nil {
		proc.signal()
	}

	m.notify(beanID)

	// Respawn with the new mode via --resume. Runs in a goroutine because
	// we're inside readOutput (same goroutine as spawnAndRun).
	go m.spawnAndRun(beanID, s)
}

// findPlanFilePath scans tool invocations for a Write to ~/.claude/plans/*.md
// and returns the file path, or empty string if not found.
func findPlanFilePath(invocations []ToolInvocation) string {
	for i := len(invocations) - 1; i >= 0; i-- {
		inv := invocations[i]
		if inv.Tool == "Write" && strings.Contains(inv.Input, "/.claude/plans/") && strings.HasSuffix(inv.Input, ".md") {
			return inv.Input
		}
	}
	return ""
}

// buildClaudeArgs constructs the CLI arguments for spawning claude.
func buildClaudeArgs(session *Session) []string {
	args := []string{
		"-p",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--disallowedTools", "EnterWorktree", "ExitWorktree",
	}
	if session.Effort != "" {
		args = append(args, "--effort", session.Effort)
	}
	if session.ActMode {
		args = append(args, "--dangerously-skip-permissions")
		// Explicitly allow editing Claude's own config files, which are
		// classified as "sensitive" and blocked even with --dangerously-skip-permissions.
		// Patterns must use absolute paths because Claude Code's Edit/Write tools
		// operate on absolute file paths internally.
		if session.WorkDir != "" {
			dir := session.WorkDir
			args = append(args,
				"--allowedTools", "Edit("+dir+"/CLAUDE.md)",
				"--allowedTools", "Write("+dir+"/CLAUDE.md)",
				"--allowedTools", "Edit("+dir+"/.claude/**)",
				"--allowedTools", "Write("+dir+"/.claude/**)",
			)
		}
	} else if session.PlanMode {
		args = append(args, "--permission-mode", "plan")
	}
	if session.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", session.SystemPrompt)
	}
	if session.SessionID != "" {
		args = append(args, "--resume", session.SessionID)
	}
	return args
}
