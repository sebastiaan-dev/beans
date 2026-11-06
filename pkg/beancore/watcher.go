package beancore

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hmans/beans/pkg/bean"
)

const debounceDelay = 100 * time.Millisecond

// EventType represents the type of change that occurred to a bean.
type EventType int

const (
	// EventCreated indicates a new bean was created.
	EventCreated EventType = iota
	// EventUpdated indicates an existing bean was modified.
	EventUpdated
	// EventDeleted indicates a bean was deleted.
	EventDeleted
)

// String returns a human-readable representation of the event type.
func (e EventType) String() string {
	switch e {
	case EventCreated:
		return "created"
	case EventUpdated:
		return "updated"
	case EventDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// BeanEvent represents a change to a bean.
type BeanEvent struct {
	Type   EventType  // The type of change
	Bean   *bean.Bean // The bean (nil for Deleted events); a shared, read-only snapshot
	BeanID string     // Always set, useful for Deleted when Bean is nil
}

// subscription represents a subscriber to bean events.
type subscription struct {
	ch chan []BeanEvent
	id uint64
}

// Subscribe creates a new subscription to bean change events.
// Returns the event channel and an unsubscribe function.
// The channel receives batches of events after debouncing.
// Callers should use defer to call the unsubscribe function.
func (c *Core) Subscribe() (<-chan []BeanEvent, func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	id := atomic.AddUint64(&c.nextSubID, 1)
	ch := make(chan []BeanEvent, 16)

	sub := &subscription{ch: ch, id: id}
	c.subscribers[id] = sub

	unsubscribe := func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		if _, ok := c.subscribers[id]; ok {
			close(ch)
			delete(c.subscribers, id)
		}
	}

	return ch, unsubscribe
}

// fanOut sends events to all subscribers (non-blocking).
// Slow subscribers will have events dropped rather than blocking others.
func (c *Core) fanOut(events []BeanEvent) {
	if len(events) == 0 {
		return
	}

	c.subMu.RLock()
	defer c.subMu.RUnlock()

	for _, sub := range c.subscribers {
		select {
		case sub.ch <- events:
			// Sent successfully
		default:
			// Subscriber is slow, drop events
			c.logWarn("subscriber %d: event channel full, dropping %d events", sub.id, len(events))
		}
	}
}

// StartWatching begins filesystem monitoring.
// Use Subscribe() to receive bean change events via a channel.
// This is the preferred API for new code; Watch() is kept for backward compatibility.
func (c *Core) StartWatching() error {
	return c.Watch(nil)
}

// Watch starts watching the .beans directory for changes.
// The onChange callback is invoked (after debouncing) whenever beans are created, modified, or deleted.
// The internal state is automatically reloaded before the callback is invoked.
// Deprecated: Use StartWatching() + Subscribe() for new code.
func (c *Core) Watch(onChange func()) error {
	c.mu.Lock()
	if c.watching {
		c.mu.Unlock()
		return nil // Already watching
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.mu.Unlock()
		return err
	}

	if err := watcher.Add(c.root); err != nil {
		watcher.Close()
		c.mu.Unlock()
		return err
	}

	// Watch all subdirectories (best effort - don't fail if any can't be watched)
	// Skip dot-prefixed subdirectories (e.g. .worktrees/, .conversations/)
	_ = filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == c.root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		_ = watcher.Add(path)
		return nil
	})

	c.watching = true
	c.done = make(chan struct{})
	c.onChange = onChange
	c.mu.Unlock()

	// Start the watcher goroutine
	go c.watchLoop(watcher)

	return nil
}

// Unwatch stops watching the .beans directory.
func (c *Core) Unwatch() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.unwatchLocked()
}

// unwatchLocked stops watching (must be called with lock held).
func (c *Core) unwatchLocked() error {
	if !c.watching {
		return nil
	}

	close(c.done)
	c.watching = false
	c.onChange = nil

	// Close all subscriber channels
	c.subMu.Lock()
	for id, sub := range c.subscribers {
		close(sub.ch)
		delete(c.subscribers, id)
	}
	c.subMu.Unlock()

	return nil
}

// watchLoop processes filesystem events with debouncing.
func (c *Core) watchLoop(watcher *fsnotify.Watcher) {
	defer watcher.Close()

	var debounceTimer *time.Timer
	var pendingMu sync.Mutex
	pendingChanges := make(map[string]fsnotify.Op)

	for {
		select {
		case <-c.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// New subdirectories (e.g. archive/, created by the first archive)
			// must be watched too; files moved into them before the watch was
			// added are picked up by scanning the directory.
			if event.Op&fsnotify.Create != 0 {
				if added := c.watchNewDir(watcher, event.Name); len(added) > 0 {
					pendingMu.Lock()
					for _, f := range added {
						pendingChanges[f] |= fsnotify.Create
					}
					pendingMu.Unlock()
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(debounceDelay, func() {
						pendingMu.Lock()
						changes := pendingChanges
						pendingChanges = make(map[string]fsnotify.Op)
						pendingMu.Unlock()
						c.handleChanges(changes)
					})
					continue
				}
			}

			// Only care about .md files within the .beans directory tree
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}

			// Verify the file is within the .beans directory
			relPath, err := filepath.Rel(c.root, event.Name)
			if err != nil || strings.HasPrefix(relPath, "..") {
				continue
			}

			// Skip events from dot-prefixed subdirectories (e.g. .worktrees/, .conversations/)
			if topDir, _, ok := strings.Cut(relPath, string(filepath.Separator)); ok && strings.HasPrefix(topDir, ".") {
				continue
			}

			// Check if this is a relevant event
			relevant := event.Op&fsnotify.Create != 0 ||
				event.Op&fsnotify.Write != 0 ||
				event.Op&fsnotify.Remove != 0 ||
				event.Op&fsnotify.Rename != 0

			if !relevant {
				continue
			}

			// Accumulate changes during debounce window
			pendingMu.Lock()
			pendingChanges[event.Name] |= event.Op
			pendingMu.Unlock()

			// Start/reset debounce timer
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				// Swap out pending changes atomically
				pendingMu.Lock()
				changes := pendingChanges
				pendingChanges = make(map[string]fsnotify.Op)
				pendingMu.Unlock()

				c.handleChanges(changes)
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Log errors but continue watching
			_ = err // In production, you might want to log this
		}
	}
}

// handleChanges processes only the files that changed, updating state incrementally.
func (c *Core) handleChanges(changes map[string]fsnotify.Op) {
	if len(changes) == 0 {
		return
	}

	c.mu.Lock()

	// Check if we're still watching
	if !c.watching {
		c.mu.Unlock()
		return
	}

	var events []BeanEvent

	// Handle creates/writes before removes, so that a bean moved between
	// directories (e.g. archived) is re-pointed at its new file first and the
	// remove of the old path is recognized as stale below.
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		ri := changes[paths[i]]&(fsnotify.Remove|fsnotify.Rename) != 0 && !c.fileExists(paths[i])
		rj := changes[paths[j]]&(fsnotify.Remove|fsnotify.Rename) != 0 && !c.fileExists(paths[j])
		if ri != rj {
			return !ri
		}
		return paths[i] < paths[j]
	})

	for _, path := range paths {
		op := changes[path]
		filename := filepath.Base(path)
		id, _ := bean.ParseFilename(filename)

		// Handle removes/renames (file is gone)
		if op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0 {
			if stored, exists := c.beans[id]; exists {
				// Only delete if the removed file is the bean's file (a moved
				// bean, e.g. archived, lives on at its new path) and it is
				// actually gone.
				rel, relErr := filepath.Rel(c.root, path)
				if relErr == nil && stored.Path != "" && filepath.Clean(stored.Path) != filepath.Clean(rel) {
					continue
				}
				if !c.fileExists(path) {
					// A linked bean keeps showing its worktree change.
					if _, linked := c.worktreeLinks[id]; linked {
						if ev, ok := c.resolveWorktreeBeanLocked(id); ok {
							events = append(events, ev)
						}
						continue
					}

					c.deleteBeanLocked(id)

					// Update search index
					if c.searchIndex != nil {
						if err := c.searchIndex.DeleteBean(id); err != nil {
							c.logWarn("failed to remove bean %s from search index: %v", id, err)
						}
					}

					events = append(events, BeanEvent{
						Type:   EventDeleted,
						Bean:   nil,
						BeanID: id,
					})

					// File is truly gone, skip Write/Create handling
					continue
				}
			}
			// File still exists (e.g., Remove+Write in same batch), fall through
			// to Write/Create handler below
		}

		// Handle creates/writes (file exists or was created)
		if op&fsnotify.Create != 0 || op&fsnotify.Write != 0 {
			newBean, err := c.loadBean(path)
			if err != nil {
				c.logWarn("failed to load bean from %s: %v", path, err)
				continue
			}

			_, existed := c.beans[newBean.ID]

			// A bean linked to a worktree is shown in its worktree version. Once
			// main has caught up with that version (e.g. the worktree branch was
			// merged), the link is stale: resolving drops it so edits go to main
			// again instead of being routed into the worktree. If a worktree
			// still has its own changes on top, those keep being shown, like the
			// initial worktree scan does.
			if _, linked := c.worktreeLinks[newBean.ID]; linked {
				if ev, ok := c.resolveWorktreeBeanLocked(newBean.ID); ok {
					events = append(events, ev)
				}
				continue
			}

			c.setBeanLocked(newBean)
			delete(c.dirty, newBean.ID) // Disk is now up-to-date

			// Update search index
			if c.searchIndex != nil {
				if err := c.searchIndex.IndexBean(newBean); err != nil {
					c.logWarn("failed to index bean %s: %v", newBean.ID, err)
				}
			}

			if existed {
				events = append(events, BeanEvent{
					Type:   EventUpdated,
					Bean:   newBean,
					BeanID: newBean.ID,
				})
			} else {
				events = append(events, BeanEvent{
					Type:   EventCreated,
					Bean:   newBean,
					BeanID: newBean.ID,
				})
			}
		}
	}

	callback := c.onChange
	c.mu.Unlock()

	// Fan out to subscribers (outside lock)
	c.fanOut(events)

	// Invoke legacy callback
	if callback != nil {
		callback()
	}
}

// fileExists checks if a file exists at the given path.
// watchNewDir adds a newly created bean subdirectory to the watcher and
// returns the .md files already in it. It returns nil for anything that is
// not a watchable bean subdirectory.
func (c *Core) watchNewDir(watcher *fsnotify.Watcher, path string) []string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil
	}
	rel, err := filepath.Rel(c.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	if top, _, _ := strings.Cut(rel, string(filepath.Separator)); strings.HasPrefix(top, ".") {
		return nil
	}
	if err := watcher.Add(path); err != nil {
		c.logWarn("failed to watch %s: %v", path, err)
		return nil
	}
	var files []string
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				_ = watcher.Add(p)
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			files = append(files, p)
		}
		return nil
	})
	// An empty new directory still counts as handled (nothing to load).
	if files == nil {
		files = []string{}
	}
	return files
}

func (c *Core) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
