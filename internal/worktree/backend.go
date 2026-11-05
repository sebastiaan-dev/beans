package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BackendKind identifies a worktree backend (the worktree.backend config value).
type BackendKind string

const (
	// BackendGit manages worktrees with plain git (the default).
	BackendGit BackendKind = "git"
	// BackendWorktrunk manages worktrees with worktrunk (`wt`).
	BackendWorktrunk BackendKind = "worktrunk"
)

// WorktrunkBinary is the name of the worktrunk executable looked up on PATH.
const WorktrunkBinary = "wt"

// Backend creates, lists and removes the git worktrees that beans shows as
// workspaces. Listing is shared: both backends read `git worktree list
// --porcelain` and the backend decides which entries are workspaces and which
// stable IDs they get. Metadata, setup status and notifications are handled by
// Manager on top of the backend.
type Backend interface {
	Kind() BackendKind

	// workspaces maps the repository's worktrees (main worktree first) to
	// workspaces with IDs. worktreeRoot is beans' worktree/metadata directory;
	// idForPath returns a previously seen ID for a worktree path, or "".
	workspaces(entries []Entry, worktreeRoot string, idForPath func(path string) string) []Worktree

	// create creates a worktree with a new branch for the workspace name,
	// starting at baseRef, and returns its branch and path.
	create(req createRequest) (branch, path string, err error)

	// creationHooks returns the backend's hooks to run after a worktree was
	// created (nil when there are none). An error means the hooks can't run
	// (e.g. they still need the user's approval); it is reported as a failed
	// setup.
	creationHooks(repoRoot, baseRef string, wt *Worktree) (*CreationHooks, error)

	// remove removes the worktree. Uncommitted changes are discarded, like
	// `git worktree remove --force`.
	remove(repoRoot string, wt *Worktree) error
}

// createRequest describes a new workspace worktree.
type createRequest struct {
	repoRoot     string
	worktreeRoot string
	name         string
	baseRef      string
}

// CreationHooks are backend hooks that run in a new worktree. Before blocks
// (it runs before beans' own worktree.setup command); After runs once Before
// and worktree.setup succeeded and may leave work running in the background.
// Each returns the command output, used in setup error messages.
type CreationHooks struct {
	Before func() (string, error)
	After  func() (string, error)
}

// ParseBackendKind validates a worktree.backend config value. The empty string
// selects the git backend.
func ParseBackendKind(s string) (BackendKind, error) {
	switch BackendKind(s) {
	case "", BackendGit:
		return BackendGit, nil
	case BackendWorktrunk:
		return BackendWorktrunk, nil
	default:
		return "", fmt.Errorf("unknown worktree backend %q (use %q or %q)", s, BackendGit, BackendWorktrunk)
	}
}

// NewBackend returns the backend for a worktree.backend config value. The
// worktrunk backend requires the wt executable on PATH.
func NewBackend(kind string) (Backend, error) {
	k, err := ParseBackendKind(kind)
	if err != nil {
		return nil, err
	}
	switch k {
	case BackendWorktrunk:
		bin, err := LookupWorktrunk()
		if err != nil {
			return nil, fmt.Errorf("worktree.backend is %q but %w", BackendWorktrunk, err)
		}
		return NewWorktrunkBackend(bin), nil
	default:
		return gitBackend{}, nil
	}
}

// LookupWorktrunk returns the path of the worktrunk executable on PATH.
func LookupWorktrunk() (string, error) {
	path, err := exec.LookPath(WorktrunkBinary)
	if err != nil {
		return "", fmt.Errorf("worktrunk (%s) was not found on PATH", WorktrunkBinary)
	}
	return path, nil
}

// WorktrunkAvailable reports whether the worktrunk executable is on PATH.
func WorktrunkAvailable() bool {
	_, err := LookupWorktrunk()
	return err == nil
}

// commandEnv returns the environment for backend subprocesses: the server's
// environment without worktrunk shell-integration directives, so wt never
// tries to change the directory of a shell that started beans-serve.
func commandEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "WORKTRUNK_DIRECTIVE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
