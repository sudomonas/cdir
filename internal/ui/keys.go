// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type keyKind int

const (
	kNone keyKind = iota
	kRune
	kEnter
	kSelect // Ctrl+Enter, or Ctrl+J where Ctrl+Enter cannot be detected
	kBackspace
	kEsc
	kTab
	kUp
	kDown
	kLeft
	kRight
	kHome
	kEnd
	kPgUp
	kPgDn
	kCtrlC
	kCtrlU
	kCtrlW
	kCtrlN
	kCtrlP
)

type key struct {
	kind keyKind
	r    rune
}

// parseKeys decodes one chunk of terminal input. Terminals send each
// escape sequence in a single write, so an ESC at the end of a chunk is the
// Escape key itself.
func parseKeys(b []byte) []key {
	var keys []key
	for len(b) > 0 {
		k, n := parseKey(b)
		if k.kind != kNone {
			keys = append(keys, k)
		}
		b = b[n:]
	}
	return keys
}

func parseKey(b []byte) (key, int) {
	if b[0] != 0x1b {
		return plainKey(b)
	}
	if len(b) == 1 || b[1] == 0x1b {
		return key{kind: kEsc}, 1
	}
	switch b[1] {
	case '[':
		return parseCSI(b)
	case 'O':
		if len(b) >= 3 {
			return key{kind: ss3Key(b[2])}, 3
		}
	}
	// Alt+key: not bound to anything.
	_, n := utf8.DecodeRune(b[1:])
	return key{}, 1 + n
}

func plainKey(b []byte) (key, int) {
	switch b[0] {
	case '\r':
		return key{kind: kEnter}, 1
	case '\n':
		return key{kind: kSelect}, 1
	case 0x7f, 0x08:
		return key{kind: kBackspace}, 1
	case '\t':
		return key{kind: kTab}, 1
	case 0x03:
		return key{kind: kCtrlC}, 1
	case 0x07: // Ctrl+G, the Emacs "cancel"
		return key{kind: kEsc}, 1
	case 0x15:
		return key{kind: kCtrlU}, 1
	case 0x17:
		return key{kind: kCtrlW}, 1
	case 0x0e:
		return key{kind: kCtrlN}, 1
	case 0x10:
		return key{kind: kCtrlP}, 1
	}
	if b[0] < 0x20 {
		return key{}, 1
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n <= 1 {
		return key{}, 1
	}
	return key{kind: kRune, r: r}, n
}

func ss3Key(c byte) keyKind {
	switch c {
	case 'A':
		return kUp
	case 'B':
		return kDown
	case 'C':
		return kRight
	case 'D':
		return kLeft
	case 'H':
		return kHome
	case 'F':
		return kEnd
	}
	return kNone
}

func parseCSI(b []byte) (key, int) {
	i := 2
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
		i++
	}
	if i >= len(b) {
		return key{}, len(b) // truncated sequence
	}
	final, n := b[i], i+1
	fields := strings.Split(string(b[2:i]), ";")
	num := func(k int) int {
		if k >= len(fields) {
			return 0
		}
		s, _, _ := strings.Cut(fields[k], ":")
		v, _ := strconv.Atoi(s)
		return v
	}
	switch final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return key{kind: ss3Key(final)}, n
	case 'u': // kitty keyboard protocol: CSI code ; mods u
		return csiU(num(0), num(1)), n
	case '~':
		switch num(0) {
		case 1, 7:
			return key{kind: kHome}, n
		case 4, 8:
			return key{kind: kEnd}, n
		case 5:
			return key{kind: kPgUp}, n
		case 6:
			return key{kind: kPgDn}, n
		case 27: // xterm modifyOtherKeys: CSI 27 ; mods ; code ~
			return csiU(num(2), num(1)), n
		}
	}
	return key{}, n
}

func csiU(code, mods int) key {
	m := 0
	if mods > 0 {
		m = mods - 1
	}
	ctrl, alt := m&4 != 0, m&2 != 0
	switch code {
	case 13:
		if ctrl {
			return key{kind: kSelect}
		}
		return key{kind: kEnter}
	case 27:
		return key{kind: kEsc}
	case 9:
		return key{kind: kTab}
	case 127, 8:
		return key{kind: kBackspace}
	}
	if ctrl && !alt && code >= 'a' && code <= 'z' {
		k, _ := plainKey([]byte{byte(code - 'a' + 1)})
		return k
	}
	if !ctrl && !alt && code >= 0x20 && code != 0x7f {
		return key{kind: kRune, r: rune(code)}
	}
	return key{}
}
