package filterquery

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hmans/beans/pkg/bean"
)

// The cases mirror frontend/src/lib/filter.test.ts so that the query
// language behaves the same in the Beans UI, the TUI and the CLI.

func mk(id string, opts ...func(*bean.Bean)) *bean.Bean {
	b := &bean.Bean{ID: id, Title: id, Status: "todo", Type: "task", Path: id + ".md"}
	for _, o := range opts {
		o(b)
	}
	return b
}

func title(s string) func(*bean.Bean)     { return func(b *bean.Bean) { b.Title = s } }
func status(s string) func(*bean.Bean)    { return func(b *bean.Bean) { b.Status = s } }
func typ(s string) func(*bean.Bean)       { return func(b *bean.Bean) { b.Type = s } }
func prio(s string) func(*bean.Bean)      { return func(b *bean.Bean) { b.Priority = s } }
func slug(s string) func(*bean.Bean)      { return func(b *bean.Bean) { b.Slug = s } }
func parent(s string) func(*bean.Bean)    { return func(b *bean.Bean) { b.Parent = s } }
func tags(s ...string) func(*bean.Bean)   { return func(b *bean.Bean) { b.Tags = s } }
func blocks(s ...string) func(*bean.Bean) { return func(b *bean.Bean) { b.Blocking = s } }
func blockedBy(s ...string) func(*bean.Bean) {
	return func(b *bean.Bean) { b.BlockedBy = s }
}
func archived() func(*bean.Bean) { return func(b *bean.Bean) { b.Path = "archive/" + b.ID + ".md" } }

func run(beans []*bean.Bean, query string) []string {
	match := Compile(Parse(query), NewContext(beans))
	ids := []string{}
	for _, b := range beans {
		if match(b) {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

func TestTokenize(t *testing.T) {
	var raws []string
	for _, tok := range Tokenize(` a  parent:"My Epic",b "two words"`) {
		raws = append(raws, tok.Raw)
	}
	if want := []string{"a", `parent:"My Epic",b`, `"two words"`}; !reflect.DeepEqual(raws, want) {
		t.Errorf("tokens = %q, want %q", raws, want)
	}
	if got, want := Tokenize("ab cd"), []Token{{"ab", 0, 2}, {"cd", 3, 5}}; !reflect.DeepEqual(got, want) {
		t.Errorf("positions = %+v, want %+v", got, want)
	}
	// Offsets are byte offsets, also after multi-byte runes.
	if got, want := Tokenize("ä b"), []Token{{"ä", 0, 2}, {"b", 3, 4}}; !reflect.DeepEqual(got, want) {
		t.Errorf("multi-byte positions = %+v, want %+v", got, want)
	}
}

func TestParse(t *testing.T) {
	cl := func(f Field, neg bool, op Op, values ...string) Term {
		if values == nil {
			values = []string{}
		}
		return Term{Field: f, Negate: neg, Op: op, Values: values}
	}
	cases := []struct {
		query string
		terms []Term
	}{
		{"", nil},
		{"auth", []Term{Text("auth")}},
		{"status:todo,Draft", []Term{cl(FieldStatus, false, OpEq, "todo", "draft")}},
		{"priority:>=high", []Term{cl(FieldPriority, false, OpGE, "high")}},
		{"-type:bug", []Term{cl(FieldType, true, OpEq, "bug")}},
		{"tags:ui prio:low under:abc", []Term{
			cl(FieldTag, false, OpEq, "ui"),
			cl(FieldPriority, false, OpEq, "low"),
			cl(FieldParent, false, OpEq, "abc"),
		}},
		{`milestone:"Version 0.2"`, []Term{cl(FieldMilestone, false, OpEq, "Version 0.2")}},
		{"status:", []Term{cl(FieldStatus, false, OpEq)}},
		{"http://x", []Term{Text("http://x")}},
		{`"two words"`, []Term{Text("two words")}},
		{`""`, nil},
		{"Ready:Yes -done:no archived:no", []Term{
			cl(FieldReady, false, OpEq, "yes"),
			cl(FieldDone, true, OpEq, "no"),
			cl(FieldArchived, false, OpEq, "no"),
		}},
		// Comparisons are only parsed for priority.
		{"status:>=todo", []Term{cl(FieldStatus, false, OpEq, ">=todo")}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			if got := Parse(tc.query).Terms; !reflect.DeepEqual(got, tc.terms) {
				t.Errorf("Parse(%q) = %+v, want %+v", tc.query, got, tc.terms)
			}
		})
	}

	if !Parse("status: ").IsEmpty() {
		t.Error("incomplete clauses should be empty filters")
	}
	if Parse("status:todo").IsEmpty() {
		t.Error("status:todo is not empty")
	}
}

func TestFormatRoundTrip(t *testing.T) {
	for _, q := range []string{
		"status:todo,draft priority:>=high auth",
		`-tag:ui milestone:"Version 0.2" "two words"`,
		"blocked:no",
		"ready:yes done:no archived:yes",
	} {
		if got := Parse(q).String(); got != q {
			t.Errorf("format(parse(%q)) = %q", q, got)
		}
	}
}

func TestInvalidValues(t *testing.T) {
	clause := func(q string) Term { return Parse(q).Terms[0] }
	cases := []struct {
		query string
		want  []string
	}{
		{"status:todo,nope", []string{"nope"}},
		{"priority:>=urgent", []string{"urgent"}},
		{"blocked:maybe", []string{"maybe"}},
		{"ready:sometimes", []string{"sometimes"}},
		{"tag:anything", nil},
	}
	for _, tc := range cases {
		if got := InvalidValues(clause(tc.query)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("InvalidValues(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestEditing(t *testing.T) {
	t.Run("toggles values in the plain clause", func(t *testing.T) {
		q := Parse("auth")
		q = q.ToggleValue(FieldStatus, "todo")
		if got := q.String(); got != "auth status:todo" {
			t.Errorf("got %q", got)
		}
		q = q.ToggleValue(FieldStatus, "draft")
		if got := q.String(); got != "auth status:todo,draft" {
			t.Errorf("got %q", got)
		}
		if got := q.SelectedValues(FieldStatus); !reflect.DeepEqual(got, []string{"todo", "draft"}) {
			t.Errorf("selected = %q", got)
		}
		q = q.ToggleValue(FieldStatus, "todo").ToggleValue(FieldStatus, "draft")
		if got := q.String(); got != "auth" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("leaves negated and comparison clauses alone", func(t *testing.T) {
		q := Parse("priority:>=high -priority:critical").ToggleValue(FieldPriority, "low")
		if got := q.String(); got != "priority:>=high -priority:critical priority:low" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("replaces clauses with SetValues in place", func(t *testing.T) {
		q := Parse("a parent:x b parent:y").SetValues(FieldParent, "z")
		if got := q.String(); got != "a parent:z b" {
			t.Errorf("got %q", got)
		}
		if got := q.SetValues(FieldParent).String(); got != "a b" {
			t.Errorf("got %q", got)
		}
		cmp := Parse("x priority:high").SetClause(Term{Field: FieldPriority, Op: OpLE, Values: []string{"low"}})
		if got := cmp.String(); got != "x priority:<=low" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("sets and reads booleans", func(t *testing.T) {
		yes, no := true, false
		q := Parse("x").SetBool(FieldBlocked, &yes)
		if got := q.String(); got != "x blocked:yes" {
			t.Errorf("got %q", got)
		}
		if v, ok := q.BoolSelection(FieldBlocked); !ok || !v {
			t.Errorf("selection = %v %v", v, ok)
		}
		q = q.SetBool(FieldBlocked, &no)
		if v, ok := q.BoolSelection(FieldBlocked); !ok || v {
			t.Errorf("selection = %v %v", v, ok)
		}
		if _, ok := q.SetBool(FieldBlocked, nil).BoolSelection(FieldBlocked); ok {
			t.Error("cleared selection should be unset")
		}
	})

	t.Run("removes terms and clears clauses", func(t *testing.T) {
		q := Parse("status:todo auth tag:ui")
		if got := q.RemoveTerm(0).String(); got != "auth tag:ui" {
			t.Errorf("got %q", got)
		}
		if got := q.ClearClauses().String(); got != "auth" {
			t.Errorf("got %q", got)
		}
		if got := q.String(); got != "status:todo auth tag:ui" {
			t.Errorf("editing must not modify the original: %q", got)
		}
	})

	t.Run("does not alias the original's values", func(t *testing.T) {
		q := Parse("status:todo,draft")
		_ = q.ToggleValue(FieldStatus, "completed")
		r := q.ToggleValue(FieldStatus, "todo")
		if got := q.String(); got != "status:todo,draft" {
			t.Errorf("original changed: %q", got)
		}
		if got := r.String(); got != "status:draft" {
			t.Errorf("got %q", got)
		}
	})
}

func TestCompile(t *testing.T) {
	beans := []*bean.Bean{
		mk("beans-m1", typ("milestone"), title("Version 0.2"), slug("version-0-2")),
		mk("beans-e1", typ("epic"), title("Login Epic"), parent("beans-m1")),
		mk("beans-a", title("Auth Feature"), typ("feature"), prio("high"), tags("UI"), parent("beans-e1")),
		mk("beans-b", title("DB Bug"), typ("bug"), status("draft"), prio("critical")),
		mk("beans-c", title("Cleanup"), status("in-progress"), prio("low"), tags("chore")),
		mk("beans-d", title("Default prio"), status("completed")),
		mk("beans-x", title("Blocker"), blocks("beans-c")),
		mk("beans-y", title("Done blocker"), status("completed"), blocks("beans-d")),
		mk("beans-z", title("Blocked via field"), blockedBy("beans-b")),
		mk("beans-zc", title("Child of blocked"), parent("beans-z")),
	}
	all := make([]string, len(beans))
	for i, b := range beans {
		all[i] = b.ID
	}

	cases := []struct {
		query string
		want  []string
	}{
		{"", all},
		{"auth", []string{"beans-a"}},
		{"AUTH feature", []string{"beans-a"}},
		{"bug", []string{"beans-b"}},
		{"status:draft,in-progress", []string{"beans-b", "beans-c"}},
		{"-status:todo", []string{"beans-b", "beans-c", "beans-d", "beans-y"}},
		{"type:bug,feature", []string{"beans-a", "beans-b"}},
		{"priority:>=high", []string{"beans-a", "beans-b"}},
		{"priority:>high", []string{"beans-b"}},
		{"priority:<=low", []string{"beans-c"}},
		{"priority:<normal", []string{"beans-c"}},
		{"priority:normal status:completed", []string{"beans-d", "beans-y"}},
		{"tag:ui", []string{"beans-a"}},
		{"tag:ui,chore", []string{"beans-a", "beans-c"}},
		{"milestone:version-0-2", []string{"beans-e1", "beans-a"}},
		{`milestone:"version 0.2"`, []string{"beans-e1", "beans-a"}},
		{"milestone:m1", []string{"beans-e1", "beans-a"}},
		{"epic:beans-m1", []string{}},
		{`epic:"Login Epic"`, []string{"beans-a"}},
		{"parent:beans-e1", []string{"beans-a"}},
		{"parent:nope", []string{}},
		{"blocked:yes", []string{"beans-c", "beans-z", "beans-zc"}},
		{"blocked:no type:bug", []string{"beans-b"}},
		{"status:nope", []string{}},
		{"status: auth", []string{"beans-a"}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			if got := run(beans, tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("run(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}

	t.Run("survives parent cycles", func(t *testing.T) {
		cyclic := []*bean.Bean{mk("a", parent("b")), mk("b", parent("a"))}
		if got := run(cyclic, "parent:a"); !reflect.DeepEqual(got, []string{"b"}) {
			t.Errorf("parent:a = %v", got)
		}
		if got := run(cyclic, "blocked:no"); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("blocked:no = %v", got)
		}
		if got := run(cyclic, "done:no ready:yes"); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("done:no ready:yes = %v", got)
		}
	})
}

// TestCompileExtensions covers the fields the TUI and CLI add to the Beans
// UI's syntax.
func TestCompileExtensions(t *testing.T) {
	beans := []*bean.Bean{
		mk("todo"),
		mk("draft", status("draft")),
		mk("wip", status("in-progress")),
		mk("done", status("completed"), typ("epic")),
		mk("scrapped", status("scrapped")),
		mk("child-of-done", parent("done")),
		mk("grandchild-of-done", parent("child-of-done")),
		mk("blocker", blocks("blocked")),
		mk("blocked"),
		mk("child-of-blocked", parent("blocked")),
		mk("blocked-by-done", blockedBy("done")),
		mk("old", status("completed"), archived()),
		mk("old-open", archived()),
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"ready:yes", []string{"todo", "blocker", "blocked-by-done", "old-open"}},
		{"ready:no", []string{"draft", "wip", "done", "scrapped", "child-of-done", "grandchild-of-done", "blocked", "child-of-blocked", "old"}},
		{"-ready:yes archived:no", []string{"draft", "wip", "done", "scrapped", "child-of-done", "grandchild-of-done", "blocked", "child-of-blocked"}},
		{"done:yes", []string{"done", "scrapped", "child-of-done", "grandchild-of-done", "old"}},
		{"done:no", []string{"todo", "draft", "wip", "blocker", "blocked", "child-of-blocked", "blocked-by-done", "old-open"}},
		{"archived:yes", []string{"old", "old-open"}},
		{"archived:no done:no ready:yes", []string{"todo", "blocker", "blocked-by-done"}},
		{"archived:maybe", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			if got := run(beans, tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("run(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestMatchesText(t *testing.T) {
	b := mk("beans-q1", title("Hello"), typ("epic"), status("draft"), tags("ux"))
	for _, text := range []string{"hell", "EPIC", "draf", "q1", "ux", ""} {
		if !MatchesText(b, text) {
			t.Errorf("MatchesText(%q) = false", text)
		}
	}
	if MatchesText(b, "nope") {
		t.Error("MatchesText(nope) = true")
	}
}

func TestFilter(t *testing.T) {
	beans := []*bean.Bean{
		mk("root"),
		mk("mid", parent("root")),
		mk("leaf", parent("mid"), typ("bug")),
		mk("other"),
	}
	got := Filter(beans[2:], Parse("type:bug parent:root"), NewContext(beans))
	if len(got) != 1 || got[0].ID != "leaf" {
		t.Errorf("Filter = %v", got)
	}
	// Without a context only the given beans are known.
	if got := Filter(beans[2:], Parse("parent:root"), nil); len(got) != 0 {
		t.Errorf("Filter without context = %v", got)
	}
	if got := Filter(beans, Parse(""), nil); len(got) != len(beans) {
		t.Errorf("empty query should keep all beans")
	}
}

func TestFieldHelpCoversAllFields(t *testing.T) {
	for _, f := range Fields {
		if strings.TrimSpace(FieldHelp[f]) == "" {
			t.Errorf("no help for %s", f)
		}
	}
}
