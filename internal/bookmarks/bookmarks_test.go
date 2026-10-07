// SPDX-License-Identifier: GPL-3.0-or-later

package bookmarks

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseValid(t *testing.T) {
	f, warns := Parse([]byte("# comment\n\nnapkin=/home/u/Projects/Napkin\nnotes = /home/u/My Notes\n"))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	got := f.Bookmarks()
	want := []Bookmark{{"napkin", "/home/u/Projects/Napkin"}, {"notes", "/home/u/My Notes"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bookmark %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseDuplicateLastWins(t *testing.T) {
	f, warns := Parse([]byte("a=/one\nb=/two\na=/three\n"))
	if p, _ := f.Lookup("a"); p != "/three" {
		t.Errorf("Lookup(a) = %q, want /three", p)
	}
	if len(warns) != 1 || warns[0].Line != 1 {
		t.Errorf("want one duplicate warning for line 1, got %v", warns)
	}
	bms := f.Bookmarks()
	if len(bms) != 2 || bms[0].Name != "b" || bms[1].Name != "a" {
		t.Errorf("Bookmarks() = %v", bms)
	}
}

func TestParseMalformed(t *testing.T) {
	in := "no-equals-sign\nbad name=/x\n-dash=/x\nrel=some/relative\nempty=\nok=/fine\n"
	f, warns := Parse([]byte(in))
	if len(warns) != 5 {
		t.Errorf("got %d warnings, want 5: %v", len(warns), warns)
	}
	if bms := f.Bookmarks(); len(bms) != 1 || bms[0].Name != "ok" {
		t.Errorf("Bookmarks() = %v", bms)
	}
	// Malformed lines survive a rewrite untouched.
	f.Set("new", "/new")
	if out := string(f.Bytes()); !strings.HasPrefix(out, in) {
		t.Errorf("rewrite lost data:\n%s", out)
	}
}

func TestPathsWithSpecialCharacters(t *testing.T) {
	for _, p := range []string{"/a b/c", "/x=y", "/new\nline", `/back\slash`, "/tab\there", "/ünïcødé/日本"} {
		f := &File{}
		f.Set("n", p)
		g, warns := Parse(f.Bytes())
		if len(warns) != 0 {
			t.Errorf("%q: warnings %v", p, warns)
		}
		if got, _ := g.Lookup("n"); got != p {
			t.Errorf("round trip %q -> %q (file %q)", p, got, f.Bytes())
		}
	}
}

func TestSetReplacesInPlaceAndDedups(t *testing.T) {
	f, _ := Parse([]byte("a=/1\nb=/2\na=/3\nc=/4\n"))
	f.Set("a", "/new")
	if got := string(f.Bytes()); got != "b=/2\na=/new\nc=/4\n" {
		t.Errorf("got %q", got)
	}
}

func TestDelete(t *testing.T) {
	f, _ := Parse([]byte("a=/1\nb=/2\na=/3\n"))
	if !f.Delete("a") || f.Delete("zzz") {
		t.Fatal("Delete return values wrong")
	}
	if got := string(f.Bytes()); got != "b=/2\n" {
		t.Errorf("got %q", got)
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"napkin", "my-project", "med_notes", "A1", "_x"} {
		if !ValidName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", "-a", "--add", "a/b", `a\b`, "a:b", "a=b", "a b", "a\tb", "ü"} {
		if ValidName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestStoreAtomicUpdate(t *testing.T) {
	dir := t.TempDir()
	s := Store{Path: filepath.Join(dir, "sub", "bookmarks")}
	if f, _, err := s.Load(); err != nil || len(f.Bookmarks()) != 0 {
		t.Fatalf("missing file should load empty: %v", err)
	}
	if err := s.Update(func(f *File) error { f.Set("a", "/a"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(f *File) error { return ErrNoChange }); err != nil {
		t.Fatal(err)
	}
	f, _, _ := s.Load()
	if p, _ := f.Lookup("a"); p != "/a" {
		t.Errorf("got %q", p)
	}
	ents, _ := os.ReadDir(filepath.Dir(s.Path))
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestStoreKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles-bookmarks")
	os.WriteFile(real, []byte("a=/a\n"), 0o600)
	link := filepath.Join(dir, "bookmarks")
	os.Symlink(real, link)
	s := Store{Path: link}
	if err := s.Update(func(f *File) error { f.Set("b", "/b"); return nil }); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}
	data, _ := os.ReadFile(real)
	if string(data) != "a=/a\nb=/b\n" {
		t.Errorf("target content %q", data)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode not preserved: %v", fi.Mode())
	}
}

func TestStoreConcurrentUpdates(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "bookmarks")}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := string(rune('a' + i))
			if err := s.Update(func(f *File) error { f.Set(name, "/"+name); return nil }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	f, warns, _ := s.Load()
	if n := len(f.Bookmarks()); n != 20 || len(warns) != 0 {
		t.Errorf("got %d bookmarks, warnings %v; updates were lost", n, warns)
	}
}
