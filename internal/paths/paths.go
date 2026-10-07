// SPDX-License-Identifier: GPL-3.0-or-later

// Package paths turns user-supplied path arguments into absolute directory
// paths and checks that they can be entered.
//
// Paths are handled logically, the same way the shell's `cd` does: symbolic
// links are preserved and ".." is resolved lexically. Nothing here resolves
// symlinks unless explicitly asked to.
package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Expand replaces a leading "~", "$HOME" or "${HOME}" with home.
//
// The shell normally expands these before cdir sees them; this only matters
// when an argument was quoted. No other shell syntax is interpreted.
func Expand(p, home string) string {
	for _, prefix := range []string{"~", "$HOME", "${HOME}"} {
		if p == prefix {
			return home
		}
		if strings.HasPrefix(p, prefix+"/") {
			return home + p[len(prefix):]
		}
	}
	return p
}

// Abs makes p absolute relative to cwd and cleans it lexically.
func Abs(p, cwd string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// Resolve expands and absolutizes a path argument.
func Resolve(arg, cwd, home string) string {
	return Abs(Expand(arg, home), cwd)
}

// Abbrev shortens a path below home to "~/...", for display only.
func Abbrev(p, home string) string {
	if home == "" || home == "/" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// Errors returned by CheckDir.
var (
	ErrNotExist   = errors.New("no such directory")
	ErrNotDir     = errors.New("not a directory")
	ErrPermission = errors.New("permission denied")
	ErrBrokenLink = errors.New("broken symbolic link")
)

// IsMissing reports whether err means the directory does not exist at all
// (as opposed to existing but being unusable).
func IsMissing(err error) bool {
	return errors.Is(err, ErrNotExist) || errors.Is(err, ErrBrokenLink)
}

// CheckDir verifies that p (following symlinks) is a directory the current
// user may cd into.
func CheckDir(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if lfi, lerr := os.Lstat(p); lerr == nil && lfi.Mode()&fs.ModeSymlink != 0 {
				return ErrBrokenLink
			}
			return ErrNotExist
		case errors.Is(err, fs.ErrPermission):
			return ErrPermission
		case errors.Is(err, syscall.ENOTDIR):
			// A path component is a regular file.
			return ErrNotExist
		}
		return err
	}
	if !fi.IsDir() {
		return ErrNotDir
	}
	// cd needs search (execute) permission on the directory itself.
	const xOK = 1
	if syscall.Access(p, xOK) != nil {
		return ErrPermission
	}
	return nil
}

// Printable replaces control characters and invalid UTF-8 so that file
// names cannot inject escape sequences into the terminal.
func Printable(s string) string {
	clean := true
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '�' {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '�' {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}
