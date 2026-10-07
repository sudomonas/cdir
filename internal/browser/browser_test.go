// SPDX-License-Identifier: GPL-3.0-or-later

package browser

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNaturalSort(t *testing.T) {
	in := []string{"Videos", "10", "documents", "2", "Downloads", "100", "file10", "file2", "File1"}
	sort.Slice(in, func(i, j int) bool { return NaturalLess(in[i], in[j]) })
	want := []string{"2", "10", "100", "documents", "Downloads", "File1", "file2", "file10", "Videos"}
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("got %v, want %v", in, want)
		}
	}
}

func TestList(t *testing.T) {
	d := t.TempDir()
	for _, n := range []string{"beta", "Alpha", ".hidden", "dir10", "dir9"} {
		os.Mkdir(filepath.Join(d, n), 0o755)
	}
	os.WriteFile(filepath.Join(d, "afile"), nil, 0o644)
	os.Symlink("beta", filepath.Join(d, "linkdir"))
	os.Symlink("nowhere", filepath.Join(d, "broken"))

	names := func(es []Entry) []string {
		var s []string
		for _, e := range es {
			s = append(s, e.Name)
		}
		return s
	}
	es, err := List(d, Options{Natural: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Alpha", "beta", "dir9", "dir10", "linkdir"}
	if got := names(es); len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}
	if !es[4].Link || !es[4].Dir || es[4].Target != "beta" {
		t.Errorf("symlinked dir entry wrong: %+v", es[4])
	}

	es, _ = List(d, Options{Hidden: true, Files: true, Natural: true})
	got := names(es)
	// Directories first, then files (including the broken link).
	if got[0] != ".hidden" || got[len(got)-2] != "afile" || got[len(got)-1] != "broken" {
		t.Errorf("got %v", got)
	}
	if !es[len(es)-1].Broken {
		t.Error("broken link not flagged")
	}
}
