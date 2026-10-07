# cdir

`cd` with a memory and a visual directory picker.

```console
$ c napkin          # jump to a saved directory
$ c                 # browse interactively from here, pick a destination
```

A single small static binary, no dependencies beyond the Go standard
library, no daemon, no network, no database. It is a directory launcher,
not a file manager.

## Features

- **Named bookmarks**: `c napkin` → `~/Projects/Napkin`. Unique prefixes
  work (`c nap`); ambiguous ones show a picker instead of guessing.
- **Dired-inspired browser**: keyboard-driven (arrows or `hjkl`), starts in
  the current directory, directories only by default.
- **Bookmark from the browser** (`a`) and a **bookmark list** (`b`).
- **Fuzzy filter** of the current directory (`/`).
- **Really changes your shell's directory**, via a tiny bash/zsh function.
- **Plain-text bookmark file**, written atomically and with locking.
- **Tab completion** of bookmark names.
- Handles spaces, Unicode, leading dashes, newlines, symlinks,
  unreadable and deleted directories.

## Installation

Requires Go ≥ 1.22 to build. Nothing is needed at runtime.

```sh
git clone https://github.com/sudomonas/cdir.git
cd cdir
make            # builds ./cdir
make install    # installs into ~/.local (no root needed)
```

`make install` puts:

| File | Location |
|---|---|
| `cdir` | `~/.local/bin/cdir` |
| man page | `~/.local/share/man/man1/cdir.1` |
| shell integration | `~/.local/share/cdir/{bash,zsh}.sh` |
| completions for `cdir` | `~/.local/share/{bash-completion/completions,zsh/site-functions}` |

Make sure `~/.local/bin` is on your `PATH` (add this to `~/.bashrc` or
`~/.zshrc` if it is not):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Manual alternative: `go build -o cdir ./cmd/cdir && cp cdir ~/.local/bin/`.
System-wide: `sudo make install PREFIX=/usr/local`.

## Shell integration

This is required for `c` to change your directory. Add **one line** at the
end of your shell startup file:

```sh
# ~/.bashrc
source ~/.local/share/cdir/bash.sh
```

```sh
# ~/.zshrc  — after `compinit`, so completion can register
source ~/.local/share/cdir/zsh.sh
```

Open a new terminal (or `source` the file) and you have the `c` command.

**Why a shell function?** A program cannot change the working directory of
the shell that started it — it only changes its own. So `cdir` prints the
chosen directory on stdout, and `c` does this in your shell:

```sh
dir="$(command cdir "$@")" || return   # simplified
cd -- "$dir"
```

`c` changes directory only if `cdir` exits with status 0, so cancelling or
mistyping a name leaves you where you were. Commands that just print
information (`-l`, `-a`, `-d`, `-h`, `-v`) are run directly, so their
output is shown normally and can be piped.

## Quick start

```console
$ c -a napkin ~/Projects/Napkin
Added bookmark:
napkin → /home/aragorn/Projects/Napkin

$ c napkin

$ pwd
/home/aragorn/Projects/Napkin
```

Or interactively:

```text
$ cd ~/Projects
$ c                      browser opens in ~/Projects
  j                      highlight Napkin/
  a  ⏎                   bookmark it as "napkin" (name pre-filled)
  Space                  choose Napkin/ — you are now in ~/Projects/Napkin
```

## Bookmark management

```text
c NAME                 go to bookmark (exact match, else unique prefix)
c -a NAME [PATH]       add; PATH defaults to the current directory
c -a -f NAME PATH      replace an existing bookmark without asking
c -a -m NAME PATH      allow a directory that does not exist (yet)
c -d NAME...           delete
c -l                   list
c -i [NAME|PATH]       open the browser at a bookmark or path
```

Adding a name that already exists asks before replacing it (or fails, with
a hint to use `-f`, when there is no terminal). `c -l` marks bookmarks whose
directory is gone as `[missing]`; they are never deleted automatically.

Names may contain letters, digits, `_` and `-`, and may not start with `-`
(so no name can be confused with an option).

Bookmarks are stored in `~/.config/cdir/bookmarks` (`$XDG_CONFIG_HOME` is
respected), one `name=/absolute/path` per line. Everything after the first
`=` is the path, spaces included; in paths `\\`, `\n` and `\r` encode a
backslash, newline and carriage return. Edit it by hand if you like — see
[`examples/bookmarks`](examples/bookmarks). Writes go to a temporary file
that is synced and renamed into place, under a lock, so the file cannot be
corrupted by an interrupted or concurrent write. Lines cdir cannot parse are
reported and preserved, never dropped.

## Interactive navigation

The list shows `./` (where you are), `../`, then subdirectories in natural
order (`disc2` before `disc10`).

**Enter moves you around; Space picks the destination.**

| Key | Action |
|---|---|
| `j` `k` `↓` `↑` | move (also `g`/`G`, `PgUp`/`PgDn`, `Home`/`End`) |
| `Enter` `l` `→` | open highlighted directory (on `./`: choose it) |
| `Backspace` `h` `←` | parent directory |
| `Space` / `Ctrl+Enter` | **choose highlighted directory** and exit |
| `/` | fuzzy-filter this directory (`Enter` open, `Ctrl+J` choose, `Tab` keep, `Esc` clear) |
| `a` / `A` | bookmark highlighted / current directory |
| `b` | bookmark list (`Enter` go, `l` browse from it, `d` delete, `/` filter) |
| `~` | home directory |
| `.` / `f` | toggle hidden directories / files |
| `?` | help |
| `q` `Esc` `Ctrl+C` | quit, directory unchanged |

`Ctrl+Enter` is only distinguishable from `Enter` on terminals that support
the kitty keyboard protocol (Ghostty, kitty, Alacritty ≥ 0.13, foot,
WezTerm). `Space` and `Ctrl+J` work everywhere, including the Linux console
and Konsole.

## Configuration

Optional. `~/.config/cdir/config`:

```ini
show_hidden=false      # show dot-directories at startup
show_files=false       # list files (dimmed) below directories
follow_symlinks=false  # browser returns physical paths instead of logical ones
sort=natural           # or alpha
```

Colours come from your terminal's palette (bold, blue, dim, reverse video
only), so light and dark themes both work. Set `NO_COLOR=1` to disable
colour.

**Symlinks** behave like `cd`: paths are kept as you wrote/navigated them
and `..` is resolved lexically. Bookmarking `~/link` stores `~/link`, not
its target.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success (a directory was printed) |
| 1 | general error |
| 2 | invalid arguments or bookmark name |
| 3 | bookmark not found |
| 4 | cancelled |
| 5 | target directory missing or not enterable |

## Troubleshooting

- **`c` prints a path but doesn't change directory** — you are running the
  binary, not the function. Check `type c`; it should say
  `c is a function`. Source the shell integration file in your rc file.
- **`c: command not found`** — the integration isn't sourced; see above.
- **`cdir: command not found`** — `~/.local/bin` is not on `PATH`.
- **`c` does something else** — something else is named `c`. The
  integration removes an alias called `c`, but a function defined later in
  your rc file will override it; source cdir last.
- **zsh: no completion** — source `zsh.sh` after `compinit`.
- **Ctrl+Enter acts like Enter** — your terminal doesn't report it
  distinctly; use `Space` or `Ctrl+J`.
- **`man cdir` not found** — with `man-db`, `~/.local/share/man` is found
  automatically when `~/.local/bin` is on `PATH`; otherwise add it to
  `MANPATH`.
- **Terminal left in a weird state** — shouldn't happen (the terminal is
  restored on exit, SIGTERM, SIGHUP and panics); `reset` fixes it.

## Development

```sh
make test        # go vet + unit tests + end-to-end tests
```

The end-to-end tests in `tests/` build the binary and drive it through a
pseudo-terminal, including an interactive `bash` with the integration
sourced, checking that the shell's `$PWD` changes after selection and does
not change after cancel or errors (zsh is tested too if installed).

```text
cmd/cdir/          main
internal/cli/      argument parsing, commands, exit codes
internal/bookmarks file format, atomic writes, locking
internal/browser/  directory listing, natural sort
internal/ui/       raw terminal, keys, rendering, browser/bookmark views
internal/paths/    expansion, normalisation, directory checks
internal/fuzzy/    subsequence matcher
internal/config/   config file
shell/             bash.sh, zsh.sh — the `c` function + completion
completions/       completion for the `cdir` binary itself
man/cdir.1
```

## Design philosophy

- **Two workflows**: `c name` when you know where you're going, `c` when
  you don't. Everything else serves those.
- **`c name` is a file read and a `stat`** — no UI, no scanning, about a
  millisecond.
- **stdout is sacred**: it carries only the chosen path. The UI draws on
  `/dev/tty`.
- **Zero dependencies**: raw mode, window size and locking use Linux
  syscalls from the standard library, so there is no TUI framework.
- **Never lose user data**: atomic writes, locking, unparseable lines kept.
- **Not a file manager**: no copying, deleting, previews or plugins.

## License

Copyright (C) 2026 sudomonas

This program is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option)
any later version. It is distributed WITHOUT ANY WARRANTY; see
[`LICENSE`](LICENSE) for details.
