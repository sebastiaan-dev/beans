package worktree

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"time"
)

// TerminalIO holds the standard streams of an interactive terminal command
// (`beans start --worktree`). Backend commands that may prompt the user (e.g.
// worktrunk's approval prompt) or print progress use them directly.
type TerminalIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Open returns the workspace with the given name for use from the user's
// terminal, creating it when it doesn't exist yet. created reports whether a
// new worktree was created.
//
// With the git backend the workspace is created exactly like beans-serve does
// (Create: branch beans/<name> under the worktree root) and Open waits for
// the worktree.setup command. An existing workspace with the ID <name> is
// reused.
//
// With the worktrunk backend Open runs the user's normal `wt switch` for the
// branch <name> (with --create unless the branch exists) with the terminal's
// streams, so all of worktrunk's hooks run, including the switch hooks
// beans-serve skips (this is the user's own terminal workflow), and wt can
// prompt for approval of project hooks. --no-cd is passed because a child
// process can't change the calling shell's directory. An existing worktree of
// the branch is reused (`wt switch` switches to it and runs the switch hooks).
// For a new worktree, beans' worktree.setup command runs afterwards.
func (m *Manager) Open(name string, tio TerminalIO) (wt *Worktree, created bool, err error) {
	if err := ValidateName(name); err != nil {
		return nil, false, err
	}
	if m.backend.Kind() == BackendWorktrunk {
		return m.openWorktrunk(name, tio)
	}
	return m.openGit(name, tio)
}

func (m *Manager) openGit(name string, tio TerminalIO) (*Worktree, bool, error) {
	if existing, err := m.find(func(wt Worktree) bool { return wt.ID == name }); err != nil {
		return nil, false, err
	} else if existing != nil {
		return existing, false, nil
	}

	// Create runs the setup asynchronously (beans-serve reports it via the
	// setup status); a terminal command waits for it instead.
	type setupResult struct {
		success bool
		errMsg  string
	}
	done := make(chan setupResult, 1)
	prev := m.onSetupDone
	m.onSetupDone = func(id string, success bool, errMsg string) {
		if prev != nil {
			prev(id, success, errMsg)
		}
		if id == name {
			done <- setupResult{success, errMsg}
		}
	}
	defer func() { m.onSetupDone = prev }()

	if m.setupCommand != "" {
		fmt.Fprintf(tio.Stderr, "Running worktree.setup: %s\n", m.setupCommand)
	}
	wt, err := m.Create(name)
	if err != nil {
		return nil, false, err
	}
	if wt.Setup == SetupRunning || wt.Setup == SetupFailed {
		if res := <-done; !res.success {
			return wt, true, fmt.Errorf("worktree %s was created at %s, but its setup failed: %s", name, wt.Path, res.errMsg)
		}
		wt.Setup = SetupDone
	}
	return wt, true, nil
}

func (m *Manager) openWorktrunk(name string, tio TerminalIO) (*Worktree, bool, error) {
	wb, ok := m.backend.(*worktrunkBackend)
	if !ok {
		return nil, false, fmt.Errorf("worktrunk backend unavailable")
	}
	byBranch := func(wt Worktree) bool { return wt.Branch == name }
	existing, err := m.find(byBranch)
	if err != nil {
		return nil, false, err
	}

	args := []string{"-C", m.repoRoot, "switch"}
	if !m.branchExists(name) {
		// Branch from the latest base ref, like Create.
		m.fetchBaseRef()
		args = append(args, "--create")
		if m.baseRef != "" {
			args = append(args, "--base", m.baseRef)
		}
	}
	args = append(args, name, "--no-cd")
	cmd := exec.Command(wb.bin, args...)
	cmd.Env = commandEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tio.Stdin, tio.Stdout, tio.Stderr
	if err := cmd.Run(); err != nil {
		return nil, false, fmt.Errorf("wt switch %s failed: %w", name, err)
	}

	wt, err := m.find(byBranch)
	if err != nil {
		return nil, false, err
	}
	if wt == nil {
		return nil, false, fmt.Errorf("wt switch %s succeeded, but git lists no worktree for branch %q", name, name)
	}
	if existing != nil {
		return wt, false, nil
	}

	// Record the workspace like Create does, so beans-serve names it and
	// sorts it to the top.
	m.mu.Lock()
	m.idMu.Lock()
	m.pathIDs[wt.Path] = wt.ID
	m.idMu.Unlock()
	now := time.Now().UTC()
	if err := m.saveMeta(wt.ID, &worktreeMeta{Name: name, LastActiveAt: &now}); err != nil {
		log.Printf("[worktree] warning: failed to save metadata for %s: %v", wt.ID, err)
	}
	m.mu.Unlock()
	wt.Name = name

	if m.setupCommand != "" {
		fmt.Fprintf(tio.Stderr, "Running worktree.setup: %s\n", m.setupCommand)
		setup := exec.Command("sh", "-c", m.setupCommand)
		setup.Dir = wt.Path
		setup.Stdin, setup.Stdout, setup.Stderr = tio.Stdin, tio.Stdout, tio.Stderr
		if err := setup.Run(); err != nil {
			return wt, true, fmt.Errorf("worktree %s was created at %s, but worktree.setup failed: %w", name, wt.Path, err)
		}
	}
	return wt, true, nil
}

// find returns the first workspace matching match, or nil.
func (m *Manager) find(match func(Worktree) bool) (*Worktree, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	worktrees, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	for i := range worktrees {
		if match(worktrees[i]) {
			return &worktrees[i], nil
		}
	}
	return nil, nil
}

// branchExists reports whether the repository has a local branch.
func (m *Manager) branchExists(branch string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = m.repoRoot
	return cmd.Run() == nil
}
