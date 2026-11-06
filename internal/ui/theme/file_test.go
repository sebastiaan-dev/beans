package theme

import "testing"

// TestParsePaletteNullSlots checks that null entries of the ansi list leave
// their slot unset instead of shifting the later colors (repos-6n7h).
func TestParsePaletteNullSlots(t *testing.T) {
	for name, in := range map[string]string{
		"flow null":  "ansi: [null, '#ff0000']\n",
		"flow tilde": "ansi: [~, '#ff0000']\n",
		"block":      "ansi:\n  -\n  - '#ff0000'\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePalette([]byte(in))
			if err != nil {
				t.Fatal(err)
			}
			if got.ANSI[1] != "#ff0000" {
				t.Errorf("ansi[1] = %q, want #ff0000 (slot shifted?)", got.ANSI[1])
			}
			// Slot 0 stays unset (nothing to derive it from), and slot 1 is
			// not copied into it.
			if got.ANSI[0] != "" {
				t.Errorf("ansi[0] = %q, want unset", got.ANSI[0])
			}
			if got.Danger != "#ff0000" {
				t.Errorf("danger = %q, want derived from ansi[1]", got.Danger)
			}
		})
	}
	// A list of 16 entries with nulls is still within the limit.
	in := "ansi: [null, null, null, null, null, null, null, null, null, null, null, null, null, null, null, '#00ff00']\n"
	got, err := ParsePalette([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.ANSI[15] != "#00ff00" {
		t.Errorf("ansi[15] = %q", got.ANSI[15])
	}
}
