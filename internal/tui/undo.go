package tui

import (
	"context"
	"fmt"
	"slices"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// Undo ("u") of changes made in this TUI session.
//
// Every change records a snapshot of the affected beans from before the
// change and their etags right after it. Undo restores the snapshots via
// UpdateBean, but only if the beans are still exactly as the change left
// them: a bean that changed since (in an editor, the CLI, the web UI or an
// agent) is not clobbered; undo then refuses and drops the entry.

// maxUndoEntries bounds the undo history.
const maxUndoEntries = 100

// beanSnapshot is the state of a bean before a change.
type beanSnapshot struct {
	before    *bean.Bean
	afterETag string // etag right after the change
}

// undoEntry is one undoable change.
type undoEntry struct {
	label string // what changed, e.g. "status of abcd"
	snaps []beanSnapshot
	// created is the ID of a bean created by the change (undo deletes it);
	// createdETag its etag after the change.
	created     string
	createdETag string
}

// undoState is the undo history of the session.
type undoState struct {
	entries []undoEntry
	// editing is the snapshot of the bean opened in $EDITOR.
	editing *bean.Bean
}

func (u *undoState) push(e undoEntry) {
	u.entries = append(u.entries, e)
	if n := len(u.entries) - maxUndoEntries; n > 0 {
		u.entries = slices.Delete(u.entries, 0, n)
	}
}

func (u *undoState) pop() (undoEntry, bool) {
	n := len(u.entries)
	if n == 0 {
		return undoEntry{}, false
	}
	e := u.entries[n-1]
	u.entries = u.entries[:n-1]
	return e, true
}

// snapshotBeans copies the current state of the beans with the given IDs
// (Core.Get returns copies).
func (a *App) snapshotBeans(ids []string) []*bean.Bean {
	var out []*bean.Bean
	for _, id := range ids {
		if b, err := a.core.Get(id); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// recordChange pushes an undo entry for the beans that changed since the
// snapshots were taken.
func (a *App) recordChange(label string, before []*bean.Bean) {
	e := undoEntry{label: label}
	for _, b := range before {
		cur, err := a.core.Get(b.ID)
		if err != nil {
			continue
		}
		if after := cur.ETag(); after != b.ETag() {
			e.snaps = append(e.snaps, beanSnapshot{before: b, afterETag: after})
		}
	}
	if len(e.snaps) > 0 {
		a.undo.push(e)
	}
}

// trackChange runs change, which modifies the beans with the given IDs, and
// records it for undo.
func (a *App) trackChange(label string, ids []string, change func()) {
	before := a.snapshotBeans(ids)
	change()
	a.recordChange(label, before)
}

// recordCreate records the creation of a bean for undo.
func (a *App) recordCreate(id string) {
	if b, err := a.core.Get(id); err == nil {
		a.undo.push(undoEntry{label: "create of " + a.shortID(id), created: id, createdETag: b.ETag()})
	}
}

// changeLabel describes a change of field for the beans with the given IDs.
func (a *App) changeLabel(field string, ids []string) string {
	if len(ids) == 1 {
		return field + " of " + a.shortID(ids[0])
	}
	return fmt.Sprintf("%s of %d beans", field, len(ids))
}

func (a *App) shortID(id string) string {
	if a.config == nil {
		return id
	}
	return shortID(id, a.config.Beans.Prefix)
}

// undoConflict returns why the entry can't be undone without clobbering
// later changes, or "".
func (a *App) undoConflict(e undoEntry) string {
	if e.created != "" {
		cur, err := a.core.Get(e.created)
		if err != nil {
			return a.shortID(e.created) + " no longer exists"
		}
		if cur.ETag() != e.createdETag {
			return a.shortID(e.created) + " was changed since"
		}
		if len(a.core.FindIncomingLinks(e.created)) > 0 {
			return "other beans link to " + a.shortID(e.created)
		}
		return ""
	}
	for _, s := range e.snaps {
		cur, err := a.core.Get(s.before.ID)
		if err != nil {
			return a.shortID(s.before.ID) + " no longer exists"
		}
		if cur.ETag() != s.afterETag {
			return a.shortID(s.before.ID) + " was changed since"
		}
	}
	return ""
}

// restoreInput returns the update restoring bean b to snapshot s.
func restoreInput(cur, s *bean.Bean, ifMatch string) model.UpdateBeanInput {
	title, status, typ, prio := s.Title, s.Status, s.Type, s.Priority
	body, parent, order := s.Body, s.Parent, s.Order
	tags := slices.Clone(s.Tags)
	if tags == nil {
		tags = []string{} // nil would keep the current tags
	}
	in := model.UpdateBeanInput{
		Title:    &title,
		Status:   &status,
		Type:     &typ,
		Priority: &prio,
		Body:     &body,
		Parent:   &parent,
		Order:    &order,
		Tags:     tags,
		IfMatch:  &ifMatch,
	}
	in.AddBlocking, in.RemoveBlocking = listDiff(cur.Blocking, s.Blocking)
	in.AddBlockedBy, in.RemoveBlockedBy = listDiff(cur.BlockedBy, s.BlockedBy)
	return in
}

// listDiff returns the elements to add to and remove from cur to get want.
func listDiff(cur, want []string) (add, remove []string) {
	for _, w := range want {
		if !slices.Contains(cur, w) {
			add = append(add, w)
		}
	}
	for _, c := range cur {
		if !slices.Contains(want, c) {
			remove = append(remove, c)
		}
	}
	return add, remove
}

// undoLast undoes the most recent change and returns a message describing
// the outcome.
func (a *App) undoLast() string {
	e, ok := a.undo.pop()
	if !ok {
		return "Nothing to undo"
	}
	if why := a.undoConflict(e); why != "" {
		return fmt.Sprintf("Can't undo %s: %s (dropped from undo history)", e.label, why)
	}
	ctx := context.Background()
	if e.created != "" {
		if _, err := a.resolver.DeleteBean(ctx, e.created); err != nil {
			return fmt.Sprintf("Undo of %s failed: %v", e.label, err)
		}
		return fmt.Sprintf("Undid %s (deleted)", e.label)
	}
	// Restore in reverse order of recording, checking each etag again at
	// write time in case a bean changed after the check above.
	for i := len(e.snaps) - 1; i >= 0; i-- {
		s := e.snaps[i]
		cur, err := a.core.Get(s.before.ID)
		if err != nil {
			return fmt.Sprintf("Undo of %s failed: %v", e.label, err)
		}
		if _, err := a.resolver.UpdateBean(ctx, s.before.ID, restoreInput(cur, s.before, s.afterETag)); err != nil {
			return fmt.Sprintf("Undo of %s failed: %v", e.label, err)
		}
		if restored, err := a.core.Get(s.before.ID); err == nil {
			a.undo.rebase(s.before.ETag(), restored.ETag())
		}
	}
	return "Undid " + e.label
}

// rebase updates the etags expected by older entries after an undo: the
// bean is back in the state they left it in, except for updated_at, which
// the restoring write bumped.
func (u *undoState) rebase(oldETag, newETag string) {
	for i := range u.entries {
		e := &u.entries[i]
		if e.created != "" && e.createdETag == oldETag {
			e.createdETag = newETag
		}
		for j := range e.snaps {
			if e.snaps[j].afterETag == oldETag {
				e.snaps[j].afterETag = newETag
			}
		}
	}
}
