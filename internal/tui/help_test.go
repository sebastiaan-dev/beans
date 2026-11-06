package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestHelpOverlayUsesTerminalForeground guards against hardcoding a light
// foreground for help descriptions, which is invisible on light terminal themes.
func TestHelpOverlayUsesTerminalForeground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	view := newHelpOverlayModel(120, 40).View()

	const white = "38;2;255;255;255" // truecolor foreground #fff
	if strings.Contains(view, white) {
		t.Error("help overlay renders text with a hardcoded white foreground")
	}
	if !strings.Contains(view, "New bean / child") {
		t.Error("help overlay is missing shortcut descriptions")
	}
}
