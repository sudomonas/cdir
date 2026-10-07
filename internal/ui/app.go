// SPDX-License-Identifier: GPL-3.0-or-later

// Package ui implements the interactive directory browser and bookmark list.
//
// All drawing goes to /dev/tty; the selected directory is returned to the
// caller, which prints it on stdout.
package ui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"cdir/internal/bookmarks"
	"cdir/internal/browser"
	"cdir/internal/config"
	"cdir/internal/fuzzy"
	"cdir/internal/paths"
)

var (
	// ErrCancelled is returned when the user quits without choosing.
	ErrCancelled = errors.New("cancelled")
	// ErrNoTerminal is returned when there is no terminal to draw on.
	ErrNoTerminal = errors.New("no terminal available")
)

// Options configures a UI session.
type Options struct {
	Start     string // directory to start browsing in
	Home      string
	Store     bookmarks.Store
	Config    config.Config
	Warnings  []string // shown once at startup
	Bookmarks bool     // start in the bookmark list; Esc cancels instead of going back
	Query     string   // initial bookmark filter
}

// Run shows the UI and returns the chosen directory.
func Run(opt Options) (dir string, err error) {
	t, err := openTerminal()
	if err != nil {
		return "", err
	}
	defer t.close()
	defer func() {
		if r := recover(); r != nil {
			t.close()
			panic(r)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(sigs)
	defer signal.Stop(winch)

	input := make(chan []byte, 16)
	go t.readLoop(input)

	a := newApp(t, opt)
	for !a.done {
		a.render()
		select {
		case chunk, ok := <-input:
			if !ok {
				return "", ErrCancelled
			}
			for _, k := range parseKeys(chunk) {
				a.handle(k)
				if a.done {
					break
				}
			}
		case <-winch:
		case <-sigs:
			return "", ErrCancelled
		}
	}
	if a.result == "" {
		return "", ErrCancelled
	}
	return a.result, nil
}

type view int

const (
	viewBrowse view = iota
	viewBookmarks
)

type rowKind int

const (
	rowSelf   rowKind = iota // "./"  — the current directory
	rowParent                // "../"
	rowEntry
)

type row struct {
	kind rowKind
	e    browser.Entry
}

type mark struct {
	bookmarks.Bookmark
	status error // result of paths.CheckDir
}

// lineEdit is a one-line text input shown on the status line.
type lineEdit struct {
	label   string
	text    []rune
	onEnter func(string)
}

type confirmation struct {
	question string
	onYes    func()
}

type app struct {
	t     *terminal
	opt   Options
	style style

	view    view
	help    bool
	done    bool
	result  string
	msg     string
	msgErr  bool
	edit    *lineEdit
	confirm *confirmation
	picker  bool
	listed  bool // browse listing loaded
	hidden  bool
	files   bool

	// directory browser
	dir       string
	entries   []browser.Entry
	rows      []row
	cursor    int
	offset    int
	query     string
	searching bool

	// bookmark list
	marks      []mark
	shown      []int // indices into marks after filtering
	bcursor    int
	boffset    int
	bquery     string
	bsearching bool
}

func newApp(t *terminal, opt Options) *app {
	a := &app{
		t:      t,
		opt:    opt,
		style:  newStyle(),
		hidden: opt.Config.ShowHidden,
		files:  opt.Config.ShowFiles,
		picker: opt.Bookmarks,
		dir:    opt.Start,
	}
	if len(opt.Warnings) > 0 {
		a.setErr(opt.Warnings[0])
	}
	if opt.Bookmarks {
		a.view = viewBookmarks
		a.bquery = opt.Query
		a.loadMarks()
		return a
	}
	a.ensureListed()
	return a
}

// ensureListed loads the browser listing, falling back to $HOME and / if
// the start directory cannot be read.
func (a *app) ensureListed() {
	if a.listed {
		return
	}
	for _, d := range []string{a.dir, a.opt.Home, "/"} {
		if d == "" {
			continue
		}
		if err := a.chdir(d, ""); err == nil {
			return
		} else if d == a.dir {
			a.setErr(fmt.Sprintf("Cannot open %s: %v", a.abbrev(d), err))
		}
	}
}

// ---- messages ---------------------------------------------------------

func (a *app) setMsg(s string) { a.msg, a.msgErr = s, false }
func (a *app) setErr(s string) { a.msg, a.msgErr = s, true }

func (a *app) abbrev(p string) string { return paths.Printable(paths.Abbrev(p, a.opt.Home)) }

// ---- browser ----------------------------------------------------------

func (a *app) listOpts() browser.Options {
	return browser.Options{Hidden: a.hidden, Files: a.files, Natural: a.opt.Config.Sort != "alpha"}
}

// chdir switches the browser to dir, placing the cursor on focus if given.
func (a *app) chdir(dir, focus string) error {
	if err := paths.CheckDir(dir); err != nil {
		return err
	}
	entries, err := browser.List(dir, a.listOpts())
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return paths.ErrPermission
		}
		return err
	}
	a.dir, a.entries, a.listed = dir, entries, true
	a.query, a.searching = "", false
	a.rebuildRows()
	a.cursor, a.offset = a.defaultCursor(), 0
	if focus != "" {
		a.focus(focus)
	}
	return nil
}

func (a *app) enter(dir, focus string) {
	if err := a.chdir(dir, focus); err != nil {
		a.setErr(fmt.Sprintf("Cannot enter %s: %v", a.abbrev(dir), err))
	}
}

func (a *app) reload() {
	name := ""
	if r, ok := a.current(); ok && r.kind == rowEntry {
		name = r.e.Name
	}
	q, s := a.query, a.searching
	a.enter(a.dir, name)
	a.query, a.searching = q, s
	if q != "" {
		a.rebuildRows()
		a.cursor = 0
	}
}

func (a *app) rebuildRows() {
	a.rows = a.rows[:0]
	if a.query == "" {
		a.rows = append(a.rows, row{kind: rowSelf})
		if a.dir != "/" {
			a.rows = append(a.rows, row{kind: rowParent})
		}
		for _, e := range a.entries {
			a.rows = append(a.rows, row{kind: rowEntry, e: e})
		}
		return
	}
	for _, i := range fuzzy.Filter(a.query, len(a.entries), func(i int) string { return a.entries[i].Name }) {
		a.rows = append(a.rows, row{kind: rowEntry, e: a.entries[i]})
	}
}

// defaultCursor is the first real entry, or "../" in an empty directory.
func (a *app) defaultCursor() int {
	for i, r := range a.rows {
		if r.kind == rowEntry {
			return i
		}
	}
	return len(a.rows) - 1
}

func (a *app) focus(name string) {
	for i, r := range a.rows {
		if r.kind == rowEntry && r.e.Name == name {
			a.cursor = i
			return
		}
	}
}

func (a *app) current() (row, bool) {
	if a.cursor < 0 || a.cursor >= len(a.rows) {
		return row{}, false
	}
	return a.rows[a.cursor], true
}

// target returns the directory a row stands for.
func (a *app) target(r row) (string, bool) {
	switch r.kind {
	case rowSelf:
		return a.dir, true
	case rowParent:
		return filepath.Dir(a.dir), true
	}
	if !r.e.Dir {
		return "", false
	}
	return filepath.Join(a.dir, r.e.Name), true
}

func (a *app) open() {
	r, ok := a.current()
	if !ok {
		return
	}
	switch r.kind {
	case rowSelf:
		a.choose(a.dir)
	case rowParent:
		a.up()
	default:
		if dir, ok := a.target(r); ok {
			a.enter(dir, "")
		} else if r.e.Broken {
			a.setErr(paths.Printable(r.e.Name) + ": broken symbolic link")
		} else {
			a.setErr(paths.Printable(r.e.Name) + " is not a directory")
		}
	}
}

func (a *app) selectCurrent() {
	r, ok := a.current()
	if !ok {
		a.choose(a.dir)
		return
	}
	if dir, ok := a.target(r); ok {
		a.choose(dir)
	} else {
		a.setErr(paths.Printable(r.e.Name) + " is not a directory")
	}
}

func (a *app) up() {
	if a.dir == "/" {
		return
	}
	a.enter(filepath.Dir(a.dir), filepath.Base(a.dir))
}

// choose finishes the session with dir as the result.
func (a *app) choose(dir string) {
	if a.opt.Config.FollowSymlinks {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
	}
	if err := paths.CheckDir(dir); err != nil {
		a.setErr(fmt.Sprintf("Cannot use %s: %v", a.abbrev(dir), err))
		return
	}
	a.result, a.done = dir, true
}

func (a *app) cancel() { a.result, a.done = "", true }

func move(cursor, delta, n int) int {
	cursor += delta
	if cursor >= n {
		cursor = n - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	return cursor
}

func (a *app) pageSize() int {
	_, h := a.t.size()
	return max(1, listHeight(h)-1)
}

// navKey applies a cursor-movement key; it reports whether k was one.
func (a *app) navKey(k key, cursor *int, n int) bool {
	switch {
	case k.kind == kUp || k.kind == kCtrlP:
		*cursor = move(*cursor, -1, n)
	case k.kind == kDown || k.kind == kCtrlN:
		*cursor = move(*cursor, 1, n)
	case k.kind == kPgUp:
		*cursor = move(*cursor, -a.pageSize(), n)
	case k.kind == kPgDn:
		*cursor = move(*cursor, a.pageSize(), n)
	case k.kind == kHome:
		*cursor = 0
	case k.kind == kEnd:
		*cursor = max(0, n-1)
	default:
		return false
	}
	return true
}

func (a *app) vimNav(k key, cursor *int, n int) bool {
	if k.kind != kRune {
		return false
	}
	switch k.r {
	case 'j':
		*cursor = move(*cursor, 1, n)
	case 'k':
		*cursor = move(*cursor, -1, n)
	case 'g':
		*cursor = 0
	case 'G':
		*cursor = max(0, n-1)
	default:
		return false
	}
	return true
}

// editText applies a text-editing key to s; it reports whether k was one.
func editText(s *[]rune, k key) bool {
	switch k.kind {
	case kRune:
		*s = append(*s, k.r)
	case kBackspace:
		if len(*s) > 0 {
			*s = (*s)[:len(*s)-1]
		}
	case kCtrlU:
		*s = (*s)[:0]
	case kCtrlW:
		t := *s
		for len(t) > 0 && t[len(t)-1] == ' ' {
			t = t[:len(t)-1]
		}
		for len(t) > 0 && t[len(t)-1] != ' ' && t[len(t)-1] != '/' {
			t = t[:len(t)-1]
		}
		*s = t
	default:
		return false
	}
	return true
}

// ---- key handling -----------------------------------------------------

func (a *app) handle(k key) {
	if k.kind == kCtrlC {
		a.cancel()
		return
	}
	a.msg = ""
	switch {
	case a.help:
		a.help = false
	case a.confirm != nil:
		c := a.confirm
		a.confirm = nil
		if k.kind == kRune && (k.r == 'y' || k.r == 'Y') {
			c.onYes()
		} else {
			a.setMsg("Cancelled")
		}
	case a.edit != nil:
		a.handleEdit(k)
	case a.view == viewBookmarks:
		a.handleBookmarks(k)
	case a.searching:
		a.handleSearch(k)
	default:
		a.handleBrowse(k)
	}
}

func (a *app) handleEdit(k key) {
	e := a.edit
	switch k.kind {
	case kEsc:
		a.edit = nil
		a.setMsg("Cancelled")
	case kEnter, kSelect:
		e.onEnter(string(e.text))
	default:
		editText(&e.text, k)
	}
}

func (a *app) handleBrowse(k key) {
	if a.navKey(k, &a.cursor, len(a.rows)) || a.vimNav(k, &a.cursor, len(a.rows)) {
		return
	}
	switch k.kind {
	case kEnter, kRight:
		a.open()
	case kSelect:
		a.selectCurrent()
	case kBackspace, kLeft:
		a.up()
	case kEsc:
		if a.query != "" {
			a.clearFilter()
		} else {
			a.cancel()
		}
	case kRune:
		switch k.r {
		case 'l':
			a.open()
		case 'h', '-':
			a.up()
		case ' ':
			a.selectCurrent()
		case '~':
			a.enter(a.opt.Home, "")
		case '/':
			a.searching = true
		case 'a':
			if r, ok := a.current(); ok {
				if dir, ok := a.target(r); ok {
					a.promptBookmark(dir)
				} else {
					a.setErr("Only directories can be bookmarked")
				}
			}
		case 'A':
			a.promptBookmark(a.dir)
		case 'b':
			a.showBookmarks()
		case '.':
			a.hidden = !a.hidden
			a.reload()
			a.setMsg(onOff("Hidden directories", a.hidden))
		case 'f':
			a.files = !a.files
			a.reload()
			a.setMsg(onOff("Files", a.files))
		case '?':
			a.help = true
		case 'q':
			a.cancel()
		}
	}
}

func onOff(what string, on bool) string {
	if on {
		return what + " shown"
	}
	return what + " hidden"
}

func (a *app) clearFilter() {
	a.query, a.searching = "", false
	a.rebuildRows()
	a.cursor = a.defaultCursor()
}

// handleSearch filters the current directory as the user types.
// Enter opens the best match, Tab keeps the filter and returns to
// normal keys, Esc clears it.
func (a *app) handleSearch(k key) {
	if a.navKey(k, &a.cursor, len(a.rows)) {
		return
	}
	switch k.kind {
	case kEsc:
		a.clearFilter()
	case kEnter:
		if r, ok := a.current(); ok {
			if dir, ok := a.target(r); ok {
				a.enter(dir, "")
			} else {
				a.setErr(paths.Printable(r.e.Name) + " is not a directory")
			}
		}
	case kSelect:
		if len(a.rows) > 0 {
			a.selectCurrent()
		}
	case kTab:
		a.searching = false
	case kBackspace:
		if a.query == "" {
			a.searching = false
			return
		}
		fallthrough
	default:
		q := []rune(a.query)
		if editText(&q, k) {
			a.query = string(q)
			a.rebuildRows()
			a.cursor = 0
			if a.query == "" {
				a.cursor = a.defaultCursor()
			}
		}
	}
}

// ---- bookmarks --------------------------------------------------------

func (a *app) loadMarks() {
	f, warns, err := a.opt.Store.Load()
	if err != nil {
		a.setErr("Cannot read bookmarks: " + err.Error())
		f = &bookmarks.File{}
	} else if len(warns) > 0 {
		a.setErr(fmt.Sprintf("bookmarks %s (run c -l for details)", warns[0]))
	}
	a.marks = a.marks[:0]
	for _, b := range f.Bookmarks() {
		a.marks = append(a.marks, mark{b, paths.CheckDir(b.Path)})
	}
	a.filterMarks()
}

func (a *app) filterMarks() {
	a.shown = fuzzy.Filter(a.bquery, len(a.marks), func(i int) string { return a.marks[i].Name })
	a.bcursor = move(a.bcursor, 0, len(a.shown))
}

func (a *app) showBookmarks() {
	a.view = viewBookmarks
	a.bquery, a.bsearching, a.bcursor, a.boffset = "", false, 0, 0
	a.loadMarks()
	if len(a.marks) == 0 && a.msg == "" {
		a.setMsg("No bookmarks yet — press a in the browser to add one")
	}
}

func (a *app) currentMark() (mark, bool) {
	if a.bcursor < 0 || a.bcursor >= len(a.shown) {
		return mark{}, false
	}
	return a.marks[a.shown[a.bcursor]], true
}

func (a *app) chooseMark() {
	m, ok := a.currentMark()
	if !ok {
		return
	}
	if m.status != nil {
		a.setErr(fmt.Sprintf("%s: %s: %v", m.Name, a.abbrev(m.Path), m.status))
		return
	}
	a.choose(m.Path)
}

func (a *app) browseMark() {
	m, ok := a.currentMark()
	if !ok {
		return
	}
	if err := a.chdir(m.Path, ""); err != nil {
		a.setErr(fmt.Sprintf("Cannot enter %s: %v", a.abbrev(m.Path), err))
		return
	}
	a.view = viewBrowse
}

func (a *app) backToBrowser() {
	a.view = viewBrowse
	a.ensureListed()
}

func (a *app) handleBookmarks(k key) {
	n := len(a.shown)
	if a.bsearching {
		if a.navKey(k, &a.bcursor, n) {
			return
		}
		switch k.kind {
		case kEsc:
			a.bquery, a.bsearching = "", false
			a.filterMarks()
		case kEnter, kSelect:
			a.chooseMark()
		case kTab:
			a.bsearching = false
		case kBackspace:
			if a.bquery == "" {
				a.bsearching = false
				return
			}
			fallthrough
		default:
			q := []rune(a.bquery)
			if editText(&q, k) {
				a.bquery = string(q)
				a.bcursor = 0
				a.filterMarks()
			}
		}
		return
	}
	if a.navKey(k, &a.bcursor, n) || a.vimNav(k, &a.bcursor, n) {
		return
	}
	switch k.kind {
	case kEnter, kSelect:
		a.chooseMark()
	case kRight:
		a.browseMark()
	case kLeft, kBackspace:
		a.backToBrowser()
	case kEsc:
		switch {
		case a.bquery != "":
			a.bquery = ""
			a.filterMarks()
		case a.picker:
			a.cancel()
		default:
			a.backToBrowser()
		}
	case kRune:
		switch k.r {
		case ' ':
			a.chooseMark()
		case 'l':
			a.browseMark()
		case 'h', 'b':
			a.backToBrowser()
		case '/':
			a.bsearching = true
		case 'd':
			if m, ok := a.currentMark(); ok {
				a.confirm = &confirmation{
					question: fmt.Sprintf("Delete bookmark '%s' (%s)? [y/N] ", m.Name, a.abbrev(m.Path)),
					onYes:    func() { a.deleteMark(m.Name) },
				}
			}
		case '?':
			a.help = true
		case 'q':
			a.cancel()
		}
	}
}

func (a *app) deleteMark(name string) {
	err := a.opt.Store.Update(func(f *bookmarks.File) error {
		if !f.Delete(name) {
			return bookmarks.ErrNoChange
		}
		return nil
	})
	if err != nil {
		a.setErr("Cannot delete bookmark: " + err.Error())
		return
	}
	a.loadMarks()
	a.setMsg("Deleted " + name)
}

// suggestName derives a bookmark name from a directory's base name.
func suggestName(dir string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(filepath.Base(dir)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 && (r == '-' || r == ' ' || r == '.' || unicode.IsPunct(r)) {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func (a *app) promptBookmark(dir string) {
	a.edit = &lineEdit{
		label: "Bookmark " + a.abbrev(dir) + " as: ",
		text:  []rune(suggestName(dir)),
		onEnter: func(name string) {
			name = strings.TrimSpace(name)
			if name == "" {
				a.edit = nil
				a.setMsg("Cancelled")
				return
			}
			if !bookmarks.ValidName(name) {
				a.setErr("Names may use letters, digits, '_' and '-' (not first)")
				return
			}
			a.edit = nil
			f, _, err := a.opt.Store.Load()
			if err != nil {
				a.setErr("Cannot read bookmarks: " + err.Error())
				return
			}
			if old, ok := f.Lookup(name); ok {
				if old == dir {
					a.setMsg(fmt.Sprintf("Already saved: %s → %s", name, a.abbrev(dir)))
					return
				}
				a.confirm = &confirmation{
					question: fmt.Sprintf("'%s' already points to %s. Replace? [y/N] ", name, a.abbrev(old)),
					onYes:    func() { a.saveBookmark(name, dir) },
				}
				return
			}
			a.saveBookmark(name, dir)
		},
	}
}

func (a *app) saveBookmark(name, dir string) {
	err := a.opt.Store.Update(func(f *bookmarks.File) error {
		f.Set(name, dir)
		return nil
	})
	if err != nil {
		a.setErr("Cannot save bookmark: " + err.Error())
		return
	}
	a.setMsg(fmt.Sprintf("Saved: %s → %s", name, paths.Printable(dir)))
	if a.view == viewBookmarks {
		a.loadMarks()
	}
}
