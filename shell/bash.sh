# SPDX-License-Identifier: GPL-3.0-or-later
# cdir shell integration for bash.
#
# Add to ~/.bashrc:
#     source ~/.local/share/cdir/bash.sh
#
# Defines the function `c` and its completion. A program cannot change its
# parent shell's working directory, so `c` runs `cdir`, captures the
# directory it prints on stdout and runs `cd` itself, in this shell.

unalias c 2>/dev/null

c() {
    # Commands that only manage or list bookmarks print text for humans and
    # do not choose a directory: run them as-is so output reaches the
    # terminal (or a pipe) untouched.
    case "${1-}" in
        -i|--interactive|--) ;;
        -*) command cdir "$@"; return ;;
    esac

    local dir rc
    # Command substitution strips *all* trailing newlines, which would
    # corrupt a directory name that ends in one. Append a sentinel, then
    # remove it and exactly the one newline cdir prints.
    dir="$(command cdir "$@"; rc=$?; printf x; exit "$rc")"
    rc=$?
    [ "$rc" -eq 0 ] || return "$rc"
    dir="${dir%x}"
    dir="${dir%$'\n'}"
    [ -n "$dir" ] || return 1
    builtin cd -- "$dir"
}

_cdir_c_complete() {
    local cur=${COMP_WORDS[COMP_CWORD]} first=${COMP_WORDS[1]-}
    local IFS=$'\n'
    if [ "$COMP_CWORD" -eq 1 ]; then
        if [[ $cur == -* ]]; then
            COMPREPLY=($(compgen -W $'-a\n-d\n-l\n-i\n-h\n-v\n-f\n-m\n--add\n--delete\n--list\n--interactive\n--help\n--version\n--force\n--allow-missing' -- "$cur"))
        else
            COMPREPLY=($(compgen -W "$(command cdir --names 2>/dev/null)" -- "$cur"))
        fi
        return
    fi
    case $first in
        -d|--delete)
            COMPREPLY=($(compgen -W "$(command cdir --names 2>/dev/null)" -- "$cur")) ;;
        -i|--interactive)
            if [ "$COMP_CWORD" -eq 2 ]; then
                compopt -o filenames 2>/dev/null
                COMPREPLY=($(compgen -W "$(command cdir --names 2>/dev/null)" -- "$cur") $(compgen -d -- "$cur"))
            fi ;;
        -a|--add|-af|-fa|-f|--force)
            # c -a NAME PATH: complete the path (the last positional word).
            if [ "$COMP_CWORD" -ge 3 ]; then
                compopt -o filenames 2>/dev/null
                COMPREPLY=($(compgen -d -- "$cur"))
            fi ;;
    esac
}
complete -F _cdir_c_complete c
