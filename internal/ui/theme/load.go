package theme

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hmans/beans/pkg/config"
)

// DefaultCacheDir returns the directory for cached theme exports
// (<user cache dir>/beans/themes), or "" when there is no cache dir.
func DefaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "beans", "themes")
}

// Load returns the theme for spec. The terminal theme is nil: it has no
// colors of its own and uses the terminal's ANSI palette.
func Load(ctx context.Context, spec config.ThemeSpec, nvim NvimOptions) (*Theme, error) {
	switch spec.Kind {
	case config.ThemeNvim:
		if nvim.CacheDir == "" {
			nvim.CacheDir = DefaultCacheDir()
		}
		return LoadNvim(ctx, spec.Colorscheme, nvim)
	case config.ThemeFile:
		return LoadFile(spec.Path)
	default:
		return nil, nil
	}
}
