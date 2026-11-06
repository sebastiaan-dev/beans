package theme

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// paletteFile is the on-disk palette format (JSON or YAML; JSON is valid
// YAML). It mirrors Theme's JSON form, but allows fewer than 16 ANSI slots.
type paletteFile struct {
	Name       string   `yaml:"name"`
	Background string   `yaml:"background"`
	Fg         string   `yaml:"fg"`
	Bg         string   `yaml:"bg"`
	Muted      string   `yaml:"muted"`
	Border     string   `yaml:"border"`
	Accent     string   `yaml:"accent"`
	Selection  string   `yaml:"selection"`
	Success    string   `yaml:"success"`
	Warning    string   `yaml:"warning"`
	Danger     string   `yaml:"danger"`
	Info       string   `yaml:"info"`
	Hint       string   `yaml:"hint"`
	ANSI       []string `yaml:"ansi"`
}

// LoadFile reads a palette file. Example (YAML):
//
//	name: my-theme
//	background: dark        # or light
//	fg: "#c5c9c7"
//	bg: "#22262d"
//	muted: "#75797f"
//	border: "#393b44"
//	accent: "#8ba4b0"
//	selection: "#393b44"
//	success: "#98bb6c"
//	warning: "#dca561"
//	danger: "#c34043"
//	info: "#658594"
//	hint: "#6a9589"
//	ansi: ["#22262d", "#c4746e", ...]   # up to 16 slots, 0-15
//
// Every key is optional; unset roles and slots are derived from each other.
// Unknown keys and invalid colors are errors.
func LoadFile(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading theme file: %w", err)
	}
	t, err := ParsePalette(data)
	if err != nil {
		return nil, fmt.Errorf("theme file %s: %w", path, err)
	}
	return t, nil
}

// ParsePalette parses palette file contents (see LoadFile).
func ParsePalette(data []byte) (*Theme, error) {
	var p paletteFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty palette")
		}
		return nil, err
	}
	if len(p.ANSI) > ANSISlots {
		return nil, fmt.Errorf("ansi: %d colors given, at most %d allowed", len(p.ANSI), ANSISlots)
	}
	t := &Theme{
		Name: p.Name, Background: p.Background,
		Fg: p.Fg, Bg: p.Bg, Muted: p.Muted, Border: p.Border, Accent: p.Accent,
		Selection: p.Selection, Success: p.Success, Warning: p.Warning,
		Danger: p.Danger, Info: p.Info, Hint: p.Hint,
	}
	copy(t.ANSI[:], p.ANSI)
	if err := t.normalize(); err != nil {
		return nil, err
	}
	if t.isEmpty() {
		return nil, errors.New("palette defines no colors")
	}
	t.complete()
	return t, nil
}
