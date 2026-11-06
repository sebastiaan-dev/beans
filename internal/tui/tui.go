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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/config"
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

// App is the main TUI application model
type App struct {
	// The panes of the browse view (browse.go).
	list   listModel      // the bean tree and the filter query (list.go)
	detail detailModel    // the detail pane (detail.go)
	bar    filterBarModel // the filter bar (filterbar.go)

	// The open overlay (overlay.go) and the overlays' state.
	state          viewState
	picker         pickerModel       // picker.go
	helpOverlay    helpOverlayModel  // help.go
	wsOverlay      workspacesOverlay // workspaces.go
	palette        commandPalette    // cmdpalette.go
	recentCommands []string          // IDs of the palette's recently run commands

	ws       *workspaces // workspace state (see workspaces.go)
	core     *beancore.Core
	resolver *beangraph.CoreResolver
	config   *config.Config
	width    int
	height   int
	program  *tea.Program // reference to program for sending messages from watcher

	// Views beyond the tree: Ready, Blocked, Audit (views.go)
	views viewsState

	// The reader (reader.go): one bean on the whole screen; nil if closed.
	reader *readerModel

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

	// Undo history of changes made in this session (undo.go)
	undo undoState

	// Mouse support (mouse.go): the regions of the last rendered screen
	// and the click state.
	hits    *hitMap
	mouse   mouseState
	mouseOn bool // mouse reporting is enabled

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

func (a *App) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	if a.handleQuickActionMsg(msg) || a.handleViewMsg(msg) {
		return nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.MouseMsg:
		return a.handleMouse(msg)

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
		a.readerBeansLoaded()
		return cmd

	case errMsg:
		a.list, cmd = a.list.Update(msg)
		return cmd

	case beansChangedMsg:
		// Beans changed on disk - refresh (and the workspaces' status)
		return tea.Batch(a.list.loadBeans, a.ws.refreshCmd(a.core))

	case workspacesChangedMsg:
		return a.ws.refreshCmd(a.core)

	case workspacesLoadedMsg:
		return a.ws.apply(msg, a.core)

	case workspaceStartedMsg:
		return tea.Batch(a.handleWorkspaceStarted(msg), a.restoreMouse())

	case themeReloadedMsg:
		return a.applyThemeReload(msg)

	case openPickerMsg:
		return a.openPicker(msg)

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

	case statusSelectedMsg:
		a.trackChange(a.changeLabel("status", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Status: &msg.status})
		})
		return a.closePicker(true)

	case typeSelectedMsg:
		a.trackChange(a.changeLabel("type", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Type: &msg.beanType})
		})
		return a.closePicker(true)

	case prioritySelectedMsg:
		a.trackChange(a.changeLabel("priority", msg.beanIDs), msg.beanIDs, func() {
			a.updateBeans(msg.beanIDs, model.UpdateBeanInput{Priority: &msg.priority})
		})
		return a.closePicker(true)

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
		return tea.Batch(a.list.loadBeans, a.restoreMouse())

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

	// Other messages go to the open overlay (e.g. the palette's text input).
	if o := a.activeOverlay(); o != nil {
		return o.update(a, msg)
	}
	return nil
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

// View renders the current view, painted with the theme's background (see
// ui.PaintBackground; a no-op with the terminal theme).
func (a *App) View() string {
	return ui.PaintBackground(a.view(), a.width, a.height)
}

// view renders the current view without the theme background.
// It records the clickable regions of the screen for the mouse (mouse.go).
func (a *App) view() string {
	a.hits = &hitMap{}
	if a.width < minAppWidth || a.height < minAppHeight {
		return a.tooSmallView()
	}
	if o := a.activeOverlay(); o != nil {
		return a.overlayView(o)
	}
	return a.baseView()
}

// baseView renders the view under the overlays: the reader if it is open,
// else the browse view.
func (a *App) baseView() string {
	if a.reader != nil {
		return a.readerView()
	}
	return a.browseView()
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
	app.mouseOn = mouseEnabled()
	p := tea.NewProgram(app, programOptions(app.mouseOn)...)

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
