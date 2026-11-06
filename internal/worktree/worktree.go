// Package worktree manages git worktrees associated with beans.
package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hmans/beans/internal/gitutil"
	"github.com/hmans/beans/pkg/bean"
)

// DefaultFetchTimeout is the default timeout for git fetch operations during
// worktree creation. Set below the HTTP server's WriteTimeout (15s) to prevent
// the HTTP connection from dying before the fetch completes.
const DefaultFetchTimeout = 10 * time.Second

// SetupStatus represents the state of a worktree's post-creation setup.
type SetupStatus string

const (
	SetupNone    SetupStatus = ""        // no setup configured or already done
	SetupRunning SetupStatus = "running" // setup command is executing
	SetupDone    SetupStatus = "done"    // setup completed successfully
	SetupFailed  SetupStatus = "failed"  // setup command failed
)

// Worktree represents a git worktree.
type Worktree struct {
	ID           string
	Branch       string // empty for detached worktrunk worktrees
	Path         string
	Detached     bool        // HEAD is detached (e.g. during a rebase)
	Name         string      // Human-readable name
	Description  string      // Auto-generated summary of what this workspace is doing
	BeanIDs      []string    // Bean IDs detected from changes vs base branch
	Setup        SetupStatus // post-creation setup status (runtime only)
	SetupError   string      // error message if setup failed
	LastActiveAt time.Time   // When an agent last completed a turn in this worktree
}

// SetupDoneFunc is called when a worktree's setup command finishes.
// Receives the worktree ID, success status, and error message (if failed).
type SetupDoneFunc func(worktreeID string, success bool, errMsg string)

// Manager handles git worktree operations for a repository.
type Manager struct {
	repoRoot     string
	worktreeRoot string // git backend: where worktrees are created; both backends: metadata files (e.g. ~/.beans/worktrees/<project>/)
	backend      Backend
	baseRef      string
	setupCommand string        // shell command to run after worktree creation
	fetchTimeout time.Duration // timeout for git fetch during worktree creation (0 = skip fetch)
	mu           sync.RWMutex

	// setupStatuses tracks runtime setup status for worktrees (not persisted)
	setupStatuses map[string]setupState

	// onSetupDone is called when a worktree's setup command finishes
	onSetupDone SetupDoneFunc

	// subscribers for worktree change events
	subMu       sync.Mutex
	subscribers []chan struct{}

	// pathIDs remembers the ID of each worktree path seen on a branch, so a
	// worktrunk worktree keeps its ID while detached.
	idMu    sync.Mutex
	pathIDs map[string]string
	// seen holds the workspaces (ID -> path) at the last pruneRemoved call
	// (nil until the first one).
	seen map[string]string
}

// setupState tracks the runtime setup status and error for a worktree.
type setupState struct {
	status SetupStatus
	err    string
}

// ManagerOption is a functional option for configuring a Manager.
type ManagerOption func(*Manager)

// WithBackend sets the worktree backend (default: the git backend).
func WithBackend(b Backend) ManagerOption {
	return func(m *Manager) {
		if b != nil {
			m.backend = b
		}
	}
}

// WithFetchTimeout sets the timeout for git fetch operations during worktree creation.
// A value of 0 disables the fetch entirely. Negative values use the default timeout.
func WithFetchTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		m.fetchTimeout = d
	}
}

// NewManager creates a new worktree manager for the given repository root.
// worktreeRoot is the directory for metadata files and, with the git backend,
// the worktrees themselves (e.g. ~/.beans/worktrees/<project>/).
// baseRef is the git ref to use as the starting point for new branches (e.g. "main").
// setupCommand is an optional shell command to run inside new worktrees after creation.
func NewManager(repoRoot, worktreeRoot, baseRef, setupCommand string, opts ...ManagerOption) *Manager {
	m := &Manager{
		repoRoot:      repoRoot,
		worktreeRoot:  worktreeRoot,
		backend:       gitBackend{},
		baseRef:       baseRef,
		setupCommand:  setupCommand,
		fetchTimeout:  DefaultFetchTimeout,
		setupStatuses: make(map[string]setupState),
		pathIDs:       make(map[string]string),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// RepoRoot returns the path to the main repository root.
func (m *Manager) RepoRoot() string {
	return m.repoRoot
}

// Backend returns the kind of worktree backend in use.
func (m *Manager) Backend() BackendKind {
	return m.backend.Kind()
}

// BaseRef returns the configured base ref for worktree branches.
func (m *Manager) BaseRef() string {
	return m.baseRef
}

// fetchBaseRef fetches the base ref from the remote so that worktrees branch
// from the latest code. Handles both plain refs ("main") and remote-tracking
// refs ("origin/main"). Logs warnings on failure but does not return an error —
// a stale ref is better than refusing to create a worktree.
//
// The fetch is bounded by m.fetchTimeout. If the timeout is 0, the fetch is
// skipped entirely (useful for airgapped environments or when the remote is
// known to be unavailable).
func (m *Manager) fetchBaseRef() {
	if m.baseRef == "" {
		return
	}

	if m.fetchTimeout == 0 {
		log.Printf("[worktree] fetch timeout is 0, skipping remote fetch")
		return
	}

	// Determine which remote and ref to fetch.
	// "origin/main" → remote="origin", ref="main"
	// "main"        → remote="origin", ref="main"
	remote := "origin"
	ref := m.baseRef
	if strings.Contains(m.baseRef, "/") {
		parts := strings.SplitN(m.baseRef, "/", 2)
		remote = parts[0]
		ref = parts[1]
	}

	// Skip fetch if the remote doesn't exist (e.g., local-only test repos)
	checkCmd := exec.Command("git", "remote", "get-url", remote)
	checkCmd.Dir = m.repoRoot
	if err := checkCmd.Run(); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), m.fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "fetch", remote, ref)
	cmd.Dir = m.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[worktree] warning: git fetch %s %s timed out after %s", remote, ref, m.fetchTimeout)
		} else {
			log.Printf("[worktree] warning: git fetch %s %s failed: %s", remote, ref, strings.TrimSpace(string(out)))
		}
	}
}

// SetOnSetupDone registers a callback that fires when a worktree's setup command finishes.
func (m *Manager) SetOnSetupDone(fn SetupDoneFunc) {
	m.onSetupDone = fn
}

// Subscribe returns a channel that receives a signal whenever worktrees change.
// The caller should call Unsubscribe when done.
func (m *Manager) Subscribe() chan struct{} {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	ch := make(chan struct{}, 1)
	m.subscribers = append(m.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscription channel.
func (m *Manager) Unsubscribe(ch chan struct{}) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Notify sends a signal to all subscribers. Exported so external watchers
// (e.g., the worktree bean file watcher) can trigger a refresh.
func (m *Manager) Notify() {
	m.notify()
}

// notify sends a signal to all subscribers.
func (m *Manager) notify() {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- struct{}{}:
		default:
			// Non-blocking: if the channel already has a pending signal, skip
		}
	}
}

// List returns the workspaces of the repository: for the git backend the
// worktrees on beans/* branches, for the worktrunk backend every linked
// worktree. The main worktree is never included. Worktrees are returned in
// git's order (creation order, oldest first).
func (m *Manager) List() ([]Worktree, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worktrees, err := m.listLocked()
	if err != nil {
		return nil, err
	}

	for i := range worktrees {
		wt := &worktrees[i]
		if meta := m.loadMetaFor(wt.ID, wt.Path); meta != nil {
			wt.Name = meta.Name
			wt.Description = meta.Description
			if meta.LastActiveAt != nil {
				wt.LastActiveAt = *meta.LastActiveAt
			}
		}
		if wt.Name == "" && m.backend.Kind() == BackendWorktrunk {
			// Worktrees adopted from worktrunk (or plain git) are named
			// after their branch, or their directory when detached.
			wt.Name = wt.Branch
			if wt.Name == "" {
				wt.Name = filepath.Base(wt.Path)
			}
		}
		wt.BeanIDs = m.DetectBeanIDs(wt.Path)
		if st, ok := m.setupStatuses[wt.ID]; ok {
			wt.Setup = st.status
			wt.SetupError = st.err
		}
	}
	return worktrees, nil
}

// listLocked lists the workspaces (IDs, branches and paths only) from
// `git worktree list --porcelain`. Must be called with m.mu held.
func (m *Manager) listLocked() ([]Worktree, error) {
	entries, err := m.entries()
	if err != nil {
		return nil, err
	}
	return m.workspacesOf(entries), nil
}

// entries returns all worktrees of the repository, main worktree first.
func (m *Manager) entries() ([]Entry, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = m.repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseEntries(string(out)), nil
}

// workspacesOf maps worktree entries to workspaces and remembers their IDs.
func (m *Manager) workspacesOf(entries []Entry) []Worktree {
	worktrees := m.backend.workspaces(entries, m.worktreeRoot, m.knownIDForPath)
	m.rememberIDs(worktrees)
	return worktrees
}

// pruneRemoved forgets the metadata and setup status of workspaces whose
// worktree was removed outside beans (e.g. `wt remove`, `git worktree
// remove`) since the last call, so a new worktree that later gets the same ID
// doesn't inherit them. A workspace that merely changed its ID while its
// worktree still exists (e.g. a branch switch) keeps its metadata. The first
// call only records the current workspaces.
func (m *Manager) pruneRemoved() {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.entries()
	if err != nil {
		return
	}
	existing := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.Prunable {
			existing[e.Path] = true
		}
	}
	current := make(map[string]string)
	for _, wt := range m.workspacesOf(entries) {
		current[wt.ID] = wt.Path
	}

	m.idMu.Lock()
	seen := m.seen
	m.seen = current
	m.idMu.Unlock()

	for id, path := range seen {
		if _, ok := current[id]; ok || existing[path] {
			continue
		}
		log.Printf("[worktree] worktree %s (%s) was removed outside beans; forgetting its metadata", id, path)
		m.removeMeta(id)
		delete(m.setupStatuses, id)
		m.idMu.Lock()
		delete(m.pathIDs, path)
		m.idMu.Unlock()
	}
}

// findLocked returns the workspace with the given ID. Must be called with m.mu held.
func (m *Manager) findLocked(id string) (*Worktree, error) {
	worktrees, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	for i := range worktrees {
		if worktrees[i].ID == id {
			return &worktrees[i], nil
		}
	}
	return nil, fmt.Errorf("no worktree with id %s", id)
}

// Path returns the filesystem path of the workspace with the given ID, looked
// up from git (worktrunk places worktrees wherever its path template says, and
// git backend worktrees may have been created from another workspace).
func (m *Manager) Path(id string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wt, err := m.findLocked(id)
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

// rememberIDs records the IDs of worktrees on a branch, so a worktree keeps
// its ID while it is temporarily detached (e.g. during a rebase).
func (m *Manager) rememberIDs(worktrees []Worktree) {
	m.idMu.Lock()
	defer m.idMu.Unlock()
	for _, wt := range worktrees {
		if !wt.Detached {
			m.pathIDs[wt.Path] = wt.ID
		}
	}
}

// knownIDForPath returns the ID a worktree path had before, from memory or
// from the path recorded in a metadata file, or "".
func (m *Manager) knownIDForPath(path string) string {
	m.idMu.Lock()
	id := m.pathIDs[path]
	m.idMu.Unlock()
	if id != "" {
		return id
	}
	matches, _ := filepath.Glob(filepath.Join(m.worktreeRoot, "*"+metaSuffix))
	sort.Strings(matches)
	for _, file := range matches {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var meta worktreeMeta
		if json.Unmarshal(data, &meta) == nil && meta.Path != "" && samePath(meta.Path, path) {
			return strings.TrimSuffix(filepath.Base(file), metaSuffix)
		}
	}
	return ""
}

// samePath reports whether two paths refer to the same location, resolving
// symlinks (e.g. /tmp vs /private/tmp on macOS).
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// DetectBeanIDs returns bean IDs found in the worktree's diff vs the base branch.
// It filters for .beans/*.md files, excluding dot-prefixed subdirs (like .worktrees/,
// .conversations/) and the archive/ directory.
func (m *Manager) DetectBeanIDs(worktreePath string) []string {
	changes, err := gitutil.AllChangesVsUpstream(worktreePath, m.baseRef)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var ids []string

	for _, change := range changes {
		// Must be directly under .beans/ and end with .md
		rest, ok := strings.CutPrefix(change.Path, ".beans/")
		if !ok {
			continue
		}

		// Exclude subdirectories: anything with a / is nested
		if strings.Contains(rest, "/") {
			continue
		}

		// Must be a .md file
		if !strings.HasSuffix(rest, ".md") {
			continue
		}

		// Skip if the file was deleted in the worktree
		fullPath := filepath.Join(worktreePath, ".beans", rest)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		id, _ := bean.ParseFilename(rest)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	sort.Strings(ids)
	return ids
}

// Create creates a new worktree for a workspace with the given name and stores
// the name as metadata. With the git backend the branch is beans/<name> and
// the worktree is placed in the worktree root; with the worktrunk backend the
// branch is <name> and worktrunk's path template places it. The workspace ID
// is the name (for worktrunk: the ID derived from the branch, which equals the
// name for all valid names except ones with repeated '_').
func (m *Manager) Create(name string) (*Worktree, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Refuse names whose ID is already used: reusing it would hand the other
	// workspace's conversation, port and metadata to the new one.
	if existing, err := m.listLocked(); err == nil {
		for _, wt := range existing {
			if wt.ID == name || (m.backend.Kind() == BackendWorktrunk && wt.Branch == name) {
				return nil, fmt.Errorf("a workspace named %q already exists (branch %q at %s)", name, wt.Branch, wt.Path)
			}
		}
	}

	// Fetch the latest base ref from origin so worktrees branch from up-to-date code.
	// This is especially important for PR-based workflows using origin/<branch> as base_ref.
	m.fetchBaseRef()

	branch, worktreePath, err := m.backend.create(createRequest{
		repoRoot:     m.repoRoot,
		worktreeRoot: m.worktreeRoot,
		name:         name,
		baseRef:      m.baseRef,
	})
	if err != nil {
		return nil, err
	}

	id := name
	if m.backend.Kind() != BackendGit {
		// The ID is derived from the listing, like for any other worktree.
		id = ""
		if listed, err := m.listLocked(); err == nil {
			for _, wt := range listed {
				if samePath(wt.Path, worktreePath) {
					id, worktreePath = wt.ID, wt.Path
					break
				}
			}
		}
		if id == "" {
			return nil, fmt.Errorf("worktree %s was created at %s but git does not list it", branch, worktreePath)
		}
	}

	wt := &Worktree{
		ID:     id,
		Branch: branch,
		Path:   worktreePath,
		Name:   name,
	}

	m.idMu.Lock()
	m.pathIDs[worktreePath] = id
	if m.seen != nil {
		m.seen[id] = worktreePath
	}
	m.idMu.Unlock()

	// Save the name metadata with initial LastActiveAt so new worktrees
	// sort to the top (most recently created first)
	now := time.Now().UTC()
	if err := m.saveMeta(id, &worktreeMeta{Name: name, LastActiveAt: &now}); err != nil {
		log.Printf("[worktree] warning: failed to save metadata for %s: %v", id, err)
	}

	m.startSetupLocked(wt)

	log.Printf("[worktree] created worktree %s (name=%s, branch=%s, path=%s, backend=%s)", id, name, branch, worktreePath, m.backend.Kind())
	m.notify()
	return wt, nil
}

// startSetupLocked runs the post-creation setup asynchronously: the backend's
// blocking creation hooks (worktrunk pre-start), then the worktree.setup
// command, then the backend's background hooks (worktrunk post-start). The
// setup status covers all of them; nothing is tracked when there is nothing to
// run. Must be called with m.mu held.
func (m *Manager) startSetupLocked(wt *Worktree) {
	hooks, hooksErr := m.backend.creationHooks(m.repoRoot, m.baseRef, wt)
	if hooksErr != nil {
		errMsg := hooksErr.Error()
		log.Printf("[worktree] setup for %s not run: %s", wt.ID, errMsg)
		m.setupStatuses[wt.ID] = setupState{status: SetupFailed, err: errMsg}
		wt.Setup, wt.SetupError = SetupFailed, errMsg
		if m.onSetupDone != nil {
			go m.onSetupDone(wt.ID, false, errMsg)
		}
		return
	}
	if hooks == nil {
		hooks = &CreationHooks{}
	}
	if hooks.Before == nil && hooks.After == nil && m.setupCommand == "" {
		return
	}

	m.setupStatuses[wt.ID] = setupState{status: SetupRunning}
	wt.Setup = SetupRunning

	id, worktreePath := wt.ID, wt.Path
	go func() {
		out, err := m.runSetup(hooks, worktreePath)

		m.mu.Lock()
		if err != nil {
			log.Printf("[worktree] setup failed in %s: %s: %v", worktreePath, out, err)
			m.setupStatuses[id] = setupState{status: SetupFailed, err: out}
		} else {
			log.Printf("[worktree] setup completed in %s", worktreePath)
			m.setupStatuses[id] = setupState{status: SetupDone}
		}
		m.mu.Unlock()

		m.notify()

		if m.onSetupDone != nil {
			m.onSetupDone(id, err == nil, out)
		}
	}()
}

// runSetup runs the setup steps in order and stops at the first failure.
// It returns the failing step's output (or the error text) on failure.
func (m *Manager) runSetup(hooks *CreationHooks, worktreePath string) (string, error) {
	fail := func(out string, err error) (string, error) {
		if out == "" {
			out = err.Error()
		}
		var approvalErr *ApprovalError
		if errors.As(err, &approvalErr) {
			out = approvalErr.Error()
		}
		return out, err
	}
	if hooks.Before != nil {
		log.Printf("[worktree] running creation hooks (pre-start) in %s", worktreePath)
		if out, err := hooks.Before(); err != nil {
			return fail(out, err)
		}
	}
	if m.setupCommand != "" {
		log.Printf("[worktree] running setup command in %s: %s", worktreePath, m.setupCommand)
		setupCmd := exec.Command("sh", "-c", m.setupCommand)
		setupCmd.Dir = worktreePath
		if out, err := setupCmd.CombinedOutput(); err != nil {
			return fail(strings.TrimSpace(string(out)), err)
		}
	}
	if hooks.After != nil {
		log.Printf("[worktree] starting background creation hooks (post-start) in %s", worktreePath)
		if out, err := hooks.After(); err != nil {
			return fail(out, err)
		}
	}
	return "", nil
}

// worktreeMeta is the metadata stored alongside standalone worktrees.
type worktreeMeta struct {
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	Port         int        `json:"port,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	// Path is the worktree's location when the metadata was last written.
	// Used to keep IDs of detached worktrunk worktrees stable and to ignore
	// stale metadata left behind by a worktree removed outside beans.
	Path string `json:"path,omitempty"`
}

const metaSuffix = ".meta.json"

// metaPath returns the path to the metadata file for a worktree ID.
func (m *Manager) metaPath(id string) string {
	return filepath.Join(m.worktreeRoot, id+metaSuffix)
}

// loadMetaFor loads the metadata of the worktree with the given ID and path.
// With the worktrunk backend, metadata recorded for another path belongs to a
// worktree that was removed outside beans and is ignored.
func (m *Manager) loadMetaFor(id, path string) *worktreeMeta {
	meta := m.loadMeta(id)
	if meta != nil && m.backend.Kind() == BackendWorktrunk && meta.Path != "" && path != "" && !samePath(meta.Path, path) {
		return nil
	}
	return meta
}

// pathForID returns the remembered path of a worktree ID, or "".
func (m *Manager) pathForID(id string) string {
	m.idMu.Lock()
	defer m.idMu.Unlock()
	for path, pid := range m.pathIDs {
		if pid == id {
			return path
		}
	}
	return ""
}

// loadMeta loads the metadata for a worktree, if it exists.
func (m *Manager) loadMeta(id string) *worktreeMeta {
	data, err := os.ReadFile(m.metaPath(id))
	if err != nil {
		return nil
	}
	var meta worktreeMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}

// saveMeta saves metadata for a worktree, recording its current path.
func (m *Manager) saveMeta(id string, meta *worktreeMeta) error {
	if path := m.pathForID(id); path != "" {
		meta.Path = path
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(m.metaPath(id), data, 0644)
}

// removeMeta removes the metadata file for a worktree.
func (m *Manager) removeMeta(id string) {
	os.Remove(m.metaPath(id))
}

// SavePort persists the allocated port for a worktree into its metadata file.
func (m *Manager) SavePort(id string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.loadMetaFor(id, m.pathForID(id))
	if meta == nil {
		meta = &worktreeMeta{}
	}
	meta.Port = port
	return m.saveMeta(id, meta)
}

// GetPort returns the persisted port for a worktree, or 0 if none is stored.
func (m *Manager) GetPort(id string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if meta := m.loadMetaFor(id, m.pathForID(id)); meta != nil {
		return meta.Port
	}
	return 0
}

// TouchLastActive updates the LastActiveAt timestamp for a worktree to now
// and notifies subscribers. Called when an agent completes a turn.
func (m *Manager) TouchLastActive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.loadMetaFor(id, m.pathForID(id))
	if meta == nil {
		meta = &worktreeMeta{}
	}
	now := time.Now().UTC()
	meta.LastActiveAt = &now
	if err := m.saveMeta(id, meta); err != nil {
		return fmt.Errorf("save last_active_at: %w", err)
	}
	m.notify()
	return nil
}

// UpdateDescription updates the description for a worktree and notifies subscribers.
func (m *Manager) UpdateDescription(id, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.loadMetaFor(id, m.pathForID(id))
	if meta == nil {
		meta = &worktreeMeta{}
	}
	meta.Description = description
	if err := m.saveMeta(id, meta); err != nil {
		return fmt.Errorf("save description: %w", err)
	}
	m.notify()
	return nil
}

// Remove removes the worktree with the given ID, discarding uncommitted
// changes. The actual worktree is looked up from git (not computed), so this
// works even when the worktree was created from a different repo
// root/workspace or by another tool. With the worktrunk backend `wt remove`
// runs its pre/post-remove hooks and deletes the branch only if it is merged.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.findLocked(id)
	if err != nil {
		return fmt.Errorf("worktree %s not found: %w", id, err)
	}

	if err := m.backend.remove(m.repoRoot, wt); err != nil {
		log.Printf("[worktree] failed to remove worktree %s at %s: %v", id, wt.Path, err)
		return err
	}

	log.Printf("[worktree] removed worktree %s (path=%s)", id, wt.Path)
	m.removeMeta(id)
	delete(m.setupStatuses, id)
	m.idMu.Lock()
	delete(m.pathIDs, wt.Path)
	delete(m.seen, id)
	m.idMu.Unlock()
	m.notify()
	return nil
}
