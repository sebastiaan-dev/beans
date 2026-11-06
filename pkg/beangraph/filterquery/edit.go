package filterquery

import "slices"

// Editing helpers for UIs that edit a query through pickers (the Beans UI's
// filter popover, the terminal UI's filter bar). Each returns a new query and
// leaves the receiver untouched.

func (t Term) isPlain(field Field) bool {
	return t.Field == field && !t.Negate && (t.Op == OpEq || t.Op == "")
}

func (q Query) clone() Query {
	terms := make([]Term, len(q.Terms))
	copy(terms, q.Terms)
	return Query{Terms: terms}
}

// SelectedValues returns the values of the first positive `field:a,b` clause
// (what a picker shows as checked).
func (q Query) SelectedValues(field Field) []string {
	for _, t := range q.Terms {
		if t.isPlain(field) {
			return t.Values
		}
	}
	return nil
}

// Clauses returns all clauses for the field, in query order.
func (q Query) Clauses(field Field) []Term {
	var out []Term
	for _, t := range q.Terms {
		if !t.IsText() && t.Field == field {
			out = append(out, t)
		}
	}
	return out
}

// HasField reports whether the query has an active clause for the field.
func (q Query) HasField(field Field) bool {
	for _, t := range q.Clauses(field) {
		if len(t.Values) > 0 {
			return true
		}
	}
	return false
}

// ToggleValue toggles one value in the first positive clause for field,
// creating or removing the clause as needed.
func (q Query) ToggleValue(field Field, value string) Query {
	out := q.clone()
	for i, t := range out.Terms {
		if !t.isPlain(field) {
			continue
		}
		var values []string
		if slices.Contains(t.Values, value) {
			for _, v := range t.Values {
				if v != value {
					values = append(values, v)
				}
			}
		} else {
			values = append(slices.Clone(t.Values), value)
		}
		if len(values) == 0 {
			out.Terms = slices.Delete(out.Terms, i, i+1)
		} else {
			t.Values = values
			out.Terms[i] = t
		}
		return out
	}
	out.Terms = append(out.Terms, Clause(field, value))
	return out
}

// SetClause replaces every clause for field with term (or with nothing if
// term has no values), at the position of the first replaced clause.
func (q Query) SetClause(term Term) Query {
	index := -1
	var terms []Term
	for _, t := range q.Terms {
		if !t.IsText() && t.Field == term.Field {
			if index < 0 {
				index = len(terms)
			}
			continue
		}
		terms = append(terms, t)
	}
	if len(term.Values) > 0 {
		if term.Op == "" {
			term.Op = OpEq
		}
		if index < 0 {
			index = len(terms)
		}
		terms = slices.Insert(terms, index, term)
	}
	return Query{Terms: terms}
}

// SetValues replaces every clause for field with a single positive clause
// (or none if values is empty).
func (q Query) SetValues(field Field, values ...string) Query {
	return q.SetClause(Clause(field, values...))
}

// RemoveTerm drops the term at index.
func (q Query) RemoveTerm(index int) Query {
	out := q.clone()
	if index >= 0 && index < len(out.Terms) {
		out.Terms = slices.Delete(out.Terms, index, index+1)
	}
	return out
}

// ClearClauses drops all field clauses, keeping only the free-text terms.
func (q Query) ClearClauses() Query {
	var terms []Term
	for _, t := range q.Terms {
		if t.IsText() {
			terms = append(terms, t)
		}
	}
	return Query{Terms: terms}
}

// BoolSelection returns the value of the first positive clause of a yes/no
// field (blocked:yes → true), and false for ok if there is none.
func (q Query) BoolSelection(field Field) (value, ok bool) {
	values := q.SelectedValues(field)
	if len(values) == 0 {
		return false, false
	}
	return ParseBool(values[0])
}

// SetBool sets a yes/no field, or removes it if value is nil.
func (q Query) SetBool(field Field, value *bool) Query {
	switch {
	case value == nil:
		return q.SetValues(field)
	case *value:
		return q.SetValues(field, "yes")
	default:
		return q.SetValues(field, "no")
	}
}
