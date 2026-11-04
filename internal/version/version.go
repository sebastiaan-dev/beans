package version

import (
	"fmt"
	"runtime/debug"
)

// Set via ldflags at build time (release builds and `mise build`).
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a formatted version string.
//
// Builds without ldflags (e.g. `go install github.com/hmans/beans/cmd/beans@v0.4.2`
// or a plain `go build`) fall back to the module version and VCS metadata that
// the Go toolchain embeds in the binary.
func String() string {
	info, _ := debug.ReadBuildInfo()
	v, c, d := resolve(Version, Commit, Date, info)
	return fmt.Sprintf("beans %s (%s) built %s", v, c, d)
}

// resolve fills in unset (default) version and commit from build info. The
// build date is left alone: build info only records the commit time.
func resolve(version, commit, date string, info *debug.BuildInfo) (string, string, string) {
	if info == nil {
		return version, commit, date
	}

	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}

	var revision string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if commit == "unknown" && revision != "" {
		commit = revision
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if modified {
			commit += "-dirty"
		}
	}
	return version, commit, date
}
