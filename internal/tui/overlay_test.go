package tui

import (
	"strings"
	"testing"
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
