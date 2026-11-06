package beangraph

import (
	"slices"
	"testing"
	"time"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

func beanIDs(beans []*bean.Bean) []string {
	out := make([]string, len(beans))
	for i, b := range beans {
		out[i] = b.ID
	}
	return out
}

func TestRankNextCandidates(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) *time.Time { ts := now.Add(-d); return &ts }
	prios := config.Default().PriorityNames()

	tests := []struct {
		name         string
		beans        []*bean.Bean
		openChildren map[string]int
		want         []string
	}{
		{
			name: "priority order",
			beans: []*bean.Bean{
				{ID: "low", Priority: "low", Type: "task"},
				{ID: "deferred", Priority: "deferred", Type: "task"},
				{ID: "crit", Priority: "critical", Type: "task"},
				{ID: "normal", Priority: "normal", Type: "task"},
				{ID: "high", Priority: "high", Type: "task"},
			},
			want: []string{"crit", "high", "normal", "low", "deferred"},
		},
		{
			name: "unset priority is normal, unknown last",
			beans: []*bean.Bean{
				{ID: "weird", Priority: "whatever", Type: "task"},
				{ID: "low", Priority: "low", Type: "task"},
				{ID: "unset", Type: "task", CreatedAt: at(time.Hour)},
				{ID: "normal", Priority: "normal", Type: "task", CreatedAt: at(time.Minute)},
			},
			want: []string{"unset", "normal", "low", "weird"},
		},
		{
			name: "leaf before container at equal priority",
			beans: []*bean.Bean{
				{ID: "epic", Type: "epic", CreatedAt: at(3 * time.Hour)},
				{ID: "milestone", Type: "milestone", CreatedAt: at(4 * time.Hour)},
				{ID: "feature", Type: "feature", CreatedAt: at(time.Hour)},
				{ID: "parent-task", Type: "task", CreatedAt: at(5 * time.Hour)},
			},
			openChildren: map[string]int{"parent-task": 1},
			want:         []string{"feature", "parent-task", "milestone", "epic"},
		},
		{
			name: "priority beats leaf preference",
			beans: []*bean.Bean{
				{ID: "task", Type: "task"},
				{ID: "epic", Type: "epic", Priority: "high"},
			},
			want: []string{"epic", "task"},
		},
		{
			name: "oldest first, missing dates last, then id",
			beans: []*bean.Bean{
				{ID: "b-nodate", Type: "bug"},
				{ID: "new", Type: "bug", CreatedAt: at(time.Minute)},
				{ID: "a-nodate", Type: "bug"},
				{ID: "old", Type: "bug", CreatedAt: at(time.Hour)},
			},
			want: []string{"old", "new", "a-nodate", "b-nodate"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RankNextCandidates(tt.beans, prios, tt.openChildren)
			if got := beanIDs(tt.beans); !slices.Equal(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}
