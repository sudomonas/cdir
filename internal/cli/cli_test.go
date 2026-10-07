// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdir/internal/bookmarks"
	"cdir/internal/ui"
)

type harness struct {
	t      *testing.T
	root   string
	env    *Env
	out    *bytes.Buffer
	errb   *bytes.Buffer
	uiOpts *ui.Options
	uiRet  string
	uiErr  error
	answer bool
	asked  bool
}

func newHarness(t *testing.T) *harness {
	root := t.TempDir()
	for _, d := range []string{"home/Projects/Napkin", "home/Projects/Segue", "home/My Notes", "work"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	h := &harness{t: t, root: root, out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	h.env = &Env{
		Stdout:     h.out,
		Stderr:     h.errb,
		Home:       filepath.Join(root, "home"),
		Cwd:        filepath.Join(root, "work"),
		Version:    "test",
		Store:      bookmarks.Store{Path: filepath.Join(root, "cfg", "bookmarks")},
		ConfigPath: filepath.Join(root, "cfg", "config"),
		Interactive: func(o ui.Options) (string, error) {
			h.uiOpts = &o
			return h.uiRet, h.uiErr
		},
		Confirm: func(string) (bool, error) { h.asked = true; return h.answer, nil },
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.errb.Reset()
	h.uiOpts = nil
	return Run(args, h.env)
}

func (h *harness) p(rel string) string { return filepath.Join(h.root, rel) }

func (h *harness) expect(code int, args ...string) {
	h.t.Helper()
	if got := h.run(args...); got != code {
		h.t.Fatalf("c %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), got, code, h.out, h.errb)
	}
}

func TestAddAndJump(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-a", "napkin", "~/Projects/Napkin")
	h.expect(ExitOK, "napkin")
	if got := h.out.String(); got != h.p("home/Projects/Napkin")+"\n" {
		t.Errorf("stdout = %q", got)
	}
	if h.uiOpts != nil {
		t.Error("direct lookup must not start the UI")
	}
}

func TestAddDefaultsToCwdAndRelative(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-a", "here")
	h.expect(ExitOK, "--add", "notes", "../home/My Notes")
	h.expect(ExitOK, "here")
	if h.out.String() != h.p("work")+"\n" {
		t.Errorf("got %q", h.out)
	}
	h.expect(ExitOK, "notes")
	if h.out.String() != h.p("home/My Notes")+"\n" {
		t.Errorf("got %q", h.out)
	}
}

func TestAddDuplicate(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-a", "x", h.p("home/Projects/Napkin"))

	h.answer = false
	h.expect(ExitCancelled, "-a", "x", h.p("home/Projects/Segue"))
	if !h.asked {
		t.Error("should have asked before replacing")
	}
	h.expect(ExitOK, "x")
	if !strings.Contains(h.out.String(), "Napkin") {
		t.Error("declined replace still changed the bookmark")
	}

	h.answer = true
	h.expect(ExitOK, "-a", "x", h.p("home/Projects/Segue"))
	h.expect(ExitOK, "x")
	if !strings.Contains(h.out.String(), "Segue") {
		t.Error("confirmed replace did not change the bookmark")
	}

	h.asked = false
	h.expect(ExitOK, "-a", "-f", "x", h.p("home/Projects/Napkin"))
	h.expect(ExitOK, "--force", "-a", "x", h.p("home/Projects/Segue"))
	h.expect(ExitOK, "-af", "x", h.p("home/Projects/Napkin"))
	if h.asked {
		t.Error("--force must not ask")
	}

	h.env.Confirm = func(string) (bool, error) { return false, ui.ErrNoTerminal }
	h.expect(ExitError, "-a", "x", h.p("home/Projects/Segue"))
}

func TestAddValidation(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitMissing, "-a", "nope", "/does/not/exist")
	h.expect(ExitOK, "-a", "-m", "nope", "/does/not/exist")
	os.WriteFile(h.p("file"), nil, 0o644)
	h.expect(ExitMissing, "-a", "-m", "f", h.p("file"))
	for _, bad := range []string{"a/b", "a=b", "a b", "a:b", `a\b`} {
		h.expect(ExitUsage, "-a", bad, h.p("work"))
	}
	h.expect(ExitUsage, "-a", "--", "-l", h.p("work"))
	h.expect(ExitUsage, "-a")
	h.expect(ExitUsage, "-a", "x", "y", "z")
}

func TestJumpErrors(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitNotFound, "nothing")
	h.expect(ExitOK, "-a", "-m", "gone", "/does/not/exist")
	h.expect(ExitMissing, "gone")
	if h.out.Len() != 0 {
		t.Error("nothing may be printed on stdout on failure")
	}
	h.expect(ExitUsage, "not/a/name")
	h.expect(ExitUsage, "a", "b")
}

func TestPrefixAndAmbiguity(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-a", "napkin", h.p("home/Projects/Napkin"))
	h.expect(ExitOK, "-a", "segue", h.p("home/Projects/Segue"))
	h.expect(ExitOK, "nap")
	if h.out.String() != h.p("home/Projects/Napkin")+"\n" || h.uiOpts != nil {
		t.Errorf("unique prefix should jump directly: %q", h.out)
	}

	h.expect(ExitOK, "-a", "napkin-old", h.p("home/Projects/Segue"))
	h.expect(ExitOK, "napkin") // exact match beats prefix
	if h.uiOpts != nil {
		t.Error("exact match must not open the picker")
	}

	h.uiRet = h.p("home/Projects/Segue")
	h.expect(ExitOK, "nap")
	if h.uiOpts == nil || !h.uiOpts.Bookmarks || h.uiOpts.Query != "nap" {
		t.Fatalf("ambiguous prefix should open the picker: %+v", h.uiOpts)
	}
	if h.out.String() != h.uiRet+"\n" {
		t.Errorf("stdout = %q", h.out)
	}

	h.uiErr = ui.ErrNoTerminal
	h.expect(ExitNotFound, "nap")
	if !strings.Contains(h.errb.String(), "napkin-old") {
		t.Errorf("candidates not listed: %s", h.errb)
	}
}

func TestDeleteAndList(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-a", "napkin", h.p("home/Projects/Napkin"))
	h.expect(ExitOK, "-a", "-m", "gone", "/does/not/exist")

	h.env.StdoutTTY = true
	h.expect(ExitOK, "-l")
	out := h.out.String()
	if !strings.Contains(out, "napkin  ~/Projects/Napkin") || !strings.Contains(out, "[missing]") {
		t.Errorf("list output:\n%s", out)
	}
	h.env.StdoutTTY = false
	h.expect(ExitOK, "--list")
	if h.out.String() != "napkin\t"+h.p("home/Projects/Napkin")+"\ngone\t/does/not/exist\n" {
		t.Errorf("plain list: %q", h.out)
	}

	h.expect(ExitNotFound, "-d", "gone", "unknown")
	h.expect(ExitOK, "--names")
	if h.out.String() != "napkin\n" {
		t.Errorf("names after delete: %q", h.out)
	}
	h.expect(ExitUsage, "-d")
	h.expect(ExitUsage, "-l", "extra")
	h.expect(ExitUsage, "-l", "-d", "x")
	h.expect(ExitUsage, "-f", "napkin")
}

func TestInteractive(t *testing.T) {
	h := newHarness(t)
	h.uiRet = h.p("home")
	h.expect(ExitOK)
	if h.uiOpts.Start != h.env.Cwd || h.out.String() != h.p("home")+"\n" {
		t.Errorf("start %q, stdout %q", h.uiOpts.Start, h.out)
	}

	h.uiErr = ui.ErrCancelled
	h.expect(ExitCancelled)
	if h.out.Len() != 0 {
		t.Error("cancel must print nothing")
	}
	h.uiErr = nil

	h.expect(ExitOK, "-a", "napkin", h.p("home/Projects/Napkin"))
	h.expect(ExitOK, "-i", "napkin")
	if h.uiOpts.Start != h.p("home/Projects/Napkin") {
		t.Errorf("-i NAME start = %q", h.uiOpts.Start)
	}
	h.expect(ExitOK, "-i", "~/Projects")
	if h.uiOpts.Start != h.p("home/Projects") {
		t.Errorf("-i PATH start = %q", h.uiOpts.Start)
	}
	h.expect(ExitNotFound, "-i", "nosuch")
	h.expect(ExitMissing, "-i", "./nosuch")
}

func TestHelpVersion(t *testing.T) {
	h := newHarness(t)
	h.expect(ExitOK, "-h")
	if !strings.Contains(h.out.String(), "Usage") {
		t.Error("help missing")
	}
	h.expect(ExitOK, "--version")
	if h.out.String() != "cdir test\n" {
		t.Errorf("version %q", h.out)
	}
	h.expect(ExitUsage, "--bogus")
}
