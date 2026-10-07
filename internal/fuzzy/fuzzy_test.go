// SPDX-License-Identifier: GPL-3.0-or-later

package fuzzy

import "testing"

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		p, s string
		ok   bool
	}{
		{"nap", "Napkin", true},
		{"npk", "Napkin", true},
		{"NAP", "napkin", true},
		{"kp", "Napkin", false},
		{"", "anything", true},
		{"日", "日本", true},
	} {
		if _, ok := Match(c.p, c.s); ok != c.ok {
			t.Errorf("Match(%q, %q) = %v", c.p, c.s, ok)
		}
	}
}

func TestFilterRanking(t *testing.T) {
	items := []string{"snapshots", "Napkin Notes", "Napkin", "unrelated"}
	got := Filter("nap", len(items), func(i int) string { return items[i] })
	want := []int{2, 1, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
