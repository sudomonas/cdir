// SPDX-License-Identifier: GPL-3.0-or-later

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	home, cwd := "/home/u", "/work/dir"
	cases := map[string]string{
		"~":               "/home/u",
		"~/Projects":      "/home/u/Projects",
		"$HOME/Projects":  "/home/u/Projects",
		"${HOME}/x":       "/home/u/x",
		"./foo":           "/work/dir/foo",
		"../foo":          "/work/foo",
		"foo/../bar":      "/work/dir/bar",
		"/absolute/path/": "/absolute/path",
		"with spaces":     "/work/dir/with spaces",
		"Ünïcode/日本語":     "/work/dir/Ünïcode/日本語",
		"~user/x":         "/work/dir/~user/x", // not ours to expand
		"$HOMEWORK":       "/work/dir/$HOMEWORK",
		"-dash":           "/work/dir/-dash",
	}
	for in, want := range cases {
		if got := Resolve(in, cwd, home); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAbbrev(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u": "~", "/home/u/x": "~/x", "/home/user2": "/home/user2", "/etc": "/etc",
	} {
		if got := Abbrev(in, "/home/u"); got != want {
			t.Errorf("Abbrev(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckDir(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "dir with space"), 0o755)
	os.Mkdir(filepath.Join(d, "日本"), 0o755)
	os.WriteFile(filepath.Join(d, "file"), nil, 0o644)
	os.Symlink("dir with space", filepath.Join(d, "link"))
	os.Symlink("nowhere", filepath.Join(d, "broken"))
	os.Mkdir(filepath.Join(d, "noexec"), 0o600)

	cases := map[string]error{
		"dir with space": nil,
		"日本":             nil,
		"link":           nil,
		"file":           ErrNotDir,
		"missing":        ErrNotExist,
		"file/sub":       ErrNotExist,
		"broken":         ErrBrokenLink,
	}
	if os.Geteuid() != 0 {
		cases["noexec"] = ErrPermission
	}
	for name, want := range cases {
		if got := CheckDir(filepath.Join(d, name)); got != want {
			t.Errorf("CheckDir(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPrintable(t *testing.T) {
	if got := Printable("a\x1b[31mb\nc"); got != "a?[31mb?c" {
		t.Errorf("got %q", got)
	}
	if got := Printable("ok ü"); got != "ok ü" {
		t.Errorf("got %q", got)
	}
}
