// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"os"
	"strings"

	"cdir/internal/paths"
)

// style holds SGR sequences. Only the terminal's own palette (the 16 basic
// colors, bold, dim, reverse) is used, so light and dark themes both work.
type style struct {
	reset, bold, dim, rev, dir, err string
}

func newStyle() style {
	s := style{reset: "\x1b[m", bold: "\x1b[1m", dim: "\x1b[2m", rev: "\x1b[7m", dir: "\x1b[1;34m", err: "\x1b[31m"}
	if os.Getenv("NO_COLOR") != "" {
		s.dir, s.err = s.bold, s.bold
	}
	return s
}

// Screen layout: header, list, status line, footer.
func listHeight(h int) int { return max(1, h-3) }

func scroll(cursor, offset, n, height int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	if offset > n-height {
		offset = n - height
	}
	return max(0, offset)
}

var helpText = []string{
	"Directory browser",
	"  j k  ↑ ↓          move              g G  Home End   first / last",
	"  Enter  l  →       open directory    Backspace h ←   parent directory",
	"  Space  Ctrl+Enter select highlighted directory and exit (Ctrl+J also works)",
	"  Enter on ./       select the directory you are standing in",
	"  /                 filter (Enter opens, Tab keeps filter, Esc clears)",
	"  a                 bookmark highlighted directory",
	"  A                 bookmark current directory",
	"  b                 bookmark list",
	"  ~                 home directory",
	"  .                 show/hide hidden directories",
	"  f                 show/hide files",
	"  q  Esc  Ctrl+C    quit without changing directory",
	"",
	"Bookmark list",
	"  Enter  Space      go to bookmark       l →       browse from bookmark",
	"  /                 filter               d         delete (asks first)",
	"  Esc  h  b         back to browser      q         quit",
	"",
	"Press any key to close this help.",
}

func (a *app) render() {
	w, h := a.t.size()
	s := a.style
	var out strings.Builder
	out.WriteString("\x1b[?25l")
	line := func(y int, text string) {
		fmt.Fprintf(&out, "\x1b[%d;1H%s\x1b[K", y, text)
	}

	// Header.
	var title, info string
	if a.view == viewBookmarks {
		title = "Bookmarks"
		info = fmt.Sprintf("%d", len(a.marks))
		if a.bquery != "" {
			info = fmt.Sprintf("%d/%d", len(a.shown), len(a.marks))
		}
	} else {
		title = a.abbrev(a.dir)
		var flags []string
		if a.hidden {
			flags = append(flags, "hidden")
		}
		if a.files {
			flags = append(flags, "files")
		}
		info = strings.Join(flags, " ")
	}
	tw := w - 2 - strWidth(info) - 2
	title = fitLeft(title, tw)
	gap := max(1, w-2-strWidth(title)-strWidth(info)-1)
	line(1, " "+s.bold+title+s.reset+strings.Repeat(" ", gap)+s.dim+info+s.reset)

	// List.
	lh := listHeight(h)
	if a.help {
		for i := 0; i < lh; i++ {
			text := ""
			if i < len(helpText) {
				text = " " + fit(helpText[i], w-1)
			}
			line(2+i, text)
		}
	} else if a.view == viewBookmarks {
		a.renderMarks(line, w, lh)
	} else {
		a.renderRows(line, w, lh)
	}

	// Status line: prompt, confirmation or active filter.
	cursorCol := 0
	switch {
	case a.confirm != nil:
		q := fit(a.confirm.question, w-1)
		line(h-1, " "+s.bold+q+s.reset)
		cursorCol = 2 + strWidth(q)
	case a.edit != nil:
		label := fit(a.edit.label, w/2)
		text := fitLeft(string(a.edit.text), w-2-strWidth(label))
		line(h-1, " "+s.bold+label+s.reset+text)
		cursorCol = 2 + strWidth(label) + strWidth(text)
	case a.view == viewBrowse && (a.searching || a.query != ""):
		text := fitLeft(paths.Printable(a.query), w-4)
		line(h-1, " /"+text)
		if a.searching {
			cursorCol = 3 + strWidth(text)
		}
	case a.view == viewBookmarks && (a.bsearching || a.bquery != ""):
		text := fitLeft(paths.Printable(a.bquery), w-4)
		line(h-1, " /"+text)
		if a.bsearching {
			cursorCol = 3 + strWidth(text)
		}
	default:
		line(h-1, "")
	}

	// Footer: message, or key hints.
	switch {
	case a.msg != "" && a.msgErr:
		line(h, " "+s.err+fit(a.msg, w-1)+s.reset)
	case a.msg != "":
		line(h, " "+fit(a.msg, w-1))
	default:
		line(h, " "+s.dim+fit(a.hints(), w-1)+s.reset)
	}

	if cursorCol > 0 && cursorCol <= w {
		fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[?25h", h-1, cursorCol)
	}
	a.t.write(out.String())
}

func (a *app) hints() string {
	switch {
	case a.help:
		return "any key: close help"
	case a.confirm != nil:
		return "y: yes   any other key: no"
	case a.edit != nil:
		return "Enter: save   Esc: cancel   Ctrl+U: clear"
	case a.view == viewBookmarks && a.bsearching:
		return "type to filter   Enter: go   Tab: keep filter   Esc: clear"
	case a.view == viewBookmarks:
		return "Enter: go   l: browse   d: delete   /: filter   Esc: back   q: quit   ?: help"
	case a.searching:
		return "type to filter   Enter: open   Ctrl+J: select   Tab: keep filter   Esc: clear"
	}
	return "Enter: open   Space: select   Bksp: up   /: search   a: bookmark   b: bookmarks   .: hidden   ?: help   q: quit"
}

func (a *app) renderRows(line func(int, string), w, lh int) {
	s := a.style
	a.offset = scroll(a.cursor, a.offset, len(a.rows), lh)
	for i := 0; i < lh; i++ {
		idx := a.offset + i
		if idx >= len(a.rows) {
			if idx == 0 && a.query != "" {
				line(2, " "+s.dim+"no matches"+s.reset)
				continue
			}
			line(2+i, "")
			continue
		}
		r := a.rows[idx]
		var name, note string
		isDir := true
		switch r.kind {
		case rowSelf:
			name, note = "./", "  this directory"
		case rowParent:
			name = "../"
		default:
			name = paths.Printable(r.e.Name)
			isDir = r.e.Dir
			if isDir {
				name += "/"
			}
			if r.e.Link {
				note = "  → " + paths.Printable(r.e.Target)
				if r.e.Broken {
					note += "  [broken]"
				}
			}
		}
		if idx == a.cursor {
			line(2+i, s.rev+" "+pad(name+note, w-1)+s.reset)
			continue
		}
		name = fit(name, w-1)
		note = fit(note, w-1-strWidth(name))
		color := ""
		if isDir {
			color = s.dir
		}
		line(2+i, " "+color+name+s.reset+s.dim+note+s.reset)
	}
}

func (a *app) renderMarks(line func(int, string), w, lh int) {
	s := a.style
	nameW := 0
	for _, i := range a.shown {
		nameW = max(nameW, len(a.marks[i].Name))
	}
	nameW = min(nameW, max(8, w/3))
	a.boffset = scroll(a.bcursor, a.boffset, len(a.shown), lh)
	for i := 0; i < lh; i++ {
		idx := a.boffset + i
		if idx >= len(a.shown) {
			switch {
			case idx == 0 && len(a.marks) == 0:
				line(2, " "+s.dim+"no bookmarks"+s.reset)
			case idx == 0:
				line(2, " "+s.dim+"no matches"+s.reset)
			default:
				line(2+i, "")
			}
			continue
		}
		m := a.marks[a.shown[idx]]
		name := pad(m.Name, nameW)
		p := a.abbrev(m.Path)
		note := ""
		if m.status != nil {
			note = "  [missing]"
			if !paths.IsMissing(m.status) {
				note = "  [" + m.status.Error() + "]"
			}
		}
		rest := w - 1 - nameW - 2 - strWidth(note)
		p = fitLeft(p, rest)
		if idx == a.bcursor {
			line(2+i, s.rev+" "+pad(name+"  "+p+note, w-1)+s.reset)
			continue
		}
		line(2+i, " "+s.bold+name+s.reset+"  "+p+s.err+note+s.reset)
	}
}
