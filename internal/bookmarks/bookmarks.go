// SPDX-License-Identifier: GPL-3.0-or-later

// Package bookmarks reads and writes the bookmark file.
//
// Format: one bookmark per line,
//
//	name=/absolute/path
//
// Names match [A-Za-z0-9_][A-Za-z0-9_-]* and therefore never contain '=',
// so everything after the first '=' is the path, even if the path itself
// contains '=' or spaces. In the path, a backslash escapes itself and the
// line-breaking characters: "\\" is a backslash, "\n" a newline and "\r" a
// carriage return. Blank lines and lines starting with '#' are comments.
//
// If a name occurs more than once, the last entry wins. Lines that cannot be
// parsed are ignored, reported as warnings and preserved verbatim when the
// file is rewritten, so cdir never destroys data it does not understand.
package bookmarks

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Bookmark is a named directory.
type Bookmark struct {
	Name string
	Path string
}

// line is one line of the file. Comments and malformed lines keep only raw.
type line struct {
	raw  string
	name string
	path string
}

// File is a parsed bookmark file that can be modified and written back.
type File struct {
	lines []line
}

// Warning describes a line that was ignored or overridden.
type Warning struct {
	Line int
	Msg  string
}

func (w Warning) String() string { return fmt.Sprintf("line %d: %s", w.Line, w.Msg) }

// ValidName reports whether name is an acceptable bookmark name. Names may
// not start with '-', which also keeps them from colliding with options.
func ValidName(name string) bool {
	if name == "" || name[0] == '-' {
		return false
	}
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// Parse parses bookmark file contents. It never fails; problems are
// returned as warnings.
func Parse(data []byte) (*File, []Warning) {
	f := &File{}
	var warns []Warning
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" && len(data) <= 1 {
		return f, nil
	}
	lastSeen := map[string]int{}
	for i, raw := range strings.Split(text, "\n") {
		l := line{raw: raw}
		s := strings.TrimLeft(strings.TrimSuffix(raw, "\r"), " \t")
		if s != "" && s[0] != '#' {
			name, val, ok := strings.Cut(s, "=")
			name = strings.TrimRight(name, " \t")
			path := unescape(strings.TrimLeft(val, " \t"))
			switch {
			case !ok:
				warns = append(warns, Warning{i + 1, "missing '=' (ignored)"})
			case !ValidName(name):
				warns = append(warns, Warning{i + 1, fmt.Sprintf("invalid bookmark name %q (ignored)", name)})
			case !filepath.IsAbs(path):
				warns = append(warns, Warning{i + 1, fmt.Sprintf("path for %q is not absolute (ignored)", name)})
			default:
				if prev, dup := lastSeen[name]; dup {
					warns = append(warns, Warning{prev, fmt.Sprintf("%q is redefined on line %d, which wins", name, i+1)})
				}
				lastSeen[name] = i + 1
				l.name, l.path = name, path
			}
		}
		f.lines = append(f.lines, l)
	}
	return f, warns
}

// Bookmarks returns the effective bookmarks in file order.
func (f *File) Bookmarks() []Bookmark {
	last := map[string]int{}
	for i, l := range f.lines {
		if l.name != "" {
			last[l.name] = i
		}
	}
	var out []Bookmark
	for i, l := range f.lines {
		if l.name != "" && last[l.name] == i {
			out = append(out, Bookmark{l.name, l.path})
		}
	}
	return out
}

// Lookup returns the path for name.
func (f *File) Lookup(name string) (string, bool) {
	for i := len(f.lines) - 1; i >= 0; i-- {
		if f.lines[i].name == name {
			return f.lines[i].path, true
		}
	}
	return "", false
}

// Set adds or replaces a bookmark. An existing bookmark keeps its position;
// any duplicate definitions are removed.
func (f *File) Set(name, path string) {
	nl := line{raw: name + "=" + escape(path), name: name, path: path}
	idx := -1
	for i := len(f.lines) - 1; i >= 0; i-- {
		if f.lines[i].name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		f.lines = append(f.lines, nl)
		return
	}
	out := f.lines[:0]
	for i, l := range f.lines {
		switch {
		case i == idx:
			out = append(out, nl)
		case l.name != name:
			out = append(out, l)
		}
	}
	f.lines = out
}

// Delete removes every definition of name and reports whether one existed.
func (f *File) Delete(name string) bool {
	found := false
	out := f.lines[:0]
	for _, l := range f.lines {
		if l.name == name {
			found = true
			continue
		}
		out = append(out, l)
	}
	f.lines = out
	return found
}

// Bytes serializes the file.
func (f *File) Bytes() []byte {
	var b strings.Builder
	for _, l := range f.lines {
		b.WriteString(l.raw)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// EscapePath encodes a path the way it is stored in the file.
func EscapePath(p string) string { return escape(p) }

func escape(p string) string {
	if !strings.ContainsAny(p, "\\\n\r") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`)
	return r.Replace(p)
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			}
		}
		// Unknown escapes are kept literally.
		b.WriteByte(s[i])
	}
	return b.String()
}

// ErrNoChange can be returned from an Update callback to skip writing.
var ErrNoChange = errors.New("no change")
