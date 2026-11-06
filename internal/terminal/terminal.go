package terminal

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	gopty "github.com/aymanbagabas/go-pty"

	"github.com/hmans/beans/internal/worktree"
)

const scrollbackSize = 64 * 1024 // 64KB

// Termination timeouts used by Session.Close. Variables so tests can shorten them.
var (
	// terminateGracePeriod is how long Close waits for the session's processes to
	// exit after asking them to (SIGHUP/SIGTERM + PTY hangup) before force-killing.
	terminateGracePeriod = 3 * time.Second
	// killGracePeriod is how long Close waits after force-killing before giving up.
	killGracePeriod = 2 * time.Second
)

// ErrSessionBlocked is returned when a session cannot be created because its ID is
// currently blocked (e.g. while the owning workspace is being destroyed).
var ErrSessionBlocked = errors.New("terminal session is shutting down")

// RingBuffer is a fixed-size circular buffer for terminal scrollback.
type RingBuffer struct {
	buf []byte
	cap int
	w   int // write position
	len int // bytes stored (capped at cap)
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{buf: make([]byte, size), cap: size}
}

// Write appends data to the ring buffer, overwriting oldest data if full.
func (r *RingBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}

	if len(p) >= r.cap {
		// Data larger than buffer — keep only the last cap bytes
		copy(r.buf, p[len(p)-r.cap:])
		r.w = 0
		r.len = r.cap
		return
	}

	// Copy in up to two segments (before and after wrap)
	n := copy(r.buf[r.w:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
	}
	r.w = (r.w + len(p)) % r.cap
	r.len += len(p)
	if r.len > r.cap {
		r.len = r.cap
	}
}

// Bytes returns the buffer contents in chronological order.
func (r *RingBuffer) Bytes() []byte {
	if r.len == 0 {
		return nil
	}
	if r.len < r.cap {
		return append([]byte(nil), r.buf[:r.len]...)
	}
	// Wrapped: oldest data starts at write position
	result := make([]byte, r.cap)
	copy(result, r.buf[r.w:])
	copy(result[r.cap-r.w:], r.buf[:r.w])
	return result
}

// Session represents an active PTY session with scrollback buffering.
type Session struct {
	id  string
	pty gopty.Pty
	cmd *gopty.Cmd
	mu  sync.Mutex

	scrollback *RingBuffer
	scrollMu   sync.Mutex

	// Client attachment: the current output channel for a connected WebSocket.
	// Protected by attachMu. Nil when no client is attached.
	attachMu sync.Mutex
	clientCh chan []byte

	done   chan struct{} // closed when readLoop exits (shell exited)
	exited chan struct{} // closed when the shell process has exited and been reaped

	closeOnce sync.Once
}

// newSession wraps a started command and its PTY in a Session and starts the
// background goroutines that pump output and reap the process.
func newSession(id string, p gopty.Pty, cmd *gopty.Cmd) *Session {
	sess := &Session{
		id:         id,
		pty:        p,
		cmd:        cmd,
		scrollback: NewRingBuffer(scrollbackSize),
		done:       make(chan struct{}),
		exited:     make(chan struct{}),
	}
	go sess.readLoop()
	go func() {
		defer close(sess.exited)
		_ = cmd.Wait()
	}()
	return sess
}

// Write sends input to the PTY.
func (s *Session) Write(data []byte) (int, error) {
	return s.pty.Write(data)
}

// Resize changes the PTY window size.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pty.Resize(int(cols), int(rows))
}

// Attach connects a client to receive PTY output.
// Returns the current scrollback contents and a channel for new output.
// Calling Attach again replaces the previous client (the old channel is orphaned).
func (s *Session) Attach() ([]byte, <-chan []byte) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()

	ch := make(chan []byte, 256)
	s.clientCh = ch

	s.scrollMu.Lock()
	sb := s.scrollback.Bytes()
	s.scrollMu.Unlock()

	return sb, ch
}

// Detach disconnects the current client if it matches the given channel.
// This prevents a slow-to-exit old handler from clearing a new client's channel.
func (s *Session) Detach(ch <-chan []byte) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()

	// Only clear if this is still the active client
	if s.clientCh != nil {
		// Compare by reading from the same underlying channel
		var asChan <-chan []byte = s.clientCh
		if asChan == ch {
			s.clientCh = nil
		}
	}
}

// Done returns a channel that is closed when the shell process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Alive returns true if the shell process is still running.
func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// readLoop continuously reads PTY output, writes to scrollback buffer,
// and forwards to the attached client channel.
func (s *Session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			s.scrollMu.Lock()
			s.scrollback.Write(data)
			s.scrollMu.Unlock()

			s.attachMu.Lock()
			ch := s.clientCh
			s.attachMu.Unlock()

			if ch != nil {
				select {
				case ch <- data:
				default:
					// Client too slow — data is preserved in scrollback
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// Close terminates the session's processes and closes the PTY. It blocks until the
// processes have exited: every process in the PTY session is first asked to stop (SIGHUP,
// SIGTERM and a PTY hangup), and is force-killed if it is still around after
// terminateGracePeriod. Close is idempotent and safe to call concurrently.
func (s *Session) Close() {
	s.closeOnce.Do(s.terminate)
}

func (s *Session) terminate() {
	pid := 0
	if s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}

	if pid > 0 {
		terminateSession(pid)
	}
	s.mu.Lock()
	_ = s.pty.Close()
	s.mu.Unlock()

	if s.waitExit(pid, terminateGracePeriod) {
		return
	}

	log.Printf("[terminal] session %s did not exit within %s, killing", s.id, terminateGracePeriod)
	if pid > 0 {
		killSession(pid)
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if !s.waitExit(pid, killGracePeriod) {
		log.Printf("[terminal] session %s still has running processes after kill", s.id)
	}
}

// waitExit waits until the shell process has exited and no other process in its
// PTY session remains, or until the timeout elapses. Returns true on exit.
func (s *Session) waitExit(pid int, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	select {
	case <-s.exited:
	case <-deadline.C:
		return false
	}

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for pid > 0 && sessionAlive(pid) {
		select {
		case <-ticker.C:
		case <-deadline.C:
			return false
		}
	}
	return true
}

// closeSlave closes the slave end of a Unix PTY in the parent process after
// the child has been started. This is necessary on Linux so the master gets
// EOF when the child exits. On non-Unix platforms this is a no-op.
func closeSlave(p gopty.Pty) {
	if up, ok := p.(gopty.UnixPty); ok {
		up.Slave().Close()
	}
}

// defaultShell returns the default shell for the current platform.
func defaultShell() string {
	if runtime.GOOS == "windows" {
		if ps, err := exec.LookPath("pwsh.exe"); err == nil {
			return ps
		}
		return "cmd.exe"
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

// EnvFunc returns extra environment variables for a given session ID.
// Used to inject workspace-specific env vars (e.g. BEANS_WORKSPACE_PORT).
type EnvFunc func(sessionID string) []string

// Manager manages PTY sessions keyed by session ID.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// blocked counts active blocks per session ID; while > 0, no session with
	// that ID may be created (see CloseAndBlock).
	blocked map[string]int
	envFunc EnvFunc
}

// NewManager creates a new terminal session manager.
// An optional EnvFunc can provide extra environment variables per session.
func NewManager(envFunc EnvFunc) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		blocked:  make(map[string]int),
		envFunc:  envFunc,
	}
}

// Create spawns a new interactive login shell session, replacing (and
// terminating) any existing session with the same ID.
func (m *Manager) Create(sessionID, workDir string, cols, rows uint16) (*Session, error) {
	return m.replace(sessionID, func() (*Session, error) {
		return m.spawn(sessionID, workDir, cols, rows, "-l")
	})
}

// CreateWithCommand spawns a new PTY session that runs a specific command
// (via shell -c) instead of an interactive login shell. The session ends
// when the command exits. Any existing session with the same ID is terminated.
func (m *Manager) CreateWithCommand(sessionID, workDir string, cols, rows uint16, command string) (*Session, error) {
	return m.replace(sessionID, func() (*Session, error) {
		return m.spawn(sessionID, workDir, cols, rows, "-l", "-c", command)
	})
}

// GetOrCreate returns an existing alive session or creates a new one.
// The bool return indicates whether an existing session was reused (reconnection).
func (m *Manager) GetOrCreate(sessionID, workDir string, cols, rows uint16) (*Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.sessions[sessionID]; ok {
		if existing.Alive() {
			_ = existing.Resize(cols, rows)
			return existing, true, nil
		}
		// Shell exited — clean up the dead session in the background
		// (its process is already gone, so this only releases resources).
		delete(m.sessions, sessionID)
		go existing.Close()
	}

	if m.blocked[sessionID] > 0 {
		return nil, false, ErrSessionBlocked
	}

	sess, err := m.spawn(sessionID, workDir, cols, rows, "-l")
	if err != nil {
		return nil, false, err
	}
	m.sessions[sessionID] = sess
	return sess, false, nil
}

// replace terminates any existing session with the given ID (without holding
// the manager lock, since termination may take a while) and registers the
// session returned by start.
func (m *Manager) replace(sessionID string, start func() (*Session, error)) (*Session, error) {
	m.mu.Lock()
	if m.blocked[sessionID] > 0 {
		m.mu.Unlock()
		return nil, ErrSessionBlocked
	}
	old := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	if old != nil {
		old.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blocked[sessionID] > 0 {
		return nil, ErrSessionBlocked
	}
	sess, err := start()
	if err != nil {
		return nil, err
	}
	if raced := m.sessions[sessionID]; raced != nil {
		// Another session was registered while we were terminating the old one.
		go raced.Close()
	}
	m.sessions[sessionID] = sess
	return sess, nil
}

// spawn starts the default shell with the given arguments on a new PTY.
// It does not register the session with the manager.
func (m *Manager) spawn(sessionID, workDir string, cols, rows uint16, args ...string) (*Session, error) {
	shell := defaultShell()

	p, err := gopty.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create PTY: %w", err)
	}

	if err := p.Resize(int(cols), int(rows)); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to resize PTY: %w", err)
	}

	// Shell-integration directives belong to the shell that started
	// beans-serve; see worktree.ChildEnv.
	env := append(worktree.ChildEnv(), "TERM=xterm-256color")
	if m.envFunc != nil {
		env = append(env, m.envFunc(sessionID)...)
	}

	cmd := p.Command(shell, args...)
	cmd.Dir = workDir
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	closeSlave(p)

	return newSession(sessionID, p, cmd), nil
}

// Get retrieves an existing session.
func (m *Manager) Get(sessionID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

// Close terminates and removes a specific session, waiting for its processes to exit.
func (m *Manager) Close(sessionID string) {
	closeAll(m.take(sessionID))
}

// CloseAndBlock terminates the given sessions (concurrently), waiting for their
// processes to exit, and prevents sessions with these IDs from being created
// until the returned release function is called. Use it when the resources a
// session depends on (such as a worktree directory) are about to disappear.
func (m *Manager) CloseAndBlock(sessionIDs ...string) (release func()) {
	m.mu.Lock()
	for _, id := range sessionIDs {
		m.blocked[id]++
	}
	m.mu.Unlock()

	closeAll(m.take(sessionIDs...))

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, id := range sessionIDs {
				if m.blocked[id]--; m.blocked[id] <= 0 {
					delete(m.blocked, id)
				}
			}
		})
	}
}

// Shutdown terminates all active sessions.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	closeAll(sessions)
}

// take removes the given sessions from the manager and returns them.
func (m *Manager) take(sessionIDs ...string) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sessions []*Session
	for _, id := range sessionIDs {
		if s, ok := m.sessions[id]; ok {
			sessions = append(sessions, s)
			delete(m.sessions, id)
		}
	}
	return sessions
}

// closeAll closes the given sessions concurrently and waits for all of them.
func closeAll(sessions []*Session) {
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Close()
		}()
	}
	wg.Wait()
}
