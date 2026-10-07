// SPDX-License-Identifier: GPL-3.0-or-later

// Package browser lists and sorts directory contents for the interactive UI.
package browser

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Entry is one item in a directory listing.
type Entry struct {
	Name   string
	Dir    bool   // a directory, or a symlink that points to one
	Link   bool   // a symbolic link
	Target string // link target as stored in the link
	Broken bool   // a symlink whose target does not exist
}

// Options controls what List returns.
type Options struct {
	Hidden  bool // include names starting with '.'
	Files   bool // include non-directories
	Natural bool // natural sort ("a2" < "a10") instead of plain alphabetical
}

// List reads dir and returns its entries, directories first. If the
// directory can only be partially read, the readable part is returned.
func List(dir string, opt Options) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil && len(des) == 0 {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if !opt.Hidden && strings.HasPrefix(name, ".") {
			continue
		}
		e := Entry{Name: name, Dir: de.IsDir()}
		if de.Type()&os.ModeSymlink != 0 {
			full := filepath.Join(dir, name)
			e.Link = true
			e.Target, _ = os.Readlink(full)
			if fi, err := os.Stat(full); err == nil {
				e.Dir = fi.IsDir()
			} else {
				e.Broken = true
			}
		}
		if !e.Dir && !opt.Files {
			continue
		}
		out = append(out, e)
	}
	less := AlphaLess
	if opt.Natural {
		less = NaturalLess
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return less(out[i].Name, out[j].Name)
	})
	return out, nil
}

// AlphaLess compares case-insensitively, falling back to byte order.
func AlphaLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// NaturalLess compares case-insensitively and treats runs of digits as
// numbers, so "track2" sorts before "track10".
func NaturalLess(a, b string) bool {
	if c := naturalCmp([]rune(a), []rune(b)); c != 0 {
		return c < 0
	}
	return a < b
}

func naturalCmp(a, b []rune) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := trimZeros(a[si:i]), trimZeros(b[sj:j])
			if len(na) != len(nb) {
				return cmp(len(na), len(nb))
			}
			for k := range na {
				if na[k] != nb[k] {
					return cmp(int(na[k]), int(nb[k]))
				}
			}
			continue
		}
		ca, cb := unicode.ToLower(a[i]), unicode.ToLower(b[j])
		if ca != cb {
			return cmp(int(ca), int(cb))
		}
		i++
		j++
	}
	return cmp(len(a)-i, len(b)-j)
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func trimZeros(r []rune) []rune {
	for len(r) > 1 && r[0] == '0' {
		r = r[1:]
	}
	return r
}

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
