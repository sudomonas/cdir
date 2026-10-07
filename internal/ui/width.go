// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"unicode"
)

// runeWidth approximates the number of terminal cells a rune occupies.
func runeWidth(r rune) int {
	switch {
	case r == 0x200d || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf && r != 0x303f,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

func strWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// fit truncates s to at most w cells, marking the cut with "…".
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strWidth(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}

// fitLeft truncates s from the left, keeping its end (for paths).
func fitLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strWidth(s) <= w {
		return s
	}
	rs := []rune(s)
	used, i := 0, len(rs)
	for i > 0 && used+runeWidth(rs[i-1]) <= w-1 {
		i--
		used += runeWidth(rs[i])
	}
	return "…" + string(rs[i:])
}

// pad right-pads s with spaces to exactly w cells (truncating if needed).
func pad(s string, w int) string {
	s = fit(s, w)
	if n := w - strWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}
