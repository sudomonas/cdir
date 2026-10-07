// SPDX-License-Identifier: GPL-3.0-or-later

// Package cli parses cdir's command line and dispatches to the bookmark
// store and the interactive UI.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"cdir/internal/bookmarks"
	"cdir/internal/config"
	"cdir/internal/paths"
	"cdir/internal/ui"
)

// Exit codes.
const (
	ExitOK        = 0 // directory printed / command succeeded
	ExitError     = 1 // general error
	ExitUsage     = 2 // invalid command or arguments
	ExitNotFound  = 3 // bookmark not found
	ExitCancelled = 4 // cancelled by the user
	ExitMissing   = 5 // target directory does not exist or cannot be entered
)

// Env is everything Run needs from the outside world.
type Env struct {
	Stdout, Stderr io.Writer
	StdoutTTY      bool
	Home, Cwd      string
	Version        string
	Store          bookmarks.Store
	ConfigPath     string
	// Interactive runs the TUI.
	Interactive func(ui.Options) (string, error)
	// Confirm asks a yes/no question on the terminal. It returns
	// ui.ErrNoTerminal when there is nobody to ask.
	Confirm func(question string) (bool, error)
}

// Main runs cdir with the real environment.
func Main(args []string, version string) int {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd() // honours $PWD, so symlinked paths are kept
	if err != nil {
		cwd = ""
	}
	env := &Env{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		StdoutTTY:   ui.IsTerminal(os.Stdout.Fd()),
		Home:        home,
		Cwd:         cwd,
		Version:     version,
		Store:       bookmarks.Store{Path: config.BookmarksPath(home)},
		ConfigPath:  config.Path(home),
		Interactive: ui.Run,
		Confirm:     askTTY,
	}
	return Run(args, env)
}

const usage = `Usage:
  c                      browse interactively from the current directory
  c NAME                 go to bookmark NAME (a unique prefix also works)
  c -i [NAME|PATH]       browse starting from a bookmark or path
  c -a NAME [PATH]       bookmark PATH (default: current directory) as NAME
  c -d NAME...           delete bookmarks
  c -l                   list bookmarks
  c -h | -v              help | version

Options for -a:
  -f, --force            replace an existing bookmark without asking
  -m, --allow-missing    accept a directory that does not exist (yet)

Long forms: --interactive --add --delete --list --help --version.
Browser keys: Enter open, Space select, Backspace up, / search,
a bookmark, b bookmarks, ? help, q quit.  See cdir(1).
`

type options struct {
	mode         string // "", add, delete, list, interactive, help, version, names
	force        bool
	allowMissing bool
	args         []string
}

type usageError string

func (e usageError) Error() string { return string(e) }

func parse(args []string) (options, error) {
	var o options
	setMode := func(m, flag string) error {
		if o.mode != "" && o.mode != m {
			return usageError(fmt.Sprintf("%s cannot be combined with --%s", flag, o.mode))
		}
		o.mode = m
		return nil
	}
	var flags []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.args = append(o.args, args[i+1:]...)
			i = len(args)
			continue
		case strings.HasPrefix(a, "--"):
			flags = []string{a}
		case len(a) > 1 && a[0] == '-':
			flags = flags[:0]
			for _, c := range a[1:] {
				flags = append(flags, "-"+string(c))
			}
		default:
			o.args = append(o.args, a)
			continue
		}
		for _, f := range flags {
			var err error
			switch f {
			case "-a", "--add":
				err = setMode("add", f)
			case "-d", "--delete":
				err = setMode("delete", f)
			case "-l", "--list":
				err = setMode("list", f)
			case "-i", "--interactive":
				err = setMode("interactive", f)
			case "-h", "--help":
				err = setMode("help", f)
			case "-v", "--version":
				err = setMode("version", f)
			case "--names": // used by shell completion
				err = setMode("names", f)
			case "-f", "--force":
				o.force = true
			case "-m", "--allow-missing":
				o.allowMissing = true
			default:
				err = usageError("unknown option " + f)
			}
			if err != nil {
				return o, err
			}
		}
	}
	if (o.force || o.allowMissing) && o.mode != "add" {
		return o, usageError("--force and --allow-missing only apply to --add")
	}
	n := len(o.args)
	bad := false
	switch o.mode {
	case "":
		bad = n > 1
	case "add":
		bad = n < 1 || n > 2
	case "delete":
		bad = n < 1
	case "interactive":
		bad = n > 1
	default:
		bad = n > 0
	}
	if bad {
		return o, usageError("wrong number of arguments")
	}
	return o, nil
}

// Run executes one cdir command and returns the exit code.
func Run(args []string, env *Env) int {
	o, err := parse(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "cdir: %v\nTry 'c -h' for help.\n", err)
		return ExitUsage
	}
	switch o.mode {
	case "help":
		fmt.Fprint(env.Stdout, usage)
		return ExitOK
	case "version":
		fmt.Fprintf(env.Stdout, "cdir %s\n", env.Version)
		return ExitOK
	case "names":
		return names(env)
	case "list":
		return list(env)
	case "add":
		p := "."
		if len(o.args) == 2 {
			p = o.args[1]
		}
		return add(env, o, o.args[0], p)
	case "delete":
		return del(env, o.args)
	case "interactive":
		start := env.Cwd
		if len(o.args) == 1 {
			var code int
			if start, code = resolveStart(env, o.args[0]); code != ExitOK {
				return code
			}
		}
		return browse(env, ui.Options{Start: start})
	}
	if len(o.args) == 1 {
		return jump(env, o.args[0])
	}
	return browse(env, ui.Options{Start: env.Cwd})
}

func errorf(env *Env, code int, format string, a ...any) int {
	fmt.Fprintf(env.Stderr, "cdir: "+format+"\n", a...)
	return code
}

func load(env *Env) (*bookmarks.File, []bookmarks.Warning, bool) {
	f, warns, err := env.Store.Load()
	if err != nil {
		errorf(env, ExitError, "cannot read bookmarks: %v", err)
		return nil, nil, false
	}
	return f, warns, true
}

// jump is the fast path: read the file, look up the name, print the path.
func jump(env *Env, name string) int {
	if !bookmarks.ValidName(name) {
		return errorf(env, ExitUsage, "%q is not a bookmark name (to browse a path use: c -i PATH)", name)
	}
	f, _, ok := load(env)
	if !ok {
		return ExitError
	}
	if p, ok := f.Lookup(name); ok {
		return emit(env, name, p)
	}

	// No exact match: a unique prefix is accepted; otherwise show the
	// candidates rather than guessing.
	var prefix []bookmarks.Bookmark
	for _, b := range f.Bookmarks() {
		if strings.HasPrefix(strings.ToLower(b.Name), strings.ToLower(name)) {
			prefix = append(prefix, b)
		}
	}
	if len(prefix) == 1 {
		return emit(env, prefix[0].Name, prefix[0].Path)
	}
	var candidates []string
	if len(prefix) > 1 {
		for _, b := range prefix {
			candidates = append(candidates, b.Name)
		}
	} else {
		for _, b := range f.Bookmarks() {
			if subsequence(strings.ToLower(name), strings.ToLower(b.Name)) {
				candidates = append(candidates, b.Name)
			}
		}
	}
	if len(candidates) == 0 {
		return errorf(env, ExitNotFound, "no bookmark named '%s' (see c -l)", name)
	}
	dir, err := runUI(env, ui.Options{Start: env.Cwd, Bookmarks: true, Query: name})
	if errors.Is(err, ui.ErrNoTerminal) { // nobody to ask: list the choices
		fmt.Fprintf(env.Stderr, "cdir: '%s' is ambiguous; candidates:\n", name)
		for _, c := range candidates {
			fmt.Fprintf(env.Stderr, "  %s\n", c)
		}
		return ExitNotFound
	}
	return finish(env, dir, err)
}

func subsequence(p, s string) bool {
	for _, r := range s {
		if p == "" {
			break
		}
		if strings.HasPrefix(p, string(r)) {
			p = p[len(string(r)):]
		}
	}
	return p == ""
}

func emit(env *Env, name, p string) int {
	if err := paths.CheckDir(p); err != nil {
		return errorf(env, ExitMissing, "bookmark '%s': %s: %v", name, p, err)
	}
	fmt.Fprintln(env.Stdout, p)
	return ExitOK
}

func resolveStart(env *Env, arg string) (string, int) {
	if bookmarks.ValidName(arg) && !strings.HasPrefix(arg, ".") {
		f, _, ok := load(env)
		if !ok {
			return "", ExitError
		}
		if p, ok := f.Lookup(arg); ok {
			if err := paths.CheckDir(p); err != nil {
				return "", errorf(env, ExitMissing, "bookmark '%s': %s: %v", arg, p, err)
			}
			return p, ExitOK
		}
	}
	p := paths.Resolve(arg, env.Cwd, env.Home)
	if err := paths.CheckDir(p); err != nil {
		if bookmarks.ValidName(arg) && paths.IsMissing(err) {
			return "", errorf(env, ExitNotFound, "no bookmark or directory named '%s'", arg)
		}
		return "", errorf(env, ExitMissing, "%s: %v", p, err)
	}
	return p, ExitOK
}

func runUI(env *Env, opt ui.Options) (string, error) {
	if opt.Start == "" {
		opt.Start = env.Home
	}
	opt.Home = env.Home
	opt.Store = env.Store
	opt.Config, opt.Warnings = config.Load(env.ConfigPath)
	return env.Interactive(opt)
}

func browse(env *Env, opt ui.Options) int {
	dir, err := runUI(env, opt)
	return finish(env, dir, err)
}

// finish prints the directory chosen in the UI or reports why there is none.
func finish(env *Env, dir string, err error) int {
	switch {
	case errors.Is(err, ui.ErrCancelled):
		return ExitCancelled
	case errors.Is(err, ui.ErrNoTerminal):
		return errorf(env, ExitError, "interactive mode needs a terminal")
	case err != nil:
		return errorf(env, ExitError, "%v", err)
	}
	fmt.Fprintln(env.Stdout, dir)
	return ExitOK
}

func names(env *Env) int {
	f, _, err := env.Store.Load()
	if err != nil {
		return ExitError
	}
	for _, b := range f.Bookmarks() {
		fmt.Fprintln(env.Stdout, b.Name)
	}
	return ExitOK
}

func warn(env *Env, warns []bookmarks.Warning) {
	for _, w := range warns {
		fmt.Fprintf(env.Stderr, "cdir: %s: %s\n", env.Store.Path, w)
	}
}

func list(env *Env) int {
	f, warns, ok := load(env)
	if !ok {
		return ExitError
	}
	warn(env, warns)
	bms := f.Bookmarks()
	if !env.StdoutTTY {
		// Machine-readable: NAME<TAB>PATH, path escaped as in the file.
		for _, b := range bms {
			fmt.Fprintf(env.Stdout, "%s\t%s\n", b.Name, bookmarks.EscapePath(b.Path))
		}
		return ExitOK
	}
	if len(bms) == 0 {
		fmt.Fprintln(env.Stdout, "No bookmarks yet. Add one with: c -a NAME [PATH]")
		return ExitOK
	}
	nameW, pathW := 0, 0
	shown := make([]string, len(bms))
	for i, b := range bms {
		nameW = max(nameW, len(b.Name))
		shown[i] = paths.Printable(paths.Abbrev(b.Path, env.Home))
		pathW = max(pathW, len([]rune(shown[i])))
	}
	fmt.Fprint(env.Stdout, "Bookmarks\n\n")
	for i, b := range bms {
		note := ""
		if err := paths.CheckDir(b.Path); err != nil {
			note = "[missing]"
			if !paths.IsMissing(err) {
				note = "[" + err.Error() + "]"
			}
		}
		if note == "" {
			fmt.Fprintf(env.Stdout, "%-*s  %s\n", nameW, b.Name, shown[i])
		} else {
			fmt.Fprintf(env.Stdout, "%-*s  %-*s  %s\n", nameW, b.Name, pathW, shown[i], note)
		}
	}
	return ExitOK
}

type existsError struct{ name, path string }

func (e existsError) Error() string {
	return fmt.Sprintf("bookmark '%s' already exists (%s); use -f to replace it", e.name, e.path)
}

var errDeclined = errors.New("declined")

func add(env *Env, o options, name, arg string) int {
	if !bookmarks.ValidName(name) {
		return errorf(env, ExitUsage, "invalid bookmark name %q: use letters, digits, '_' and '-' (not as first character)", name)
	}
	if env.Cwd == "" && !strings.HasPrefix(paths.Expand(arg, env.Home), "/") {
		return errorf(env, ExitError, "cannot determine the current directory")
	}
	p := paths.Resolve(arg, env.Cwd, env.Home)
	if err := paths.CheckDir(p); err != nil {
		if !(o.allowMissing && paths.IsMissing(err)) {
			hint := ""
			if paths.IsMissing(err) {
				hint = " (use --allow-missing to add it anyway)"
			}
			return errorf(env, ExitMissing, "%s: %v%s", p, err, hint)
		}
		fmt.Fprintf(env.Stderr, "cdir: warning: %s does not exist\n", p)
	}

	var old string
	var unchanged bool
	err := env.Store.Update(func(f *bookmarks.File) error {
		cur, exists := f.Lookup(name)
		if exists {
			if cur == p {
				unchanged = true
				return bookmarks.ErrNoChange
			}
			if !o.force {
				ok, err := env.Confirm(fmt.Sprintf(
					"Bookmark '%s' already exists.\n\nCurrent:\n  %s\n\nReplace it? [y/N] ", name, cur))
				if errors.Is(err, ui.ErrNoTerminal) {
					return existsError{name, cur}
				}
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			old = cur
		}
		f.Set(name, p)
		return nil
	})
	var ee existsError
	switch {
	case errors.As(err, &ee):
		return errorf(env, ExitError, "%v", ee)
	case errors.Is(err, errDeclined):
		fmt.Fprintln(env.Stderr, "Not replaced.")
		return ExitCancelled
	case err != nil:
		return errorf(env, ExitError, "cannot save bookmark: %v", err)
	case unchanged:
		fmt.Fprintf(env.Stdout, "Bookmark already exists:\n%s → %s\n", name, paths.Printable(p))
	case old != "":
		fmt.Fprintf(env.Stdout, "Replaced bookmark:\n%s → %s\n(was %s)\n", name, paths.Printable(p), paths.Printable(old))
	default:
		fmt.Fprintf(env.Stdout, "Added bookmark:\n%s → %s\n", name, paths.Printable(p))
	}
	return ExitOK
}

func del(env *Env, names []string) int {
	var removed []bookmarks.Bookmark
	var missing []string
	err := env.Store.Update(func(f *bookmarks.File) error {
		for _, n := range names {
			if p, ok := f.Lookup(n); ok {
				f.Delete(n)
				removed = append(removed, bookmarks.Bookmark{Name: n, Path: p})
			} else {
				missing = append(missing, n)
			}
		}
		if len(removed) == 0 {
			return bookmarks.ErrNoChange
		}
		return nil
	})
	if err != nil {
		return errorf(env, ExitError, "cannot save bookmarks: %v", err)
	}
	for _, b := range removed {
		fmt.Fprintf(env.Stdout, "Deleted bookmark: %s → %s\n", b.Name, paths.Printable(b.Path))
	}
	for _, n := range missing {
		fmt.Fprintf(env.Stderr, "cdir: no bookmark named '%s'\n", n)
	}
	if len(missing) > 0 {
		return ExitNotFound
	}
	return ExitOK
}

// askTTY asks a yes/no question on the controlling terminal.
func askTTY(q string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, ui.ErrNoTerminal
	}
	defer tty.Close()
	fmt.Fprint(tty, q)
	line, _ := bufio.NewReader(tty).ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}
