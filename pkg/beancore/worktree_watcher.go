package beancore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hmans/beans/pkg/bean"
)

// worktreeWatcher tracks a single worktree's .beans/ directory.
type worktreeWatcher struct {
	worktreePath string // root of the worktree (e.g., ~/.beans/worktrees/<project>/<id>)
	beansDir     string // .beans/ dir inside the worktree
	done         chan struct{}
}

// WatchWorktreeBeans starts watching a worktree's .beans/ directory for bean changes.
// When beans change in the worktree, they are merged into the runtime state as dirty
// (not persisted to the main repo's disk).
// Returns nil if the worktree's .beans/ directory doesn't exist.
func (c *Core) WatchWorktreeBeans(worktreePath string) error {
	beansDir := filepath.Join(worktreePath, BeansDir)

	// Check if the worktree has a .beans/ directory
	if _, err := os.Stat(beansDir); os.IsNotExist(err) {
		return nil // No .beans/ dir in this worktree, nothing to watch
	}

	c.mu.Lock()
	// Check if already watching this worktree
	if c.worktreeWatchers == nil {
		c.worktreeWatchers = make(map[string]*worktreeWatcher)
	}
	if _, exists := c.worktreeWatchers[worktreePath]; exists {
		c.mu.Unlock()
		return nil // Already watching
	}

	wt := &worktreeWatcher{
		worktreePath: worktreePath,
		beansDir:     beansDir,
		done:         make(chan struct{}),
	}
	c.worktreeWatchers[worktreePath] = wt
	c.mu.Unlock()

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.mu.Lock()
		delete(c.worktreeWatchers, worktreePath)
		c.mu.Unlock()
		return err
	}

	if err := watcher.Add(beansDir); err != nil {
		watcher.Close()
		c.mu.Lock()
		delete(c.worktreeWatchers, worktreePath)
		c.mu.Unlock()
		return err
	}

	// Also watch subdirectories (like archive/)
	_ = filepath.WalkDir(beansDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == beansDir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		_ = watcher.Add(path)
		return nil
	})

	// Initial scan: load existing bean files from the worktree into runtime state.
	// Without this, beans created in worktrees won't be resolvable via Get()
	// until the watcher picks up a change event.
	c.loadWorktreeBeansInitial(wt)

	// Start the watcher goroutine
	go c.worktreeWatchLoop(wt, watcher)

	c.logWarn("watching worktree beans: %s", beansDir)
	return nil
}

// loadWorktreeBeansInitial scans a worktree's .beans/ directory and loads
// bean files that differ from the main version into runtime state.
// Only beans that are new or modified compared to main are loaded and linked.
func (c *Core) loadWorktreeBeansInitial(wt *worktreeWatcher) {
	entries, err := os.ReadDir(wt.beansDir)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(wt.beansDir, entry.Name())
		newBean, err := c.loadBeanFrom(path, wt.beansDir)
		if err != nil {
			continue
		}

		// Only load and link if the bean differs from the main version.
		// Worktrees branch from main, so most beans are identical copies.
		if existing, exists := c.beans[newBean.ID]; exists {
			if existing.ETag() == newBean.ETag() {
				continue // Same content as main — skip
			}
		}

		c.beans[newBean.ID] = newBean
		c.dirty[newBean.ID] = true
		c.worktreeLinks[newBean.ID] = wt.worktreePath

		if c.searchIndex != nil {
			_ = c.searchIndex.IndexBean(newBean)
		}
	}
}

// UnwatchWorktreeBeans stops watching a worktree's .beans/ directory.
func (c *Core) UnwatchWorktreeBeans(worktreePath string) {
	c.mu.Lock()
	wt, exists := c.worktreeWatchers[worktreePath]
	if exists {
		delete(c.worktreeWatchers, worktreePath)
		// Clear all worktree links for this worktree
		for id, wtp := range c.worktreeLinks {
			if wtp == worktreePath {
				delete(c.worktreeLinks, id)
			}
		}
	}
	c.mu.Unlock()

	if exists {
		close(wt.done)
		c.logWarn("stopped watching worktree beans: %s", wt.beansDir)
	}
}

// UnwatchAllWorktrees stops watching all worktree .beans/ directories.
func (c *Core) UnwatchAllWorktrees() {
	c.mu.Lock()
	watchers := c.worktreeWatchers
	c.worktreeWatchers = make(map[string]*worktreeWatcher)
	c.worktreeLinks = make(map[string]string)
	c.mu.Unlock()

	for _, wt := range watchers {
		close(wt.done)
	}
}

// worktreeWatchLoop processes filesystem events from a worktree with debouncing.
func (c *Core) worktreeWatchLoop(wt *worktreeWatcher, watcher *fsnotify.Watcher) {
	defer watcher.Close()

	var debounceTimer *time.Timer
	var pendingMu sync.Mutex
	pendingChanges := make(map[string]fsnotify.Op)

	for {
		select {
		case <-wt.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only care about .md files
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}

			// Verify the file is within the worktree's .beans/ directory
			relPath, err := filepath.Rel(wt.beansDir, event.Name)
			if err != nil || strings.HasPrefix(relPath, "..") {
				continue
			}

			// Skip events from dot-prefixed subdirectories
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

			pendingMu.Lock()
			pendingChanges[event.Name] |= event.Op
			pendingMu.Unlock()

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				pendingMu.Lock()
				changes := pendingChanges
				pendingChanges = make(map[string]fsnotify.Op)
				pendingMu.Unlock()

				c.handleWorktreeChanges(wt, changes)
			})

		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// handleWorktreeChanges processes bean file changes from a worktree.
// Creates/writes are merged into runtime state as dirty.
// Deletes either revert to the main-repo version or remove the bean from runtime.
func (c *Core) handleWorktreeChanges(wt *worktreeWatcher, changes map[string]fsnotify.Op) {
	if len(changes) == 0 {
		return
	}

	c.mu.Lock()

	var events []BeanEvent

	for path, op := range changes {
		isDelete := op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0
		isCreateOrWrite := op&fsnotify.Create != 0 || op&fsnotify.Write != 0

		if isDelete && !isCreateOrWrite {
			filename := filepath.Base(path)
			id, _ := bean.ParseFilename(filename)
			if id == "" {
				continue
			}

			// The bean was moved or renamed within the worktree (e.g. into
			// archive/, or a stale copy was replaced): the worktree still has
			// it, so merge the remaining copy instead of dropping the bean.
			if remaining := findBeanFilesIn(wt.beansDir, id); len(remaining) > 0 {
				path = remaining[0]
				isCreateOrWrite = true
			}
		}

		if isDelete && !isCreateOrWrite {
			// File was deleted in the worktree. Either revert to the
			// main-repo version or remove the bean from runtime.
			id, _ := bean.ParseFilename(filepath.Base(path))

			// Try to load from the main .beans/ directory.
			// The filename may differ (different slug), so search by ID.
			mainPath := c.findMainBeanFile(id)
			if mainPath != "" {
				mainBean, err := c.loadBeanFrom(mainPath, c.root)
				if err != nil {
					continue
				}
				// Bean exists in main — revert to that version
				c.beans[id] = mainBean
				delete(c.dirty, id)
				delete(c.worktreeLinks, id)

				if c.searchIndex != nil {
					_ = c.searchIndex.IndexBean(mainBean)
				}

				events = append(events, BeanEvent{
					Type:   EventUpdated,
					Bean:   mainBean,
					BeanID: id,
				})
			} else if _, existed := c.beans[id]; existed {
				// Bean was worktree-only — remove from runtime
				delete(c.beans, id)
				delete(c.dirty, id)
				delete(c.worktreeLinks, id)

				if c.searchIndex != nil {
					_ = c.searchIndex.DeleteBean(id)
				}

				events = append(events, BeanEvent{
					Type:   EventDeleted,
					BeanID: id,
				})
			}
			continue
		}

		if !isCreateOrWrite {
			continue
		}

		// Check the file still exists (handles rapid create+delete)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		newBean, err := c.loadBeanFrom(path, wt.beansDir)
		if err != nil {
			c.logWarn("failed to load worktree bean from %s: %v", path, err)
			continue
		}

		// Compare against the main repo's version on disk.
		// If they match (e.g. after a rebase), don't link — this bean
		// isn't actually modified in the worktree.
		mainPath := c.findMainBeanFile(newBean.ID)
		if mainPath != "" {
			mainBean, mainErr := c.loadBeanFrom(mainPath, c.root)
			if mainErr == nil && mainBean.ETag() == newBean.ETag() {
				// Worktree version matches main — clear any stale link
				if _, wasLinked := c.worktreeLinks[newBean.ID]; wasLinked {
					c.beans[newBean.ID] = mainBean
					delete(c.dirty, newBean.ID)
					delete(c.worktreeLinks, newBean.ID)
					if c.searchIndex != nil {
						_ = c.searchIndex.IndexBean(mainBean)
					}
					events = append(events, BeanEvent{
						Type:   EventUpdated,
						Bean:   mainBean,
						BeanID: newBean.ID,
					})
				}
				continue
			}
		}

		_, existed := c.beans[newBean.ID]
		c.beans[newBean.ID] = newBean
		c.dirty[newBean.ID] = true // Mark as dirty — came from worktree, not persisted to main
		c.worktreeLinks[newBean.ID] = wt.worktreePath

		// Update search index
		if c.searchIndex != nil {
			if err := c.searchIndex.IndexBean(newBean); err != nil {
				c.logWarn("failed to index worktree bean %s: %v", newBean.ID, err)
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

	c.mu.Unlock()

	// Fan out events to subscribers
	c.fanOut(events)

	// Notify worktree manager so the worktree subscription re-emits
	// with updated detected bean IDs
	if len(events) > 0 && c.onWorktreeBeansChanged != nil {
		c.onWorktreeBeansChanged()
	}
}

// WriteBeanToWorktree writes the runtime version of a bean into a worktree's
// .beans/ directory, so that an agent started in that worktree sees the bean as
// beans-serve sees it. New worktrees are created from the configured base ref,
// which doesn't include uncommitted edits to the main working copy (e.g. a
// description edited in Beans UI just before Start Work), runtime-only changes,
// or beans that were never committed at all.
//
// The file becomes a change on the worktree's branch and travels with its PR,
// like any other bean change made in the worktree. If the worktree already has
// identical content, nothing is written, so no spurious change is introduced.
// Copies of the bean under a different filename (e.g. a stale slug, or the
// other side of archive/) are removed so the worktree never holds two files
// with the same ID.
//
// Runtime state is not modified: the worktree copy equals the runtime version,
// so it is neither dirty nor linked until the worktree changes it.
// Returns whether the worktree's .beans/ directory was changed.
func (c *Core) WriteBeanToWorktree(id, worktreePath string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	b, _, err := c.findBeanLocked(id)
	if err != nil {
		return false, err
	}

	beansDir := filepath.Join(worktreePath, BeansDir)
	relPath := b.Path
	if relPath == "" {
		relPath = bean.BuildFilename(b.ID, b.Slug)
	}

	// Leave an identical copy alone, so no spurious change is introduced.
	if wtBean, err := c.loadBeanFrom(filepath.Join(beansDir, relPath), beansDir); err == nil && wtBean.ETag() == b.ETag() {
		return removeStaleBeanCopies(beansDir, b.ID, filepath.Join(beansDir, relPath))
	}

	content, err := b.Render()
	if err != nil {
		return false, err
	}
	if _, err := writeWorktreeBeanFile(beansDir, b.ID, relPath, content); err != nil {
		return true, err
	}
	return true, nil
}

// writeWorktreeBeanFile writes a bean file at relPath (relative to beansDir)
// and removes any other copies of the bean (e.g. a stale slug, or the other
// side of archive/), so the directory never holds two files with the same ID.
// Returns whether stale copies were removed.
func writeWorktreeBeanFile(beansDir, id, relPath string, content []byte) (bool, error) {
	target := filepath.Join(beansDir, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return false, fmt.Errorf("creating worktree beans dir: %w", err)
	}
	if err := os.WriteFile(target, content, 0644); err != nil {
		return false, fmt.Errorf("writing worktree bean: %w", err)
	}
	return removeStaleBeanCopies(beansDir, id, target)
}

// removeStaleBeanCopies removes all files for the bean in beansDir (including
// archive/) except keep. Returns whether any file was removed.
func removeStaleBeanCopies(beansDir, id, keep string) (bool, error) {
	removed := false
	for _, path := range findBeanFilesIn(beansDir, id) {
		if path == keep {
			continue
		}
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("removing stale worktree copy of %s: %w", id, err)
		}
		removed = true
	}
	return removed, nil
}

// findBeanFilesIn returns the paths of all bean files for the given ID in a
// .beans/ directory, including its archive/ subdirectory.
func findBeanFilesIn(beansDir, id string) []string {
	var paths []string
	for _, dir := range []string{beansDir, filepath.Join(beansDir, ArchiveDir)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if fileID, _ := bean.ParseFilename(entry.Name()); fileID == id {
				paths = append(paths, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return paths
}

// loadWorktreeCopyLocked loads a worktree's copy of a bean, or returns nil if
// the worktree has none (or it can't be parsed). Must be called with c.mu held.
func (c *Core) loadWorktreeCopyLocked(worktreePath, id string) *bean.Bean {
	beansDir := filepath.Join(worktreePath, BeansDir)
	for _, path := range findBeanFilesIn(beansDir, id) {
		if b, err := c.loadBeanFrom(path, beansDir); err == nil {
			return b
		}
	}
	return nil
}

// findMainBeanFile searches the main .beans/ directory for a file matching the
// given bean ID. Returns the full path or empty string if not found.
// Must be called with c.mu held.
func (c *Core) findMainBeanFile(id string) string {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		fileID, _ := bean.ParseFilename(entry.Name())
		if fileID == id {
			return filepath.Join(c.root, entry.Name())
		}
	}
	return ""
}

// loadBeanFrom reads and parses a bean file, calculating its relative path from the given root.
func (c *Core) loadBeanFrom(path string, root string) (*bean.Bean, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := bean.Parse(f)
	if err != nil {
		return nil, err
	}

	// Set metadata from path (relative to the root, not the worktree)
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	b.Path = relPath

	// Extract ID and slug from filename
	filename := filepath.Base(path)
	b.ID, b.Slug = bean.ParseFilename(filename)

	// Apply defaults
	if b.Type == "" {
		b.Type = "task"
	}
	if b.Priority == "" {
		b.Priority = "normal"
	}
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.Blocking == nil {
		b.Blocking = []string{}
	}
	if b.CreatedAt == nil {
		if b.UpdatedAt != nil {
			b.CreatedAt = b.UpdatedAt
		} else {
			info, statErr := os.Stat(path)
			if statErr == nil {
				modTime := info.ModTime().UTC().Truncate(time.Second)
				b.CreatedAt = &modTime
			}
		}
	}
	if b.UpdatedAt == nil {
		b.UpdatedAt = b.CreatedAt
	}

	return b, nil
}
