package worktree

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// watchDebounce coalesces the bursts of file events of one git operation.
	watchDebounce = 250 * time.Millisecond
	// watchSettle re-runs the change callback once more after a burst:
	// `git worktree add` registers a worktree before checking out its files,
	// so its .beans/ directory may not exist yet at the first callback.
	watchSettle = 2 * time.Second
	// watchPollInterval is used when file watching is unavailable.
	watchPollInterval = 3 * time.Second
)

// GitCommonDir returns the absolute path of the repository's common git
// directory (the main worktree's .git), which holds the admin directories of
// all linked worktrees under worktrees/.
func GitCommonDir(repoRoot string) (string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WatchExternalChanges watches the repository for worktrees being added or
// removed, or switching branch, by anyone (beans, `wt switch -c`, `wt remove`,
// `git worktree add/remove`, ...). It watches <git-common-dir>/worktrees/ and
// each worktree's admin directory (for HEAD changes), falling back to polling
// if file watching is unavailable. On a change it calls onChange (if non-nil;
// e.g. to reconcile bean watchers and ports) and then notifies subscribers.
// Calls are debounced and never run concurrently. The returned function stops
// watching.
func (m *Manager) WatchExternalChanges(onChange func()) (stop func(), err error) {
	commonDir, err := GitCommonDir(m.repoRoot)
	if err != nil {
		return nil, err
	}

	w := &externalWatcher{
		commonDir:    commonDir,
		worktreesDir: filepath.Join(commonDir, "worktrees"),
		done:         make(chan struct{}),
		fire: func() {
			m.pruneRemoved()
			if onChange != nil {
				onChange()
			}
			m.notify()
		},
	}

	m.pruneRemoved() // record the current workspaces

	fsw, err := fsnotify.NewWatcher()
	if err == nil {
		err = fsw.Add(commonDir)
		if err != nil {
			fsw.Close()
		}
	}
	if err != nil {
		log.Printf("[worktree] file watching unavailable (%v); polling %s every %s", err, w.worktreesDir, watchPollInterval)
		go w.poll()
	} else {
		w.addWorktreeDirs(fsw)
		go w.loop(fsw)
	}

	var once sync.Once
	return func() { once.Do(func() { close(w.done) }) }, nil
}

type externalWatcher struct {
	commonDir    string
	worktreesDir string
	fire         func()
	done         chan struct{}

	mu       sync.Mutex
	debounce *time.Timer
	settle   *time.Timer
	fireMu   sync.Mutex // serializes fire()
}

// addWorktreeDirs watches worktrees/ and every worktree admin directory in it.
func (w *externalWatcher) addWorktreeDirs(fsw *fsnotify.Watcher) {
	if err := fsw.Add(w.worktreesDir); err != nil {
		return // no linked worktrees yet; worktrees/ is created with the first one
	}
	entries, _ := os.ReadDir(w.worktreesDir)
	for _, e := range entries {
		if e.IsDir() {
			_ = fsw.Add(filepath.Join(w.worktreesDir, e.Name()))
		}
	}
}

// relevant reports whether a file event can change the worktree list:
// worktrees/ itself, an admin directory being added/removed, or an admin
// directory's HEAD (branch switch, detach) or gitdir (worktree moved) file.
// Other files (index, logs, ...) change constantly and are ignored, so a git
// status in a worktree can't cause a refresh loop.
func (w *externalWatcher) relevant(name string) bool {
	if name == w.worktreesDir {
		return true
	}
	rel, err := filepath.Rel(w.worktreesDir, name)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	switch len(parts) {
	case 1:
		return true // admin directory added or removed
	case 2:
		return parts[1] == "HEAD" || parts[1] == "gitdir" || parts[1] == "locked"
	}
	return false
}

func (w *externalWatcher) loop(fsw *fsnotify.Watcher) {
	defer fsw.Close()
	defer w.stopTimers()
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-fsw.Events:
			if !ok {
				return
			}
			if !w.relevant(ev.Name) {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if ev.Name == w.worktreesDir {
					w.addWorktreeDirs(fsw)
				} else if filepath.Dir(ev.Name) == w.worktreesDir {
					_ = fsw.Add(ev.Name)
				}
			}
			w.schedule()
		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			log.Printf("[worktree] watch error: %v", err)
		}
	}
}

// schedule (re)starts the debounce timer and the settle timer.
func (w *externalWatcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(watchDebounce, w.run)
	if w.settle != nil {
		w.settle.Stop()
	}
	w.settle = time.AfterFunc(watchSettle, w.run)
}

func (w *externalWatcher) stopTimers() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	if w.settle != nil {
		w.settle.Stop()
	}
}

func (w *externalWatcher) run() {
	select {
	case <-w.done:
		return
	default:
	}
	w.fireMu.Lock()
	defer w.fireMu.Unlock()
	w.fire()
}

// poll checks worktrees/ periodically when file watching is unavailable.
func (w *externalWatcher) poll() {
	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()
	last := w.snapshot()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if cur := w.snapshot(); cur != last {
				last = cur
				w.run()
			}
		}
	}
}

// snapshot summarizes the admin directories and their HEADs.
func (w *externalWatcher) snapshot() string {
	entries, err := os.ReadDir(w.worktreesDir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		head, _ := os.ReadFile(filepath.Join(w.worktreesDir, e.Name(), "HEAD"))
		parts = append(parts, e.Name()+"="+strings.TrimSpace(string(head)))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
