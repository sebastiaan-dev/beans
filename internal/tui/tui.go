package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
	"github.com/hmans/beans/pkg/safepath"
)

// viewState represents which view is currently active
type viewState int

const (
	viewList viewState = iota
	viewTagPicker
	viewParentPicker
	viewStatusPicker
	viewTypePicker
	viewBlockingPicker
	viewPriorityPicker
	viewCreateModal
	viewHelpOverlay
	viewFilterPicker
)

// beansChangedMsg is sent when beans change on disk (via file watcher)
type beansChangedMsg struct{}

// openTagPickerMsg requests opening the tag picker
type openTagPickerMsg struct{}

// tagSelectedMsg is sent when a tag is selected from the picker
type tagSelectedMsg struct {
	tag string
}

// backToListMsg returns from a full-screen picker to the browse view
type backToListMsg struct{}

// clearFilterMsg is sent to clear any active filter
type clearFilterMsg struct{}

// copyBeanIDMsg requests copying bean ID(s) to the clipboard
type copyBeanIDMsg struct {
	ids []string
}

// openEditorMsg requests opening the editor for a bean
type openEditorMsg struct {
	beanID   string
	beanPath string
}

// editorFinishedMsg is sent when the editor closes
type editorFinishedMsg struct {
	err error
}

// openParentPickerMsg requests opening the parent picker for bean(s)
type openParentPickerMsg struct {
	beanIDs       []string // IDs of beans to update
	beanTitle     string   // Display title (single title or "N selected beans")
	beanTypes     []string // Types of the beans (to filter eligible parents)
	currentParent string   // Only meaningful for single bean
}

// App is the main TUI application model
type App struct {
	state          viewState
	list           listModel
	detail         detailModel
	tagPicker      tagPickerModel
	parentPicker   parentPickerModel
	statusPicker   statusPickerModel
	typePicker     typePickerModel
	blockingPicker blockingPickerModel
	priorityPicker priorityPickerModel
	createModal    createModalModel
	helpOverlay    helpOverlayModel
	filterPicker   filterPickerModel
	core           *beancore.Core
	resolver       *beangraph.CoreResolver
	config         *config.Config
	width          int
	height         int
	program        *tea.Program // reference to program for sending messages from watcher

	// Browse view state
	focus        focusArea // pane receiving keys
	detailPinned string    // bean opened from a relation that is not in the tree
	detailStale  bool      // beans were reloaded; refresh the detail pane
	revealID     string    // bean to move the cursor to after the next load

	// Key chord state - tracks partial key sequences like "g" waiting for "t"
	// or "z" waiting for a fold command
	pendingKey string

	// Batched key state - keys held back until a view change requested by an
	// earlier key of the same batch has been applied (see handleKeys)
	heldKeys    []tea.KeyMsg
	holdingKeys bool

	// Modal state - tracks view behind modal pickers
	previousState viewState

	// Editor state - tracks bean being edited to update updated_at on save
	editingBeanID      string
	editingBeanModTime time.Time
}

// New creates a new TUI application
func New(core *beancore.Core, cfg *config.Config) *App {
	resolver := &beangraph.CoreResolver{Core: core}
	return &App{
		state:    viewList,
		core:     core,
		resolver: resolver,
		config:   cfg,
		list:     newListModel(resolver, cfg),
		detail:   newDetailModel(resolver, cfg),
	}
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	return a.list.Init()
}

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	a.syncLayout()
	return a, cmd
}

// updateBeans applies an update to each bean and continues with other beans
// if one fails.
func (a *App) updateBeans(ids []string, input model.UpdateBeanInput) {
	for _, id := range ids {
		_, _ = a.resolver.UpdateBean(context.Background(), id, input)
	}
}

// closePicker returns from a picker to the browse view and refreshes, in
// case beans changed while it was open. Batch edits clear the selection.
func (a *App) closePicker(clearSelection bool) tea.Cmd {
	a.state = viewList
	if clearSelection {
		clear(a.list.selectedBeans)
	}
	return a.list.loadBeans
}

// handleChord handles the second key of a chord ("g t", "g g", "z o", ...).
func (a *App) handleChord(first string, msg tea.KeyMsg) tea.Cmd {
	switch first + " " + msg.String() {
	case "g t":
		return func() tea.Msg { return openTagPickerMsg{} }
	case "g g":
		if a.focus == focusTree {
			a.list.moveCursor(-len(a.list.rows))
		} else {
			a.detail.bodyOffset = 0
		}
	case "z o":
		a.list.foldOpen()
	case "z c":
		a.list.foldClose()
	case "z a":
		a.list.foldToggle()
	case "z R":
		a.list.openAll()
	case "z M":
		a.list.closeAll()
	}
	// Other second keys cancel the chord and are ignored.
	return nil
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case tea.KeyMsg:
		// Keep the order of keys while earlier keys of a batch are pending.
		if a.holdingKeys {
			a.heldKeys = append(a.heldKeys, msg)
			return nil
		}
		// Several runes delivered at once are individual shortcuts, unless a
		// text input is receiving them.
		if keys, ok := splitBatchedRunes(msg); ok && !a.acceptsTextInput() {
			return a.handleKeys(keys)
		}

		// Clear status messages on any keypress
		a.list.statusMessage = ""

		if msg.String() == "ctrl+c" {
			return tea.Quit
		}

		// Handle key chord sequences
		if a.state == viewList && !a.list.searching {
			if first := a.pendingKey; first != "" {
				a.pendingKey = ""
				return a.handleChord(first, msg)
			}
			switch msg.String() {
			case "g":
				a.pendingKey = "g"
				return nil
			case "z":
				if a.focus == focusTree {
					a.pendingKey = "z"
					return nil
				}
			}
		}

		// Clear pending key on any other key press
		a.pendingKey = ""

		// Moving the focus between the panes and opening relations is
		// handled here, synchronously, so that the keys that follow already
		// go to the right pane.
		if a.state == viewList && !a.list.searching {
			if handled := a.handleFocusKey(msg.String()); handled {
				return nil
			}
		}

		switch msg.String() {
		case "?":
			// Open help overlay from the browse view
			if a.state == viewList && !a.list.searching {
				a.previousState = a.state
				a.helpOverlay = newHelpOverlayModel(a.width, a.height)
				a.state = viewHelpOverlay
				return a.helpOverlay.Init()
			}
		case "q":
			switch a.state {
			case viewTagPicker, viewParentPicker, viewStatusPicker, viewTypePicker, viewBlockingPicker, viewPriorityPicker, viewHelpOverlay:
				return tea.Quit
			case viewList:
				// Only quit if not typing a search
				if !a.list.searching {
					return tea.Quit
				}
			}
		}

	case releaseHeldKeysMsg:
		keys := a.heldKeys
		a.heldKeys = nil
		a.holdingKeys = false
		return a.handleKeys(keys)

	case beansLoadedMsg:
		a.list, cmd = a.list.Update(msg)
		a.detailStale = true
		if a.revealID != "" {
			a.list.reveal(a.revealID)
			a.revealID = ""
		}
		return cmd

	case beansChangedMsg:
		// Beans changed on disk - refresh
		return a.list.loadBeans

	case themeReloadedMsg:
		return a.applyThemeReload(msg)

	case openTagPickerMsg:
		// Collect all tags with their counts
		tags := a.collectTagsWithCounts()
		if len(tags) == 0 {
			// No tags in system, don't open picker
			return nil
		}
		a.tagPicker = newTagPickerModel(tags, a.width, a.height)
		a.state = viewTagPicker
		return a.tagPicker.Init()

	case tagSelectedMsg:
		a.state = viewList
		a.list.setTagFilter(msg.tag)
		return a.list.loadBeans

	case backToListMsg:
		a.state = viewList
		return nil

	case openParentPickerMsg:
		// Check if all bean types can have parents
		for _, beanType := range msg.beanTypes {
			if beancore.ValidParentTypes(beanType) == nil {
				// At least one bean type (e.g., milestone) cannot have parents - don't open the picker
				return nil
			}
		}
		a.previousState = a.state // Remember where we came from for the modal background
		a.parentPicker = newParentPickerModel(msg.beanIDs, msg.beanTitle, msg.beanTypes, msg.currentParent, a.resolver, a.config, a.width, a.height)
		a.state = viewParentPicker
		return a.parentPicker.Init()

	case closeParentPickerMsg:
		return a.closePicker(false)

	case parentSelectedMsg:
		// Set the new parent via GraphQL mutation for all beans
		var parentID *string
		if msg.parentID != "" {
			parentID = &msg.parentID
		}
		for _, beanID := range msg.beanIDs {
			// Continue with other beans even if one fails
			_, _ = a.resolver.SetParent(context.Background(), beanID, parentID, nil)
		}
		return a.closePicker(true)

	case openStatusPickerMsg:
		a.previousState = a.state
		a.statusPicker = newStatusPickerModel(msg.beanIDs, msg.beanTitle, msg.currentStatus, a.config, a.width, a.height)
		a.state = viewStatusPicker
		return a.statusPicker.Init()

	case closeStatusPickerMsg:
		return a.closePicker(false)

	case statusSelectedMsg:
		a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Status: &msg.status})
		return a.closePicker(true)

	case openTypePickerMsg:
		a.previousState = a.state
		a.typePicker = newTypePickerModel(msg.beanIDs, msg.beanTitle, msg.currentType, a.config, a.width, a.height)
		a.state = viewTypePicker
		return a.typePicker.Init()

	case closeTypePickerMsg:
		return a.closePicker(false)

	case typeSelectedMsg:
		a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Type: &msg.beanType})
		return a.closePicker(true)

	case openPriorityPickerMsg:
		a.previousState = a.state
		a.priorityPicker = newPriorityPickerModel(msg.beanIDs, msg.beanTitle, msg.currentPriority, a.config, a.width, a.height)
		a.state = viewPriorityPicker
		return a.priorityPicker.Init()

	case closePriorityPickerMsg:
		return a.closePicker(false)

	case prioritySelectedMsg:
		a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Priority: &msg.priority})
		return a.closePicker(true)

	case openHelpMsg:
		a.previousState = a.state
		a.helpOverlay = newHelpOverlayModel(a.width, a.height)
		a.state = viewHelpOverlay
		return a.helpOverlay.Init()

	case closeHelpMsg:
		a.state = a.previousState
		return nil

	case openBlockingPickerMsg:
		a.previousState = a.state
		a.blockingPicker = newBlockingPickerModel(msg.beanID, msg.beanTitle, msg.currentBlocking, a.resolver, a.config, a.width, a.height)
		a.state = viewBlockingPicker
		return a.blockingPicker.Init()

	case closeBlockingPickerMsg:
		return a.closePicker(false)

	case blockingConfirmedMsg:
		// Apply all blocking changes via GraphQL mutations, continuing with
		// other changes if one fails
		for _, targetID := range msg.toAdd {
			_, _ = a.resolver.AddBlocking(context.Background(), msg.beanID, targetID, nil)
		}
		for _, targetID := range msg.toRemove {
			_, _ = a.resolver.RemoveBlocking(context.Background(), msg.beanID, targetID, nil)
		}
		return a.closePicker(false)

	case openCreateModalMsg:
		a.previousState = a.state
		a.createModal = newCreateModalModel(a.width, a.height)
		a.state = viewCreateModal
		return a.createModal.Init()

	case closeCreateModalMsg:
		a.state = a.previousState
		return nil

	case beanCreatedMsg:
		// Create the bean via GraphQL mutation; status and type use the configured defaults
		createdBean, err := a.resolver.CreateBean(context.Background(), model.CreateBeanInput{
			Title: msg.title,
		})
		a.state = viewList
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Failed to create bean: %v", err)
			return nil
		}
		// Select the new bean and open it in the editor
		a.revealID = createdBean.ID
		return tea.Batch(
			a.list.loadBeans,
			func() tea.Msg {
				return openEditorMsg{beanID: createdBean.ID, beanPath: createdBean.Path}
			},
		)

	case openEditorMsg:
		// Launch editor for the bean file
		editor := getEditor()
		fullPath, err := safepath.SafeJoin(a.core.Root(), msg.beanPath)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("unsafe bean path: %v", err)
			return nil
		}

		// Record the bean ID and file mod time before editing
		a.editingBeanID = msg.beanID
		if info, err := os.Stat(fullPath); err == nil {
			a.editingBeanModTime = info.ModTime()
		}

		c := exec.Command(editor, fullPath)
		return tea.ExecProcess(c, func(err error) tea.Msg {
			return editorFinishedMsg{err: err}
		})

	case editorFinishedMsg:
		// Editor closed - check if file was modified and update updated_at if so
		if a.editingBeanID != "" {
			a.touchEditedBean()
			// Clear editing state
			a.editingBeanID = ""
			a.editingBeanModTime = time.Time{}
		}
		return a.list.loadBeans

	case clearFilterMsg:
		a.list.clearFilter()
		return a.list.loadBeans

	case openFilterPickerMsg:
		a.previousState = a.state
		a.filterPicker = newFilterPickerModel(a.list.filters, a.collectTagsWithCounts(), a.width, a.height)
		a.state = viewFilterPicker
		return a.filterPicker.Init()

	case closeFilterPickerMsg:
		a.state = a.previousState
		return nil

	case filtersAppliedMsg:
		a.state = a.previousState
		a.list.setFilters(msg.filters)
		return a.list.loadBeans

	case copyBeanIDMsg:
		text := strings.Join(msg.ids, ", ")
		if err := clipboard.WriteAll(text); err != nil {
			a.list.statusMessage = fmt.Sprintf("Failed to copy: %v", err)
		} else if len(msg.ids) == 1 {
			a.list.statusMessage = fmt.Sprintf("Copied %s to clipboard", msg.ids[0])
		} else {
			a.list.statusMessage = fmt.Sprintf("Copied %d bean IDs to clipboard", len(msg.ids))
		}
		return nil
	}

	// Forward all messages to the current view
	switch a.state {
	case viewList:
		if a.focus == focusDetail {
			a.detail, cmd = a.detail.Update(msg)
		} else {
			a.list, cmd = a.list.Update(msg)
		}
	case viewTagPicker:
		a.tagPicker, cmd = a.tagPicker.Update(msg)
	case viewParentPicker:
		a.parentPicker, cmd = a.parentPicker.Update(msg)
	case viewStatusPicker:
		a.statusPicker, cmd = a.statusPicker.Update(msg)
	case viewTypePicker:
		a.typePicker, cmd = a.typePicker.Update(msg)
	case viewPriorityPicker:
		a.priorityPicker, cmd = a.priorityPicker.Update(msg)
	case viewBlockingPicker:
		a.blockingPicker, cmd = a.blockingPicker.Update(msg)
	case viewCreateModal:
		a.createModal, cmd = a.createModal.Update(msg)
	case viewHelpOverlay:
		a.helpOverlay, cmd = a.helpOverlay.Update(msg)
	case viewFilterPicker:
		a.filterPicker, cmd = a.filterPicker.Update(msg)
	}

	return cmd
}

// handleFocusKey handles the keys moving between the tree and the detail
// pane and reports whether key was one of them.
func (a *App) handleFocusKey(key string) bool {
	if a.focus == focusTree {
		if key != "enter" && key != "tab" {
			return false
		}
		if a.list.currentBean() != nil {
			a.focus = focusDetail
			a.syncDetail()
			a.detail.focus()
		}
		return true
	}
	switch key {
	case "esc":
		a.focusTree()
	case "/", "F", "H", "c":
		// Tree-wide actions return to the tree and are handled there.
		a.focusTree()
		return false
	case "enter":
		if id := a.detail.selectedRelation(); id != "" {
			a.showBean(id, true)
		}
	case "backspace":
		if id, ok := a.detail.popHistory(); ok {
			a.showBean(id, false)
		} else {
			a.focusTree()
		}
	default:
		return false
	}
	return true
}

// showBean shows a bean in the detail pane, moving the tree cursor to it if
// it is in the (filtered) tree. record adds the current bean to the detail
// history.
func (a *App) showBean(id string, record bool) {
	if cur := a.detail.bean; cur != nil && record {
		a.detail.history = append(a.detail.history, cur.ID)
	}
	if a.list.reveal(id) {
		a.detailPinned = ""
	} else {
		a.detailPinned = id
	}
	a.syncDetail()
}

// touchEditedBean sets updated_at of the bean being edited if the editor
// modified its file.
func (a *App) touchEditedBean() {
	b, err := a.core.Get(a.editingBeanID)
	if err != nil {
		return
	}
	fullPath, err := safepath.SafeJoin(a.core.Root(), b.Path)
	if err != nil {
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil || !info.ModTime().After(a.editingBeanModTime) {
		return
	}
	// File was modified - reload from disk first to get the user's changes,
	// then call Update to set updated_at
	_ = a.core.Load()
	if b, err = a.core.Get(a.editingBeanID); err == nil {
		_ = a.core.Update(b, nil)
	}
}

// acceptsTextInput reports whether the active view currently routes typed
// characters into a text field, so batched runes must be kept intact.
func (a *App) acceptsTextInput() bool {
	switch a.state {
	case viewList:
		return a.list.searching
	case viewTagPicker:
		return a.tagPicker.list.FilterState() == list.Filtering
	case viewParentPicker:
		return a.parentPicker.list.FilterState() == list.Filtering
	case viewStatusPicker:
		return a.statusPicker.list.FilterState() == list.Filtering
	case viewTypePicker:
		return a.typePicker.list.FilterState() == list.Filtering
	case viewPriorityPicker:
		return a.priorityPicker.list.FilterState() == list.Filtering
	case viewBlockingPicker:
		return a.blockingPicker.list.FilterState() == list.Filtering
	case viewCreateModal:
		return true
	case viewFilterPicker:
		// The filter modal splits batched runes itself outside its tag search.
		return true
	}
	return false
}

// releaseHeldKeysMsg delivers keys held back by handleKeys.
type releaseHeldKeysMsg struct{}

// handleKeys handles key messages one after another, as if they had been
// typed separately, splitting batched runes into single-key shortcuts.
//
// A key may return a command whose message changes the view (e.g. "c" opens
// the create modal via openCreateModalMsg). The remaining keys, and any keys
// arriving in the meantime, are therefore held back until that command's
// messages have been processed. Once a text input has focus, the remaining
// keys are delivered to it directly as text.
func (a *App) handleKeys(keys []tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd
	for len(keys) > 0 {
		if a.acceptsTextInput() {
			for _, k := range keys {
				_, cmd := a.Update(k)
				cmds = append(cmds, cmd)
			}
			break
		}
		if split, ok := splitBatchedRunes(keys[0]); ok {
			keys = append(split, keys[1:]...)
		}
		k := keys[0]
		keys = keys[1:]
		_, cmd := a.Update(k)
		if cmd != nil && len(keys) > 0 && !a.acceptsTextInput() {
			a.heldKeys = keys
			a.holdingKeys = true
			release := func() tea.Msg { return releaseHeldKeysMsg{} }
			cmds = append(cmds, tea.Sequence(cmd, release))
			break
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// collectTagsWithCounts returns all tags with their usage counts
func (a *App) collectTagsWithCounts() []tagWithCount {
	beans, _ := a.resolver.Beans(context.Background(), nil)
	tagCounts := make(map[string]int)
	for _, b := range beans {
		for _, tag := range b.Tags {
			tagCounts[tag]++
		}
	}

	tags := make([]tagWithCount, 0, len(tagCounts))
	for tag, count := range tagCounts {
		tags = append(tags, tagWithCount{tag: tag, count: count})
	}

	return tags
}

// View renders the current view, painted with the theme's background (see
// ui.PaintBackground; a no-op with the terminal theme).
func (a *App) View() string {
	return ui.PaintBackground(a.view(), a.width, a.height)
}

// view renders the current view without the theme background.
func (a *App) view() string {
	switch a.state {
	case viewList:
		return a.browseView()
	case viewTagPicker:
		return a.tagPicker.View()
	case viewParentPicker:
		return a.parentPicker.ModalView(a.browseView(), a.width, a.height)
	case viewStatusPicker:
		return a.statusPicker.ModalView(a.browseView(), a.width, a.height)
	case viewTypePicker:
		return a.typePicker.ModalView(a.browseView(), a.width, a.height)
	case viewPriorityPicker:
		return a.priorityPicker.ModalView(a.browseView(), a.width, a.height)
	case viewBlockingPicker:
		return a.blockingPicker.ModalView(a.browseView(), a.width, a.height)
	case viewCreateModal:
		return a.createModal.ModalView(a.browseView(), a.width, a.height)
	case viewHelpOverlay:
		return a.helpOverlay.ModalView(a.browseView(), a.width, a.height)
	case viewFilterPicker:
		return a.filterPicker.ModalView(a.browseView(), a.width, a.height)
	}
	return ""
}

// getEditor returns the user's preferred editor using the fallback chain:
// $VISUAL -> $EDITOR -> vi -> nano
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	// Fallback chain: vi is more universal, nano as last resort
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi"
	}
	return "nano"
}

func init() {
	ui.OnThemeApplied(rebuildThemeStyles)
}

// rebuildThemeStyles drops caches that captured the previous theme after
// ui.ApplyTheme. All other TUI styles are built at render time from the
// accessors in palette.go.
func rebuildThemeStyles() {
	resetMarkdownRenderers()
}

// Run starts the TUI application with file watching
func Run(core *beancore.Core, cfg *config.Config) error {
	// Apply the configured theme (with its background) before building any
	// view. A theme that fails to load falls back to terminal colors.
	themeSettings, themeErr := ui.ApplyConfiguredThemeSettings(cfg, true)
	ui.WarnThemeError(themeErr)

	app := New(core, cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())

	// Reload the theme when its sources change (see theme_reload.go).
	defer startThemeReload(cfg, themeSettings, p.Send)()

	// Store reference to program for sending messages from watcher
	app.program = p

	// Start file watching
	if err := core.StartWatching(); err != nil {
		return err
	}
	defer core.Unwatch()

	// Subscribe to bean events
	eventCh, unsubscribe := core.Subscribe()
	defer unsubscribe()

	// Forward events to TUI in a goroutine
	go func() {
		for range eventCh {
			// Send message to TUI when beans change
			if app.program != nil {
				app.program.Send(beansChangedMsg{})
			}
		}
	}()

	_, err := p.Run()
	return err
}
