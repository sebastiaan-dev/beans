package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestMousePaletteEntries(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 100, 30)
	pressKey(t, app, ":")
	screen := screenLines(app)
	items := app.hits.all(hitModalItem)
	if len(items) == 0 {
		t.Fatal("no palette entries recorded")
	}
	for _, reg := range items {
		e := app.palette.entries[reg.index]
		want := ""
		if e.cmd != nil {
			want = e.cmd.paletteTitle()
		} else {
			want = e.bean.Title
		}
		if got := cellText(screen, reg.r); !strings.Contains(got, want[:min(len(want), 20)]) {
			t.Errorf("entry %d region shows %q, want %q", reg.index, got, want)
		}
	}

	// The wheel moves the cursor; a click runs the entry.
	modal := region(t, app, hitModal, nil).r
	wheel(t, app, modal, true)
	if app.palette.cursor != 1 {
		t.Errorf("wheel: cursor %d", app.palette.cursor)
	}
	for _, reg := range items {
		if e := app.palette.entries[reg.index]; e.cmd != nil && e.cmd.id == "help" {
			click(t, app, reg.r)
		}
	}
	if app.state != viewHelpOverlay {
		t.Fatalf("clicking Show keyboard shortcuts: state %v", app.state)
	}
}

func TestMouseHelpWheelScrolls(t *testing.T) {
	withStableRendering(t)
	app := newDemoApp(t, 70, 24)
	pressKey(t, app, "?")
	wheel(t, app, region(t, app, hitModal, nil).r, true)
	if app.helpOverlay.offset != 1 {
		t.Errorf("wheel should scroll the help: offset %d", app.helpOverlay.offset)
	}
}

// TestOverlayModalWideBackground checks that the modal lands on the same
// cells whatever the background holds: wide runes (CJK, emoji) and ANSI
// styles before, under and after it.
func TestOverlayModalWideBackground(t *testing.T) {
	withStableRendering(t)
	const width, height = 20, 3
	modal := "[MODAL]" // 7 cells, centered at x = 6
	tests := []struct {
		name, bg, want string
	}{
		{"ascii", "abcdefghijklmnopqrst", "abcdef[MODAL]nopqrst"},
		{"cjk before", "漢字かなabcdefghijkl", "漢字か[MODAL]fghijkl"},
		{"emoji before", "🚀🚀🚀abcdefghijklmn", "🚀🚀🚀[MODAL]hijklmn"},
		// A wide rune cut by the modal's left or right edge becomes spaces.
		{"wide on left edge", "abcde漢fghijklmnopqr", "abcde [MODAL]lmnopqr"},
		{"wide on right edge", "abcdefghijkl漢mnopqr", "abcdef[MODAL] mnopqr"},
		{"styled", "\x1b[1;31m漢字\x1b[0m\x1b[38;2;1;2;3mabcdefghijklmnop\x1b[0m", "漢字ab[MODAL]jklmnop"},
		{"short", "漢", "漢    [MODAL]       "},
		{"too wide", "漢字かなabcdefghijklmnopqrstuvwxyz", "漢字か[MODAL]fghijkl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bg := tt.bg + "\n" + tt.bg + "\n" + tt.bg
			out := strings.Split(overlayModal(bg, modal, width, height), "\n")
			if len(out) != height {
				t.Fatalf("got %d lines, want %d", len(out), height)
			}
			if got := stripAnsi(out[1]); got != tt.want {
				t.Errorf("modal line = %q, want %q", got, tt.want)
			}
			if w := lipgloss.Width(out[1]); w != width {
				t.Errorf("modal line is %d cells wide, want %d", w, width)
			}
		})
	}
}
