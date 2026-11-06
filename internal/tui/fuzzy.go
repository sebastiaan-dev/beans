package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fuzzy matching for the command palette.
//
// Scores are deterministic: the same query and text always give the same
// score, and callers break ties by their own order. Each word of the query
// must match the text, as a substring (preferred: at the start, then at a
// word start) or as a subsequence.

const (
	scorePrefix     = 300 // substring at the start of the text
	scoreWordStart  = 200 // substring at the start of a word
	scoreSubstring  = 100 // substring elsewhere
	scoreExactBonus = 100 // the word is the whole text
	scorePerRune    = 10  // per matched rune of a substring
	// Subsequence matches score below every substring match.
	scoreSubseqMax = scoreSubstring - 1
)

// fuzzyScore reports how well query matches text, case-insensitively. ok is
// false if a word of the query does not match. An empty query matches
// everything with score 0.
func fuzzyScore(query, text string) (score int, ok bool) {
	t := strings.ToLower(text)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		s, ok := wordScore(w, t)
		if !ok {
			return 0, false
		}
		score += s
	}
	return score, true
}

// wordScore scores one lower-case query word against lower-case text.
func wordScore(w, t string) (int, bool) {
	best, found := 0, false
	for from := 0; from <= len(t); {
		i := strings.Index(t[from:], w)
		if i < 0 {
			break
		}
		i += from
		s := scoreSubstring
		switch {
		case i == 0:
			s = scorePrefix
		case isWordStart(t, i):
			s = scoreWordStart
		}
		if len(w) == len(t) {
			s += scoreExactBonus
		}
		s += utf8.RuneCountInString(w) * scorePerRune
		if !found || s > best {
			best, found = s, true
		}
		_, size := utf8.DecodeRuneInString(t[i:])
		from = i + max(size, 1)
	}
	if found {
		return best, true
	}
	return subsequenceScore(w, t)
}

// subsequenceScore scores w as a subsequence of t: consecutive runes and
// runes at word starts score higher, gaps lower. To keep the matches
// meaningful, the runes must lie close together (within twice the word's
// length) or all start words, like an acronym ("sbw" for "start bean in a
// workspace"). Every start position is tried and the best match wins.
func subsequenceScore(w, t string) (int, bool) {
	wr := []rune(w)
	if len(wr) == 0 {
		return 0, true
	}
	tr := []rune(t)
	starts := make([]bool, len(tr))
	off := 0
	for i, r := range tr {
		starts[i] = isWordStart(t, off)
		off += utf8.RuneLen(r)
	}
	best, found := 0, false
	// Acronym: the runes are initials of words, in order.
	var initials []rune
	for i, r := range tr {
		if starts[i] {
			initials = append(initials, r)
		}
	}
	if j := matchRunes(wr, initials); j == len(wr) {
		best, found = min(13*len(wr), scoreSubseqMax), true
	}
	for start := range tr {
		if tr[start] != wr[0] {
			continue
		}
		score, j, last, acronym := 0, 0, -2, true
		for i := start; i < len(tr) && j < len(wr); i++ {
			if tr[i] != wr[j] {
				continue
			}
			score += 5
			if i == last+1 {
				score += 10
			}
			if starts[i] {
				score += 8
			} else if i != last+1 {
				acronym = false
			}
			if last >= 0 {
				score -= min(i-last-1, 5)
			}
			last = i
			j++
		}
		if j < len(wr) {
			break // later starts can't match either
		}
		if span := last - start + 1; span > 2*len(wr) && !acronym {
			continue
		}
		score = max(min(score, scoreSubseqMax), 1)
		if !found || score > best {
			best, found = score, true
		}
	}
	return best, found
}

// isWordStart reports whether the rune at byte offset i of s starts a word.
func isWordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}

// matchRunes returns how many runes of w match t as a subsequence.
func matchRunes(w, t []rune) int {
	j := 0
	for _, r := range t {
		if j < len(w) && r == w[j] {
			j++
		}
	}
	return j
}
