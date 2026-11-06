package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// Quick actions of the browse view: inline create and rename, checkbox
// toggling, jumping to the next bean, undo and manual reordering.

// moveBeanMsg asks the App to move a bean up or down among its siblings.
type moveBeanMsg struct {
	id   string
	down bool
}

// reload reloads the beans synchronously, so that the tree reflects a
// change before the next key is handled.
func (a *App) reload() {
	a.update(a.list.loadBeans())
}

// handleQuickKey handles the global quick action keys of the browse view
// ("n" and "u", from either pane) and reports whether key was one of them.
func (a *App) handleQuickKey(key string) (tea.Cmd, bool) {
	switch key {
	case "n":
		a.jumpToNext()
	case "u":
		a.list.statusMessage = a.undoLast()
		a.reload()
	default:
		return nil, false
	}
	return nil, true
}

// handleQuickActionMsg handles the messages of the quick actions and
// reports whether msg was one of them.
func (a *App) handleQuickActionMsg(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case inlineCreateMsg:
		a.createInline(msg)
	case inlineRenameMsg:
		title := msg.title
		var err error
		a.trackChange(a.changeLabel("title", []string{msg.id}), []string{msg.id}, func() {
			_, err = a.resolver.UpdateBean(context.Background(), msg.id, model.UpdateBeanInput{Title: &title})
		})
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Failed to rename: %v", err)
		} else {
			a.list.statusMessage = "Renamed " + a.shortID(msg.id) + glyphs().sep() + "u to undo"
		}
		a.reload()
	case toggleCheckboxMsg:
		a.toggleCheckbox(msg)
	case moveBeanMsg:
		a.moveBean(msg)
	default:
		return false
	}
	return true
}

// createInline creates the bean entered in the tree's inline input and
// selects it.
func (a *App) createInline(msg inlineCreateMsg) {
	ctx := context.Background()
	input := model.CreateBeanInput{Title: msg.title}
	if msg.beanType != "" {
		input.Type = &msg.beanType
	}
	if msg.parentID != "" {
		input.Parent = &msg.parentID
	}
	created, err := a.resolver.CreateBean(ctx, input)
	if err != nil {
		a.list.statusMessage = fmt.Sprintf("Failed to create bean: %v", err)
		return
	}
	// Place a new sibling right after the selected bean if the siblings are
	// ordered manually.
	if anchor := a.list.byID[msg.afterID]; anchor != nil {
		if all, err := a.resolver.Beans(ctx, nil); err == nil {
			if key := orderAfter(siblingGroup(all, created, a.sortBeans), created.ID, anchor.ID); key != "" {
				_, _ = a.resolver.UpdateBean(ctx, created.ID, model.UpdateBeanInput{Order: &key})
			}
		}
	}
	a.recordCreate(created.ID)
	a.reload()
	if a.list.reveal(created.ID) {
		a.list.statusMessage = fmt.Sprintf("Created %s%se to edit%su to undo", a.shortID(created.ID), glyphs().sep(), glyphs().sep())
	} else {
		a.list.statusMessage = fmt.Sprintf("Created %s (hidden by the filters)%su to undo", a.shortID(created.ID), glyphs().sep())
	}
}

// sortBeans sorts beans like the tree.
func (a *App) sortBeans(beans []*bean.Bean) {
	bean.SortByStatusPriorityAndType(beans, a.config.StatusNames(), a.config.PriorityNames(), a.config.TypeNames())
}

// siblingGroup returns the beans sharing b's tree parent and status (b
// included) in display order: the beans among which b can be moved.
func siblingGroup(all []*bean.Bean, b *bean.Bean, sortFn func([]*bean.Bean)) []*bean.Bean {
	byID := make(map[string]*bean.Bean, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	parent := treeParentID(b, byID)
	var group []*bean.Bean
	for _, x := range all {
		if x.Status == b.Status && treeParentID(x, byID) == parent {
			group = append(group, x)
		}
	}
	sortFn(group)
	return group
}

// orderAfter returns an order key placing the bean id right after anchorID
// in group, or "" if the anchor has no order key (placing the bean would
// then require ordering all siblings) or there is no room.
func orderAfter(group []*bean.Bean, id, anchorID string) string {
	var rest []*bean.Bean
	for _, b := range group {
		if b.ID != id {
			rest = append(rest, b)
		}
	}
	for i, b := range rest {
		if b.ID != anchorID {
			continue
		}
		if b.Order == "" {
			return ""
		}
		next := ""
		if i+1 < len(rest) {
			next = rest[i+1].Order
		}
		key := bean.OrderBetween(b.Order, next)
		if key <= b.Order || (next != "" && key >= next) {
			return ""
		}
		return key
	}
	return ""
}

// toggleCheckbox toggles a checkbox of a bean's body and saves the body.
func (a *App) toggleCheckbox(msg toggleCheckboxMsg) {
	b, err := a.core.Get(msg.id)
	if err != nil {
		a.list.statusMessage = fmt.Sprintf("Failed to toggle checkbox: %v", err)
		return
	}
	items := findCheckboxes(b.Body)
	body, ok := toggleCheckbox(b.Body, msg.index)
	if !ok {
		return
	}
	a.trackChange(a.changeLabel("checkbox", []string{msg.id}), []string{msg.id}, func() {
		_, err = a.resolver.UpdateBean(context.Background(), msg.id, model.UpdateBeanInput{Body: &body})
	})
	switch {
	case err != nil:
		a.list.statusMessage = fmt.Sprintf("Failed to toggle checkbox: %v", err)
	case items[msg.index].checked:
		a.list.statusMessage = fmt.Sprintf("Unchecked %q", items[msg.index].text)
	default:
		a.list.statusMessage = fmt.Sprintf("Checked %q", items[msg.index].text)
	}
	a.reload()
}

// jumpToNext moves the cursor to the bean `beans next` suggests.
func (a *App) jumpToNext() {
	b, err := a.resolver.NextBean(context.Background())
	switch {
	case err != nil:
		a.list.statusMessage = fmt.Sprintf("Failed to find the next bean: %v", err)
		return
	case b == nil:
		a.list.statusMessage = "No beans ready to work on"
		return
	}
	if a.focus == focusDetail {
		a.focusTree()
	}
	if a.list.reveal(b.ID) {
		a.list.statusMessage = fmt.Sprintf("Next: %s %s", a.shortID(b.ID), b.Title)
	} else {
		a.list.statusMessage = fmt.Sprintf("Next is %s %s, hidden by the filters (esc clears them)", a.shortID(b.ID), b.Title)
	}
}

// moveBean moves a bean one visible sibling up or down by changing order
// keys, like drag and drop in the web UI's backlog.
func (a *App) moveBean(msg moveBeanMsg) {
	m := &a.list
	b := m.byID[msg.id]
	i := m.rowIndex(msg.id)
	if b == nil || i < 0 {
		return
	}
	// The next visible sibling in the direction of the move.
	delta, dir := -1, "up"
	if msg.down {
		delta, dir = 1, "down"
	}
	target := -1
	for j := i + delta; j >= 0 && j < len(m.rows); j += delta {
		if d := m.rows[j].depth; d < m.rows[i].depth {
			break
		} else if d == m.rows[i].depth {
			target = j
			break
		}
	}
	if target < 0 {
		a.list.statusMessage = fmt.Sprintf("Can't move %s %s: no sibling there", a.shortID(b.ID), dir)
		return
	}
	t := m.rows[target].bean
	if t.Status != b.Status {
		a.list.statusMessage = fmt.Sprintf("Can't move %s past %s beans: siblings are sorted by status first", a.shortID(b.ID), t.Status)
		return
	}
	changes := planOrderMove(siblingGroup(m.beans, b, a.sortBeans), b.ID, t.ID, msg.down)
	if len(changes) == 0 {
		return
	}
	ids := make([]string, 0, len(changes))
	for id := range changes {
		ids = append(ids, id)
	}
	var failed error
	a.trackChange(a.changeLabel("order", []string{b.ID}), ids, func() {
		for id, key := range changes {
			if _, err := a.resolver.UpdateBean(context.Background(), id, model.UpdateBeanInput{Order: &key}); err != nil && failed == nil {
				failed = err
			}
		}
	})
	if failed != nil {
		a.list.statusMessage = fmt.Sprintf("Failed to move: %v", failed)
	} else {
		a.list.statusMessage = fmt.Sprintf("Moved %s %s%su to undo", a.shortID(b.ID), dir, glyphs().sep())
	}
	a.reload()
	a.list.reveal(b.ID)
}
