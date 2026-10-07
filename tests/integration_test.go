// SPDX-License-Identifier: GPL-3.0-or-later

// End-to-end tests: build the real binary and drive it, and a real shell,
// through a pseudo-terminal.
package tests

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var binDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cdir-bin-")
	if err != nil {
		panic(err)
	}
	binDir = dir
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "cdir"), "../cmd/cdir")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func openPTY(t *testing.T) (master, slave *os.File) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pseudo-terminal support:", err)
	}
	var unlock int32
	var n uint32
	if err := ioctl(m.Fd(), syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatal(err)
	}
	if err := ioctl(m.Fd(), syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		t.Fatal(err)
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	ws := struct{ Row, Col, X, Y uint16 }{24, 100, 0, 0}
	ioctl(s.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
	return m, s
}

// session is a process whose controlling terminal is a pty.
type session struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
	stdout bytes.Buffer
	mu     sync.Mutex
	screen bytes.Buffer
	done   chan struct{}
}

// fixture creates a directory tree and an isolated config directory.
type fixture struct {
	root, home, cfg string
}

func newFixture(t *testing.T) fixture {
	root := t.TempDir()
	f := fixture{root: root, home: filepath.Join(root, "home"), cfg: filepath.Join(root, "cfg")}
	for _, d := range []string{"home/Projects/Napkin/src", "home/Projects/Segue", "home/Projects/ClinLang",
		"home/Documents/Notes", "home/With Space", "home/-dash", "home/Ünïcode 日本", "home/.secret"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(f.home, "Projects", "README"), nil, 0o644)
	os.Symlink(filepath.Join(f.home, "Projects", "Napkin"), filepath.Join(f.home, "napkin-link"))
	return f
}

func (f fixture) env() []string {
	return []string{
		"HOME=" + f.home,
		"XDG_CONFIG_HOME=" + f.cfg,
		"PATH=" + binDir + ":/usr/bin:/bin",
		"TERM=xterm-256color",
		"NO_COLOR=1",
		"LANG=C.UTF-8",
	}
}

func (f fixture) bookmarks(t *testing.T, content string) {
	os.MkdirAll(filepath.Join(f.cfg, "cdir"), 0o755)
	if err := os.WriteFile(filepath.Join(f.cfg, "cdir", "bookmarks"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// start runs name with args; the pty is stdin/stderr and controlling
// terminal, stdout is captured separately (like $(...) in the shell).
func start(t *testing.T, f fixture, dir string, captureStdout bool, name string, args ...string) *session {
	m, s := openPTY(t)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(f.env(), "PWD="+dir)
	cmd.Stdin, cmd.Stderr = s, s
	ss := &session{t: t, master: m, slave: s, cmd: cmd, done: make(chan struct{})}
	if captureStdout {
		cmd.Stdout = &ss.stdout
	} else {
		cmd.Stdout = s
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := m.Read(buf)
			ss.mu.Lock()
			ss.screen.Write(buf[:n])
			ss.mu.Unlock()
			if err != nil {
				close(ss.done)
				return
			}
		}
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		m.Close()
		s.Close()
	})
	return ss
}

// waitFor waits until the terminal output contains text.
func (s *session) waitFor(text string) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := bytes.Contains(s.screen.Bytes(), []byte(text))
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t.Fatalf("timed out waiting for %q; screen:\n%q", text, s.screen.String())
}

// send writes keys, each as a separate chunk like a person typing.
func (s *session) send(keys ...string) {
	for _, k := range keys {
		s.master.Write([]byte(k))
		time.Sleep(40 * time.Millisecond)
	}
}

func (s *session) wait() int {
	s.t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- s.cmd.Wait() }()
	select {
	case err := <-ch:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			s.t.Fatal(err)
		}
		return 0
	case <-time.After(5 * time.Second):
		s.t.Fatal("process did not exit")
	}
	return -1
}

// assertRestored checks the terminal was left in canonical mode with
// echo, on the main screen.
func (s *session) assertRestored() {
	s.t.Helper()
	var tio syscall.Termios
	if err := ioctl(s.slave.Fd(), syscall.TCGETS, unsafe.Pointer(&tio)); err != nil {
		s.t.Fatal(err)
	}
	if tio.Lflag&syscall.ICANON == 0 || tio.Lflag&syscall.ECHO == 0 {
		s.t.Error("terminal left in raw mode")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scr := s.screen.String()
	if strings.LastIndex(scr, "\x1b[?1049l") < strings.LastIndex(scr, "\x1b[?1049h") {
		s.t.Error("alternate screen not left")
	}
}

func cdir(t *testing.T, f fixture, dir string, args ...string) *session {
	return start(t, f, dir, true, filepath.Join(binDir, "cdir"), args...)
}

func TestBrowseEnterAndSelect(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, filepath.Join(f.home, "Projects"))
	s.waitFor("ClinLang/")
	if strings.Contains(s.screen.String(), "README") {
		t.Error("files should be hidden by default")
	}
	// Cursor starts on ClinLang; j → Napkin; Enter opens it; src/ shown;
	// Space selects the highlighted src.
	s.send("j", "\r")
	s.waitFor("src/")
	s.send(" ")
	if code := s.wait(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got, want := s.stdout.String(), filepath.Join(f.home, "Projects", "Napkin", "src")+"\n"; got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
	s.assertRestored()
}

func TestSelectCurrentDirectory(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, filepath.Join(f.home, "Projects"))
	s.waitFor("Segue/")
	// Down twice → Segue, Enter, then Enter on "./" (top) chooses Segue itself.
	s.send("j", "j", "\r")
	s.waitFor("this directory")
	s.send("g", "\r")
	if code := s.wait(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := s.stdout.String(); got != filepath.Join(f.home, "Projects", "Segue")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestCtrlEnterAndParent(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, filepath.Join(f.home, "Projects", "Napkin"))
	s.waitFor("src/")
	// Backspace to parent: cursor lands on Napkin; Ctrl+Enter (kitty
	// encoding) selects it.
	s.send("\x7f")
	s.waitFor("Segue/")
	s.send("\x1b[13;5u")
	s.wait()
	if got := s.stdout.String(); got != filepath.Join(f.home, "Projects", "Napkin")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestCancel(t *testing.T) {
	for name, k := range map[string]string{"q": "q", "esc": "\x1b", "ctrl-c": "\x03"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			s := cdir(t, f, f.home)
			s.waitFor("Projects/")
			s.send(k)
			if code := s.wait(); code != 4 {
				t.Errorf("exit %d, want 4", code)
			}
			if s.stdout.Len() != 0 {
				t.Errorf("stdout %q, want nothing", s.stdout.String())
			}
			s.assertRestored()
		})
	}
}

func TestSigtermRestoresTerminal(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, f.home)
	s.waitFor("Projects/")
	s.cmd.Process.Signal(syscall.SIGTERM)
	if code := s.wait(); code != 4 {
		t.Errorf("exit %d, want 4", code)
	}
	s.assertRestored()
}

func TestSearch(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, filepath.Join(f.home, "Projects"))
	s.waitFor("Segue/")
	s.send("/", "s", "g", "\r") // fuzzy "sg" → Segue, Enter opens it
	s.waitFor("this directory")
	s.send("g", " ")
	s.wait()
	if got := s.stdout.String(); got != filepath.Join(f.home, "Projects", "Segue")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestHiddenToggleAndUnicode(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, f.home)
	s.waitFor("Ünïcode 日本/")
	if strings.Contains(s.screen.String(), ".secret") {
		t.Error("hidden directory shown by default")
	}
	s.send(".")
	s.waitFor(".secret/")
	s.send("/", "日", "\n") // filter, Ctrl+J selects the match directly
	s.wait()
	if got := s.stdout.String(); got != filepath.Join(f.home, "Ünïcode 日本")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestBookmarkFromBrowser(t *testing.T) {
	f := newFixture(t)
	s := cdir(t, f, filepath.Join(f.home, "Projects"))
	s.waitFor("Napkin/")
	s.send("j", "a")
	s.waitFor("as: \x1b[mnapkin")
	s.send("\r")
	s.waitFor("Saved: napkin")
	s.send("q")
	s.wait()
	data, _ := os.ReadFile(filepath.Join(f.cfg, "cdir", "bookmarks"))
	if want := "napkin=" + filepath.Join(f.home, "Projects", "Napkin") + "\n"; string(data) != want {
		t.Errorf("bookmarks file %q, want %q", data, want)
	}
}

func TestBookmarkList(t *testing.T) {
	f := newFixture(t)
	f.bookmarks(t, "napkin="+filepath.Join(f.home, "Projects/Napkin")+"\nnotes="+filepath.Join(f.home, "Documents/Notes")+"\n")
	s := cdir(t, f, f.home)
	s.waitFor("Projects/")
	s.send("b")
	s.waitFor("notes")
	s.send("j", "\r")
	if code := s.wait(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := s.stdout.String(); got != filepath.Join(f.home, "Documents/Notes")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestBookmarkDeleteInList(t *testing.T) {
	f := newFixture(t)
	f.bookmarks(t, "a="+f.home+"\nb="+f.home+"\n")
	s := cdir(t, f, f.home)
	s.waitFor("Projects/")
	s.send("b")
	s.waitFor("Bookmarks")
	s.send("d")
	s.waitFor("Delete bookmark 'a'")
	s.send("y")
	s.waitFor("Deleted a")
	s.send("q")
	s.wait()
	data, _ := os.ReadFile(filepath.Join(f.cfg, "cdir", "bookmarks"))
	if string(data) != "b="+f.home+"\n" {
		t.Errorf("bookmarks file %q", data)
	}
}

func TestAmbiguousPicker(t *testing.T) {
	f := newFixture(t)
	f.bookmarks(t, "nap1="+filepath.Join(f.home, "Projects")+"\nnap2="+filepath.Join(f.home, "Documents")+"\n")
	s := cdir(t, f, f.home, "nap")
	s.waitFor("nap2")
	s.send("j", "\r")
	s.wait()
	if got := s.stdout.String(); got != filepath.Join(f.home, "Documents")+"\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestPermissionDeniedKeepsRunning(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	f := newFixture(t)
	locked := filepath.Join(f.home, "Locked")
	os.Mkdir(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	s := cdir(t, f, f.home)
	s.waitFor("Locked/")
	s.send("/", "Locked", "\r")
	s.waitFor("Cannot enter ~/Locked: permission denied")
	s.send("\x1b", "q") // leave the filter, then quit
	if code := s.wait(); code != 4 {
		t.Errorf("exit %d", code)
	}
}

// TestShellIntegration runs an interactive bash with the wrapper sourced,
// and checks that the *shell's* working directory changes — or does not.
func TestShellIntegration(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			sh, err := exec.LookPath(shell)
			if err != nil {
				t.Skip(shell + " not installed")
			}
			f := newFixture(t)
			out := filepath.Join(f.root, "pwd.log")
			src, _ := filepath.Abs(filepath.Join("..", "shell", shell+".sh"))
			args := []string{"--norc", "--noprofile", "-i"}
			if shell == "zsh" {
				args = []string{"-f", "-i"}
			}
			s := start(t, f, f.home, false, sh, args...)
			// Each call appends the shell's PWD to the log and prints a unique
			// marker (computed by the shell, so the echoed input never matches).
			n := 0
			log := func() string {
				n++
				return fmt.Sprintf("builtin pwd >> %q; echo LOGGED-$((%d+0))\r", out, n)
			}
			mark := func() string { return fmt.Sprintf("LOGGED-%d\r", n) }

			s.send(fmt.Sprintf("source %q && echo READY\r", src))
			s.waitFor("READY")

			// 1. Interactive: cancel leaves PWD alone.
			s.send("c\r")
			s.waitFor("Projects/")
			s.send("q")
			s.waitFor("\x1b[?1049l")
			s.send(log())
			s.waitFor(mark())

			// 2. Interactive: navigate and select changes the shell's PWD.
			s.send("c\r")
			s.waitFor("Projects/")
			s.send("/", "Proj", "\r")
			s.waitFor("ClinLang/")
			s.send("j", " ") // Napkin
			s.send(log())
			s.waitFor(mark())

			// 3. Bookmarks: add, list output is visible, jump.
			s.send(fmt.Sprintf("c -a dash %q && c -a uni %q && c -l\r",
				filepath.Join(f.home, "-dash"), filepath.Join(f.home, "Ünïcode 日本")))
			s.waitFor("Bookmarks")
			s.send("c dash\r")
			s.send(log())
			s.send("c uni\r")
			s.send(log())
			// 4. Unknown bookmark: error, PWD unchanged.
			s.send("c nonexistent; echo RC-$?\r")
			s.waitFor("RC-3")
			s.send(log())
			// 5. Path ending in a newline survives command substitution.
			nl := filepath.Join(f.home, "trail\n")
			os.Mkdir(nl, 0o755)
			s.send(fmt.Sprintf("c -a nl \"%s/trail\"$'\\n' >/dev/null && c nl\r", f.home))
			s.send(log())
			// 6. Symlinked bookmark keeps the logical path.
			s.send(fmt.Sprintf("c -a link %q >/dev/null && c link\r", filepath.Join(f.home, "napkin-link")))
			s.send(log())
			s.waitFor(mark())
			s.send("exit\r")
			s.wait()

			data, _ := os.ReadFile(out)
			got := string(data)
			want := strings.Join([]string{
				f.home,
				filepath.Join(f.home, "Projects", "Napkin"),
				filepath.Join(f.home, "-dash"),
				filepath.Join(f.home, "Ünïcode 日本"),
				filepath.Join(f.home, "Ünïcode 日本"),
				nl,
				filepath.Join(f.home, "napkin-link"),
			}, "\n") + "\n"
			if got != want {
				t.Errorf("shell PWD log:\n%q\nwant:\n%q", got, want)
			}
		})
	}
}
