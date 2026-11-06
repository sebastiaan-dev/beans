// Package filterquery implements the structured bean filter query language
// shared by the Beans UI's planning views (frontend/src/lib/filter.ts), the
// terminal UI and `beans list --query`.
//
// A filter is a plain query string. This package parses it into terms,
// formats terms back into a query string, edits it (for UIs that offer
// pickers next to the text input) and matches beans against it. It is pure:
// no I/O, no global state.
//
// Syntax (terms are ANDed; comma-separated values within a term are ORed):
//
//	status:todo,draft      bean status is one of the values
//	type:bug,feature       bean type is one of the values
//	priority:high,critical priority is one of the values (empty = normal)
//	priority:>=high        priority comparison (>=, <=, >, <); higher = more urgent
//	tag:ui,backend         bean has at least one of the tags
//	parent:<id|title>      bean is a descendant (any depth) of the referenced bean
//	milestone:<id|title>   like parent:, but the ancestor must be a milestone
//	epic:<id|title>        like parent:, but the ancestor must be an epic
//	blocked:yes|no         bean is (not) blocked, directly or via an ancestor
//	-status:completed      a leading "-" negates a field term
//	"some words"           quotes group values/text containing spaces
//	anything else          free-text search on title, id, type, status and tags
//
// Extensions used by the terminal UI and the CLI (the Beans UI treats them as
// free text for now):
//
//	ready:yes|no           bean is (not) ready to start, like `beans list --ready`:
//	                       not blocked, not draft/in-progress/completed/scrapped
//	                       and no completed/scrapped ancestor
//	done:yes|no            bean is (not) completed/scrapped, itself or via an ancestor
//	archived:yes|no        bean is (not) stored in the archive directory
package filterquery

import (
	"slices"
	"strings"
	"unicode"
)

// Field is a filterable bean property.
type Field string

const (
	FieldStatus    Field = "status"
	FieldType      Field = "type"
	FieldPriority  Field = "priority"
	FieldTag       Field = "tag"
	FieldParent    Field = "parent"
	FieldMilestone Field = "milestone"
	FieldEpic      Field = "epic"
	FieldBlocked   Field = "blocked"
	FieldReady     Field = "ready"
	FieldDone      Field = "done"
	FieldArchived  Field = "archived"
)

// Fields lists all fields in the order they are offered to users.
var Fields = []Field{
	FieldStatus, FieldType, FieldPriority, FieldTag, FieldParent, FieldMilestone,
	FieldEpic, FieldBlocked, FieldReady, FieldDone, FieldArchived,
}

// fieldAliases are alternative spellings accepted by the parser (normalized
// on parse).
var fieldAliases = map[string]Field{
	"tags":  FieldTag,
	"prio":  FieldPriority,
	"under": FieldParent,
}

// FieldHelp is a short description of each field.
var FieldHelp = map[Field]string{
	FieldStatus:    "Status is one of…",
	FieldType:      "Type is one of…",
	FieldPriority:  "Priority is one of…, or >=, <= a level",
	FieldTag:       "Has one of the tags…",
	FieldParent:    "Below a bean (any depth)",
	FieldMilestone: "Below a milestone",
	FieldEpic:      "Below an epic",
	FieldBlocked:   "Blocked: yes or no",
	FieldReady:     "Ready to start: yes or no",
	FieldDone:      "Completed/scrapped: yes or no",
	FieldArchived:  "Archived: yes or no",
}

// Known values, mirroring the Beans UI.
var (
	Statuses = []string{"draft", "todo", "in-progress", "completed", "scrapped"}
	Types    = []string{"milestone", "epic", "feature", "bug", "task"}
	// Priorities are ordered from most to least urgent.
	Priorities = []string{"critical", "high", "normal", "low", "deferred"}
)

const defaultPriority = "normal"

// IsParentField reports whether the field's values reference other beans.
func (f Field) IsParentField() bool {
	return f == FieldParent || f == FieldMilestone || f == FieldEpic
}

// IsBoolField reports whether the field takes yes/no values.
func (f Field) IsBoolField() bool {
	switch f {
	case FieldBlocked, FieldReady, FieldDone, FieldArchived:
		return true
	}
	return false
}

// Op is a comparison operator; only priority supports anything but OpEq.
type Op string

const (
	OpEq Op = "="
	OpGE Op = ">="
	OpLE Op = "<="
	OpGT Op = ">"
	OpLT Op = "<"
)

// Term is a single term of a query: either a field clause or free text.
type Term struct {
	// Field is empty for free-text terms.
	Field  Field
	Negate bool
	Op     Op
	// Values are the alternatives (ORed), lower-cased except for references
	// to beans (parent, milestone, epic).
	Values []string
	// Text is the value of a free-text term.
	Text string
}

// IsText reports whether the term is free text.
func (t Term) IsText() bool { return t.Field == "" }

// Clause returns a positive field clause with the given values.
func Clause(field Field, values ...string) Term {
	return Term{Field: field, Op: OpEq, Values: values}
}

// Text returns a free-text term.
func Text(text string) Term { return Term{Text: text} }

// Query is a parsed filter query.
type Query struct {
	Terms []Term
}

// Token is a whitespace-separated part of a query string.
type Token struct {
	Raw        string // including quotes
	Start, End int    // byte offsets
}

// Tokenize splits a query on whitespace, keeping double-quoted sections
// together.
func Tokenize(query string) []Token {
	var tokens []Token
	rs := []rune(query)
	// Byte offsets of each rune, for Token.Start/End.
	offs := make([]int, len(rs)+1)
	o := 0
	for i, r := range rs {
		offs[i] = o
		o += len(string(r))
	}
	offs[len(rs)] = o

	i := 0
	for i < len(rs) {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		start := i
		inQuote := false
		for i < len(rs) && (inQuote || !unicode.IsSpace(rs[i])) {
			if rs[i] == '"' {
				inQuote = !inQuote
			}
			i++
		}
		tokens = append(tokens, Token{Raw: string(rs[start:i]), Start: offs[start], End: offs[i]})
	}
	return tokens
}

func unquote(s string) string { return strings.ReplaceAll(s, `"`, "") }

// splitValues splits on commas that are not inside double quotes.
func splitValues(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, ch := range s {
		if ch == '"' {
			inQuote = !inQuote
		}
		if ch == ',' && !inQuote {
			parts = append(parts, cur.String())
			cur.Reset()
		} else {
			cur.WriteRune(ch)
		}
	}
	return append(parts, cur.String())
}

// ResolveField returns the field with the given name or alias.
func ResolveField(name string) (Field, bool) {
	lower := Field(strings.ToLower(name))
	if slices.Contains(Fields, lower) {
		return lower, true
	}
	f, ok := fieldAliases[string(lower)]
	return f, ok
}

// splitFieldToken splits "-name:rest" into its parts. name consists of ASCII
// letters only.
func splitFieldToken(raw string) (negate bool, name, rest string, ok bool) {
	s := raw
	if strings.HasPrefix(s, "-") {
		negate = true
		s = s[1:]
	}
	i := 0
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	if i == 0 || i >= len(s) || s[i] != ':' {
		return false, "", "", false
	}
	return negate, s[:i], s[i+1:], true
}

var compareOps = []Op{OpGE, OpLE, OpGT, OpLT, OpEq}

// ParseToken parses a single token into a term. ok is false for an empty
// token (e.g. `""`).
func ParseToken(raw string) (Term, bool) {
	negate, name, rest, isField := splitFieldToken(raw)
	if isField {
		if field, known := ResolveField(name); known {
			op := OpEq
			if field == FieldPriority {
				for _, o := range compareOps {
					if strings.HasPrefix(rest, string(o)) {
						op = o
						rest = rest[len(o):]
						break
					}
				}
			}
			values := []string{}
			for _, v := range splitValues(rest) {
				v = strings.TrimSpace(unquote(v))
				if v == "" {
					continue
				}
				if !field.IsParentField() {
					v = strings.ToLower(v)
				}
				values = append(values, v)
			}
			return Term{Field: field, Negate: negate, Op: op, Values: values}, true
		}
	}
	value := strings.TrimSpace(unquote(raw))
	if value == "" {
		return Term{}, false
	}
	return Text(value), true
}

// Parse parses a query string. Parsing never fails: unknown fields are free
// text and unknown values simply match nothing (see InvalidValues).
func Parse(query string) Query {
	var q Query
	for _, tok := range Tokenize(query) {
		if term, ok := ParseToken(tok.Raw); ok {
			q.Terms = append(q.Terms, term)
		}
	}
	return q
}

func quoteIfNeeded(value string) string {
	if strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '"' }) {
		return `"` + strings.ReplaceAll(value, `"`, "") + `"`
	}
	return value
}

// String formats the term in canonical query syntax.
func (t Term) String() string {
	if t.IsText() {
		return quoteIfNeeded(t.Text)
	}
	var b strings.Builder
	if t.Negate {
		b.WriteByte('-')
	}
	b.WriteString(string(t.Field))
	b.WriteByte(':')
	if t.Op != OpEq && t.Op != "" {
		b.WriteString(string(t.Op))
	}
	for i, v := range t.Values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteIfNeeded(v))
	}
	return b.String()
}

// String formats the query in canonical query syntax.
func (q Query) String() string {
	parts := make([]string, len(q.Terms))
	for i, t := range q.Terms {
		parts[i] = t.String()
	}
	return strings.Join(parts, " ")
}

// ActiveClauses returns the clauses that constrain results (incomplete
// `status:` terms do not).
func (q Query) ActiveClauses() []Term {
	var out []Term
	for _, t := range q.Terms {
		if !t.IsText() && len(t.Values) > 0 {
			out = append(out, t)
		}
	}
	return out
}

// Texts returns the values of the free-text terms.
func (q Query) Texts() []string {
	var out []string
	for _, t := range q.Terms {
		if t.IsText() {
			out = append(out, t.Text)
		}
	}
	return out
}

// IsEmpty reports whether the query matches every bean.
func (q Query) IsEmpty() bool {
	for _, t := range q.Terms {
		if t.IsText() || len(t.Values) > 0 {
			return false
		}
	}
	return true
}

// KnownValues returns the values the parser accepts for a field, or nil when
// any value is allowed.
func KnownValues(field Field) []string {
	switch field {
	case FieldStatus:
		return Statuses
	case FieldType:
		return Types
	case FieldPriority:
		return Priorities
	}
	if field.IsBoolField() {
		return []string{"yes", "no"}
	}
	return nil
}

var boolValues = map[string]bool{
	"yes": true, "true": true, "y": true, "1": true,
	"no": false, "false": false, "n": false, "0": false,
}

// ParseBool interprets a yes/no value.
func ParseBool(v string) (value, ok bool) {
	value, ok = boolValues[strings.ToLower(v)]
	return value, ok
}

// InvalidValues returns the values of a clause that cannot match anything
// (typos etc.), for UI hints.
func InvalidValues(t Term) []string {
	if t.IsText() {
		return nil
	}
	if t.Field.IsBoolField() {
		var out []string
		for _, v := range t.Values {
			if _, ok := boolValues[v]; !ok {
				out = append(out, v)
			}
		}
		return out
	}
	if t.Op != OpEq && t.Field != FieldPriority {
		return t.Values
	}
	known := KnownValues(t.Field)
	if known == nil {
		return nil
	}
	var out []string
	for _, v := range t.Values {
		if !slices.Contains(known, v) {
			out = append(out, v)
		}
	}
	return out
}
