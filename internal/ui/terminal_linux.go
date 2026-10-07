// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package ui

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// terminal is the controlling terminal (/dev/tty) in raw mode. The UI never
// touches stdout, which is reserved for the selected path.
type terminal struct {
	f      *os.File
	orig   syscall.Termios
	kitty  bool
	once   sync.Once
	closed bool
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// IsTerminal reports whether fd refers to a terminal.
func IsTerminal(fd uintptr) bool {
	var t syscall.Termios
	return ioctl(fd, syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

func openTerminal() (*terminal, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrNoTerminal
	}
	t := &terminal{f: f}
	if err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t.orig)); err != nil {
		f.Close()
		return nil, ErrNoTerminal
	}
	raw := t.orig
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(f.Fd(), syscall.TCSETS, unsafe.Pointer(&raw)); err != nil {
		f.Close()
		return nil, fmt.Errorf("cannot configure terminal: %w", err)
	}
	// Alternate screen, hidden cursor. On terminals that implement the kitty
	// keyboard protocol, also ask for unambiguous key reports so that
	// Ctrl+Enter can be told apart from Enter (others ignore the request).
	// The Linux console is skipped because it misreads that sequence.
	t.kitty = os.Getenv("TERM") != "linux"
	init := "\x1b[?1049h\x1b[?25l"
	if t.kitty {
		init += "\x1b[>1u"
	}
	t.f.WriteString(init)
	return t, nil
}

// close restores the terminal exactly as it was. It is safe to call more
// than once and from a signal path.
func (t *terminal) close() {
	t.once.Do(func() {
		reset := "\x1b[m\x1b[?25h"
		if t.kitty {
			reset += "\x1b[<u"
		}
		reset += "\x1b[?1049l"
		t.f.WriteString(reset)
		ioctl(t.f.Fd(), syscall.TCSETS, unsafe.Pointer(&t.orig))
		t.closed = true
	})
}

func (t *terminal) size() (w, h int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(t.f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

func (t *terminal) write(s string) { t.f.WriteString(s) }

// readLoop sends raw input chunks until the terminal goes away.
func (t *terminal) readLoop(out chan<- []byte) {
	buf := make([]byte, 256)
	for {
		n, err := t.f.Read(buf)
		if n > 0 {
			out <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			close(out)
			return
		}
	}
}
