package beancore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

// worktreeWatcher tracks a single worktree's .beans/ directory.
type worktreeWatcher struct {
	worktreePath string // root of the worktree (e.g., ~/.beans/worktrees/<project>/<id>)
	beansDir     string // .beans/ dir inside the worktree
	done         chan struct{}

	// refreshMu serializes the initial scan and the processing of event
	// batches, so changed-bean sets are applied in the order they were computed.
	refreshMu sync.Mutex

	// changed holds the IDs of the beans the worktree changed relative to its
	// base (see worktreeChangedBeanIDs), recomputed whenever the worktree's
	// .beans/ directory changes. Only these beans may override runtime state.
	// changedKnown is false if the set couldn't be determined (not a git
	// worktree, or no merge-base with the base ref); every copy that differs
	// from the main version counts as a change then. Guarded by Core.mu.
	changed      map[string]bool
	changedKnown bool
	// scanned is set once the initial scan determined the changed set;
	// before that, the worktree's copies don't count as changes. Guarded by
	// Core.mu.
	scanned bool
}

// mayChange reports whether the worktree's copy of a bean may count as a
// worktree change. Must be called with Core.mu held.
func (wt *worktreeWatcher) mayChange(id string) bool {
	return wt.scanned && (!wt.changedKnown || wt.changed[id])
}

// candidateIDs returns the IDs of the beans whose worktree copy may count as
// a worktree change. Must be called with Core.mu held.
func (wt *worktreeWatcher) candidateIDs() map[string]bool {
	if !wt.scanned {
		return nil
	}
	if wt.changedKnown {
		return wt.changed
	}
	return beanIDsIn(wt.beansDir)
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

	// Initial scan: load the worktree's bean changes into runtime state.
	// Without this, beans created in worktrees won't be resolvable via Get()
	// until the watcher picks up a change event.
	c.fanOut(c.loadWorktreeBeansInitial(wt))

	// Start the watcher goroutine
	go c.worktreeWatchLoop(wt, watcher)

	c.logWarn("watching worktree beans: %s", beansDir)
	return nil
}

// loadWorktreeBeansInitial merges the beans a worktree changed relative to its
// base into runtime state and returns the resulting events. A worktree created
// from the base ref holds the committed version of every bean; those untouched
// copies must not override main's uncommitted edits or another worktree's
// change, so only beans in the worktree's changed set are considered.
func (c *Core) loadWorktreeBeansInitial(wt *worktreeWatcher) []BeanEvent {
	wt.refreshMu.Lock()
	defer wt.refreshMu.Unlock()

	changed, known := worktreeChangedBeanIDs(wt.worktreePath, c.worktreeBaseRef())
	ids := changed
	if !known {
		ids = beanIDsIn(wt.beansDir)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.worktreeWatchers[wt.worktreePath] != wt {
		return nil // unwatched meanwhile
	}
	wt.changed, wt.changedKnown, wt.scanned = changed, known, true

	var events []BeanEvent
	for _, id := range sortedIDs(ids) {
		if ev, ok := c.resolveWorktreeBeanLocked(id); ok {
			events = append(events, ev)
		}
	}
	return events
}

// worktreeBaseRef returns the ref worktree changes are computed against.
func (c *Core) worktreeBaseRef() string {
	if c.config == nil {
		return config.DefaultWorktreeBaseRef
	}
	return c.config.GetWorktreeBaseRef()
}

// resolveWorktreeBeanLocked decides which version of a bean runtime state shows
// and applies it, returning the event for subscribers if anything changed.
//
// A watched worktree's copy is a candidate if the worktree changed the bean
// relative to its base and the copy differs from the main version (runtime
// state for unlinked beans, main's file for linked ones, whose runtime state
// is a worktree's). Without candidates, a linked bean reverts to main's file
// (or is dropped if main doesn't have it). With several candidates, the most
// recently modified copy wins (ties broken by worktree path): it is the latest
// edit, matching what live updates show, and the result doesn't depend on the
// order in which worktrees are watched. Must be called with c.mu held.
func (c *Core) resolveWorktreeBeanLocked(id string) (BeanEvent, bool) {
	prev := c.beans[id]
	prevLink, linked := c.worktreeLinks[id]

	base := prev
	if linked {
		base = nil
		if mainPath := c.findMainBeanFile(id); mainPath != "" {
			mainBean, err := c.loadBeanFrom(mainPath, c.root)
			if err != nil {
				c.logWarn("failed to load bean from %s: %v", mainPath, err)
				return BeanEvent{}, false
			}
			base = mainBean
		}
	}

	type candidate struct {
		path    string
		b       *bean.Bean
		modTime time.Time
	}
	var candidates []candidate
	for path, wt := range c.worktreeWatchers {
		if !wt.mayChange(id) {
			continue
		}
		b, modTime := c.loadWorktreeCopyWithModTimeLocked(wt.beansDir, id)
		if b == nil || (base != nil && base.ETag() == b.ETag()) {
			continue
		}
		candidates = append(candidates, candidate{path, b, modTime})
	}

	if len(candidates) == 0 {
		if !linked {
			return BeanEvent{}, false
		}
		delete(c.worktreeLinks, id)
		delete(c.dirty, id)
		if base == nil {
			delete(c.beans, id)
			if c.searchIndex != nil {
				_ = c.searchIndex.DeleteBean(id)
			}
			return BeanEvent{Type: EventDeleted, BeanID: id}, true
		}
		c.beans[id] = base
		if c.searchIndex != nil {
			_ = c.searchIndex.IndexBean(base)
		}
		return BeanEvent{Type: EventUpdated, Bean: base, BeanID: id}, true
	}

	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].modTime.After(candidates[j].modTime)
		}
		return candidates[i].path < candidates[j].path
	})
	winner := candidates[0]
	if len(candidates) > 1 && winner.path != prevLink {
		paths := make([]string, len(candidates))
		for i, cand := range candidates {
			paths[i] = cand.path
		}
		c.logWarn("bean %s is changed in several worktrees (%s); showing the most recently modified one: %s",
			id, strings.Join(paths, ", "), winner.path)
	}

	if linked && prevLink == winner.path && prev != nil && prev.Path == winner.b.Path && prev.ETag() == winner.b.ETag() {
		return BeanEvent{}, false
	}
	c.beans[id] = winner.b
	c.dirty[id] = true // came from a worktree, not persisted to main
	c.worktreeLinks[id] = winner.path
	if c.searchIndex != nil {
		if err := c.searchIndex.IndexBean(winner.b); err != nil {
			c.logWarn("failed to index worktree bean %s: %v", id, err)
		}
	}
	if prev == nil {
		return BeanEvent{Type: EventCreated, Bean: winner.b, BeanID: id}, true
	}
	return BeanEvent{Type: EventUpdated, Bean: winner.b, BeanID: id}, true
}

// loadWorktreeCopyWithModTimeLocked loads a worktree's copy of a bean and its
// modification time, or returns nil if the worktree has none (or it can't be
// parsed). Must be called with c.mu held.
func (c *Core) loadWorktreeCopyWithModTimeLocked(beansDir, id string) (*bean.Bean, time.Time) {
	for _, path := range findBeanFilesIn(beansDir, id) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if b, err := c.loadBeanFrom(path, beansDir); err == nil {
			return b, info.ModTime()
		}
	}
	return nil, time.Time{}
}

// beanIDsIn returns the IDs of all bean files in a .beans/ directory,
// including its archive/ subdirectory.
func beanIDsIn(beansDir string) map[string]bool {
	ids := make(map[string]bool)
	for _, dir := range []string{beansDir, filepath.Join(beansDir, ArchiveDir)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if id, _ := bean.ParseFilename(entry.Name()); id != "" {
				ids[id] = true
			}
		}
	}
	return ids
}

func sortedIDs(ids map[string]bool) []string {
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	return sorted
}

// reapplyWorktreeChangesLocked re-resolves every bean linked to a worktree or
// changed in a watched worktree, e.g. after runtime state was reloaded from
// main's disk. Returns the resulting events. Must be called with c.mu held.
func (c *Core) reapplyWorktreeChangesLocked() []BeanEvent {
	ids := make(map[string]bool, len(c.worktreeLinks))
	for id := range c.worktreeLinks {
		ids[id] = true
	}
	for _, wt := range c.worktreeWatchers {
		for id := range wt.candidateIDs() {
			ids[id] = true
		}
	}
	var events []BeanEvent
	for _, id := range sortedIDs(ids) {
		if ev, ok := c.resolveWorktreeBeanLocked(id); ok {
			events = append(events, ev)
		}
	}
	return events
}

// unlinkWorktreesLocked re-resolves the beans linked to the given (no longer
// watched) worktrees: each falls back to another watched worktree's change,
// or else to main's version (it is dropped if main doesn't have it), so the
// unwatched worktree's changes neither linger in runtime state nor get
// written to main by a later edit. Returns the resulting events. Must be
// called with c.mu held, after the worktrees' watchers were removed.
func (c *Core) unlinkWorktreesLocked(worktreePaths map[string]bool) []BeanEvent {
	ids := make(map[string]bool)
	for id, wtp := range c.worktreeLinks {
		if worktreePaths[wtp] {
			ids[id] = true
		}
	}
	var events []BeanEvent
	for _, id := range sortedIDs(ids) {
		if ev, ok := c.resolveWorktreeBeanLocked(id); ok {
			events = append(events, ev)
		}
		// Resolving keeps the link if main's file can't be loaded; never
		// route writes to an unwatched worktree.
		if worktreePaths[c.worktreeLinks[id]] {
			delete(c.worktreeLinks, id)
		}
	}
	return events
}

// UnwatchWorktreeBeans stops watching a worktree's .beans/ directory. Beans
// linked to the worktree revert to another watched worktree's change or to
// main's version; subscribers are notified.
func (c *Core) UnwatchWorktreeBeans(worktreePath string) {
	c.mu.Lock()
	wt, exists := c.worktreeWatchers[worktreePath]
	var events []BeanEvent
	if exists {
		delete(c.worktreeWatchers, worktreePath)
		events = c.unlinkWorktreesLocked(map[string]bool{worktreePath: true})
	}
	c.mu.Unlock()

	if exists {
		close(wt.done)
		c.logWarn("stopped watching worktree beans: %s", wt.beansDir)
	}
	c.fanOut(events)
}

// SyncWorktreeWatchers reconciles the worktree watchers with the given set of
// active worktree paths: it starts watching worktrees that aren't watched yet
// (e.g. created outside beans, or whose .beans/ directory appeared since the
// last sync) and stops watching worktrees that are gone. Paths are compared
// after resolving symlinks, so the same worktree reached through different
// paths (e.g. /tmp vs /private/tmp) is watched once. Returns the first error
// from starting a watcher; the remaining worktrees are still synced.
func (c *Core) SyncWorktreeWatchers(worktreePaths []string) error {
	wanted := make(map[string]string, len(worktreePaths)) // canonical -> path
	for _, p := range worktreePaths {
		canon := canonicalPath(p)
		if _, ok := wanted[canon]; !ok {
			wanted[canon] = p
		}
	}

	c.mu.RLock()
	watched := make(map[string]string, len(c.worktreeWatchers)) // canonical -> watched key
	for p := range c.worktreeWatchers {
		watched[canonicalPath(p)] = p
	}
	c.mu.RUnlock()

	for canon, key := range watched {
		if _, ok := wanted[canon]; !ok {
			c.UnwatchWorktreeBeans(key)
		}
	}

	var firstErr error
	for canon, p := range wanted {
		if _, ok := watched[canon]; ok {
			continue
		}
		if err := c.WatchWorktreeBeans(p); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("watching worktree %s: %w", p, err)
		}
	}
	return firstErr
}

// WatchedWorktrees returns the paths of the worktrees whose .beans/
// directories are being watched, sorted.
func (c *Core) WatchedWorktrees() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	paths := make([]string, 0, len(c.worktreeWatchers))
	for p := range c.worktreeWatchers {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// canonicalPath resolves symlinks in p, or returns it cleaned if that fails
// (e.g. the worktree was already deleted).
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// UnwatchAllWorktrees stops watching all worktree .beans/ directories. Beans
// linked to worktrees revert to main's version; subscribers are notified.
func (c *Core) UnwatchAllWorktrees() {
	c.mu.Lock()
	watchers := c.worktreeWatchers
	c.worktreeWatchers = make(map[string]*worktreeWatcher)
	paths := make(map[string]bool, len(c.worktreeLinks))
	for _, wtp := range c.worktreeLinks {
		paths[wtp] = true
	}
	events := c.unlinkWorktreesLocked(paths)
	c.mu.Unlock()

	for _, wt := range watchers {
		close(wt.done)
	}
	c.fanOut(events)
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

// handleWorktreeChanges processes bean file changes from a worktree. It
// recomputes the worktree's changed-bean set and re-resolves the beans whose
// files changed or whose membership in the set changed. A file event for a
// bean the worktree hasn't changed relative to its base (e.g. a checkout or
// rebase rewriting the base version) doesn't affect runtime state, unless the
// bean was linked to this worktree: then the worktree reverted its change and
// the bean falls back to another worktree's change or main's version.
func (c *Core) handleWorktreeChanges(wt *worktreeWatcher, changes map[string]fsnotify.Op) {
	if len(changes) == 0 {
		return
	}

	wt.refreshMu.Lock()
	defer wt.refreshMu.Unlock()

	changed, known := worktreeChangedBeanIDs(wt.worktreePath, c.worktreeBaseRef())

	ids := make(map[string]bool)
	for path := range changes {
		if id, _ := bean.ParseFilename(filepath.Base(path)); id != "" {
			ids[id] = true
		}
	}

	c.mu.Lock()
	if c.worktreeWatchers[wt.worktreePath] != wt {
		c.mu.Unlock()
		return // unwatched meanwhile
	}
	// Beans entering or leaving the changed set (e.g. after a commit was
	// dropped by a rebase) are re-resolved too.
	for id := range wt.changed {
		if !changed[id] {
			ids[id] = true
		}
	}
	for id := range changed {
		if !wt.changed[id] {
			ids[id] = true
		}
	}
	wt.changed, wt.changedKnown, wt.scanned = changed, known, true

	var events []BeanEvent
	for _, id := range sortedIDs(ids) {
		if !wt.mayChange(id) && c.worktreeLinks[id] != wt.worktreePath {
			continue // untouched by this worktree: its copy is irrelevant
		}
		if ev, ok := c.resolveWorktreeBeanLocked(id); ok {
			events = append(events, ev)
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

// findMainBeanFile searches the main .beans/ directory (including archive/)
// for a file matching the given bean ID. Returns the full path or empty string
// if not found. Must be called with c.mu held.
func (c *Core) findMainBeanFile(id string) string {
	if paths := findBeanFilesIn(c.root, id); len(paths) > 0 {
		return paths[0]
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
