package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// smallStates are browse view states checked at small terminal sizes.
var smallStates = []struct {
	name string
	keys []string
}{
	{"tree", nil},
	{"detail", []string{"j", "j", "enter"}},
	{"detail-body", []string{"j", "j", "enter", "tab"}},
	{"filter-bar", []string{"F"}},
	{"dropdown-type", []string{"F", "l", "enter"}},
	{"dropdown-tags", []string{"g", "t"}},
	{"search", []string{"/", "x"}},
}

// TestSmallTerminalFrames checks the frame at every size from the minimum
// (40x12) up to 80x24: every line exactly the terminal width, the outer
// frame intact and every box closed.
func TestSmallTerminalFrames(t *testing.T) {
	withStableRendering(t)
	for _, ascii := range []bool{false, true} {
		setASCII(ascii)
		for _, st := range smallStates {
			for w := 40; w <= 80; w += 5 {
				for h := 12; h <= 24; h++ {
					if ascii && h%4 != 0 {
						continue // ASCII lays out like Unicode; spot-check it
					}
					app := newDemoApp(t, w, h)
					pressKeys(t, app, st.keys...)
					screen := stripAnsi(app.View())
					t.Run(fmt.Sprintf("%s/%dx%d/ascii=%v", st.name, w, h, ascii), func(t *testing.T) {
						checkFrame(t, screen, w, h)
						if strings.Contains(screen, "Terminal too small") {
							t.Errorf("minimum size or larger reported as too small")
						}
						if !ascii && (st.name == "tree" || st.name == "filter-bar" || st.name == "search") {
							checkBoxesClosed(t, screen)
						}
					})
				}
			}
		}
	}
	setASCII(false)
}

// checkBoxesClosed asserts that every box opened inside the frame (a top
// left corner) is closed in the same column further down, i.e. no box is
// cut off at the bottom.
func checkBoxesClosed(t *testing.T, screen string) {
	t.Helper()
	g := glyphs() // Unicode: ASCII corners are ambiguous
	lines := strings.Split(screen, "\n")
	grid := make([][]string, len(lines))
	for i, l := range lines {
		for _, r := range l {
			grid[i] = append(grid[i], string(r))
		}
	}
	for i := 1; i < len(grid)-1; i++ {
		for j := 1; j < len(grid[i])-1; j++ {
			if grid[i][j] != g.TopLeft {
				continue
			}
			closed := false
			for k := i + 1; k < len(grid)-1; k++ {
				if j < len(grid[k]) && grid[k][j] == g.BottomLeft {
					closed = true
					break
				}
				if j >= len(grid[k]) || grid[k][j] != g.Vertical {
					break
				}
			}
			if !closed {
				t.Errorf("box at line %d, column %d is not closed:\n%s", i, j, screen)
				return
			}
		}
	}
}

// TestDetailScrollsAsAWhole checks that on short terminals all detail boxes
// stay reachable: the pane scrolls instead of cutting off the lower boxes.
func TestDetailScrollsAsAWhole(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{50, 16}, {60, 18}, {40, 12}, {70, 14}, {120, 18}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			app := newDemoApp(t, size[0], size[1])
			pressKeys(t, app, "j", "j", "enter")
			if !app.detail.layout().scroll {
				t.Fatalf("detail pane does not scroll at %dx%d", size[0], size[1])
			}
			seen := map[string]bool{}
			collect := func() {
				screen := stripAnsi(app.View())
				checkFrame(t, screen, size[0], size[1])
				for _, label := range []string{"Title", "Status", "Parent", "Body", "Relations", "History", "TOTP support", "Recovery codes", "t003", "ETag"} {
					if strings.Contains(screen, label) {
						seen[label] = true
					}
				}
			}
			collect()
			// The relations are focused first and visible with their cursor.
			if screen := stripAnsi(app.View()); !strings.Contains(screen, glyphs().Cursor+" parent") {
				t.Errorf("relation cursor not visible:\n%s", screen)
			}
			pressKeys(t, app, "tab", "g", "g") // body: scroll through the whole pane
			for i := 0; i < 40; i++ {
				collect()
				pressKey(t, app, "j")
			}
			collect()
			for _, label := range []string{"Title", "Status", "Parent", "Body", "Relations", "History", "TOTP support", "Recovery codes", "t003", "ETag"} {
				if !seen[label] {
					t.Errorf("%q never shown while scrolling", label)
				}
			}
			// The end shows the history box completely; home shows the title.
			pressKey(t, app, "G")
			screen := stripAnsi(app.View())
			if !strings.Contains(screen, "ETag") || !strings.Contains(screen, glyphs().BottomLeft+glyphs().Horizontal+glyphs().Horizontal) {
				t.Errorf("end does not show the history:\n%s", screen)
			}
			app.Update(tea.KeyMsg{Type: tea.KeyHome})
			if screen := stripAnsi(app.View()); !strings.Contains(screen, "Title") {
				t.Errorf("home does not show the title:\n%s", screen)
			}
			// Moving the checkbox cursor keeps it visible.
			pressKey(t, app, "]")
			if screen := stripAnsi(app.View()); !strings.Contains(screen, "Recovery codes") {
				t.Errorf("current checkbox not visible:\n%s", screen)
			}
		})
	}

	// Large enough terminals do not scroll the pane.
	app := newDemoApp(t, 70, 24)
	pressKeys(t, app, "j", "j", "enter")
	if app.detail.layout().scroll || app.detail.paneOffset != 0 {
		t.Error("detail pane scrolls at 70x24")
	}
}

// TestShortTerminalTreeOnly checks that wide but short terminals show only
// the tree, with the detail pane on enter.
func TestShortTerminalTreeOnly(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 140, wideMinHeight-1)
	if app.layout().wide {
		t.Fatal("short terminal uses side-by-side panes")
	}
	if screen := stripAnsi(app.View()); strings.Contains(screen, "Title") || !strings.Contains(screen, "Beans") {
		t.Errorf("short terminal should show the tree only:\n%s", screen)
	}
	pressKeys(t, app, "j", "j", "enter")
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "Relations") || strings.Contains(screen, "Beans") {
		t.Errorf("enter should show the detail pane instead of the tree:\n%s", screen)
	}
	app.Update(tea.WindowSizeMsg{Width: 140, Height: wideMinHeight})
	if !app.layout().wide {
		t.Error("140x16 should use side-by-side panes")
	}
}

func TestTerminalTooSmall(t *testing.T) {
	withStableRendering(t)
	for _, size := range [][2]int{{39, 12}, {40, 11}, {20, 8}, {10, 2}} {
		app := newDemoApp(t, size[0], size[1])
		for _, keys := range [][]string{nil, {"?"}, {"j", "s"}} {
			pressKeys(t, app, keys...)
			screen := stripAnsi(app.View())
			if !strings.Contains(screen, "Terminal") {
				t.Errorf("%dx%d %v: no too-small message:\n%s", size[0], size[1], keys, screen)
			}
			lines := strings.Split(screen, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
			}
			for _, l := range lines {
				if len([]rune(l)) != size[0] {
					t.Errorf("%dx%d: line %q has the wrong width", size[0], size[1], l)
				}
			}
			pressKey(t, app, "esc")
		}
	}
	app := newDemoApp(t, 39, 12)
	if screen := stripAnsi(app.View()); !strings.Contains(screen, "39x12, beans needs 40x12") {
		t.Errorf("message lacks the sizes:\n%s", screen)
	}
}
