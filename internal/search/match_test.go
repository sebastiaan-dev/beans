package search

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/bean"
)

// testCorpus builds a deterministic corpus large enough to span many Match chunks.
func testCorpus(n int) []*bean.Bean {
	rng := rand.New(rand.NewSource(42))
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
		"india", "juliet", "kilo", "lima", "login", "logout", "authentication", "user", "password", "the", "and"}
	pick := func(k int) string {
		ws := make([]string, k)
		for i := range ws {
			ws[i] = words[rng.Intn(len(words))]
		}
		return strings.Join(ws, " ")
	}
	beans := make([]*bean.Bean, n)
	for i := range beans {
		title := pick(3)
		beans[i] = &bean.Bean{
			ID:    fmt.Sprintf("test-%04d", i),
			Slug:  bean.Slugify(title),
			Title: title,
			Body:  pick(30) + fmt.Sprintf(" number %d", i%50),
		}
	}
	return beans
}

func TestMatchEqualsIndexSearch(t *testing.T) {
	beans := testCorpus(3*matchChunkSize + 17)

	idx := setupTestIndex(t)
	if err := idx.IndexBeans(beans); err != nil {
		t.Fatalf("IndexBeans() error = %v", err)
	}

	queries := []string{
		"login",
		"LOGIN",
		"title:alpha",
		"slug:bravo",
		"body:charlie",
		"logn~1",
		"auth*",
		"lo?in",
		`"user login"`,
		`title:"alpha bravo"`,
		"+alpha +bravo",
		"+alpha -bravo",
		"alpha bravo",
		"-alpha",
		"/log(in|out)/",
		"42",
		"test-0007",
		"the",
		"nonexistent",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			want, err := idx.Search(q, -1)
			if err != nil {
				t.Fatalf("Index.Search() error = %v", err)
			}
			got, err := Match(beans, q)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			slices.Sort(want)
			gotSorted := slices.Clone(got)
			slices.Sort(gotSorted)
			if !slices.Equal(gotSorted, want) {
				t.Errorf("Match(%q) returned %d IDs, Index.Search returned %d; sets differ", q, len(got), len(want))
			}
		})
	}
}

func TestMatchPreservesInputOrder(t *testing.T) {
	beans := testCorpus(2*matchChunkSize + 5)
	got, err := Match(beans, "alpha")
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Match() returned no results")
	}
	pos := make(map[string]int, len(beans))
	for i, b := range beans {
		pos[b.ID] = i
	}
	if !slices.IsSortedFunc(got, func(a, b string) int { return pos[a] - pos[b] }) {
		t.Error("Match() results are not in input order")
	}
}

func TestMatchReturnsAllResults(t *testing.T) {
	n := DefaultSearchLimit + 500
	beans := make([]*bean.Bean, n)
	for i := range beans {
		beans[i] = &bean.Bean{ID: fmt.Sprintf("b%05d", i), Title: "common title"}
	}
	got, err := Match(beans, "common")
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if len(got) != n {
		t.Errorf("Match() returned %d results, want %d", len(got), n)
	}
}

func TestMatchInvalidQuery(t *testing.T) {
	if _, err := Match(nil, `"unterminated`); err == nil {
		t.Error("Match() with invalid query succeeded, want error")
	}
}

func TestMatchEmpty(t *testing.T) {
	got, err := Match(nil, "anything")
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Match() on no beans = %v, want none", got)
	}
}

func TestSearch_AllResults(t *testing.T) {
	idx := setupTestIndex(t)
	n := DefaultSearchLimit + 10
	beans := make([]*bean.Bean, n)
	for i := range beans {
		beans[i] = &bean.Bean{ID: fmt.Sprintf("b%05d", i), Title: "common title"}
	}
	if err := idx.IndexBeans(beans); err != nil {
		t.Fatalf("IndexBeans() error = %v", err)
	}
	got, err := idx.Search("common", -1)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != n {
		t.Errorf("Search(-1) returned %d results, want %d", len(got), n)
	}
}
