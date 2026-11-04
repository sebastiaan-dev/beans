package version

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name                              string
		version, commit, date             string
		info                              *debug.BuildInfo
		wantVersion, wantCommit, wantDate string
	}{
		{
			name:    "ldflags win over build info",
			version: "v1.2.3", commit: "abc1234", date: "2026-01-01T00:00:00Z",
			info:        &debug.BuildInfo{Main: debug.Module{Version: "v0.0.1"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "ffffffffffff"}}},
			wantVersion: "v1.2.3", wantCommit: "abc1234", wantDate: "2026-01-01T00:00:00Z",
		},
		{
			name:    "go install from module proxy",
			version: "dev", commit: "unknown", date: "unknown",
			info:        &debug.BuildInfo{Main: debug.Module{Version: "v0.4.2"}},
			wantVersion: "v0.4.2", wantCommit: "unknown", wantDate: "unknown",
		},
		{
			name:    "local go build with vcs info",
			version: "dev", commit: "unknown", date: "unknown",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef"},
				{Key: "vcs.modified", Value: "true"},
			}},
			wantVersion: "dev", wantCommit: "0123456-dirty", wantDate: "unknown",
		},
		{
			name:    "no build info",
			version: "dev", commit: "unknown", date: "unknown",
			wantVersion: "dev", wantCommit: "unknown", wantDate: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, c, d := resolve(tt.version, tt.commit, tt.date, tt.info)
			if v != tt.wantVersion || c != tt.wantCommit || d != tt.wantDate {
				t.Errorf("resolve() = %q, %q, %q; want %q, %q, %q", v, c, d, tt.wantVersion, tt.wantCommit, tt.wantDate)
			}
		})
	}
}
