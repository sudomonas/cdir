// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import "testing"

func TestParseKeys(t *testing.T) {
	cases := map[string][]keyKind{
		"\r":            {kEnter},
		"\n":            {kSelect},
		"\x1b[13;5u":    {kSelect}, // kitty Ctrl+Enter
		"\x1b[27;5;13~": {kSelect}, // xterm modifyOtherKeys Ctrl+Enter
		"\x1b[13u":      {kEnter},
		"\x1b":          {kEsc},
		"\x1b[27u":      {kEsc},
		"\x1b[A\x1bOB":  {kUp, kDown},
		"\x1b[5~":       {kPgUp},
		"\x7f":          {kBackspace},
		"\x03":          {kCtrlC},
		"\x1b[99;5u":    {kCtrlC},
		"jk":            {kRune, kRune},
		"ü":             {kRune},
		"\x1bx":         {}, // Alt+x: ignored
		"\x1b[1;5A":     {kUp},
	}
	for in, want := range cases {
		got := parseKeys([]byte(in))
		if len(got) != len(want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i].kind != want[i] {
				t.Errorf("%q: got %v, want %v", in, got, want)
			}
		}
	}
}

func TestSuggestName(t *testing.T) {
	for in, want := range map[string]string{
		"/x/Napkin": "napkin", "/x/My Project.v2": "my-project-v2", "/x/.config": "config", "/x/日本": "",
	} {
		if got := suggestName(in); got != want {
			t.Errorf("suggestName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFit(t *testing.T) {
	if got := fit("abcdef", 4); got != "abc…" {
		t.Errorf("fit = %q", got)
	}
	if got := fitLeft("/a/b/c/d", 5); got != "…/c/d" {
		t.Errorf("fitLeft = %q", got)
	}
	if strWidth("日本") != 4 {
		t.Error("wide runes")
	}
}
