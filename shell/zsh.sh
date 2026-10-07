# SPDX-License-Identifier: GPL-3.0-or-later
# cdir shell integration for zsh.
#
# Add to ~/.zshrc, after compinit (so completion can be registered):
#     source ~/.local/share/cdir/zsh.sh
#
# Defines the function `c` and its completion. A program cannot change its
# parent shell's working directory, so `c` runs `cdir`, captures the
# directory it prints on stdout and runs `cd` itself, in this shell.

unalias c 2>/dev/null

c() {
    emulate -L zsh
    # Commands that only manage or list bookmarks: run them as-is so their
    # output reaches the terminal (or a pipe) untouched.
    case "${1-}" in
        -i|--interactive|--) ;;
        -*) command cdir "$@"; return ;;
    esac

    local dir rc
    # Command substitution strips *all* trailing newlines; protect directory
    # names ending in one with a sentinel, then remove it and cdir's newline.
    dir="$(command cdir "$@"; rc=$?; printf x; exit $rc)"
    rc=$?
    (( rc == 0 )) || return $rc
    dir=${dir%x}
    dir=${dir%$'\n'}
    [[ -n $dir ]] || return 1
    builtin cd -- "$dir"
}

_cdir_c() {
    local -a names
    names=(${(f)"$(command cdir --names 2>/dev/null)"})
    if (( CURRENT == 2 )); then
        if [[ $PREFIX == -* ]]; then
            compadd -- -a -d -l -i -h -v -f -m --add --delete --list \
                --interactive --help --version --force --allow-missing
        else
            compadd -a names
        fi
        return
    fi
    case $words[2] in
        -d|--delete) compadd -a names ;;
        -i|--interactive) (( CURRENT == 3 )) && { compadd -a names; _directories } ;;
        -a|--add|-af|-fa|-f|--force) (( CURRENT >= 4 )) && _directories ;;
    esac
}
(( $+functions[compdef] )) && compdef _cdir_c c
