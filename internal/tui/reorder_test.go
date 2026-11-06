package tui

import (
	"sort"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/bean"
)

// applyOrder applies order changes to copies of group and returns the IDs
// sorted by order key like the tree (ties by ID).
func applyOrder(group []*bean.Bean, changes map[string]string) []string {
	type kv struct{ id, order string }
	var out []kv
	for _, b := range group {
		o := b.Order
		if k, ok := changes[b.ID]; ok {
			o = k
		}
		out = append(out, kv{b.ID, o})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.order != "") != (b.order != "") {
			return a.order != ""
		}
		return a.order < b.order
	})
	ids := make([]string, len(out))
	for i, x := range out {
		ids[i] = x.id
	}
	return ids
}

func beansWithOrders(orders ...string) []*bean.Bean {
	group := make([]*bean.Bean, len(orders))
	for i, o := range orders {
		group[i] = &bean.Bean{ID: string(rune('a' + i)), Order: o}
	}
	return group
}

func TestPlanOrderMove(t *testing.T) {
	tests := []struct {
		name    string
		orders  []string
		id, to  string
		after   bool
		want    string // resulting order of IDs
		changes int    // number of beans written
	}{
		{"up among ordered", []string{"A", "M", "a"}, "c", "b", false, "acb", 1},
		{"down among ordered", []string{"A", "M", "a"}, "a", "b", true, "bac", 1},
		{"to the top", []string{"A", "M", "a"}, "c", "a", false, "cab", 1},
		{"to the bottom", []string{"A", "M", "a"}, "a", "c", true, "bca", 1},
		{"unordered get keys", []string{"", "", ""}, "c", "b", false, "acb", 3},
		{"partly ordered", []string{"M", "", ""}, "b", "c", true, "acb", 2},
		{"duplicates renumber", []string{"M", "M", "M"}, "c", "a", false, "cab", 3},
		// Nothing sorts between "V0" and "V00".
		{"no room renumbers", []string{"V", "V0", "V00"}, "a", "b", true, "bac", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := beansWithOrders(tt.orders...)
			changes := planOrderMove(group, tt.id, tt.to, tt.after)
			if got := strings.Join(applyOrder(group, changes), ""); got != tt.want {
				t.Errorf("order = %s, want %s (changes %v)", got, tt.want, changes)
			}
			if len(changes) != tt.changes {
				t.Errorf("wrote %d keys, want %d: %v", len(changes), tt.changes, changes)
			}
			keys := map[string]bool{}
			for _, b := range group {
				k := b.Order
				if c, ok := changes[b.ID]; ok {
					k = c
				}
				if k == "" || keys[k] {
					t.Errorf("key %q of %s is empty or duplicate", k, b.ID)
				}
				keys[k] = true
			}
		})
	}
	if planOrderMove(beansWithOrders("A", "B"), "a", "zz", false) != nil {
		t.Error("unknown target must give no plan")
	}
}

func TestOrderAfter(t *testing.T) {
	group := beansWithOrders("A", "M", "", "")
	// c is the new bean (unordered) to place after a.
	if k := orderAfter(group, "c", "a"); k <= "A" || k >= "M" {
		t.Errorf("key after a = %q, want between A and M", k)
	}
	if k := orderAfter(group, "c", "b"); k <= "M" {
		t.Errorf("key after the last ordered bean = %q, want > M", k)
	}
	if k := orderAfter(group, "c", "d"); k != "" {
		t.Errorf("unordered anchor must not place the bean, got %q", k)
	}
}
