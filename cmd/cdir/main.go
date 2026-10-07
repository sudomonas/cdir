// SPDX-License-Identifier: GPL-3.0-or-later

// Command cdir is a directory launcher and bookmark manager.
//
// It never changes directory itself: it prints the chosen directory on
// stdout and the shell function `c` (see shell/) performs the cd.
package main

import (
	"os"

	"cdir/internal/cli"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "1.0.0"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
