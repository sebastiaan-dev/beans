package beangraph

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// TestBlockingFiltersAreSymmetric checks that the blocking and blocked-by
// filters agree with the symmetric Bean.blocking and Bean.blockedBy fields
// (repos-hi8j): a link counts whichever bean records it.
func TestBlockingFiltersAreSymmetric(t *testing.T) {
	ctx := context.Background()
	r, _ := setupResolver(t)
	for _, b := range []*bean.Bean{
		// fwd blocks fwd-dep via its own blocking field
		{ID: "fwd", Title: "Forward blocker", Status: "todo", Blocking: []string{"fwd-dep"}},
		{ID: "fwd-dep", Title: "Forward dependent", Status: "todo"},
		// rev blocks rev-dep via rev-dep's blocked_by field
		{ID: "rev", Title: "Reverse blocker", Status: "todo"},
		{ID: "rev-dep", Title: "Reverse dependent", Status: "completed", BlockedBy: []string{"rev"}},
		// broken links don't count, like in the relation fields
		{ID: "broken", Title: "Broken", Status: "todo", Blocking: []string{"nope"}, BlockedBy: []string{"gone"}},
		{ID: "plain", Title: "Plain", Status: "todo"},
	} {
		if err := r.Core.Create(b); err != nil {
			t.Fatal(err)
		}
	}

	yes := true
	str := func(s string) *string { return &s }
	tests := []struct {
		name   string
		filter model.BeanFilter
		want   []string
	}{
		{"hasBlocking", model.BeanFilter{HasBlocking: &yes}, []string{"fwd", "rev"}},
		{"noBlocking", model.BeanFilter{NoBlocking: &yes}, []string{"broken", "fwd-dep", "plain", "rev-dep"}},
		{"blockingId forward", model.BeanFilter{BlockingID: str("fwd-dep")}, []string{"fwd"}},
		{"blockingId reverse", model.BeanFilter{BlockingID: str("rev-dep")}, []string{"rev"}},
		{"blockingId missing", model.BeanFilter{BlockingID: str("nope")}, nil},
		{"hasBlockedBy", model.BeanFilter{HasBlockedBy: &yes}, []string{"fwd-dep", "rev-dep"}},
		{"noBlockedBy", model.BeanFilter{NoBlockedBy: &yes}, []string{"broken", "fwd", "plain", "rev"}},
		{"blockedById forward", model.BeanFilter{BlockedByID: str("fwd")}, []string{"fwd-dep"}},
		{"blockedById reverse", model.BeanFilter{BlockedByID: str("rev")}, []string{"rev-dep"}},
		{"blockedById missing", model.BeanFilter{BlockedByID: str("gone")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Beans(ctx, &tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if ids := sortedIDs(got); !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("got %v, want %v", ids, tt.want)
			}
		})
	}

	// The filters agree with the relation fields for every bean.
	all, err := r.Beans(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	hasBlocking := map[string]bool{}
	for _, b := range mustBeans(t)(r.Beans(ctx, &model.BeanFilter{HasBlocking: &yes})) {
		hasBlocking[b.ID] = true
	}
	hasBlockedBy := map[string]bool{}
	for _, b := range mustBeans(t)(r.Beans(ctx, &model.BeanFilter{HasBlockedBy: &yes})) {
		hasBlockedBy[b.ID] = true
	}
	for _, b := range all {
		blocking := mustBeans(t)(r.BeanBlocking(ctx, b, nil))
		if (len(blocking) > 0) != hasBlocking[b.ID] {
			t.Errorf("%s: blocking field %v, hasBlocking %v", b.ID, sortedIDs(blocking), hasBlocking[b.ID])
		}
		blockedBy := mustBeans(t)(r.BeanBlockedBy(ctx, b, nil))
		if (len(blockedBy) > 0) != hasBlockedBy[b.ID] {
			t.Errorf("%s: blockedBy field %v, hasBlockedBy %v", b.ID, sortedIDs(blockedBy), hasBlockedBy[b.ID])
		}
	}
}

func sortedIDs(beans []*bean.Bean) []string {
	var ids []string
	for _, b := range beans {
		ids = append(ids, b.ID)
	}
	sort.Strings(ids)
	return ids
}

func mustBeans(t *testing.T) func([]*bean.Bean, error) []*bean.Bean {
	return func(beans []*bean.Bean, err error) []*bean.Bean {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return beans
	}
}
