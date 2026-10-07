// SPDX-License-Identifier: GPL-3.0-or-later

// Package fuzzy implements a small case-insensitive subsequence matcher.
package fuzzy

import (
	"sort"
	"unicode"
	"unicode/utf8"
)

// Match reports whether all runes of pattern occur in text, in order,
// ignoring case. The score rewards matches at the start of the text, at word
// boundaries and in consecutive runs; higher is better.
func Match(pattern, text string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	p := lower([]rune(pattern))
	t := []rune(text)
	lt := lower(append([]rune(nil), t...))
	best, found := 0, false
	for start := range lt {
		if lt[start] != p[0] {
			continue
		}
		if s, ok := score(p, t, lt, start); ok && (!found || s > best) {
			best, found = s, true
		}
	}
	return best, found
}

func score(p, t, lt []rune, start int) (int, bool) {
	s, pi, prev := 0, 0, -1
	for i := start; i < len(lt) && pi < len(p); i++ {
		if lt[i] != p[pi] {
			continue
		}
		s++
		switch {
		case i == 0:
			s += 10
		case isBoundary(t[i-1], t[i]):
			s += 8
		}
		if prev >= 0 {
			if i == prev+1 {
				s += 6
			} else {
				s -= min(i-prev-1, 5)
			}
		}
		prev = i
		pi++
	}
	return s, pi == len(p)
}

func isBoundary(prev, cur rune) bool {
	switch prev {
	case ' ', '-', '_', '.', '/':
		return true
	}
	return (unicode.IsLower(prev) && unicode.IsUpper(cur)) ||
		(!unicode.IsDigit(prev) && unicode.IsDigit(cur))
}

func lower(r []rune) []rune {
	for i := range r {
		r[i] = unicode.ToLower(r[i])
	}
	return r
}

// Filter returns the indices of the n items whose key matches pattern, best
// match first. Equal scores prefer shorter keys, then original order.
func Filter(pattern string, n int, key func(int) string) []int {
	type hit struct{ idx, score, length int }
	var hits []hit
	for i := 0; i < n; i++ {
		k := key(i)
		if s, ok := Match(pattern, k); ok {
			hits = append(hits, hit{i, s, utf8.RuneCountInString(k)})
		}
	}
	if pattern != "" {
		sort.SliceStable(hits, func(a, b int) bool {
			if hits[a].score != hits[b].score {
				return hits[a].score > hits[b].score
			}
			return hits[a].length < hits[b].length
		})
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}
