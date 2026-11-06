package tui

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
)

// viewState represents which view is currently active
type viewState int

const (
	viewList viewState = iota
	viewParentPicker
	viewStatusPicker
	viewTypePicker
	viewBlockingPicker
	viewPriorityPicker
	viewHelpOverlay
	viewWorkspaces
)

// beansChangedMsg is sent when beans change on disk (via file watcher)
type beansChangedMsg struct{}

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
	bar            filterBarModel
	parentPicker   parentPickerModel
	statusPicker   statusPickerModel
	typePicker     typePickerModel
	blockingPicker blockingPickerModel
	priorityPicker priorityPickerModel
	helpOverlay    helpOverlayModel
	ws             *workspaces // workspace state (see workspaces.go)
	wsOverlay      workspacesOverlay
	core           *beancore.Core
	resolver       *beangraph.CoreResolver
	config         *config.Config
	width          int
	height         int
	program        *tea.Program // reference to program for sending messages from watcher

	// Views beyond the tree: Ready, Blocked, Audit (views.go)
	views viewsState

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

	// Undo history of changes made in this session (undo.go)
	undo undoState

	// Editor state - tracks bean being edited to update updated_at on save
	editingBeanID      string
	editingBeanModTime time.Time
}

// New creates a new TUI application
func New(core *beancore.Core, cfg *config.Config) *App {
	resolver := &beangraph.CoreResolver{Core: core}
	app := &App{
		state:    viewList,
		core:     core,
		resolver: resolver,
		config:   cfg,
		list:     newListModel(resolver, cfg),
		detail:   newDetailModel(resolver, cfg),
		ws:       newWorkspaces(nil, nil, cfg),
	}
	app.list.ws, app.detail.ws = app.ws, app.ws
	return app
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.list.Init(), a.ws.refreshCmd(a.core))
}

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	if vc := a.viewsCmd(); vc != nil {
		cmd = tea.Batch(cmd, vc)
	}
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
		a.openFilterField(barTags)
	case "g w":
		return a.openWorkspacesOverlay()
	case "g g":
		if l := a.activeList(); l != nil && a.focus == focusTree {
			l.moveToEnd(-1)
		} else if a.focus == focusTree {
			a.list.moveCursor(-len(a.list.rows))
		} else {
			a.detail.scrollHome()
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

	if a.handleQuickActionMsg(msg) || a.handleViewMsg(msg) {
		return nil
	}

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
		if a.state == viewList && !a.typing() {
			if first := a.pendingKey; first != "" {
				a.pendingKey = ""
				return a.handleChord(first, msg)
			}
			switch msg.String() {
			case "g":
				a.pendingKey = "g"
				return nil
			case "z":
				if a.focus == focusTree && a.views.active == viewTree {
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
		if a.state == viewList && a.handleViewSearchKey(msg.String()) {
			return nil
		}
		if a.state == viewList && !a.list.typing() {
			if a.focus == focusFilter {
				if cmd, handled := a.updateFilterBar(msg); handled {
					return cmd
				}
			}
			if !a.typing() {
				if cmd, handled := a.handleViewKey(msg); handled {
					return cmd
				}
				if handled := a.handleFocusKey(msg.String()); handled {
					return nil
				}
			}
			if cmd, handled := a.handleQuickKey(msg.String()); handled {
				return cmd
			}
			if cmd, handled := a.handleWorkspaceKey(msg.String()); handled {
				return cmd
			}
		}

		switch msg.String() {
		case "?":
			// Open help overlay from the browse view
			if a.state == viewList && !a.typing() {
				a.previousState = a.state
				a.helpOverlay = newHelpOverlayModel(a.width, a.height)
				a.state = viewHelpOverlay
				return a.helpOverlay.Init()
			}
		case "q":
			switch a.state {
			case viewParentPicker, viewStatusPicker, viewTypePicker, viewBlockingPicker, viewPriorityPicker, viewHelpOverlay:
				return tea.Quit
			case viewList:
				// Only quit if not typing a query or tag
				if !a.typing() {
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
		a.onBeansLoaded()
		if a.revealID != "" {
			a.list.reveal(a.revealID)
			a.revealID = ""
		}
		return cmd

	case beansChangedMsg:
		// Beans changed on disk - refresh (and the workspaces' status)
		return tea.Batch(a.list.loadBeans, a.ws.refreshCmd(a.core))

	case workspacesChangedMsg:
		return a.ws.refreshCmd(a.core)

	case workspacesLoadedMsg:
		return a.ws.apply(msg, a.core)

	case workspaceStartedMsg:
		return a.handleWorkspaceStarted(msg)

	case themeReloadedMsg:
		return a.applyThemeReload(msg)

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
		a.trackChange(a.changeLabel("parent", msg.beanIDs), msg.beanIDs, func() {
			for _, beanID := range msg.beanIDs {
				// Continue with other beans even if one fails
				_, _ = a.resolver.SetParent(context.Background(), beanID, parentID, nil)
			}
		})
		return a.closePicker(true)

	case openStatusPickerMsg:
		a.previousState = a.state
		a.statusPicker = newStatusPickerModel(msg.beanIDs, msg.beanTitle, msg.currentStatus, a.config, a.width, a.height)
		a.state = viewStatusPicker
		return a.statusPicker.Init()

	case closeStatusPickerMsg:
		return a.closePicker(false)

	case statusSelectedMsg:
		a.trackChange(a.changeLabel("status", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Status: &msg.status})
		})
		return a.closePicker(true)

	case openTypePickerMsg:
		a.previousState = a.state
		a.typePicker = newTypePickerModel(msg.beanIDs, msg.beanTitle, msg.currentType, a.config, a.width, a.height)
		a.state = viewTypePicker
		return a.typePicker.Init()

	case closeTypePickerMsg:
		return a.closePicker(false)

	case typeSelectedMsg:
		a.trackChange(a.changeLabel("type", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Type: &msg.beanType})
		})
		return a.closePicker(true)

	case openPriorityPickerMsg:
		a.previousState = a.state
		a.priorityPicker = newPriorityPickerModel(msg.beanIDs, msg.beanTitle, msg.currentPriority, a.config, a.width, a.height)
		a.state = viewPriorityPicker
		return a.priorityPicker.Init()

	case closePriorityPickerMsg:
		return a.closePicker(false)

	case prioritySelectedMsg:
		a.trackChange(a.changeLabel("priority", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Priority: &msg.priority})
		})
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
		a.trackChange(a.changeLabel("blocking", []string{msg.beanID}), []string{msg.beanID}, func() {
			for _, targetID := range msg.toAdd {
				_, _ = a.resolver.AddBlocking(context.Background(), msg.beanID, targetID, nil)
			}
			for _, targetID := range msg.toRemove {
				_, _ = a.resolver.RemoveBlocking(context.Background(), msg.beanID, targetID, nil)
			}
		})
		return a.closePicker(false)

	case openEditorMsg:
		// Launch editor for the bean file
		editor := getEditor()
		fullPath, err := a.beanFilePath(msg.beanID, msg.beanPath)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("unsafe bean path: %v", err)
			return nil
		}

		// Record the bean ID and file mod time before editing
		a.editingBeanID = msg.beanID
		a.undo.editing = nil
		if before := a.snapshotBeans([]string{msg.beanID}); len(before) > 0 {
			a.undo.editing = before[0]
		}
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
			if a.undo.editing != nil {
				a.recordChange(a.changeLabel("edit", []string{a.editingBeanID}), []*bean.Bean{a.undo.editing})
				a.undo.editing = nil
			}
			// Clear editing state
			a.editingBeanID = ""
			a.editingBeanModTime = time.Time{}
		}
		return a.list.loadBeans

	case clearFilterMsg:
		a.list.clearFilter()
		return nil

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
		switch a.focus {
		case focusDetail:
			a.detail, cmd = a.detail.Update(msg)
		case focusFilter:
			// Keys not handled by the filter bar must not act on the tree.
			if _, isKey := msg.(tea.KeyMsg); !isKey {
				a.list, cmd = a.list.Update(msg)
			}
		default:
			// Outside the tree view, keys only reach the tree's query input.
			if _, isKey := msg.(tea.KeyMsg); !isKey || a.views.active == viewTree || a.list.typing() {
				a.list, cmd = a.list.Update(msg)
			}
		}
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
	case viewHelpOverlay:
		a.helpOverlay, cmd = a.helpOverlay.Update(msg)
	case viewWorkspaces:
		cmd = a.updateWorkspacesOverlay(msg)
	}

	return cmd
}

// handleFocusKey handles the keys moving the focus between the filter bar,
// the tree and the detail pane, and reports whether key was one of them.
//
// tab and shift+tab cycle filter bar → tree → detail (relations, body) →
// filter bar; F focuses the filter bar from anywhere.
func (a *App) handleFocusKey(key string) bool {
	if key == "F" {
		a.focusFilterBar()
		return true
	}
	switch a.focus {
	case focusFilter:
		switch key {
		case "tab":
			a.focusTree()
		case "shift+tab":
			if !a.focusDetailPane(false) {
				a.focusTree()
			}
		default:
			return false
		}
		return true
	case focusTree:
		switch key {
		case "enter", "tab":
			if !a.focusDetailPane(true) && key == "tab" {
				a.focusFilterBar()
			}
		case "shift+tab":
			a.focusFilterBar()
		default:
			return false
		}
		return true
	}
	switch key {
	case "tab":
		if a.detail.section == sectionRelations {
			a.detail.section = sectionBody
		} else {
			a.focusFilterBar()
		}
	case "shift+tab":
		if a.detail.section == sectionBody && len(a.detail.links) > 0 {
			a.detail.section = sectionRelations
		} else {
			a.focusTree()
		}
	case "esc":
		a.focusTree()
	case "/", "H", "c", "C", "r", "K", "J":
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

// focusDetailPane moves the focus to the detail pane, on its first section
// (relations, if any) or its last one (body), and reports whether there is a
// bean to show.
func (a *App) focusDetailPane(first bool) bool {
	if a.currentBean() == nil {
		return false
	}
	a.focus = focusDetail
	a.syncDetail()
	a.detail.focus()
	if !first {
		a.detail.section = sectionBody
	}
	return true
}

// typing reports whether the browse view routes typed characters into a
// text field (the query input or the tag type-ahead).
func (a *App) typing() bool {
	return a.list.typing() || (a.focus == focusFilter && a.bar.typing())
}

// showBean shows a bean in the detail pane, moving the tree cursor to it if
// it is in the (filtered) tree. record adds the current bean to the detail
// history.
func (a *App) showBean(id string, record bool) {
	if cur := a.detail.bean; cur != nil && record {
		a.detail.history = append(a.detail.history, cur.ID)
	}
	if a.selectInView(id) {
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
	fullPath, err := a.beanFilePath(b.ID, b.Path)
	if err != nil {
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil || !info.ModTime().After(a.editingBeanModTime) {
		return
	}
	// File was modified - read the user's changes, then call Update to set
	// updated_at (in the workspace for beans changed in one). Only the
	// edited file is read: reloading all beans would drop the workspace
	// versions of other beans.
	if edited, err := readEditedBean(fullPath, b); err == nil {
		_ = a.core.Update(edited, nil)
	}
}

// acceptsTextInput reports whether the active view currently routes typed
// characters into a text field, so batched runes must be kept intact.
func (a *App) acceptsTextInput() bool {
	switch a.state {
	case viewList:
		return a.typing()
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
	}
	return false
}

// releaseHeldKeysMsg delivers keys held back by handleKeys.
type releaseHeldKeysMsg struct{}

// handleKeys handles key messages one after another, as if they had been
// typed separately, splitting batched runes into single-key shortcuts.
//
// A key may return a command whose message changes the view (e.g. "s" opens
// the status picker via openStatusPickerMsg). The remaining keys, and any keys
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

// View renders the current view, painted with the theme's background (see
// ui.PaintBackground; a no-op with the terminal theme).
func (a *App) View() string {
	return ui.PaintBackground(a.view(), a.width, a.height)
}

// view renders the current view without the theme background.
func (a *App) view() string {
	if a.width < minAppWidth || a.height < minAppHeight {
		return a.tooSmallView()
	}
	switch a.state {
	case viewList:
		return a.browseView()
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
	case viewHelpOverlay:
		return a.helpOverlay.ModalView(a.browseView(), a.width, a.height)
	case viewWorkspaces:
		return overlayModal(a.browseView(), a.workspacesOverlayView(), a.width, a.height)
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

	// ASCII glyphs (tui.ascii, $BEANS_TUI_ASCII) must be chosen before any
	// view is built.
	ascii, asciiErr := cfg.GetTUIASCII()
	if asciiErr != nil {
		fmt.Fprintf(os.Stderr, "beans: warning: %v\n", asciiErr)
	}
	setASCII(ascii)

	app := New(core, cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())

	// Reload the theme when its sources change (see theme_reload.go).
	defer startThemeReload(cfg, themeSettings, p.Send)()

	// Store reference to program for sending messages from watcher
	app.program = p

	// Logs and warnings (worktree manager, worktree watchers) would draw over
	// the TUI; failures are reported in the UI instead.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
	core.SetWarnWriter(nil)
	defer core.SetWarnWriter(os.Stderr)

	// Show beans changed in workspaces (git worktrees) with their workspace
	// version; off outside git repositories.
	stopWorkspaces := setupWorkspaces(app, core, cfg, p.Send)
	defer stopWorkspaces()

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
